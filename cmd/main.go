package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/ai"
	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/chat"
	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/clock"
	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/crypto"
	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/p2p"
	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/types"
	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/web"
)

// ── Types ──

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

type safeConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (sc *safeConn) write(data []byte) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	_ = sc.conn.WriteMessage(websocket.TextMessage, data)
}

type hub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]*safeConn
}

func (h *hub) add(c *websocket.Conn) *safeConn {
	sc := &safeConn{conn: c}
	h.mu.Lock()
	h.clients[c] = sc
	h.mu.Unlock()
	return sc
}
func (h *hub) remove(c *websocket.Conn) { h.mu.Lock(); delete(h.clients, c); h.mu.Unlock() }
func (h *hub) broadcast(data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, sc := range h.clients {
		sc.write(data)
	}
}

type wsEnvelope struct {
	Type        string            `json:"type"`
	ID          string            `json:"id,omitempty"`
	SenderID    string            `json:"sender_id,omitempty"`
	SenderName  string            `json:"sender_name,omitempty"`
	Content     string            `json:"content,omitempty"`
	Mentions    []string          `json:"mentions,omitempty"`
	MentionAll  bool              `json:"mention_all,omitempty"`
	IsAI        bool              `json:"is_ai,omitempty"`
	VectorClock map[string]uint64 `json:"vector_clock,omitempty"`
	Self        bool              `json:"self,omitempty"`
	Count       int               `json:"count,omitempty"`
	PeerID      string            `json:"peer_id,omitempty"`
	// Setup fields
	Room        string `json:"room,omitempty"`
	AIName      string `json:"ai_name,omitempty"`
	AIStyle     string `json:"ai_style,omitempty"`
	AIModel     string `json:"ai_model,omitempty"`
	MemoryLimit int    `json:"memory_limit,omitempty"`
	NumCtx      int    `json:"num_ctx,omitempty"`
	// Distributed diagnostics
	MessageCount      int      `json:"message_count,omitempty"`
	VerifiedCount     int      `json:"verified_count,omitempty"`
	RejectedCount     int      `json:"rejected_count,omitempty"`
	RepairedCount     int      `json:"repaired_count,omitempty"`
	DuplicateCount    int      `json:"duplicate_count,omitempty"`
	EquivocationCount int      `json:"equivocation_count,omitempty"`
	RateLimitedCount  int      `json:"rate_limited_count,omitempty"`
	ClockBackCount    int      `json:"clock_back_count,omitempty"`
	SyncStatus        string   `json:"sync_status,omitempty"`
	Quarantined       int      `json:"quarantined,omitempty"`
	LastReject        string   `json:"last_reject,omitempty"`
	TimeModel         string   `json:"time_model,omitempty"`
	TraceKind         string   `json:"trace_kind,omitempty"`
	TraceVerdict      string   `json:"trace_verdict,omitempty"`
	TraceReason       string   `json:"trace_reason,omitempty"`
	TraceFromPeer     string   `json:"trace_from_peer,omitempty"`
	TraceSummary      string   `json:"trace_summary,omitempty"`
	TraceSteps        []string `json:"trace_steps,omitempty"`
}

type modelInfo struct {
	Name   string `json:"name"`
	Params string `json:"params"`
	RAM    string `json:"ram"`
	Desc   string `json:"desc"`
	Tags   string `json:"tags"`
	RP     int    `json:"rp"`
	Best   bool   `json:"best,omitempty"`
}

var recommendedModels = []modelInfo{
	{
		Name: "nemotron-mini", Params: "4B", RAM: "~3 GB", RP: 5, Best: true,
		Desc: "⭐ Best small model. NVIDIA-tuned for roleplay and on-device chat. Fast, commercial-friendly, and handles persona instructions well. Recommended default.",
		Tags: "roleplay, lightweight, best",
	},
	{
		Name: "llama3.2:3b", Params: "3B", RAM: "~2 GB", RP: 4,
		Desc: "Meta's compact model. Very fast and natural conversation. Great choice when two personas run on the same machine.",
		Tags: "chat, lightweight, fast",
	},
	{
		Name: "qwen3:4b", Params: "4B", RAM: "~3 GB", RP: 4,
		Desc: "Alibaba's Qwen3. Strong instruction following, excellent bilingual (English + Chinese) support. Balanced speed and quality.",
		Tags: "bilingual, instruction, fast",
	},
	{
		Name: "gemma2:2b", Params: "2B", RAM: "~2 GB", RP: 3,
		Desc: "Google's tiny model. Very fast responses. Best for low-spec machines or quick testing.",
		Tags: "lightweight, fast, testing",
	},
	{
		Name: "phi3:mini", Params: "3.8B", RAM: "~3 GB", RP: 3,
		Desc: "Microsoft's small model. Surprisingly capable for its size. Good logic and conversation flow.",
		Tags: "compact, efficient",
	},
	{
		Name: "llama3:8b", Params: "8B", RAM: "~5 GB", RP: 4,
		Desc: "Meta's mid-size model. Natural conversational tone, strong at staying in character. Solid quality on a 16 GB+ machine.",
		Tags: "chat, natural, mid-size",
	},
	{
		Name: "mistral:7b", Params: "7B", RAM: "~4.5 GB", RP: 4,
		Desc: "Fast, creative, expressive. Great for storytelling personas. Needs ~8 GB free RAM.",
		Tags: "creative, roleplay",
	},
	{
		Name: "deepseek-r1:8b", Params: "8B", RAM: "~5 GB", RP: 2,
		Desc: "Reasoning model — not ideal for casual chat, it over-thinks. Keep for reasoning demos only.",
		Tags: "reasoning, slow",
	},
}

// ── App state (initialized lazily on "setup" WS message) ──

type appState struct {
	mu         sync.Mutex
	ready      bool
	done       chan struct{}
	ctx        context.Context
	roomCtx    context.Context
	roomCancel context.CancelFunc
	hub        *hub
	identity   *crypto.Identity
	peerID     string
	userName   string
	aiName     string
	node       *p2p.Node
	p2pPort    int
	room       *p2p.ChatRoom
	vc         *clock.VectorClock
	sendMu     sync.Mutex
	prevHash   string
	batcher    *ai.Batcher
	orch       *ai.Orchestrator
	memStore   *ai.MemoryStore
	ledger     *chat.Ledger
}

func (s *appState) signAndPublish(senderName, content string, isAI bool, mentions []string, mentionAll bool) *types.Message {
	s.mu.Lock()
	if !s.ready || s.room == nil || s.vc == nil || s.identity == nil {
		s.mu.Unlock()
		return nil
	}
	room := s.room
	vc := s.vc
	identity := s.identity
	peerID := s.peerID
	ledger := s.ledger
	s.mu.Unlock()

	s.sendMu.Lock()
	vc.Increment(peerID)
	snap := vc.Snapshot()

	msg := types.NewMessage(peerID, senderName, content, mentions, mentionAll, snap, s.prevHash, isAI)
	sigData, _ := msg.SignableBytes()
	sig, _ := identity.Sign(sigData)
	msg.Signature = sig
	s.prevHash, _ = msg.Hash()
	s.sendMu.Unlock()

	if err := room.Publish(msg); err != nil {
		log.Printf("publish: %v", err)
		return nil
	}

	if ledger != nil {
		ledger.Add(msg)
	}

	data, _ := json.Marshal(envelopeFromMessage(msg, !isAI))
	s.hub.broadcast(data)
	s.broadcastDiagnostics()
	return msg
}

// stopAI immediately cancels batcher timers and in-flight Ollama inference.
func (s *appState) stopAI() {
	if s.batcher != nil {
		s.batcher.Stop()
	}
	if s.orch != nil {
		s.orch.Stop()
	}
	log.Println("[AI] Stopped — all inference cancelled")
}

func (s *appState) resetRoom() {
	s.mu.Lock()
	batcher := s.batcher
	orch := s.orch
	cancel := s.roomCancel
	node := s.node

	s.ready = false
	s.done = make(chan struct{})
	s.roomCtx = nil
	s.roomCancel = nil
	s.identity = nil
	s.peerID = ""
	s.userName = ""
	s.aiName = ""
	s.node = nil
	s.room = nil
	s.vc = nil
	s.prevHash = ""
	s.batcher = nil
	s.orch = nil
	s.memStore = nil
	s.ledger = nil
	s.mu.Unlock()

	if batcher != nil {
		batcher.Stop()
	}
	if orch != nil {
		orch.Stop()
	}
	if cancel != nil {
		cancel()
	}
	if node != nil {
		_ = node.Close()
	}
	log.Println("[Room] Reset complete")
}

func resolveMentions(content string, mentions []string, mentionAll bool) ([]string, bool) {
	parsedMentions, parsedAll := types.ParseMentions(content)
	if len(mentions) == 0 {
		mentions = parsedMentions
	}
	if !mentionAll {
		mentionAll = parsedAll
	}
	return types.NormalizeMentions(mentions), mentionAll
}

func cloneVC(in map[string]uint64) map[string]uint64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]uint64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func shortHashText(hash string) string {
	if hash == "" {
		return "<empty>"
	}
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}

func truncateText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func compactDemoJSON(v any) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return truncateText(string(data), 900)
}

func envelopeFromMessage(msg *types.Message, self bool) wsEnvelope {
	return wsEnvelope{
		Type: "chat", ID: msg.ID, SenderID: msg.SenderID,
		SenderName: msg.SenderName, Content: msg.Content,
		Mentions: append([]string(nil), msg.Mentions...), MentionAll: msg.MentionAll,
		IsAI: msg.IsAI, VectorClock: cloneVC(msg.VectorClock), Self: self,
	}
}

func aiMessageFromTypes(msg *types.Message) ai.ChatMessage {
	return ai.ChatMessage{
		ID: msg.ID, SenderID: msg.SenderID, SenderName: msg.SenderName,
		Content: msg.Content, IsAI: msg.IsAI,
		Mentions: append([]string(nil), msg.Mentions...), MentionAll: msg.MentionAll,
		VectorClock: cloneVC(msg.VectorClock),
	}
}

func (s *appState) acceptMessage(msg *types.Message, self, triggerAI bool) bool {
	s.mu.Lock()
	ledger := s.ledger
	vc := s.vc
	batcher := s.batcher
	orch := s.orch
	hub := s.hub
	s.mu.Unlock()

	if ledger == nil || msg == nil {
		return false
	}
	ok, reason := ledger.AddWithReason(msg)
	if !ok {
		if reason != "duplicate_message" {
			ledger.MarkRejectedReason(reason)
		}
		s.broadcastSecurityTrace(p2p.SecurityTraceEvent{
			FromPeer:      shortID(msg.SenderID),
			Kind:          "ledger",
			Verdict:       "drop",
			Reason:        reason,
			PacketSummary: fmt.Sprintf("message_id=%s sender=%s content=%q", shortID(msg.ID), shortID(msg.SenderID), truncateText(msg.Content, 70)),
			Steps: []string{
				"[LEDGER] signed packet reached application ledger",
				fmt.Sprintf("[CHECK] message_id=%s sender_clock=%d prev_hash=%s", shortID(msg.ID), msg.VectorClock[msg.SenderID], shortHashText(msg.PrevHash)),
				fmt.Sprintf("[DROP] reason = %s", reason),
				"[RESULT] not rendered in UI and not added to AI history",
			},
		})
		s.broadcastDiagnostics()
		return false
	}
	if vc != nil {
		vc.Merge(msg.VectorClock)
	}

	data, _ := json.Marshal(envelopeFromMessage(msg, self))
	hub.broadcast(data)
	s.broadcastSecurityTrace(p2p.SecurityTraceEvent{
		FromPeer:      shortID(msg.SenderID),
		Kind:          "ledger",
		Verdict:       "accept",
		PacketSummary: fmt.Sprintf("message_id=%s sender=%s", shortID(msg.ID), shortID(msg.SenderID)),
		Steps: []string{
			"[LEDGER] message_id is new and sender clock is monotonic",
			"[ORDER] vector clock merged into local logical time",
			"[RESULT] inserted into ledger / UI / AI history",
		},
	})

	cm := aiMessageFromTypes(msg)
	if triggerAI && batcher != nil {
		batcher.Add(cm)
	} else if orch != nil {
		orch.AddToHistory(cm)
	}
	s.broadcastDiagnostics()
	return true
}

func (s *appState) broadcastSecurityTrace(trace p2p.SecurityTraceEvent) {
	s.mu.Lock()
	hub := s.hub
	s.mu.Unlock()
	if hub == nil || len(trace.Steps) == 0 {
		return
	}
	env := wsEnvelope{
		Type:          "security_trace",
		TraceKind:     trace.Kind,
		TraceVerdict:  trace.Verdict,
		TraceReason:   trace.Reason,
		TraceFromPeer: trace.FromPeer,
		TraceSummary:  trace.PacketSummary,
		TraceSteps:    append([]string(nil), trace.Steps...),
	}
	data, _ := json.Marshal(env)
	hub.broadcast(data)
}

func (s *appState) broadcastDiagnostics() {
	s.mu.Lock()
	ledger := s.ledger
	vc := s.vc
	hub := s.hub
	room := s.room
	s.mu.Unlock()

	if ledger == nil || hub == nil {
		return
	}
	stats := ledger.Stats()
	if vc != nil {
		stats.VectorClock = vc.Snapshot()
	}
	trustStats := p2p.TrustStats{}
	if room != nil {
		trustStats = room.TrustStats()
	}
	env := wsEnvelope{
		Type:              "diagnostics",
		MessageCount:      stats.MessageCount,
		VerifiedCount:     stats.Verified,
		RejectedCount:     stats.Rejected + trustStats.Rejected,
		RepairedCount:     stats.Repaired,
		DuplicateCount:    stats.Duplicate,
		EquivocationCount: stats.Equivocation,
		RateLimitedCount:  trustStats.RateLimited,
		ClockBackCount:    stats.ClockBack,
		SyncStatus:        stats.SyncStatus,
		Quarantined:       trustStats.QuarantinedPeers,
		LastReject:        trustStats.LastRejectReason,
		TimeModel:         "Vector Clock (no physical clock sync)",
		VectorClock:       stats.VectorClock,
	}
	if env.LastReject == "" {
		env.LastReject = stats.LastReject
	}
	data, _ := json.Marshal(env)
	hub.broadcast(data)
}

func (s *appState) requestSync() {
	s.mu.Lock()
	if !s.ready || s.room == nil || s.ledger == nil {
		s.mu.Unlock()
		return
	}
	room := s.room
	ledger := s.ledger
	peerID := s.peerID
	s.mu.Unlock()

	ledger.SetSyncStatus("requesting")
	room.PublishSyncRequest(uuid.NewString(), peerID, ledger.IDs())
	s.broadcastDiagnostics()
	go func() {
		time.Sleep(3 * time.Second)
		s.mu.Lock()
		current := s.ledger
		s.mu.Unlock()
		if current != ledger {
			return
		}
		stats := ledger.Stats()
		if stats.SyncStatus == "requesting" {
			ledger.SetSyncStatus("idle")
			s.broadcastDiagnostics()
		}
	}()
}

func (s *appState) sendLedgerSnapshot(sc *safeConn) {
	s.mu.Lock()
	ledger := s.ledger
	peerID := s.peerID
	s.mu.Unlock()
	if ledger == nil || sc == nil {
		return
	}
	for _, msg := range ledger.Recent(chat.DefaultMaxMessages) {
		env := envelopeFromMessage(msg, msg.SenderID == peerID && !msg.IsAI)
		data, _ := json.Marshal(env)
		sc.write(data)
	}
	s.broadcastDiagnostics()
}

func (s *appState) publishForgedDemo() error {
	_, err := s.runDemoAttack("tamper_content")
	return err
}

type attackResult struct {
	OK            bool   `json:"ok"`
	AttackType    string `json:"attack_type"`
	DefenseLayer  string `json:"defense_layer"`
	Expected      string `json:"expected"`
	Published     bool   `json:"published"`
	Note          string `json:"note"`
	PacketSummary string `json:"packet_summary,omitempty"`
	PacketBefore  string `json:"packet_before,omitempty"`
	PacketAfter   string `json:"packet_after,omitempty"`
	Explanation   string `json:"explanation,omitempty"`
}

func (s *appState) signedDemoMessage(senderName, content string, vcOverride map[string]uint64) (*types.Message, error) {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()

	s.mu.Lock()
	if !s.ready || s.vc == nil || s.identity == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("room is not ready")
	}
	vc := s.vc
	identity := s.identity
	peerID := s.peerID
	prevHash := s.prevHash
	s.mu.Unlock()

	vc.Increment(peerID)
	snap := vc.Snapshot()
	if vcOverride != nil {
		snap = cloneVC(vcOverride)
	}
	msg := types.NewMessage(peerID, senderName, content, nil, false, snap, prevHash, false)
	sigData, err := msg.SignableBytes()
	if err != nil {
		return nil, err
	}
	sig, err := identity.Sign(sigData)
	if err != nil {
		return nil, err
	}
	msg.Signature = sig
	return msg, nil
}

func (s *appState) buildFloodDemoPayloads(n int) ([][]byte, error) {
	if n <= 0 {
		return nil, fmt.Errorf("invalid flood count")
	}
	s.mu.Lock()
	if !s.ready || s.identity == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("room is not ready")
	}
	identity := s.identity
	peerID := s.peerID
	s.mu.Unlock()

	payloads := make([][]byte, 0, n)
	prevHash := ""
	for i := 0; i < n; i++ {
		vc := map[string]uint64{peerID: uint64(i + 1)}
		msg := types.NewMessage(peerID, "FloodDemo", fmt.Sprintf("signed flood probe %d", i+1), nil, false, vc, prevHash, false)
		signData, err := msg.SignableBytes()
		if err != nil {
			return nil, err
		}
		sig, err := identity.Sign(signData)
		if err != nil {
			return nil, err
		}
		msg.Signature = sig
		prevHash, _ = msg.Hash()
		msg.Content = fmt.Sprintf("tampered flood probe %d", i+1)
		raw, err := json.Marshal(msg)
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, raw)
	}
	return payloads, nil
}

func (s *appState) runDemoAttack(attackType string) (attackResult, error) {
	s.mu.Lock()
	if !s.ready || s.room == nil || s.identity == nil {
		s.mu.Unlock()
		return attackResult{}, fmt.Errorf("room is not ready")
	}
	room := s.room
	node := s.node
	ledger := s.ledger
	peerID := s.peerID
	identity := s.identity
	s.mu.Unlock()

	result := attackResult{OK: true, AttackType: attackType, Published: true}
	var payload []byte
	var err error

	switch attackType {
	case "tamper_content":
		result.DefenseLayer = "GossipSub Validator + Ed25519 Signature"
		result.Expected = "Rejected +1, no forged chat message displayed"
		msg, err := s.signedDemoMessage("TamperDemo", "legitimate text", nil)
		if err != nil {
			return result, err
		}
		result.PacketBefore = compactDemoJSON(msg)
		msg.Content = "FORGED: content changed after signing"
		result.PacketAfter = compactDemoJSON(msg)
		payload, err = json.Marshal(msg)
		result.Note = "Published once through an isolated demo attacker peer; receivers should reject bad_signature."
		result.PacketSummary = "A signed chat message keeps the original signature but changes content after signing."
		result.Explanation = "The receiver recomputes the signable payload hash from the modified content, so Ed25519 verification fails."

	case "spoof_sync":
		result.DefenseLayer = "GossipSub Validator + Sync FromPeerID binding"
		result.Expected = "Rejected +1, Last Reject shows from_peer_mismatch"
		payload, err = json.Marshal(p2p.Control{
			Kind:       "sync_request",
			RequestID:  uuid.NewString(),
			FromPeerID: "spoofed-peer-id",
			KnownIDs:   nil,
		})
		result.Note = "Published once through an isolated demo attacker peer; receivers should reject from_peer_mismatch."
		result.PacketSummary = "A sync request claims from_peer_id=spoofed-peer-id while the forwarding peer is different."
		result.Explanation = "Honest peers bind sync controls to the actual libp2p forwarding peer, so identity spoofing is rejected."

	case "replay_message":
		result.DefenseLayer = "Ledger Dedupe"
		result.Expected = "No duplicate chat render; Duplicate counter increases on peers that already saw it"
		if ledger == nil {
			return result, fmt.Errorf("ledger is not ready")
		}
		recent := ledger.Recent(1)
		if len(recent) == 0 {
			return result, fmt.Errorf("send a normal chat message before replay demo")
		}
		if err := room.Publish(recent[0]); err != nil {
			return result, err
		}
		result.Note = "Replayed the latest valid signed message."
		result.PacketSummary = fmt.Sprintf("Replayed message_id=%s with a valid original signature.", shortID(recent[0].ID))
		result.Explanation = "The signature is valid, but the ledger already has this message_id, so it is deduplicated before UI/AI insertion."
		return result, nil

	case "malformed_payload":
		result.DefenseLayer = "GossipSub Validator JSON / payload checks"
		result.Expected = "Rejected +1, Last Reject shows invalid_json"
		payload = []byte("{not valid json")
		result.Note = "Published once through an isolated demo attacker peer; receivers should reject invalid_json."
		result.PacketAfter = string(payload)
		result.PacketSummary = "A non-JSON payload is injected into the GossipSub topic."
		result.Explanation = "The topic validator rejects it before it can be decoded as a chat or sync message."

	case "forged_sync":
		result.DefenseLayer = "Sync Response Message Revalidation"
		result.Expected = "Rejected +1, forged sync message does not enter ledger"
		msg, err := s.signedDemoMessage("ForgedSyncDemo", "legitimate sync message", nil)
		if err != nil {
			return result, err
		}
		result.PacketBefore = compactDemoJSON(msg)
		msg.Content = "FORGED SYNC: modified after signing"
		result.PacketAfter = compactDemoJSON(msg)
		payload, err = json.Marshal(p2p.Control{
			Kind:       "sync_response",
			RequestID:  uuid.NewString(),
			FromPeerID: peerID,
			Messages:   []*types.Message{msg},
		})
		result.Note = "Published once through an isolated demo attacker peer; receivers should reject the forged sync message."
		result.PacketSummary = "A sync response carries a tampered signed message inside its history repair payload."
		result.Explanation = "Sync responses are not blindly trusted; every embedded message is re-validated before ledger repair."

	case "missing_sender_clock":
		result.DefenseLayer = "Vector Clock Sanity Check"
		result.Expected = "Rejected +1, Last Reject shows missing_sender_clock"
		msg, err := s.signedDemoMessage("ClockDemo", "signed but missing sender clock", map[string]uint64{"other-peer": 1})
		if err != nil {
			return result, err
		}
		payload, err = json.Marshal(msg)
		result.Note = "Published once through an isolated demo attacker peer; receivers should reject missing_sender_clock."
		result.PacketSummary = "A signed message omits the sender's own vector-clock entry."
		result.PacketAfter = compactDemoJSON(msg)
		result.Explanation = "The message may be signed, but it has invalid logical-time metadata, so causal ordering cannot trust it."

	case "clock_regression":
		result.DefenseLayer = "Ledger monotonic sender clock guard"
		result.Expected = "No chat insert; ClockBack +1 and Last Reject shows clock_regression"
		msgPrime, err := s.signedDemoMessage("ClockDemo", "prime sender clock high watermark", map[string]uint64{peerID: 900})
		if err != nil {
			return result, err
		}
		msgLow, err := s.signedDemoMessage("ClockDemo", "signed but sender clock regressed", map[string]uint64{peerID: 100})
		if err != nil {
			return result, err
		}
		rawPrime, err := json.Marshal(msgPrime)
		if err != nil {
			return result, err
		}
		rawLow, err := json.Marshal(msgLow)
		if err != nil {
			return result, err
		}
		if node == nil || node.Host == nil {
			return result, fmt.Errorf("p2p node is not ready")
		}
		if err := room.PublishRawBurstForDemo([][]byte{rawPrime, rawLow}, node.Host); err != nil {
			return result, err
		}
		result.Note = "Published high counter then lower counter from same sender; second should be rejected as clock_regression."
		result.PacketSummary = "Two signed messages from the same sender: first clock=900, then clock=100."
		result.PacketBefore = compactDemoJSON(msgPrime)
		result.PacketAfter = compactDemoJSON(msgLow)
		result.Explanation = "Both signatures are valid, but the ledger enforces monotonic sender counters to prevent logical-time rollback."
		return result, nil

	case "equivocation_fork":
		result.DefenseLayer = "Ledger hash-chain fork detection"
		result.Expected = "Second forked message blocked; Equivocation +1 and Last Reject shows equivocation_fork"
		msgA, err := s.signedDemoMessage("ForkDemo", "branch A", map[string]uint64{peerID: 777001})
		if err != nil {
			return result, err
		}
		msgA.PrevHash = "attack-fork-prev-hash"
		sigAData, _ := msgA.SignableBytes()
		sigA, _ := identity.Sign(sigAData)
		msgA.Signature = sigA
		msgB, err := s.signedDemoMessage("ForkDemo", "branch B", map[string]uint64{peerID: 777002})
		if err != nil {
			return result, err
		}
		msgB.PrevHash = "attack-fork-prev-hash"
		sigBData, _ := msgB.SignableBytes()
		sigB, _ := identity.Sign(sigBData)
		msgB.Signature = sigB
		rawA, err := json.Marshal(msgA)
		if err != nil {
			return result, err
		}
		rawB, err := json.Marshal(msgB)
		if err != nil {
			return result, err
		}
		if node == nil || node.Host == nil {
			return result, fmt.Errorf("p2p node is not ready")
		}
		if err := room.PublishRawBurstForDemo([][]byte{rawA, rawB}, node.Host); err != nil {
			return result, err
		}
		result.Note = "Published two signed messages from the same sender using the same prev_hash; second should be blocked as equivocation."
		result.PacketSummary = "Two signed branches reuse the same prev_hash with different content."
		result.PacketBefore = compactDemoJSON(msgA)
		result.PacketAfter = compactDemoJSON(msgB)
		result.Explanation = "The hash chain detects a Byzantine sender forking its local history from the same previous hash."
		return result, nil

	case "flood_spam":
		result.DefenseLayer = "Per-peer token-bucket rate limit + quarantine"
		result.Expected = "Burst is throttled; RateLimited increases and no spam chat is rendered"
		if node == nil || node.Host == nil {
			return result, fmt.Errorf("p2p node is not ready")
		}
		payloads, err := s.buildFloodDemoPayloads(24)
		if err != nil {
			return result, err
		}
		if err := room.PublishRawBurstForDemo(payloads, node.Host); err != nil {
			return result, err
		}
		result.Note = "Published 24 tampered probe packets rapidly from one attacker peer; receivers should throttle with rate_limited without rendering chat spam."
		result.PacketSummary = "A burst of 24 packets is sent faster than the per-peer token bucket allows; probes are also tampered so early allowed packets still fail signature checks."
		result.Explanation = "Flood defense is about volume control. The demo uses non-renderable probes so the UI stays clean while Security Trace shows bad_signature first, then rate_limited/quarantine."
		return result, nil

	default:
		return result, fmt.Errorf("unknown attack type %q", attackType)
	}
	if err != nil {
		return result, err
	}
	if node == nil || node.Host == nil {
		return result, fmt.Errorf("p2p node is not ready")
	}
	if err := room.PublishRawForDemo(payload, node.Host); err != nil {
		return result, err
	}
	log.Printf("[AttackLab] Published %s demo payload", attackType)
	return result, nil
}

func (s *appState) init(env wsEnvelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ready {
		return nil
	}

	identity, err := crypto.GenerateIdentity()
	if err != nil {
		return fmt.Errorf("identity: %w", err)
	}
	s.identity = identity
	s.peerID = identity.PeerID.String()
	roomCtx, roomCancel := context.WithCancel(s.ctx)
	s.roomCtx = roomCtx
	s.roomCancel = roomCancel

	node, err := p2p.NewNode(roomCtx, identity.PrivKey, s.p2pPort)
	if err != nil {
		roomCancel()
		return fmt.Errorf("p2p node: %w", err)
	}
	s.node = node

	roomCode := env.Room
	if roomCode == "" {
		roomCode = "default"
	}
	room, err := p2p.JoinChatRoom(roomCtx, node.Host, roomCode)
	if err != nil {
		roomCancel()
		_ = node.Close()
		return fmt.Errorf("join room: %w", err)
	}
	s.room = room
	s.vc = clock.New()
	s.ledger = chat.NewLedger(chat.DefaultMaxMessages)

	memLimit := env.MemoryLimit
	if memLimit <= 0 {
		memLimit = 50
	}
	aiName := env.AIName
	if aiName == "" {
		aiName = "AI"
	}
	s.userName = env.SenderName
	s.aiName = aiName

	memStore, err := ai.NewMemoryStore(aiName, memLimit)
	if err != nil {
		log.Printf("WARN: memory store: %v", err)
	}
	s.memStore = memStore

	persona := ai.Persona{Name: aiName, Style: env.AIStyle, Model: env.AIModel}
	if persona.Style == "" {
		persona.Style = "friendly and casual"
	}
	if persona.Model == "" {
		persona.Model = "deepseek-r1:8b"
	}

	s.orch = ai.NewOrchestrator(persona, memStore, env.NumCtx, func(name, content string) ai.ChatMessage {
		log.Printf("[AI] %s says: %s", name, content)
		mentions, mentionAll := resolveMentions(content, nil, false)
		msg := s.signAndPublish(name, content, true, mentions, mentionAll)
		if msg == nil {
			return ai.ChatMessage{}
		}
		return ai.ChatMessage{
			ID: msg.ID, SenderID: msg.SenderID, SenderName: msg.SenderName,
			Content: msg.Content, IsAI: msg.IsAI,
			Mentions: append([]string(nil), msg.Mentions...), MentionAll: msg.MentionAll,
			VectorClock: cloneVC(msg.VectorClock),
		}
	})

	s.orch.SetStreamCallback(func(name, token string) {
		env := wsEnvelope{Type: "ai_token", SenderName: name, Content: token}
		data, _ := json.Marshal(env)
		s.hub.broadcast(data)
	})

	aiPersonaName := persona.Name
	s.orch.SetThinkingCallback(func(active bool) {
		room.PublishActivity(aiPersonaName, "thinking", active)
		msgType := "activity_stop"
		if active {
			msgType = "activity"
		}
		env := wsEnvelope{Type: msgType, SenderName: aiPersonaName, Content: "thinking"}
		data, _ := json.Marshal(env)
		s.hub.broadcast(data)
	})

	s.batcher = ai.NewBatcher(func(batch []ai.ChatMessage) {
		if len(batch) == 0 {
			s.orch.ForceReply()
			return
		}
		s.orch.Process(batch)
	})

	// P2P → WebSocket relay
	go func() {
		for msg := range room.Messages {
			s.acceptMessage(msg, false, true)
		}
	}()

	// P2P → WS activity relay (typing/thinking from remote peers)
	go func() {
		for act := range room.Activities {
			if act.Active {
				env := wsEnvelope{Type: "activity", SenderName: act.Name, Content: act.Action}
				data, _ := json.Marshal(env)
				s.hub.broadcast(data)
			} else {
				env := wsEnvelope{Type: "activity_stop", SenderName: act.Name, Content: act.Action}
				data, _ := json.Marshal(env)
				s.hub.broadcast(data)
			}
		}
	}()

	go func() {
		for ctrl := range room.Controls {
			switch ctrl.Kind {
			case "sync_request":
				s.mu.Lock()
				ledger := s.ledger
				peerID := s.peerID
				s.mu.Unlock()
				requester := ctrl.FromPeerID
				if ctrl.ForwardedBy != "" {
					requester = ctrl.ForwardedBy
				}
				if ledger == nil || requester == peerID {
					continue
				}
				missing := ledger.MissingFrom(ctrl.KnownIDs, 80)
				if len(missing) > 0 {
					room.PublishSyncResponse(ctrl.RequestID, peerID, missing)
				}
			case "sync_response":
				s.mu.Lock()
				ledger := s.ledger
				s.mu.Unlock()
				if ledger == nil {
					continue
				}
				ledger.SetSyncStatus("repairing")
				repaired := 0
				for _, msg := range ctrl.Messages {
					if s.acceptMessage(msg, false, false) {
						repaired++
					}
				}
				ledger.AddRepaired(repaired)
				if repaired > 0 {
					ledger.SetSyncStatus(fmt.Sprintf("repaired %d", repaired))
				} else {
					ledger.SetSyncStatus("idle")
				}
				s.broadcastDiagnostics()
			}
		}
	}()

	go func() {
		for reject := range room.Rejected {
			s.mu.Lock()
			ledger := s.ledger
			s.mu.Unlock()
			if ledger != nil {
				log.Printf("[Trust] rejected %s: %s", reject.PeerID, reject.Reason)
				ledger.MarkRejected()
				s.broadcastDiagnostics()
			}
		}
	}()

	go func() {
		for {
			select {
			case trace := <-room.Trace:
				s.broadcastSecurityTrace(trace)
			case <-roomCtx.Done():
				return
			}
		}
	}()

	// Peer count ticker + periodic member re-announcement
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				env := wsEnvelope{Type: "peers", Count: node.PeerCount()}
				data, _ := json.Marshal(env)
				s.hub.broadcast(data)
				s.broadcastDiagnostics()

				// Re-broadcast so late joiners discover us
				room.PublishActivity(s.userName, "join", true)
				room.PublishActivity(s.aiName, "join_ai", true)
			case <-roomCtx.Done():
				return
			}
		}
	}()

	s.ready = true
	close(s.done)
	go func() {
		time.Sleep(1500 * time.Millisecond)
		s.requestSync()
	}()
	log.Printf("Room %q joined — ready to chat", roomCode)
	return nil
}

// ── Ollama auto-start ──

func ensureOllama() {
	client := &http.Client{Timeout: 2 * time.Second}
	if resp, err := client.Get(ollamaBase + "/api/tags"); err == nil {
		resp.Body.Close()
		log.Println("[Ollama] Already running")
		return
	}

	bin := ollamaBin()
	if bin == "" {
		log.Println("[Ollama] Not installed — user can install from the web UI")
		return
	}

	log.Println("[Ollama] Not running — attempting auto-start...")
	cmd := exec.Command(bin, "serve")
	if err := cmd.Start(); err != nil {
		log.Printf("[Ollama] Auto-start failed: %v", err)
		return
	}

	for i := 0; i < 20; i++ {
		time.Sleep(500 * time.Millisecond)
		if resp, err := client.Get(ollamaBase + "/api/tags"); err == nil {
			resp.Body.Close()
			log.Println("[Ollama] Started successfully")
			return
		}
	}
	log.Println("[Ollama] Started process but API not responding yet")
}

// ── main ──

func main() {
	port := flag.Int("port", 8080, "HTTP port")
	p2pPort := flag.Int("p2p-port", 0, "fixed libp2p TCP port for LAN/Tailscale demos (0 = random)")
	flag.Parse()

	ensureOllama()

	state := &appState{
		ctx:     context.Background(),
		hub:     &hub{clients: make(map[*websocket.Conn]*safeConn)},
		done:    make(chan struct{}),
		p2pPort: *p2pPort,
	}

	// ── Ollama proxy APIs (work before setup) ──

	http.HandleFunc("/api/ollama/status", handleOllamaStatus)
	http.HandleFunc("/api/ollama/install", handleOllamaInstall)
	http.HandleFunc("/api/ollama/start", handleOllamaStart)
	http.HandleFunc("/api/models", handleModels)
	http.HandleFunc("/api/models/pull", handleModelPull)
	http.HandleFunc("/api/models/recommended", handleRecommended)
	http.HandleFunc("/api/memory", state.handleMemory)
	http.HandleFunc("/api/demo/attack", state.handleAttackDemo)
	http.HandleFunc("/api/demo/forge-message", state.handleForgeDemo)
	http.HandleFunc("/api/peer/info", state.handlePeerInfo)
	http.HandleFunc("/api/peer/connect", state.handlePeerConnect)

	// ── WebSocket ──

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("ws upgrade: %v", err)
			return
		}
		sc := state.hub.add(conn)
		defer func() {
			state.hub.remove(conn)
			conn.Close()
		}()

		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var env wsEnvelope
			if json.Unmarshal(raw, &env) != nil {
				continue
			}

			switch env.Type {
			case "setup":
				if err := state.init(env); err != nil {
					errMsg, _ := json.Marshal(wsEnvelope{Type: "error", Content: err.Error()})
					sc.write(errMsg)
					continue
				}
				ready, _ := json.Marshal(wsEnvelope{Type: "ready", PeerID: state.peerID})
				sc.write(ready)
				state.sendLedgerSnapshot(sc)

				// Announce join to other peers
				state.room.PublishActivity(env.SenderName, "join", true)
				aiName := env.AIName
				if aiName == "" {
					aiName = "AI"
				}
				state.room.PublishActivity(aiName, "join_ai", true)

			case "chat":
				<-state.done
				mentions, mentionAll := resolveMentions(env.Content, env.Mentions, env.MentionAll)
				msg := state.signAndPublish(env.SenderName, env.Content, false, mentions, mentionAll)
				localMsg := ai.ChatMessage{
					SenderName: env.SenderName, Content: env.Content, IsAI: false,
					Mentions: mentions, MentionAll: mentionAll,
				}
				if msg != nil {
					localMsg.ID = msg.ID
					localMsg.SenderID = msg.SenderID
					localMsg.VectorClock = cloneVC(msg.VectorClock)
				}
				state.batcher.Add(localMsg)

			case "persona":
				<-state.done
				state.orch.SetPersona(ai.Persona{
					Name: env.AIName, Style: env.AIStyle, Model: env.AIModel,
				})

			case "typing":
				<-state.done
				active := env.Content == "true"
				state.room.PublishActivity(env.SenderName, "typing", active)

			case "force_reply":
				<-state.done
				state.batcher.ForceFlush()

			case "leave":
				state.resetRoom()
			}
		}
	})

	http.Handle("/", http.FileServer(http.FS(web.Content)))

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Server → http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

// ── Ollama proxy handlers ──

const ollamaBase = "http://localhost:11434"

func ollamaBin() string {
	if p, err := exec.LookPath("ollama"); err == nil {
		return p
	}
	candidates := []string{"/usr/local/bin/ollama", "/opt/homebrew/bin/ollama"}
	if runtime.GOOS == "windows" {
		candidates = append(candidates,
			`C:\Program Files\Ollama\ollama.exe`,
			`C:\Users\`+envOrDefault("USERNAME", "")+`\AppData\Local\Programs\Ollama\ollama.exe`,
		)
	}
	for _, p := range candidates {
		if _, err := exec.LookPath(p); err == nil {
			return p
		}
	}
	return ""
}

func envOrDefault(key, def string) string {
	if v := getenvFn(key); v != "" {
		return v
	}
	return def
}

var getenvFn = os.Getenv

func ollamaInstalled() bool { return ollamaBin() != "" }

func handleOllamaStatus(w http.ResponseWriter, r *http.Request) {
	installed := ollamaInstalled()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(ollamaBase + "/api/tags")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]any{
			"running": false, "installed": installed,
			"os": runtime.GOOS, "error": err.Error(),
		})
		return
	}
	resp.Body.Close()
	json.NewEncoder(w).Encode(map[string]any{"running": true, "installed": true, "os": runtime.GOOS})
}

func handleOllamaInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}

	if ollamaInstalled() {
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "message": "already installed"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)

	writeLine := func(s string) {
		fmt.Fprintf(w, "data: %s\n\n", s)
		if flusher != nil {
			flusher.Flush()
		}
	}

	switch runtime.GOOS {
	case "darwin":
		installDarwin(writeLine)
	case "linux":
		installLinux(writeLine)
	case "windows":
		installWindows(writeLine)
	default:
		writeLine("[DONE] manual: Unsupported OS — please install from https://ollama.com/download")
		return
	}

	if ollamaInstalled() {
		writeLine("Starting Ollama...")
		startOllamaBg(writeLine)
		writeLine("[DONE] ok")
	}
}

func installDarwin(writeLine func(string)) {
	if _, err := exec.LookPath("brew"); err == nil {
		writeLine("Installing via Homebrew...")
		runCmdStreaming(exec.Command("brew", "install", "ollama"), writeLine)
		if ollamaInstalled() {
			return
		}
		writeLine("Homebrew install did not produce binary, trying direct download...")
	}

	writeLine("Downloading Ollama binary for macOS...")
	arch := runtime.GOARCH
	url := "https://ollama.com/download/ollama-darwin"
	if arch == "arm64" {
		url = "https://ollama.com/download/ollama-darwin"
	}
	dest := "/usr/local/bin/ollama"

	if err := os.MkdirAll("/usr/local/bin", 0755); err != nil {
		home := os.Getenv("HOME")
		dest = home + "/.local/bin/ollama"
		os.MkdirAll(home+"/.local/bin", 0755)
		writeLine("No write access to /usr/local/bin, installing to " + dest)
	}

	if err := downloadFile(url, dest, writeLine); err != nil {
		writeLine("Download failed: " + err.Error())
		writeLine("[DONE] error")
		return
	}
	os.Chmod(dest, 0755)
	writeLine("Ollama installed to " + dest)
}

func installLinux(writeLine func(string)) {
	writeLine("Installing via official script...")
	cmd := exec.Command("sh", "-c", "curl -fsSL https://ollama.com/install.sh | sh")
	cmd.Env = append(cmd.Environ(), "NONINTERACTIVE=1")
	runCmdStreaming(cmd, writeLine)
	if ollamaInstalled() {
		return
	}

	writeLine("Script install failed, trying direct binary download...")
	arch := runtime.GOARCH
	url := "https://ollama.com/download/ollama-linux-" + arch
	dest := "/usr/local/bin/ollama"

	if err := os.MkdirAll("/usr/local/bin", 0755); err != nil {
		home := os.Getenv("HOME")
		dest = home + "/.local/bin/ollama"
		os.MkdirAll(home+"/.local/bin", 0755)
		writeLine("No root access, installing to " + dest)
	}

	if err := downloadFile(url, dest, writeLine); err != nil {
		writeLine("Download failed: " + err.Error())
		writeLine("[DONE] error")
		return
	}
	os.Chmod(dest, 0755)
	writeLine("Ollama installed to " + dest)
}

func installWindows(writeLine func(string)) {
	if _, err := exec.LookPath("winget"); err == nil {
		writeLine("Installing via winget...")
		runCmdStreaming(exec.Command("winget", "install", "--id", "Ollama.Ollama", "-e",
			"--accept-source-agreements", "--accept-package-agreements"), writeLine)
		if ollamaInstalled() {
			return
		}
	}

	writeLine("Downloading Ollama installer for Windows...")
	tmpDir := os.TempDir()
	dest := tmpDir + `\OllamaSetup.exe`

	if err := downloadFile("https://ollama.com/download/OllamaSetup.exe", dest, writeLine); err != nil {
		writeLine("Download failed: " + err.Error())
		writeLine("[DONE] error")
		return
	}

	writeLine("Running installer (a window may appear)...")
	cmd := exec.Command(dest, "/VERYSILENT", "/NORESTART")
	if err := cmd.Run(); err != nil {
		writeLine("Silent install failed, launching installer UI...")
		exec.Command("cmd", "/c", "start", dest).Start()
		writeLine("Please complete the installer and refresh this page")
		writeLine("[DONE] manual_wait")
		return
	}
	writeLine("Ollama installed!")
}

func downloadFile(url, dest string, writeLine func(string)) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}

	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("cannot create file: %w", err)
	}
	defer out.Close()

	total := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 32*1024)
	lastPct := -1

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
			downloaded += int64(n)
			if total > 0 {
				pct := int(downloaded * 100 / total)
				if pct != lastPct && pct%10 == 0 {
					writeLine(fmt.Sprintf("Downloading... %d%% (%d MB / %d MB)", pct, downloaded/1e6, total/1e6))
					lastPct = pct
				}
			}
		}
		if readErr != nil {
			break
		}
	}
	writeLine("Download complete!")
	return nil
}

func runCmdStreaming(cmd *exec.Cmd, writeLine func(string)) {
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		writeLine("Command error: " + err.Error())
		return
	}
	buf := make([]byte, 1024)
	for {
		n, readErr := stdout.Read(buf)
		if n > 0 {
			writeLine(string(buf[:n]))
		}
		if readErr != nil {
			break
		}
	}
	if err := cmd.Wait(); err != nil {
		writeLine("Command failed: " + err.Error())
	}
}

func startOllamaBg(writeLine func(string)) {
	bin := ollamaBin()
	if bin == "" {
		bin = "ollama"
	}
	srvCmd := exec.Command(bin, "serve")
	if err := srvCmd.Start(); err != nil {
		if writeLine != nil {
			writeLine("Could not auto-start: " + err.Error())
		}
		return
	}
	client := &http.Client{Timeout: 2 * time.Second}
	for i := 0; i < 20; i++ {
		time.Sleep(500 * time.Millisecond)
		if resp, err := client.Get(ollamaBase + "/api/tags"); err == nil {
			resp.Body.Close()
			if writeLine != nil {
				writeLine("Ollama is running!")
			}
			return
		}
	}
}

func handleOllamaStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}

	client := &http.Client{Timeout: 2 * time.Second}
	if resp, err := client.Get(ollamaBase + "/api/tags"); err == nil {
		resp.Body.Close()
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "method": "already_running"})
		return
	}

	// Strategy 1: CLI binary
	bin := ollamaBin()
	if bin != "" {
		cmd := exec.Command(bin, "serve")
		if err := cmd.Start(); err == nil {
			if waitForOllama(client) {
				json.NewEncoder(w).Encode(map[string]any{"ok": true, "method": "cli"})
				return
			}
		}
	}

	// Strategy 2: macOS — open the Ollama.app (starts serve automatically)
	if runtime.GOOS == "darwin" {
		if err := exec.Command("open", "-a", "Ollama").Run(); err == nil {
			if waitForOllama(client) {
				json.NewEncoder(w).Encode(map[string]any{"ok": true, "method": "app"})
				return
			}
		}
	}

	// Strategy 3: Windows — try common install paths
	if runtime.GOOS == "windows" {
		for _, p := range []string{
			`C:\Program Files\Ollama\ollama.exe`,
			os.Getenv("LOCALAPPDATA") + `\Programs\Ollama\ollama.exe`,
		} {
			if _, err := os.Stat(p); err == nil {
				cmd := exec.Command(p, "serve")
				if cmd.Start() == nil {
					if waitForOllama(client) {
						json.NewEncoder(w).Encode(map[string]any{"ok": true, "method": "path_scan"})
						return
					}
				}
			}
		}
	}

	json.NewEncoder(w).Encode(map[string]any{
		"ok":    false,
		"error": "Could not start Ollama. Please start it manually or reinstall.",
	})
}

func waitForOllama(client *http.Client) bool {
	for i := 0; i < 30; i++ {
		time.Sleep(500 * time.Millisecond)
		if resp, err := client.Get(ollamaBase + "/api/tags"); err == nil {
			resp.Body.Close()
			return true
		}
	}
	return false
}

func handleModels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		resp, err := http.Get(ollamaBase + "/api/tags")
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		io.Copy(w, resp.Body)

	case http.MethodDelete:
		var body struct {
			Name string `json:"name"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		reqBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodDelete, ollamaBase+"/api/delete", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)

	default:
		http.Error(w, "method not allowed", 405)
	}
}

func handleModelPull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	reqBody, _ := json.Marshal(map[string]any{"name": body.Name, "stream": true})
	resp, err := http.Post(ollamaBase+"/api/pull", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)

	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			fmt.Fprintf(w, "data: %s\n\n", buf[:n])
			if ok {
				flusher.Flush()
			}
		}
		if err != nil {
			break
		}
	}
}

func handleRecommended(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(recommendedModels)
}

func (s *appState) handleMemory(w http.ResponseWriter, r *http.Request) {
	if s.memStore == nil {
		json.NewEncoder(w).Encode([]ai.Memory{})
		return
	}
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.memStore.All())
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id != "" {
			s.memStore.Delete(id)
		}
		w.WriteHeader(204)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *appState) handleForgeDemo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	result, err := s.runDemoAttack("tamper_content")
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *appState) handleAttackDemo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Type string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Type == "" {
		body.Type = "tamper_content"
	}
	result, err := s.runDemoAttack(body.Type)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *appState) handlePeerInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	node := s.node
	peerID := s.peerID
	roomReady := s.ready
	s.mu.Unlock()
	if !roomReady || node == nil {
		http.Error(w, "room is not ready", http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"peer_id": peerID,
		"addrs":   node.AddrStrings(),
	})
}

func (s *appState) handlePeerConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Multiaddr string `json:"multiaddr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Multiaddr == "" {
		http.Error(w, "multiaddr is required", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	node := s.node
	s.mu.Unlock()
	if node == nil {
		http.Error(w, "room is not ready", http.StatusConflict)
		return
	}
	if err := node.ConnectMultiaddr(body.Multiaddr); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.broadcastDiagnostics()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

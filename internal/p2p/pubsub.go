package p2p

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/security/noise"

	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/types"
)

// Activity is a lightweight event (typing, thinking) broadcast over GossipSub.
type Activity struct {
	Kind   string `json:"_kind"` // "activity"
	Name   string `json:"name"`
	Action string `json:"action"` // "typing" or "thinking"
	Active bool   `json:"active"`
}

type Control struct {
	Kind        string           `json:"_kind"`
	RequestID   string           `json:"request_id,omitempty"`
	FromPeerID  string           `json:"from_peer_id,omitempty"`
	KnownIDs    []string         `json:"known_ids,omitempty"`
	Messages    []*types.Message `json:"messages,omitempty"`
	ForwardedBy string           `json:"-"`
}

type SecurityTraceEvent struct {
	FromPeer      string   `json:"from_peer,omitempty"`
	Kind          string   `json:"kind,omitempty"`
	Verdict       string   `json:"verdict,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	PacketSummary string   `json:"packet_summary,omitempty"`
	Steps         []string `json:"steps,omitempty"`
}

// ChatRoom wraps a GossipSub topic with signature-verified message delivery.
type ChatRoom struct {
	topic      *pubsub.Topic
	topicName  string
	sub        *pubsub.Subscription
	selfID     string
	Messages   chan *types.Message
	Activities chan *Activity
	Controls   chan *Control
	Rejected   chan RejectEvent
	Trace      chan SecurityTraceEvent
	trust      *TrustTracker
	ctx        context.Context
}

// JoinChatRoom creates a GossipSub instance, joins a room-specific topic, and starts the read loop.
func JoinChatRoom(ctx context.Context, h host.Host, roomCode string) (*ChatRoom, error) {
	topicName := "dmapc-room-" + roomCode
	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		return nil, fmt.Errorf("create gossipsub: %w", err)
	}
	trust := NewTrustTracker()
	traceCh := make(chan SecurityTraceEvent, 128)
	if err := ps.RegisterTopicValidator(topicName, func(ctx context.Context, from peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
		data := msg.GetData()
		if err := validateEnvelopeData(data, from, trust); err != nil {
			emitSecurityTrace(traceCh, buildSecurityTrace(data, from, err))
			log.Printf("[PubSub] validator rejected from %s: %v", from, err)
			return pubsub.ValidationReject
		}
		trace := buildSecurityTrace(data, from, nil)
		if trace.Kind != "activity" {
			emitSecurityTrace(traceCh, trace)
		}
		return pubsub.ValidationAccept
	}); err != nil {
		return nil, fmt.Errorf("register validator: %w", err)
	}
	topic, err := ps.Join(topicName)
	if err != nil {
		return nil, fmt.Errorf("join topic %q: %w", topicName, err)
	}
	sub, err := topic.Subscribe()
	if err != nil {
		return nil, fmt.Errorf("subscribe to %q: %w", topicName, err)
	}
	log.Printf("[PubSub] Joined room %q (topic: %s)", roomCode, topicName)

	cr := &ChatRoom{
		topic:      topic,
		topicName:  topicName,
		sub:        sub,
		selfID:     h.ID().String(),
		Messages:   make(chan *types.Message, 128),
		Activities: make(chan *Activity, 256),
		Controls:   make(chan *Control, 32),
		Rejected:   make(chan RejectEvent, 32),
		Trace:      traceCh,
		trust:      trust,
		ctx:        ctx,
	}
	go cr.readLoop()
	return cr, nil
}

// Publish serialises a Message and broadcasts it to the GossipSub topic.
func (cr *ChatRoom) Publish(msg *types.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	return cr.topic.Publish(cr.ctx, data)
}

func (cr *ChatRoom) PublishRawForDemo(data []byte, publisher host.Host) error {
	return cr.PublishRawBurstForDemo([][]byte{data}, publisher)
}

func (cr *ChatRoom) PublishRawBurstForDemo(payloads [][]byte, publisher host.Host) error {
	if publisher == nil {
		return fmt.Errorf("demo publisher host is not ready")
	}
	if len(payloads) == 0 {
		return fmt.Errorf("empty demo payload")
	}
	ctx, cancel := context.WithTimeout(cr.ctx, 5*time.Second)
	defer cancel()

	peers := publisher.Network().Peers()
	if len(peers) == 0 {
		return fmt.Errorf("no connected peers; open another node in the same room before running remote attack demos")
	}

	attacker, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
		libp2p.Security(noise.ID, noise.New),
	)
	if err != nil {
		return fmt.Errorf("create demo attacker peer: %w", err)
	}
	defer attacker.Close()

	connected := 0
	for _, pid := range peers {
		addrs := publisher.Peerstore().Addrs(pid)
		if len(addrs) == 0 {
			continue
		}
		if err := attacker.Connect(ctx, peer.AddrInfo{ID: pid, Addrs: addrs}); err != nil {
			log.Printf("[AttackLab] demo attacker connect to %s: %v", pid, err)
			continue
		}
		connected++
	}
	if connected == 0 {
		return fmt.Errorf("demo attacker could not connect to any peers")
	}

	ps, err := pubsub.NewGossipSub(ctx, attacker)
	if err != nil {
		return fmt.Errorf("create demo attacker gossipsub: %w", err)
	}
	topic, err := ps.Join(cr.topicName)
	if err != nil {
		return fmt.Errorf("join demo attacker topic: %w", err)
	}
	defer topic.Close()

	// Give GossipSub a short moment to graft before publishing demo traffic.
	select {
	case <-time.After(700 * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}
	for i, payload := range payloads {
		if len(payload) == 0 {
			continue
		}
		if err := topic.Publish(ctx, payload); err != nil {
			return err
		}
		if i < len(payloads)-1 {
			select {
			case <-time.After(25 * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return nil
}

// PublishActivity broadcasts a lightweight typing/thinking event.
func (cr *ChatRoom) PublishActivity(name, action string, active bool) {
	data, _ := json.Marshal(Activity{Kind: "activity", Name: name, Action: action, Active: active})
	cr.topic.Publish(cr.ctx, data)
}

func (cr *ChatRoom) PublishSyncRequest(requestID, fromPeerID string, knownIDs []string) {
	data, _ := json.Marshal(Control{
		Kind:       "sync_request",
		RequestID:  requestID,
		FromPeerID: fromPeerID,
		KnownIDs:   append([]string(nil), knownIDs...),
	})
	cr.topic.Publish(cr.ctx, data)
}

func (cr *ChatRoom) PublishSyncResponse(requestID, fromPeerID string, messages []*types.Message) {
	data, _ := json.Marshal(Control{
		Kind:       "sync_response",
		RequestID:  requestID,
		FromPeerID: fromPeerID,
		Messages:   messages,
	})
	cr.topic.Publish(cr.ctx, data)
}

func (cr *ChatRoom) readLoop() {
	defer close(cr.Messages)
	defer close(cr.Activities)
	defer close(cr.Controls)
	defer close(cr.Rejected)
	for {
		envelope, err := cr.sub.Next(cr.ctx)
		if err != nil {
			if cr.ctx.Err() != nil {
				return
			}
			log.Printf("[PubSub] read error: %v", err)
			continue
		}
		if envelope.ReceivedFrom.String() == cr.selfID {
			continue
		}

		// Peek at the kind field to distinguish activity vs chat
		var peek struct {
			Kind string `json:"_kind"`
		}
		json.Unmarshal(envelope.Data, &peek)

		if peek.Kind == "activity" {
			var act Activity
			if json.Unmarshal(envelope.Data, &act) == nil {
				select {
				case cr.Activities <- &act:
				default:
				}
			}
			continue
		}

		if peek.Kind == "sync_request" || peek.Kind == "sync_response" {
			var ctrl Control
			if json.Unmarshal(envelope.Data, &ctrl) != nil {
				continue
			}
			if ctrl.FromPeerID == cr.selfID {
				continue
			}
			ctrl.ForwardedBy = envelope.ReceivedFrom.String()
			if ctrl.Kind == "sync_response" {
				verified := ctrl.Messages[:0]
				for _, msg := range ctrl.Messages {
					if cr.verifyMessage(msg) {
						verified = append(verified, msg)
					} else {
						cr.recordRejected(msg.SenderID, "bad_sync_message")
					}
				}
				ctrl.Messages = verified
			}
			select {
			case cr.Controls <- &ctrl:
			default:
			}
			continue
		}

		var msg types.Message
		if err := json.Unmarshal(envelope.Data, &msg); err != nil {
			log.Printf("[PubSub] invalid payload: %v", err)
			continue
		}

		if !cr.verifyMessage(&msg) {
			cr.recordRejected(msg.SenderID, "bad_chat_message")
			continue
		}

		select {
		case cr.Messages <- &msg:
		default:
			log.Printf("[PubSub] dropped chat message from %s: messages channel full", msg.SenderID)
		}
	}
}

func (cr *ChatRoom) recordRejected(peerID, reason string) {
	select {
	case cr.Rejected <- RejectEvent{PeerID: peerID, Reason: reason}:
	default:
	}
}

func (cr *ChatRoom) verifyMessage(msg *types.Message) bool {
	if err := ValidateSignedMessage(msg, cr.trust, ""); err != nil {
		log.Printf("[PubSub] forged/invalid message discarded: %v", err)
		return false
	}
	return true
}

func (cr *ChatRoom) TrustStats() TrustStats {
	return cr.trust.Stats()
}

func emitSecurityTrace(ch chan SecurityTraceEvent, trace SecurityTraceEvent) {
	if len(trace.Steps) == 0 {
		return
	}
	select {
	case ch <- trace:
	default:
	}
}

func buildSecurityTrace(data []byte, from peer.ID, validationErr error) SecurityTraceEvent {
	trace := SecurityTraceEvent{
		FromPeer: shortPeerID(peerIDString(from)),
		Verdict:  "accept",
		Steps: []string{
			fmt.Sprintf("[RECV] packet from %s (%d bytes)", shortPeerID(peerIDString(from)), len(data)),
		},
	}
	if validationErr != nil {
		trace.Verdict = "drop"
		trace.Reason = normalizeRejectReason(validationErr)
	}

	var peek struct {
		Kind string `json:"_kind"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		trace.Kind = "malformed"
		trace.PacketSummary = "invalid JSON payload"
		trace.Steps = append(trace.Steps,
			"[PARSE] JSON decoding failed",
			fmt.Sprintf("[DROP] reason = %s", trace.Reason),
			"[RESULT] not inserted into ledger / UI / AI history",
		)
		return trace
	}

	switch peek.Kind {
	case "activity":
		trace.Kind = "activity"
		trace.PacketSummary = "activity event"
		trace.Steps = append(trace.Steps, "[PARSE] decoded as activity event", "[RESULT] accepted as lightweight presence event")
	case "sync_request", "sync_response":
		var ctrl Control
		_ = json.Unmarshal(data, &ctrl)
		trace.Kind = peek.Kind
		trace.PacketSummary = fmt.Sprintf("%s request_id=%s from_peer_id=%s messages=%d", peek.Kind, shortID(ctrl.RequestID), shortPeerID(ctrl.FromPeerID), len(ctrl.Messages))
		trace.Steps = append(trace.Steps,
			fmt.Sprintf("[PARSE] decoded as %s control message", peek.Kind),
			fmt.Sprintf("[BIND] from_peer_id=%s, forwarding peer=%s", shortPeerID(ctrl.FromPeerID), shortPeerID(peerIDString(from))),
		)
		if peek.Kind == "sync_response" {
			trace.Steps = append(trace.Steps, fmt.Sprintf("[VERIFY] re-validating %d signed message(s) inside sync response", len(ctrl.Messages)))
		}
		appendTraceVerdict(&trace)
	default:
		var msg types.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			trace.Kind = "bad_chat_message"
			trace.PacketSummary = "malformed chat message"
			trace.Steps = append(trace.Steps, "[PARSE] chat JSON shape invalid")
			appendTraceVerdict(&trace)
			return trace
		}
		trace.Kind = "chat"
		trace.PacketSummary = fmt.Sprintf("chat sender=%s name=%s content=%q clock=%s", shortPeerID(msg.SenderID), msg.SenderName, truncate(msg.Content, 70), clockSummary(msg.VectorClock, msg.SenderID))
		trace.Steps = append(trace.Steps,
			"[PARSE] decoded as chat message",
			fmt.Sprintf("[FIELDS] id=%s sender_id=%s prev_hash=%s", shortID(msg.ID), shortPeerID(msg.SenderID), shortHash(msg.PrevHash)),
		)
		if signable, err := msg.SignableBytes(); err == nil {
			sum := sha256.Sum256(signable)
			trace.Steps = append(trace.Steps, fmt.Sprintf("[HASH] signable payload SHA-256 = %s", hex.EncodeToString(sum[:])[:16]))
		} else {
			trace.Steps = append(trace.Steps, fmt.Sprintf("[HASH] failed to build signable payload: %v", err))
		}
		if trace.Reason == "missing_sender_clock" {
			trace.Steps = append(trace.Steps, "[CLOCK] sender's own vector-clock entry is missing or zero")
		} else if trace.Reason == "bad_signature" {
			trace.Steps = append(trace.Steps, "[VERIFY] Ed25519 signature check failed against modified signable payload")
		} else if trace.Verdict == "accept" {
			trace.Steps = append(trace.Steps, "[VERIFY] Ed25519 signature matches sender_id public key")
		}
		appendTraceVerdict(&trace)
	}
	return trace
}

func appendTraceVerdict(trace *SecurityTraceEvent) {
	if trace.Verdict == "drop" {
		trace.Steps = append(trace.Steps,
			fmt.Sprintf("[DROP] reason = %s", trace.Reason),
			"[RESULT] not inserted into ledger / UI / AI history",
		)
		return
	}
	trace.Steps = append(trace.Steps, "[RESULT] validator accepted packet for application processing")
}

func normalizeRejectReason(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	reasons := []string{
		"bad_signature", "from_peer_mismatch", "invalid_json", "missing_sender_clock",
		"rate_limited", "quarantined", "malformed", "payload too large", "empty payload",
	}
	for _, reason := range reasons {
		if strings.Contains(msg, reason) || strings.Contains(strings.ReplaceAll(msg, " ", "_"), reason) {
			return reason
		}
	}
	if strings.Contains(msg, "bad signature") {
		return "bad_signature"
	}
	if strings.Contains(msg, "invalid character") {
		return "invalid_json"
	}
	return truncate(msg, 48)
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func shortHash(hash string) string {
	if hash == "" {
		return "<empty>"
	}
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func clockSummary(clock map[string]uint64, senderID string) string {
	if len(clock) == 0 {
		return "{}"
	}
	return fmt.Sprintf("{sender:%d entries:%d}", clock[senderID], len(clock))
}

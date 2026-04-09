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
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/ai"
	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/clock"
	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/crypto"
	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/p2p"
	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/types"
	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/web"
)

// ── Types ──

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

type hub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]struct{}
}

func (h *hub) add(c *websocket.Conn)    { h.mu.Lock(); h.clients[c] = struct{}{}; h.mu.Unlock() }
func (h *hub) remove(c *websocket.Conn) { h.mu.Lock(); delete(h.clients, c); h.mu.Unlock() }
func (h *hub) broadcast(data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		_ = c.WriteMessage(websocket.TextMessage, data)
	}
}

type wsEnvelope struct {
	Type        string            `json:"type"`
	ID          string            `json:"id,omitempty"`
	SenderID    string            `json:"sender_id,omitempty"`
	SenderName  string            `json:"sender_name,omitempty"`
	Content     string            `json:"content,omitempty"`
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
}

type modelInfo struct {
	Name    string `json:"name"`
	Params  string `json:"params"`
	RAM     string `json:"ram"`
	Desc    string `json:"desc"`
}

var recommendedModels = []modelInfo{
	{"deepseek-r1:1.5b", "1.5B", "~1.5 GB", "Fastest, has <think> reasoning"},
	{"deepseek-r1:8b", "8B", "~5 GB", "Best balance — recommended"},
	{"deepseek-r1:14b", "14B", "~9 GB", "Highest quality, slower"},
	{"llama3:8b", "8B", "~5 GB", "Strong conversational ability"},
	{"gemma2:2b", "2B", "~2 GB", "Google, lightweight and fast"},
	{"phi3:mini", "3.8B", "~3 GB", "Microsoft, good reasoning"},
}

// ── App state (initialized lazily on "setup" WS message) ──

type appState struct {
	mu       sync.Mutex
	ready    bool
	done     chan struct{} // closed once ready
	ctx      context.Context
	hub      *hub
	identity *crypto.Identity
	peerID   string
	node     *p2p.Node
	room     *p2p.ChatRoom
	vc       *clock.VectorClock
	sendMu   sync.Mutex
	prevHash string
	batcher  *ai.Batcher
	orch     *ai.Orchestrator
	memStore *ai.MemoryStore
}

func (s *appState) signAndPublish(senderName, content string, isAI bool) {
	s.sendMu.Lock()
	s.vc.Increment(s.peerID)
	snap := s.vc.Snapshot()

	msg := types.NewMessage(s.peerID, senderName, content, snap, s.prevHash, isAI)
	sigData, _ := msg.SignableBytes()
	sig, _ := s.identity.Sign(sigData)
	msg.Signature = sig
	s.prevHash, _ = msg.Hash()
	s.sendMu.Unlock()

	if err := s.room.Publish(msg); err != nil {
		log.Printf("publish: %v", err)
		return
	}

	echo := wsEnvelope{
		Type: "chat", ID: msg.ID, SenderID: s.peerID,
		SenderName: senderName, Content: content,
		IsAI: isAI, VectorClock: snap, Self: true,
	}
	data, _ := json.Marshal(echo)
	s.hub.broadcast(data)
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

	node, err := p2p.NewNode(s.ctx, identity.PrivKey)
	if err != nil {
		return fmt.Errorf("p2p node: %w", err)
	}
	s.node = node

	roomCode := env.Room
	if roomCode == "" {
		roomCode = "default"
	}
	room, err := p2p.JoinChatRoom(s.ctx, node.Host, roomCode)
	if err != nil {
		return fmt.Errorf("join room: %w", err)
	}
	s.room = room
	s.vc = clock.New()

	memLimit := env.MemoryLimit
	if memLimit <= 0 {
		memLimit = 50
	}
	aiName := env.AIName
	if aiName == "" {
		aiName = "AI"
	}

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

	s.orch = ai.NewOrchestrator(persona, memStore, env.NumCtx, func(name, content string) {
		log.Printf("[AI] %s says: %s", name, content)
		s.signAndPublish(name, content, true)
	})

	s.batcher = ai.NewBatcher(func(batch []ai.ChatMessage) {
		s.orch.Process(batch)
	})

	// P2P → WebSocket relay
	go func() {
		for msg := range room.Messages {
			s.vc.Merge(msg.VectorClock)
			env := wsEnvelope{
				Type: "chat", ID: msg.ID, SenderID: msg.SenderID,
				SenderName: msg.SenderName, Content: msg.Content,
				IsAI: msg.IsAI, VectorClock: msg.VectorClock, Self: false,
			}
			data, _ := json.Marshal(env)
			s.hub.broadcast(data)

			s.batcher.Add(ai.ChatMessage{
				SenderName: msg.SenderName, Content: msg.Content, IsAI: msg.IsAI,
			})
		}
	}()

	// Peer count ticker
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				env := wsEnvelope{Type: "peers", Count: node.PeerCount()}
				data, _ := json.Marshal(env)
				s.hub.broadcast(data)
			case <-s.ctx.Done():
				return
			}
		}
	}()

	s.ready = true
	close(s.done)
	log.Printf("Room %q joined — ready to chat", roomCode)
	return nil
}

// ── main ──

func main() {
	port := flag.Int("port", 8080, "HTTP port")
	flag.Parse()

	state := &appState{
		ctx:  context.Background(),
		hub:  &hub{clients: make(map[*websocket.Conn]struct{})},
		done: make(chan struct{}),
	}

	// ── Ollama proxy APIs (work before setup) ──

	http.HandleFunc("/api/ollama/status", handleOllamaStatus)
	http.HandleFunc("/api/models", handleModels)
	http.HandleFunc("/api/models/pull", handleModelPull)
	http.HandleFunc("/api/models/recommended", handleRecommended)
	http.HandleFunc("/api/memory", state.handleMemory)

	// ── WebSocket ──

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("ws upgrade: %v", err)
			return
		}
		state.hub.add(conn)
		defer func() { state.hub.remove(conn); conn.Close() }()

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
					_ = conn.WriteMessage(websocket.TextMessage, errMsg)
					continue
				}
				ready, _ := json.Marshal(wsEnvelope{Type: "ready", PeerID: state.peerID})
				_ = conn.WriteMessage(websocket.TextMessage, ready)

			case "chat":
				<-state.done
				state.signAndPublish(env.SenderName, env.Content, false)
				state.batcher.Add(ai.ChatMessage{
					SenderName: env.SenderName, Content: env.Content, IsAI: false,
				})

			case "persona":
				<-state.done
				state.orch.SetPersona(ai.Persona{
					Name: env.AIName, Style: env.AIStyle, Model: env.AIModel,
				})

			case "force_reply":
				<-state.done
				state.batcher.ForceFlush()
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

func handleOllamaStatus(w http.ResponseWriter, r *http.Request) {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(ollamaBase + "/api/tags")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]any{"running": false, "error": err.Error()})
		return
	}
	resp.Body.Close()
	json.NewEncoder(w).Encode(map[string]any{"running": true})
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
		var body struct{ Name string `json:"name"` }
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
	var body struct{ Name string `json:"name"` }
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


package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
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

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

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
	AIName      string            `json:"ai_name,omitempty"`
	AIStyle     string            `json:"ai_style,omitempty"`
	AIModel     string            `json:"ai_model,omitempty"`
}

func main() {
	port := flag.Int("port", 8080, "HTTP port for the Web UI")
	flag.Parse()

	ctx := context.Background()

	identity, err := crypto.GenerateIdentity()
	if err != nil {
		log.Fatalf("identity: %v", err)
	}
	peerID := identity.PeerID.String()
	log.Printf("PeerID: %s", peerID)

	node, err := p2p.NewNode(ctx, identity.PrivKey)
	if err != nil {
		log.Fatalf("p2p node: %v", err)
	}

	room, err := p2p.JoinChatRoom(ctx, node.Host)
	if err != nil {
		log.Fatalf("chat room: %v", err)
	}

	h := &hub{clients: make(map[*websocket.Conn]struct{})}

	var (
		vc       = clock.New()
		sendMu   sync.Mutex
		prevHash string
	)

	// signAndPublish creates, signs, publishes, and echoes an AI or human message.
	signAndPublish := func(senderName, content string, isAI bool) {
		sendMu.Lock()
		vc.Increment(peerID)
		snap := vc.Snapshot()

		msg := types.NewMessage(peerID, senderName, content, snap, prevHash, isAI)
		sigData, err := msg.SignableBytes()
		if err != nil {
			sendMu.Unlock()
			log.Printf("signable: %v", err)
			return
		}
		sig, err := identity.Sign(sigData)
		if err != nil {
			sendMu.Unlock()
			log.Printf("sign: %v", err)
			return
		}
		msg.Signature = sig
		prevHash, _ = msg.Hash()
		sendMu.Unlock()

		if err := room.Publish(msg); err != nil {
			log.Printf("publish: %v", err)
			return
		}

		echo := wsEnvelope{
			Type: "chat", ID: msg.ID, SenderID: peerID,
			SenderName: senderName, Content: content,
			IsAI: isAI, VectorClock: snap, Self: true,
		}
		data, _ := json.Marshal(echo)
		h.broadcast(data)
	}

	// ── AI modules ──
	orchestrator := ai.NewOrchestrator(func(name, content string) {
		log.Printf("[AI] %s says: %s", name, content)
		signAndPublish(name, content, true)
	})

	batcher := ai.NewBatcher(func(batch []ai.ChatMessage) {
		orchestrator.Process(batch)
	})

	// Relay P2P → WebSocket + feed AI batcher
	go func() {
		for msg := range room.Messages {
			vc.Merge(msg.VectorClock)

			env := wsEnvelope{
				Type: "chat", ID: msg.ID, SenderID: msg.SenderID,
				SenderName: msg.SenderName, Content: msg.Content,
				IsAI: msg.IsAI, VectorClock: msg.VectorClock, Self: false,
			}
			data, _ := json.Marshal(env)
			h.broadcast(data)

			batcher.Add(ai.ChatMessage{
				SenderName: msg.SenderName,
				Content:    msg.Content,
				IsAI:       msg.IsAI,
			})
		}
	}()

	// Periodically push peer count
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				env := wsEnvelope{Type: "peers", Count: node.PeerCount()}
				data, _ := json.Marshal(env)
				h.broadcast(data)
			case <-ctx.Done():
				return
			}
		}
	}()

	// WebSocket endpoint
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("ws upgrade: %v", err)
			return
		}
		h.add(conn)
		defer func() { h.remove(conn); conn.Close() }()

		welcome, _ := json.Marshal(wsEnvelope{Type: "system", Content: "Connected. PeerID: " + peerID})
		_ = conn.WriteMessage(websocket.TextMessage, welcome)

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
			case "chat":
				signAndPublish(env.SenderName, env.Content, false)

				batcher.Add(ai.ChatMessage{
					SenderName: env.SenderName,
					Content:    env.Content,
					IsAI:       false,
				})

			case "persona":
				orchestrator.SetPersona(ai.Persona{
					Name:  env.AIName,
					Style: env.AIStyle,
					Model: env.AIModel,
				})
				log.Printf("[AI] Persona updated: name=%q style=%q model=%q",
					env.AIName, env.AIStyle, env.AIModel)

			case "force_reply":
				log.Println("[AI] Force reply requested")
				batcher.ForceFlush()
			}
		}
	})

	http.Handle("/", http.FileServer(http.FS(web.Content)))

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Web UI → http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

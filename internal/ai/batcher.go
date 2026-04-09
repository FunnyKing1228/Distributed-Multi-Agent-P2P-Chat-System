package ai

import (
	"sync"
	"time"
)

const (
	idleTimeout     = 5 * time.Second
	maxTimeout      = 30 * time.Second
	capacityTrigger = 20
)

// ChatMessage is a lightweight struct fed into the AI context window.
type ChatMessage struct {
	SenderName string
	Content    string
	IsAI       bool
}

// Batcher implements Hybrid Context Batching with three trigger conditions:
//  1. Idle Timer  (5 s)  — resets on every incoming message; fires when chat goes quiet.
//  2. Max Timer   (30 s) — starts on the first message in a batch; never resets.
//  3. Capacity    (20)   — fires immediately when the batch reaches 20 messages.
type Batcher struct {
	mu        sync.Mutex
	messages  []ChatMessage
	idleTimer *time.Timer
	maxTimer  *time.Timer
	onFlush   func([]ChatMessage)
}

func NewBatcher(onFlush func([]ChatMessage)) *Batcher {
	return &Batcher{onFlush: onFlush}
}

// Add appends a message and manages the dual timers.
func (b *Batcher) Add(msg ChatMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()

	first := len(b.messages) == 0
	b.messages = append(b.messages, msg)

	if first {
		b.maxTimer = time.AfterFunc(maxTimeout, b.timerFlush)
	}

	if b.idleTimer != nil {
		b.idleTimer.Stop()
	}
	b.idleTimer = time.AfterFunc(idleTimeout, b.timerFlush)

	if len(b.messages) >= capacityTrigger {
		b.doFlush()
	}
}

// ForceFlush is triggered by the "Force Reply" button — always fires, even if
// the batch is empty (the orchestrator will use its accumulated history).
func (b *Batcher) ForceFlush() {
	b.mu.Lock()
	batch := b.drain()
	b.mu.Unlock()
	go b.onFlush(batch)
}

func (b *Batcher) timerFlush() {
	b.mu.Lock()
	b.doFlush()
	b.mu.Unlock()
}

// doFlush must be called with b.mu held. No-ops on empty batch.
func (b *Batcher) doFlush() {
	if len(b.messages) == 0 {
		return
	}
	batch := b.drain()
	go b.onFlush(batch)
}

// drain stops timers, copies and clears the buffer. Must be called with b.mu held.
func (b *Batcher) drain() []ChatMessage {
	if b.idleTimer != nil {
		b.idleTimer.Stop()
		b.idleTimer = nil
	}
	if b.maxTimer != nil {
		b.maxTimer.Stop()
		b.maxTimer = nil
	}
	batch := make([]ChatMessage, len(b.messages))
	copy(batch, b.messages)
	b.messages = b.messages[:0]
	return batch
}

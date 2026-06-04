package ai

import (
	"sync"
	"time"
)

const (
	idleTimeout     = 3 * time.Second
	maxTimeout      = 20 * time.Second
	capacityTrigger = 15
)

// ChatMessage is a lightweight struct fed into the AI context window.
type ChatMessage struct {
	ID          string
	SenderID    string
	SenderName  string
	Content     string
	IsAI        bool
	Mentions    []string
	MentionAll  bool
	VectorClock map[string]uint64
}

// Batcher implements Hybrid Context Batching with three trigger conditions:
//  1. Idle Timer  (3 s)  — resets on every incoming message; fires when chat goes quiet.
//  2. Max Timer   (20 s) — starts on the first message in a batch; never resets.
//  3. Capacity    (15)   — fires immediately when the batch reaches 15 messages.
type Batcher struct {
	mu        sync.Mutex
	flushMu   sync.Mutex
	messages  []ChatMessage
	idleTimer *time.Timer
	maxTimer  *time.Timer
	onFlush   func([]ChatMessage)
	stopped   bool
}

func NewBatcher(onFlush func([]ChatMessage)) *Batcher {
	return &Batcher{onFlush: onFlush}
}

// Stop cancels all pending timers and prevents further flushes.
func (b *Batcher) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stopped = true
	if b.idleTimer != nil {
		b.idleTimer.Stop()
		b.idleTimer = nil
	}
	if b.maxTimer != nil {
		b.maxTimer.Stop()
		b.maxTimer = nil
	}
	b.messages = b.messages[:0]
}

// Add appends a message and manages the dual timers.
func (b *Batcher) Add(msg ChatMessage) {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return
	}

	first := len(b.messages) == 0
	b.messages = append(b.messages, msg)

	if first {
		b.maxTimer = time.AfterFunc(maxTimeout, b.timerFlush)
	}

	if b.idleTimer != nil {
		b.idleTimer.Stop()
	}
	b.idleTimer = time.AfterFunc(idleTimeout, b.timerFlush)

	var batch []ChatMessage
	if len(b.messages) >= capacityTrigger {
		batch = b.doFlushLocked()
	}
	b.mu.Unlock()
	if len(batch) > 0 {
		b.invokeFlush(batch)
	}
}

// ForceFlush is triggered by the "Force Reply" button — always fires, even if
// the batch is empty (the orchestrator will use its accumulated history).
func (b *Batcher) ForceFlush() {
	b.mu.Lock()
	batch := b.drain()
	b.mu.Unlock()
	b.invokeFlush(batch)
}

func (b *Batcher) timerFlush() {
	b.mu.Lock()
	batch := b.doFlushLocked()
	b.mu.Unlock()
	if len(batch) > 0 {
		b.invokeFlush(batch)
	}
}

// doFlushLocked must be called with b.mu held.
func (b *Batcher) doFlushLocked() []ChatMessage {
	if len(b.messages) == 0 {
		return nil
	}
	return b.drain()
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

func (b *Batcher) invokeFlush(batch []ChatMessage) {
	b.flushMu.Lock()
	defer b.flushMu.Unlock()
	b.onFlush(batch)
}

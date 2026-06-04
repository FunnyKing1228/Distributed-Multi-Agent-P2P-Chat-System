package chat

import (
	"sort"
	"strings"
	"sync"

	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/clock"
	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/types"
)

const DefaultMaxMessages = 200

type Stats struct {
	MessageCount int
	Verified     int
	Rejected     int
	Repaired     int
	Duplicate    int
	Equivocation int
	ClockBack    int
	SyncStatus   string
	LastReject   string
	VectorClock  map[string]uint64
}

type Ledger struct {
	mu               sync.RWMutex
	max              int
	ids              map[string]struct{}
	messages         []*types.Message
	senderClock      map[string]uint64
	prevHashBySender map[string]string
	verified         int
	rejected         int
	repaired         int
	duplicate        int
	equivocation     int
	clockBack        int
	lastReject       string
	syncStatus       string
}

func NewLedger(max int) *Ledger {
	if max <= 0 {
		max = DefaultMaxMessages
	}
	return &Ledger{
		max:              max,
		ids:              make(map[string]struct{}),
		senderClock:      make(map[string]uint64),
		prevHashBySender: make(map[string]string),
		syncStatus:       "idle",
	}
}

func (l *Ledger) Add(msg *types.Message) bool {
	ok, _ := l.AddWithReason(msg)
	return ok
}

func (l *Ledger) AddWithReason(msg *types.Message) (bool, string) {
	if msg == nil || msg.ID == "" {
		return false, "invalid_message"
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, ok := l.ids[msg.ID]; ok {
		l.duplicate++
		return false, "duplicate_message"
	}

	senderClock := msg.VectorClock[msg.SenderID]
	if senderClock > 0 {
		if prev, ok := l.senderClock[msg.SenderID]; ok && senderClock <= prev {
			l.clockBack++
			l.lastReject = "clock_regression"
			return false, "clock_regression"
		}
	}
	if msg.PrevHash != "" {
		if prevID, ok := l.prevHashBySender[msg.SenderID+"|"+msg.PrevHash]; ok && prevID != msg.ID {
			l.equivocation++
			l.lastReject = "equivocation_fork"
			return false, "equivocation_fork"
		}
	}

	l.ids[msg.ID] = struct{}{}
	if msg.PrevHash != "" {
		l.prevHashBySender[msg.SenderID+"|"+msg.PrevHash] = msg.ID
	}
	if senderClock > l.senderClock[msg.SenderID] {
		l.senderClock[msg.SenderID] = senderClock
	}
	l.messages = append(l.messages, cloneMessage(msg))
	sortMessages(l.messages)
	if len(l.messages) > l.max {
		remove := l.messages[:len(l.messages)-l.max]
		for _, old := range remove {
			delete(l.ids, old.ID)
		}
		l.messages = l.messages[len(l.messages)-l.max:]
	}
	l.verified++
	return true, ""
}

func (l *Ledger) Has(id string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	_, ok := l.ids[id]
	return ok
}

func (l *Ledger) IDs() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	ids := make([]string, 0, len(l.ids))
	for id := range l.ids {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (l *Ledger) Recent(limit int) []*types.Message {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if limit <= 0 || limit > len(l.messages) {
		limit = len(l.messages)
	}
	start := len(l.messages) - limit
	out := make([]*types.Message, 0, limit)
	for _, msg := range l.messages[start:] {
		out = append(out, cloneMessage(msg))
	}
	return out
}

func (l *Ledger) MissingFrom(known []string, limit int) []*types.Message {
	knownSet := make(map[string]struct{}, len(known))
	for _, id := range known {
		knownSet[id] = struct{}{}
	}
	recent := l.Recent(limit)
	out := make([]*types.Message, 0, len(recent))
	for _, msg := range recent {
		if _, ok := knownSet[msg.ID]; !ok {
			out = append(out, msg)
		}
	}
	return out
}

func (l *Ledger) MarkRejected() {
	l.mu.Lock()
	l.rejected++
	l.mu.Unlock()
}

func (l *Ledger) MarkRejectedReason(reason string) {
	if reason == "" {
		reason = "rejected"
	}
	l.mu.Lock()
	l.rejected++
	l.lastReject = reason
	l.mu.Unlock()
}

func (l *Ledger) AddRepaired(n int) {
	if n <= 0 {
		return
	}
	l.mu.Lock()
	l.repaired += n
	l.mu.Unlock()
}

func (l *Ledger) SetSyncStatus(status string) {
	if status == "" {
		status = "idle"
	}
	l.mu.Lock()
	l.syncStatus = status
	l.mu.Unlock()
}

func (l *Ledger) Stats() Stats {
	l.mu.RLock()
	defer l.mu.RUnlock()

	vc := make(map[string]uint64)
	for _, msg := range l.messages {
		for peer, value := range msg.VectorClock {
			if value > vc[peer] {
				vc[peer] = value
			}
		}
	}

	return Stats{
		MessageCount: len(l.messages),
		Verified:     l.verified,
		Rejected:     l.rejected,
		Repaired:     l.repaired,
		Duplicate:    l.duplicate,
		Equivocation: l.equivocation,
		ClockBack:    l.clockBack,
		SyncStatus:   l.syncStatus,
		LastReject:   l.lastReject,
		VectorClock:  vc,
	}
}

func cloneMessage(msg *types.Message) *types.Message {
	if msg == nil {
		return nil
	}
	cp := *msg
	cp.Mentions = append([]string(nil), msg.Mentions...)
	cp.VectorClock = make(map[string]uint64, len(msg.VectorClock))
	for k, v := range msg.VectorClock {
		cp.VectorClock[k] = v
	}
	return &cp
}

func sortMessages(msgs []*types.Message) {
	sort.SliceStable(msgs, func(i, j int) bool {
		return CompareMessages(msgs[i], msgs[j]) < 0
	})
}

func CompareMessages(a, b *types.Message) int {
	if a == nil || b == nil {
		return 0
	}
	switch clock.Compare(a.VectorClock, b.VectorClock) {
	case clock.Before:
		return -1
	case clock.After:
		return 1
	}
	if a.SenderID != b.SenderID {
		return strings.Compare(a.SenderID, b.SenderID)
	}
	if a.ID != b.ID {
		return strings.Compare(a.ID, b.ID)
	}
	if !strings.EqualFold(a.SenderName, b.SenderName) {
		return strings.Compare(strings.ToLower(a.SenderName), strings.ToLower(b.SenderName))
	}
	return strings.Compare(a.Content, b.Content)
}

package p2p

import (
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

const QuarantineThreshold = 3
const RateLimitPerSecond = 8.0
const RateLimitBurst = 12.0

type RejectEvent struct {
	PeerID string
	Reason string
}

type TrustStats struct {
	Rejected         int
	QuarantinedPeers int
	RateLimited      int
	LastRejectReason string
}

type TrustTracker struct {
	mu          sync.RWMutex
	invalid     map[string]int
	quarantine  map[string]struct{}
	rates       map[string]peerRate
	rateLimited int
	lastReason  string
}

type peerRate struct {
	Tokens float64
	Last   time.Time
}

func NewTrustTracker() *TrustTracker {
	return &TrustTracker{
		invalid:    make(map[string]int),
		quarantine: make(map[string]struct{}),
		rates:      make(map[string]peerRate),
	}
}

func (t *TrustTracker) RecordReject(pid, reason string) {
	if pid == "" {
		pid = "unknown"
	}
	if reason == "" {
		reason = "invalid"
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	t.invalid[pid]++
	if t.invalid[pid] >= QuarantineThreshold {
		t.quarantine[pid] = struct{}{}
	}
	t.lastReason = fmt.Sprintf("%s: %s", shortPeerID(pid), reason)
}

func (t *TrustTracker) IsQuarantined(pid string) bool {
	if pid == "" {
		return false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, ok := t.quarantine[pid]
	return ok
}

func (t *TrustTracker) Stats() TrustStats {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return TrustStats{
		Rejected:         totalInvalid(t.invalid),
		QuarantinedPeers: len(t.quarantine),
		RateLimited:      t.rateLimited,
		LastRejectReason: t.lastReason,
	}
}

func (t *TrustTracker) AllowRate(pid string, now time.Time) bool {
	if pid == "" {
		pid = "unknown"
	}
	if now.IsZero() {
		now = time.Now()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state, ok := t.rates[pid]
	if !ok || state.Last.IsZero() {
		state = peerRate{Tokens: RateLimitBurst, Last: now}
	} else {
		delta := now.Sub(state.Last).Seconds()
		if delta > 0 {
			state.Tokens += delta * RateLimitPerSecond
			if state.Tokens > RateLimitBurst {
				state.Tokens = RateLimitBurst
			}
		}
		state.Last = now
	}
	if state.Tokens < 1 {
		t.invalid[pid]++
		t.rateLimited++
		if t.invalid[pid] >= QuarantineThreshold {
			t.quarantine[pid] = struct{}{}
		}
		t.lastReason = fmt.Sprintf("%s: %s", shortPeerID(pid), "rate_limited")
		t.rates[pid] = state
		return false
	}
	state.Tokens -= 1
	t.rates[pid] = state
	return true
}

func totalInvalid(invalid map[string]int) int {
	total := 0
	for _, count := range invalid {
		total += count
	}
	return total
}

func peerIDString(pid peer.ID) string {
	if pid == "" {
		return "unknown"
	}
	return pid.String()
}

func shortPeerID(pid string) string {
	if len(pid) <= 8 {
		return pid
	}
	return pid[:8]
}

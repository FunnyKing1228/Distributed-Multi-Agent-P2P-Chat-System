package clock

import "sync"

// Ordering represents the causal relationship between two vector clocks.
type Ordering int

const (
	Before     Ordering = -1 // A happened-before B
	After      Ordering = 1  // A happened-after B
	Concurrent Ordering = 0  // A and B are causally independent
	Equal      Ordering = 2  // A and B are identical
)

// VectorClock is a thread-safe vector clock keyed by peer ID.
type VectorClock struct {
	mu    sync.RWMutex
	clock map[string]uint64
}

func New() *VectorClock {
	return &VectorClock{clock: make(map[string]uint64)}
}

// Increment bumps the local node's counter by one.
func (vc *VectorClock) Increment(nodeID string) {
	vc.mu.Lock()
	vc.clock[nodeID]++
	vc.mu.Unlock()
}

// Merge takes the element-wise max of the local and remote clocks.
func (vc *VectorClock) Merge(remote map[string]uint64) {
	vc.mu.Lock()
	for k, v := range remote {
		if v > vc.clock[k] {
			vc.clock[k] = v
		}
	}
	vc.mu.Unlock()
}

// Snapshot returns a deep copy of the current clock state.
func (vc *VectorClock) Snapshot() map[string]uint64 {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	snap := make(map[string]uint64, len(vc.clock))
	for k, v := range vc.clock {
		snap[k] = v
	}
	return snap
}

// Compare determines the causal ordering between two clock snapshots.
//
//	Before     → a happened-before b
//	After      → a happened-after  b
//	Concurrent → neither precedes the other
//	Equal      → identical clocks
func Compare(a, b map[string]uint64) Ordering {
	aLess, bLess := false, false

	check := func(m map[string]uint64) {
		for k := range m {
			va, vb := a[k], b[k]
			if va < vb {
				aLess = true
			}
			if vb < va {
				bLess = true
			}
		}
	}
	check(a)
	check(b)

	switch {
	case aLess && !bLess:
		return Before
	case bLess && !aLess:
		return After
	case !aLess && !bLess:
		return Equal
	default:
		return Concurrent
	}
}

// Sum returns the total of all counters — useful as a tiebreaker for concurrent events.
func Sum(vc map[string]uint64) uint64 {
	var s uint64
	for _, v := range vc {
		s += v
	}
	return s
}

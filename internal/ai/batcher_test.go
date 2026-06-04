package ai

import (
	"sync"
	"testing"
	"time"
)

func TestBatcherFlushCallbacksAreSerialized(t *testing.T) {
	var mu sync.Mutex
	inFlight := 0
	maxInFlight := 0

	b := NewBatcher(func(_ []ChatMessage) {
		mu.Lock()
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		mu.Unlock()

		time.Sleep(10 * time.Millisecond)

		mu.Lock()
		inFlight--
		mu.Unlock()
	})
	defer b.Stop()

	for i := 0; i < capacityTrigger*2; i++ {
		b.Add(ChatMessage{ID: string(rune('a' + i%26)), SenderName: "u", Content: "x"})
	}
	b.ForceFlush()

	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if maxInFlight > 1 {
		t.Fatalf("expected serialized flush callbacks, max concurrent = %d", maxInFlight)
	}
}

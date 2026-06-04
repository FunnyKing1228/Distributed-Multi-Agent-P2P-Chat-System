package chat

import (
	"strings"
	"testing"

	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/types"
)

func TestLedgerRejectsDuplicateIDs(t *testing.T) {
	ledger := NewLedger(10)
	msg := types.NewMessage("peer-a", "Alice", "hello", nil, false, map[string]uint64{"peer-a": 1}, "", false)

	if !ledger.Add(msg) {
		t.Fatal("first add should succeed")
	}
	if ledger.Add(msg) {
		t.Fatal("duplicate add should be ignored")
	}

	stats := ledger.Stats()
	if stats.MessageCount != 1 || stats.Verified != 1 || stats.Duplicate != 1 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
}

func TestLedgerRejectsClockRegression(t *testing.T) {
	ledger := NewLedger(10)
	msg1 := types.NewMessage("peer-a", "Alice", "first", nil, false, map[string]uint64{"peer-a": 5}, "h1", false)
	msg2 := types.NewMessage("peer-a", "Alice", "second", nil, false, map[string]uint64{"peer-a": 3}, "h2", false)

	if !ledger.Add(msg1) {
		t.Fatal("first add should succeed")
	}
	ok, reason := ledger.AddWithReason(msg2)
	if ok {
		t.Fatal("clock regression should be rejected")
	}
	if reason != "clock_regression" {
		t.Fatalf("unexpected reason: %s", reason)
	}
	stats := ledger.Stats()
	if stats.ClockBack != 1 || !strings.Contains(stats.LastReject, "clock_regression") {
		t.Fatalf("unexpected stats: %#v", stats)
	}
}

func TestLedgerRejectsEquivocationFork(t *testing.T) {
	ledger := NewLedger(10)
	msg1 := types.NewMessage("peer-a", "Alice", "branch A", nil, false, map[string]uint64{"peer-a": 1}, "same-prev", false)
	msg2 := types.NewMessage("peer-a", "Alice", "branch B", nil, false, map[string]uint64{"peer-a": 2}, "same-prev", false)

	if !ledger.Add(msg1) {
		t.Fatal("first add should succeed")
	}
	ok, reason := ledger.AddWithReason(msg2)
	if ok {
		t.Fatal("equivocation fork should be rejected")
	}
	if reason != "equivocation_fork" {
		t.Fatalf("unexpected reason: %s", reason)
	}
	stats := ledger.Stats()
	if stats.Equivocation != 1 || !strings.Contains(stats.LastReject, "equivocation_fork") {
		t.Fatalf("unexpected stats: %#v", stats)
	}
}

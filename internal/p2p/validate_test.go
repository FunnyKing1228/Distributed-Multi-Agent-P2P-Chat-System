package p2p

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	appcrypto "github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/crypto"
	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/types"
)

func signedTestMessage(t *testing.T) (*types.Message, *appcrypto.Identity) {
	t.Helper()
	id, err := appcrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	msg := types.NewMessage(
		id.PeerID.String(),
		"Alice",
		"hello",
		nil,
		false,
		map[string]uint64{id.PeerID.String(): 1},
		"",
		false,
	)
	data, err := msg.SignableBytes()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := id.Sign(data)
	if err != nil {
		t.Fatal(err)
	}
	msg.Signature = sig
	return msg, id
}

func TestValidateEnvelopeAcceptsSignedMessage(t *testing.T) {
	msg, _ := signedTestMessage(t)
	raw, _ := json.Marshal(msg)
	trust := NewTrustTracker()

	if err := validateEnvelopeData(raw, "", trust); err != nil {
		t.Fatalf("expected valid message, got %v", err)
	}
	if stats := trust.Stats(); stats.Rejected != 0 {
		t.Fatalf("unexpected rejected count: %d", stats.Rejected)
	}
}

func TestValidateEnvelopeRejectsBadSignature(t *testing.T) {
	msg, _ := signedTestMessage(t)
	msg.Content = "tampered after signing"
	raw, _ := json.Marshal(msg)
	trust := NewTrustTracker()

	if err := validateEnvelopeData(raw, "", trust); err == nil {
		t.Fatal("expected forged message to be rejected")
	}
	if stats := trust.Stats(); stats.Rejected != 1 || stats.LastRejectReason == "" {
		t.Fatalf("unexpected trust stats: %#v", stats)
	}
}

func TestValidateEnvelopeRejectsMissingSenderClock(t *testing.T) {
	msg, id := signedTestMessage(t)
	msg.VectorClock = map[string]uint64{"other": 1}
	data, _ := msg.SignableBytes()
	sig, _ := id.Sign(data)
	msg.Signature = sig
	raw, _ := json.Marshal(msg)
	trust := NewTrustTracker()

	if err := validateEnvelopeData(raw, "", trust); err == nil {
		t.Fatal("expected missing sender clock to be rejected")
	}
	if stats := trust.Stats(); !strings.Contains(stats.LastRejectReason, "missing_sender_clock") {
		t.Fatalf("unexpected reject reason: %#v", stats)
	}
}

func TestValidateEnvelopeRejectsMalformedPayload(t *testing.T) {
	trust := NewTrustTracker()

	if err := validateEnvelopeData([]byte("{not valid json"), "", trust); err == nil {
		t.Fatal("expected malformed payload to be rejected")
	}
	if stats := trust.Stats(); !strings.Contains(stats.LastRejectReason, "invalid_json") {
		t.Fatalf("unexpected reject reason: %#v", stats)
	}
}

func TestTrustTrackerQuarantinesRepeatedInvalidSender(t *testing.T) {
	msg, _ := signedTestMessage(t)
	trust := NewTrustTracker()
	for i := 0; i < QuarantineThreshold; i++ {
		forged := *msg
		forged.Content = "tampered"
		raw, _ := json.Marshal(&forged)
		_ = validateEnvelopeData(raw, "", trust)
	}

	if !trust.IsQuarantined(msg.SenderID) {
		t.Fatal("sender should be quarantined after repeated invalid messages")
	}
}

func TestValidateEnvelopeRejectsForgedSyncResponse(t *testing.T) {
	msg, id := signedTestMessage(t)
	msg.Content = "tampered in sync"
	ctrl := Control{
		Kind:       "sync_response",
		RequestID:  "req-1",
		FromPeerID: id.PeerID.String(),
		Messages:   []*types.Message{msg},
	}
	raw, _ := json.Marshal(ctrl)

	if err := validateEnvelopeData(raw, "", NewTrustTracker()); err == nil {
		t.Fatal("expected forged sync response to be rejected")
	}
}

func TestValidateEnvelopeRejectsSyncFromPeerMismatch(t *testing.T) {
	msg, id := signedTestMessage(t)
	ctrl := Control{
		Kind:       "sync_response",
		RequestID:  "req-2",
		FromPeerID: id.PeerID.String(),
		Messages:   []*types.Message{msg},
	}
	raw, _ := json.Marshal(ctrl)
	other, err := appcrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateEnvelopeData(raw, other.PeerID, NewTrustTracker()); err == nil {
		t.Fatal("expected forwarded peer mismatch to be rejected")
	}
}

func TestValidateEnvelopeRejectsSpoofedSyncRequest(t *testing.T) {
	real, err := appcrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	ctrl := Control{
		Kind:       "sync_request",
		RequestID:  "req-3",
		FromPeerID: "spoofed-peer-id",
	}
	raw, _ := json.Marshal(ctrl)
	trust := NewTrustTracker()

	if err := validateEnvelopeData(raw, real.PeerID, trust); err == nil {
		t.Fatal("expected spoofed sync request to be rejected")
	}
	if stats := trust.Stats(); !strings.Contains(stats.LastRejectReason, "from_peer_mismatch") {
		t.Fatalf("unexpected reject reason: %#v", stats)
	}
}

func TestTrustTrackerRateLimitBlocksFlood(t *testing.T) {
	msg, _ := signedTestMessage(t)
	raw, _ := json.Marshal(msg)
	trust := NewTrustTracker()

	blocked := false
	for i := 0; i < 40; i++ {
		err := validateEnvelopeData(raw, "", trust)
		if err != nil && strings.Contains(err.Error(), "rate_limited") {
			blocked = true
			break
		}
	}
	if !blocked {
		t.Fatal("expected flood to trigger rate_limited")
	}
	stats := trust.Stats()
	if stats.RateLimited == 0 || !strings.Contains(stats.LastRejectReason, "rate_limited") {
		t.Fatalf("unexpected trust stats: %#v", stats)
	}
}

func TestTrustTrackerRateLimitRecoversAfterCooldown(t *testing.T) {
	trust := NewTrustTracker()
	pid := "peer-rate-test"
	for i := 0; i < 20; i++ {
		_ = trust.AllowRate(pid, time.Now())
	}
	if trust.AllowRate(pid, time.Now()) {
		t.Fatal("expected immediate call to be rate limited")
	}
	if !trust.AllowRate(pid, time.Now().Add(2*time.Second)) {
		t.Fatal("expected rate limiter to recover after cooldown")
	}
}

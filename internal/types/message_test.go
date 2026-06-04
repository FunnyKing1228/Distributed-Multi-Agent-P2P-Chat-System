package types_test

import (
	"testing"

	appcrypto "github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/crypto"
	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/types"
	"github.com/libp2p/go-libp2p/core/peer"
)

func signedMessage(t *testing.T) (*types.Message, *appcrypto.Identity) {
	t.Helper()
	id, err := appcrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	msg := types.NewMessage(
		id.PeerID.String(),
		"Alice",
		"hello @Bob",
		[]string{"Bob"},
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

func verifyMessage(t *testing.T, msg *types.Message) bool {
	t.Helper()
	pid, err := peer.Decode(msg.SenderID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := msg.SignableBytes()
	if err != nil {
		t.Fatal(err)
	}
	ok, err := appcrypto.VerifySignature(pid, data, msg.Signature)
	if err != nil {
		return false
	}
	return ok
}

func TestSignatureRejectsTamperedFields(t *testing.T) {
	tests := []struct {
		name   string
		tamper func(*types.Message)
	}{
		{name: "content", tamper: func(m *types.Message) { m.Content = "forged" }},
		{name: "mentions", tamper: func(m *types.Message) { m.Mentions = []string{"Mallory"} }},
		{name: "mention_all", tamper: func(m *types.Message) { m.MentionAll = true }},
		{name: "vector_clock", tamper: func(m *types.Message) { m.VectorClock[m.SenderID] = 42 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, _ := signedMessage(t)
			if !verifyMessage(t, msg) {
				t.Fatal("signed message should verify before tampering")
			}
			tt.tamper(msg)
			if verifyMessage(t, msg) {
				t.Fatalf("tampered %s should not verify", tt.name)
			}
		})
	}
}

func TestSignatureRejectsSenderIDSwap(t *testing.T) {
	msg, _ := signedMessage(t)
	other, err := appcrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	msg.SenderID = other.PeerID.String()
	if verifyMessage(t, msg) {
		t.Fatal("message signed by original peer should not verify as another sender")
	}
}

func TestHashChainUsesSignableMessageBytes(t *testing.T) {
	msg1, id := signedMessage(t)
	hash1, err := msg1.Hash()
	if err != nil {
		t.Fatal(err)
	}

	msg2 := types.NewMessage(
		id.PeerID.String(),
		"Alice",
		"second message",
		nil,
		false,
		map[string]uint64{id.PeerID.String(): 2},
		hash1,
		false,
	)
	if msg2.PrevHash != hash1 {
		t.Fatalf("PrevHash = %q, want %q", msg2.PrevHash, hash1)
	}

	msg1.Content = "tampered"
	hashAfterTamper, err := msg1.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if hashAfterTamper == hash1 {
		t.Fatal("hash should change when signed message fields change")
	}
}

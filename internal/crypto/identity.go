package crypto

import (
	"encoding/hex"
	"fmt"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

type Identity struct {
	PrivKey libp2pcrypto.PrivKey
	PubKey  libp2pcrypto.PubKey
	PeerID  peer.ID
}

// GenerateIdentity creates a new Ed25519 key pair and derives the libp2p PeerID.
func GenerateIdentity() (*Identity, error) {
	priv, pub, err := libp2pcrypto.GenerateEd25519Key(nil)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 key: %w", err)
	}
	pid, err := peer.IDFromPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("derive peer id: %w", err)
	}
	return &Identity{PrivKey: priv, PubKey: pub, PeerID: pid}, nil
}

// Sign returns the hex-encoded Ed25519 signature of data.
func (id *Identity) Sign(data []byte) (string, error) {
	sig, err := id.PrivKey.Sign(data)
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}
	return hex.EncodeToString(sig), nil
}

// VerifySignature extracts the public key from senderID and verifies sigHex over data.
func VerifySignature(senderID peer.ID, data []byte, sigHex string) (bool, error) {
	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return false, fmt.Errorf("decode signature hex: %w", err)
	}
	pub, err := senderID.ExtractPublicKey()
	if err != nil {
		return false, fmt.Errorf("extract public key from %s: %w", senderID, err)
	}
	return pub.Verify(data, sig)
}

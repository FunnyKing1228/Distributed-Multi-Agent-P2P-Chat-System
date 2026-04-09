package types

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

type Message struct {
	ID          string            `json:"id"`
	SenderID    string            `json:"sender_id"`
	SenderName  string            `json:"sender_name"`
	Content     string            `json:"content"`
	VectorClock map[string]uint64 `json:"vector_clock"`
	PrevHash    string            `json:"prev_hash"`
	Signature   string            `json:"signature"`
	IsAI        bool              `json:"is_ai"`
}

func NewMessage(senderID, senderName, content string, vc map[string]uint64, prevHash string, isAI bool) *Message {
	vcCopy := make(map[string]uint64, len(vc))
	for k, v := range vc {
		vcCopy[k] = v
	}
	return &Message{
		ID:          uuid.New().String(),
		SenderID:    senderID,
		SenderName:  senderName,
		Content:     content,
		VectorClock: vcCopy,
		PrevHash:    prevHash,
		IsAI:        isAI,
	}
}

// SignableBytes returns deterministic JSON of all fields except Signature.
func (m *Message) SignableBytes() ([]byte, error) {
	tmp := struct {
		ID          string            `json:"id"`
		SenderID    string            `json:"sender_id"`
		SenderName  string            `json:"sender_name"`
		Content     string            `json:"content"`
		VectorClock map[string]uint64 `json:"vector_clock"`
		PrevHash    string            `json:"prev_hash"`
		IsAI        bool              `json:"is_ai"`
	}{
		ID:          m.ID,
		SenderID:    m.SenderID,
		SenderName:  m.SenderName,
		Content:     m.Content,
		VectorClock: m.VectorClock,
		PrevHash:    m.PrevHash,
		IsAI:        m.IsAI,
	}
	return json.Marshal(tmp)
}

// Hash returns the SHA-256 hex digest of the message (excluding Signature).
func (m *Message) Hash() (string, error) {
	data, err := m.SignableBytes()
	if err != nil {
		return "", fmt.Errorf("hash message: %w", err)
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}

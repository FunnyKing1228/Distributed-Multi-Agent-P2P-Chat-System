package p2p

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	appcrypto "github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/crypto"
	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/types"
)

const maxAppPayloadBytes = 64 * 1024

func validateEnvelopeData(data []byte, forwardedBy peer.ID, trust *TrustTracker) error {
	if len(data) == 0 {
		recordValidationReject(trust, forwardedBy, "", "empty_payload")
		return errors.New("empty payload")
	}
	if len(data) > maxAppPayloadBytes {
		recordValidationReject(trust, forwardedBy, "", "payload_too_large")
		return fmt.Errorf("payload too large: %d", len(data))
	}

	var peek struct {
		Kind string `json:"_kind"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		recordValidationReject(trust, forwardedBy, "", "invalid_json")
		return err
	}

	switch peek.Kind {
	case "activity":
		return nil
	case "sync_request":
		var ctrl Control
		if err := json.Unmarshal(data, &ctrl); err != nil {
			recordValidationReject(trust, forwardedBy, "", "bad_sync_request")
			return err
		}
		if ctrl.FromPeerID == "" || ctrl.RequestID == "" {
			recordValidationReject(trust, forwardedBy, ctrl.FromPeerID, "malformed_sync_request")
			return errors.New("malformed sync request")
		}
		if forwardedBy != "" && ctrl.FromPeerID != peerIDString(forwardedBy) {
			recordValidationReject(trust, forwardedBy, ctrl.FromPeerID, "from_peer_mismatch")
			return errors.New("sync request from_peer mismatch")
		}
		if trust != nil {
			ratePID := ctrl.FromPeerID
			if forwardedBy != "" {
				ratePID = peerIDString(forwardedBy)
			}
			if !trust.AllowRate(ratePID, time.Now()) {
				return errors.New("rate_limited")
			}
		}
		if trust != nil && trust.IsQuarantined(ctrl.FromPeerID) {
			recordValidationReject(trust, forwardedBy, ctrl.FromPeerID, "quarantined_peer")
			return errors.New("quarantined peer")
		}
		return nil
	case "sync_response":
		var ctrl Control
		if err := json.Unmarshal(data, &ctrl); err != nil {
			recordValidationReject(trust, forwardedBy, "", "bad_sync_response")
			return err
		}
		if ctrl.FromPeerID == "" || ctrl.RequestID == "" {
			recordValidationReject(trust, forwardedBy, ctrl.FromPeerID, "malformed_sync_response")
			return errors.New("malformed sync response")
		}
		if forwardedBy != "" && ctrl.FromPeerID != peerIDString(forwardedBy) {
			recordValidationReject(trust, forwardedBy, ctrl.FromPeerID, "from_peer_mismatch")
			return errors.New("sync response from_peer mismatch")
		}
		if trust != nil {
			ratePID := ctrl.FromPeerID
			if forwardedBy != "" {
				ratePID = peerIDString(forwardedBy)
			}
			if !trust.AllowRate(ratePID, time.Now()) {
				return errors.New("rate_limited")
			}
		}
		if trust != nil && trust.IsQuarantined(ctrl.FromPeerID) {
			recordValidationReject(trust, forwardedBy, ctrl.FromPeerID, "quarantined_peer")
			return errors.New("quarantined peer")
		}
		for _, msg := range ctrl.Messages {
			if err := ValidateSignedMessage(msg, trust, forwardedBy); err != nil {
				return fmt.Errorf("invalid sync message: %w", err)
			}
		}
		return nil
	default:
		var msg types.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			recordValidationReject(trust, forwardedBy, "", "bad_chat_message")
			return err
		}
		if trust != nil {
			ratePID := msg.SenderID
			if forwardedBy != "" {
				ratePID = peerIDString(forwardedBy)
			}
			if !trust.AllowRate(ratePID, time.Now()) {
				return errors.New("rate_limited")
			}
		}
		return ValidateSignedMessage(&msg, trust, forwardedBy)
	}
}

func ValidateSignedMessage(msg *types.Message, trust *TrustTracker, forwardedBy peer.ID) error {
	if msg == nil {
		recordValidationReject(trust, forwardedBy, "", "nil_message")
		return errors.New("nil message")
	}
	if msg.ID == "" || msg.SenderID == "" || msg.SenderName == "" {
		recordValidationReject(trust, forwardedBy, msg.SenderID, "missing_required_fields")
		return errors.New("missing required fields")
	}
	if len(msg.Content) > 8192 {
		recordValidationReject(trust, forwardedBy, msg.SenderID, "content_too_large")
		return errors.New("content too large")
	}
	if trust != nil && trust.IsQuarantined(msg.SenderID) {
		recordValidationReject(trust, forwardedBy, msg.SenderID, "quarantined_peer")
		return errors.New("quarantined peer")
	}
	if msg.VectorClock == nil || msg.VectorClock[msg.SenderID] == 0 {
		recordValidationReject(trust, forwardedBy, msg.SenderID, "missing_sender_clock")
		return errors.New("missing sender vector clock")
	}

	senderPID, err := peer.Decode(msg.SenderID)
	if err != nil {
		recordValidationReject(trust, forwardedBy, msg.SenderID, "bad_sender_id")
		return err
	}
	data, err := msg.SignableBytes()
	if err != nil {
		recordValidationReject(trust, forwardedBy, msg.SenderID, "signable_bytes_failed")
		return err
	}
	valid, err := appcrypto.VerifySignature(senderPID, data, msg.Signature)
	if err != nil || !valid {
		recordValidationReject(trust, forwardedBy, msg.SenderID, "bad_signature")
		if err != nil {
			return err
		}
		return errors.New("bad signature")
	}
	return nil
}

func recordValidationReject(trust *TrustTracker, forwardedBy peer.ID, senderID, reason string) {
	if trust == nil {
		return
	}
	pid := senderID
	if forwardedBy != "" {
		pid = peerIDString(forwardedBy)
	} else if pid == "" {
		pid = peerIDString(forwardedBy)
	}
	trust.RecordReject(pid, reason)
}

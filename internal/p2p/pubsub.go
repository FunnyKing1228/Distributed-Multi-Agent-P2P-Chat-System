package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	appcrypto "github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/crypto"
	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/types"
)

// ChatRoom wraps a GossipSub topic with signature-verified message delivery.
type ChatRoom struct {
	topic    *pubsub.Topic
	sub      *pubsub.Subscription
	selfID   string
	Messages chan *types.Message
	ctx      context.Context
}

// JoinChatRoom creates a GossipSub instance, joins a room-specific topic, and starts the read loop.
func JoinChatRoom(ctx context.Context, h host.Host, roomCode string) (*ChatRoom, error) {
	topicName := "dmapc-room-" + roomCode
	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		return nil, fmt.Errorf("create gossipsub: %w", err)
	}
	topic, err := ps.Join(topicName)
	if err != nil {
		return nil, fmt.Errorf("join topic %q: %w", topicName, err)
	}
	sub, err := topic.Subscribe()
	if err != nil {
		return nil, fmt.Errorf("subscribe to %q: %w", topicName, err)
	}
	log.Printf("[PubSub] Joined room %q (topic: %s)", roomCode, topicName)

	cr := &ChatRoom{
		topic:    topic,
		sub:      sub,
		selfID:   h.ID().String(),
		Messages: make(chan *types.Message, 128),
		ctx:      ctx,
	}
	go cr.readLoop()
	return cr, nil
}

// Publish serialises a Message and broadcasts it to the GossipSub topic.
func (cr *ChatRoom) Publish(msg *types.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	return cr.topic.Publish(cr.ctx, data)
}

func (cr *ChatRoom) readLoop() {
	for {
		envelope, err := cr.sub.Next(cr.ctx)
		if err != nil {
			if cr.ctx.Err() != nil {
				return
			}
			log.Printf("[PubSub] read error: %v", err)
			continue
		}
		if envelope.ReceivedFrom.String() == cr.selfID {
			continue
		}

		var msg types.Message
		if err := json.Unmarshal(envelope.Data, &msg); err != nil {
			log.Printf("[PubSub] invalid payload: %v", err)
			continue
		}

		// Verify Ed25519 signature — discard forged messages
		senderPID, err := peer.Decode(msg.SenderID)
		if err != nil {
			log.Printf("[PubSub] bad sender_id %q: %v", msg.SenderID, err)
			continue
		}
		data, err := msg.SignableBytes()
		if err != nil {
			log.Printf("[PubSub] signable bytes: %v", err)
			continue
		}
		valid, err := appcrypto.VerifySignature(senderPID, data, msg.Signature)
		if err != nil || !valid {
			log.Printf("[PubSub] forged message from %s — discarded", msg.SenderID)
			continue
		}

		cr.Messages <- &msg
	}
}

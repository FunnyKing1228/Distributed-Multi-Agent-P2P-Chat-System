package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"

	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/crypto"
	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/p2p"
	"github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/types"
)

func main() {
	ctx := context.Background()

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("Enter your name: ")
	scanner.Scan()
	name := scanner.Text()

	identity, err := crypto.GenerateIdentity()
	if err != nil {
		log.Fatalf("generate identity: %v", err)
	}
	log.Printf("PeerID: %s", identity.PeerID)

	node, err := p2p.NewNode(ctx, identity.PrivKey)
	if err != nil {
		log.Fatalf("create node: %v", err)
	}

	room, err := p2p.JoinChatRoom(ctx, node.Host)
	if err != nil {
		log.Fatalf("join chat room: %v", err)
	}

	go func() {
		for msg := range room.Messages {
			fmt.Printf("\n[%s] %s\n> ", msg.SenderName, msg.Content)
		}
	}()

	peerID := identity.PeerID.String()
	vc := make(map[string]uint64)
	prevHash := ""

	fmt.Println("Chat started. Type messages and press Enter. (Ctrl+C to quit)")
	fmt.Print("> ")
	for scanner.Scan() {
		text := scanner.Text()
		if text == "" {
			fmt.Print("> ")
			continue
		}

		vc[peerID]++
		msg := types.NewMessage(peerID, name, text, vc, prevHash, false)

		data, err := msg.SignableBytes()
		if err != nil {
			log.Printf("signable bytes: %v", err)
			continue
		}
		sig, err := identity.Sign(data)
		if err != nil {
			log.Printf("sign: %v", err)
			continue
		}
		msg.Signature = sig
		prevHash, _ = msg.Hash()

		if err := room.Publish(msg); err != nil {
			log.Printf("publish: %v", err)
		}
		fmt.Print("> ")
	}
}

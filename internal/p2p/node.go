package p2p

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/libp2p/go-libp2p"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
)

const mdnsServiceTag = "ntu-chat-room"

type Node struct {
	Host  host.Host
	ctx   context.Context
	peers map[peer.ID]struct{}
	mu    sync.RWMutex
}

// NewNode creates a libp2p host bound to a random TCP port and starts mDNS discovery.
func NewNode(ctx context.Context, privKey libp2pcrypto.PrivKey) (*Node, error) {
	h, err := libp2p.New(
		libp2p.Identity(privKey),
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
	)
	if err != nil {
		return nil, fmt.Errorf("create libp2p host: %w", err)
	}

	node := &Node{
		Host:  h,
		ctx:   ctx,
		peers: make(map[peer.ID]struct{}),
	}

	svc := mdns.NewMdnsService(h, mdnsServiceTag, node)
	if err := svc.Start(); err != nil {
		return nil, fmt.Errorf("start mDNS: %w", err)
	}

	log.Printf("[P2P] Listening on %v (PeerID: %s)", h.Addrs(), h.ID())
	return node, nil
}

// HandlePeerFound implements mdns.Notifee — called when a new LAN peer is discovered.
func (n *Node) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == n.Host.ID() {
		return
	}

	n.mu.Lock()
	_, exists := n.peers[pi.ID]
	n.peers[pi.ID] = struct{}{}
	n.mu.Unlock()

	if !exists {
		log.Printf("[P2P] Discovered peer: %s", pi.ID)
	}

	if err := n.Host.Connect(n.ctx, pi); err != nil {
		log.Printf("[P2P] Connect to %s failed: %v", pi.ID, err)
	}
}

// PeerCount returns the number of discovered peers.
func (n *Node) PeerCount() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.peers)
}

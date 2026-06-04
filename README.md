# Distributed Multi-Agent P2P Chat System

A decentralized peer-to-peer chat system built with Go, libp2p, GossipSub, vector clocks, Ed25519 signatures, and local Ollama personas.

The project is designed as a distributed systems demo: every user runs their own node, joins a room code, discovers LAN peers through mDNS, broadcasts signed messages over GossipSub, and optionally brings a local AI persona into the room.

## Features

- **No central chat server:** each desktop app is a peer.
- **LAN peer discovery:** libp2p mDNS discovers peers on the same network.
- **Room-based GossipSub:** room codes map to isolated pub/sub topics.
- **Logical time:** every chat message carries a vector clock.
- **Message integrity:** messages are signed with the sender's libp2p Ed25519 identity.
- **Early fake-message rejection:** GossipSub validates signed payloads before ledger/UI/AI processing.
- **Hash chaining:** each local sender links messages with `PrevHash`.
- **Eventual consistency:** late joiners request recent signed messages from existing peers.
- **Local AI personas:** each peer can run an Ollama-backed AI agent on-device.
- **Embedded web UI:** the Go binary serves the frontend directly.

## Project Structure

```text
cmd/             Application entry point and WebSocket/P2P glue
internal/ai/     Ollama persona orchestration and batching
internal/chat/   In-memory message ledger, dedupe, ordering, diagnostics
internal/clock/  Vector clock implementation
internal/crypto/ Ed25519 identity and signature helpers
internal/p2p/    libp2p node, GossipSub room, sync control messages
internal/types/  Signed chat message schema
web/             Embedded HTML/CSS/JS frontend
```

## Run Locally

Build the actual executable used by the embedded UI:

```bash
go build -o ./dmapc ./cmd/
```

Start two nodes on the same machine:

```bash
./dmapc -port 8080
./dmapc -port 8081
```

Open:

- http://localhost:8080
- http://localhost:8081

Use the same room code in both windows. Each node should show the other peer in the member list after mDNS discovery and join re-announcement.

## Build Portable Demo Packages

Create portable zip packages for demo machines:

```bash
chmod +x ./scripts/build-release.sh
./scripts/build-release.sh
```

The generated packages are placed in `dist/`:

```text
DMAPC-demo-macos-arm64.zip
DMAPC-demo-macos-amd64.zip
DMAPC-demo-windows-amd64.zip
```

Each package includes:

- the `dmapc` / `dmapc.exe` binary
- a platform launch script
- `QUICKSTART.md`

The launch scripts start the app with:

```text
Web UI port: 8080
libp2p port: 9000
```

For the best cross-device demo, install Tailscale and Ollama on each machine before running the package.

## LAN Demo

1. Connect both computers to the same LAN or Wi-Fi.
2. Run `./dmapc -port 8080` on each machine.
3. Open `http://localhost:8080` on each machine.
4. Enter the same room code.
5. Send messages from both machines.

## Tailscale / Manual Peer Demo

Use this when the two demo computers are not on the same LAN multicast domain, so mDNS discovery may not work.

1. Install and log in to Tailscale on both computers.
2. Confirm each computer can ping the other's Tailscale IP.
3. Start each node with a fixed libp2p port:

```bash
./dmapc -port 8080 -p2p-port 9000
```

4. Open `http://localhost:8080` on both computers and join the same room code.
5. In the sidebar, copy one node's **My multiaddr**. Prefer the address that starts with the Tailscale `100.x.x.x` IP:

```text
/ip4/100.x.x.x/tcp/9000/p2p/12D3...
```

6. Paste it into the other node's **Remote multiaddr** field and press **Connect Peer**.
7. After connection, send a chat message and run the core Attack Lab demos.

This is still a private overlay network, not full public Internet NAT traversal, but it is a much better distributed-systems demo than running two browser windows on the same machine. It proves two independent hosts can connect directly, exchange GossipSub messages, sync ledgers, and independently reject malicious packets.

The sidebar's **Distributed State** panel shows:

- `Messages`: number of messages in the local ledger.
- `Verified`: signed messages accepted into the ledger.
- `Rejected`: forged or invalid messages rejected by signature checks.
- `Repaired`: messages recovered through anti-entropy sync.
- `Duplicates`: replayed message IDs blocked by the ledger.
- `Sync`: current late-join sync state.
- `Clock`: compact vector clock summary.

## Distributed Systems Concepts

### Peer-to-Peer Networking

Each node creates a libp2p host with a generated Ed25519 identity. Peers are discovered with mDNS and connected directly. Chat rooms are implemented as GossipSub topics named from the room code.

### Logical Time

The system does not require physical clock synchronization. Computer wall clocks can be wrong or different; they are not used for consistency.

Before publishing a message, the sender increments its vector clock. Receivers merge the incoming clock into their local clock. This is the application's "time synchronization": peers exchange logical time inside every signed message.

UI rendering and AI history use deterministic ordering:

1. Vector clock happened-before relation.
2. Sender peer ID.
3. Message ID.
4. Sender name and content as final tie-breakers.

Concurrent messages may appear in a deterministic total order, but the vector clock still records that neither causally happened before the other.

### Message Integrity

Every message signs all behaviorally important fields except `Signature` itself:

- `id`
- `sender_id`
- `sender_name`
- `content`
- `mentions`
- `mention_all`
- `vector_clock`
- `prev_hash`
- `is_ai`

If any of these fields are modified, verification fails and the message is discarded.

### Fake Message Suppression

This project does not try to fact-check whether a human sentence is true. "Fake message" means a protocol-level lie: forged identity, modified signed data, replayed packets, malformed payloads, invalid logical time, or abusive sync control messages.

| Attack | Example | Defense | Demo Result |
| --- | --- | --- | --- |
| Content tampering | Modify `content` after signing | Ed25519 over signable fields | `bad_signature`, no UI/AI insertion |
| Identity spoofing | Claim another `sender_id` or `from_peer_id` | Signature binding and forwarded peer checks | `from_peer_mismatch` or `bad_signature` |
| Replay / duplicate | Re-send an old signed message | Ledger message ID dedupe | `Duplicates +1`, no duplicate render |
| Malformed payload | Send invalid JSON or missing fields | GossipSub topic validator | `invalid_json` / malformed reject |
| Sync abuse | Send forged `sync_response` history | Re-validate every synced message | rejected before ledger repair |
| Clock manipulation | Omit sender's vector-clock counter | vector-clock sanity check | `missing_sender_clock` |
| Clock regression | Send lower sender counter after higher one | Ledger monotonic sender-clock guard | `Clock Back +1`, blocked from ledger |
| Equivocation fork | Same sender reuses one `prev_hash` for two branches | Ledger hash-chain fork detection | `Equivocation +1`, second branch blocked |
| Flood / spam burst | One peer sends many packets rapidly | Per-peer token bucket + quarantine | `Rate Limited` / `Quarantined` increase |

The room registers a GossipSub topic validator. Invalid chat payloads are rejected before they enter the local ledger, UI, AI context, or sync response path. The validator checks:

- valid JSON payload shape
- valid `sender_id`
- valid Ed25519 signature
- required message fields
- sender's vector clock entry exists and is greater than zero
- payload size limits

Each peer also keeps a local trust tracker. Repeated invalid messages from the same sender place that sender in local quarantine. This is not a central ban list; every peer independently decides which senders to distrust.

The sidebar shows `Rejected`, `Duplicates`, `Quarantined`, and `Last Reject` so this behavior can be demonstrated in class.
It now also shows `Rate Limited`, `Equivocation`, and `Clock Back` to visualize protocol-abuse defenses.

### Attack Lab Demo

With two peers in the same room, use the sidebar **Attack Lab** panel:

1. Send a normal chat message first so `Verified` and `Messages` are non-zero.
2. Press **Tamper Signed Content**. The receiving peer should not show the forged text; `Rejected` increases and `Last Reject` reports `bad_signature`.
3. Press **Replay Last Valid Message**. The receiving peer should not render a duplicate; `Duplicates` increases.
4. Press **Spoof Sync Identity**. The receiving peer rejects the control message because `from_peer_id` does not match the actual forwarding peer.
5. Press **Malformed Payload**, **Forged Sync Response**, and **Missing Sender Clock** to show validation at the JSON, sync, and logical-time layers.
6. Press **Clock Regression**. The first packet sets a high watermark; the second lower counter is blocked (`Clock Back +1`).
7. Press **Equivocation Fork**. Two signed branches reuse the same `prev_hash`; the second branch is blocked (`Equivocation +1`).
8. Press **Flood Spam Burst**. A rapid burst from one forwarding peer gets throttled (`Rate Limited` and maybe `Quarantined` increase).

The **Security Trace** panel exposes the non-black-box validation path. For each packet, it shows:

- `[RECV]`: which peer forwarded the packet and its size.
- `[PARSE]`: whether it decoded as chat, sync, activity, or malformed JSON.
- `[HASH]`: the SHA-256 hash of the signable application payload.
- `[VERIFY]`: whether Ed25519 verification matched the sender's public key.
- `[LEDGER]`: replay, clock-regression, and equivocation checks after signature validation.
- `[DROP]` / `[RESULT]`: why a packet was blocked and whether it reached ledger/UI/AI history.

Suggested presentation line: fake messages are not only false sentences; in a P2P system they are any packet that violates identity, integrity, time, synchronization, or behavior rules. This app visualizes each honest peer's local validation path before packets can spread into chat history or AI context.

### Eventual Consistency

GossipSub is real-time and does not replay old room messages to late joiners. To handle that, each node keeps an in-memory ledger and sends a `sync_request` shortly after joining. Existing peers reply with recent signed messages that the requester is missing. The receiver verifies signatures again before inserting them into the ledger.

## Demo Checklist

- **Basic P2P chat:** open two nodes, join the same room, send messages both ways.
- **Causal ordering:** send messages from both peers and observe stable ordering across both UIs.
- **Late join repair:** send messages on node A, then join node B; node B should repair recent history and increment `Repaired`.
- **Signature security:** run `go test ./...`; tests mutate content, mentions, vector clocks, and sender IDs to prove forged messages fail verification.
- **Attack Lab:** with two peers in one room, run `Tamper Signed Content`, `Replay Last Valid Message`, and `Spoof Sync Identity`; the receiving peer should show the expected diagnostic changes without displaying forged chat content.
- **Byzantine behavior checks:** run `Clock Regression` and `Equivocation Fork` to show defenses against causality and hash-chain manipulation.
- **DoS resistance:** run `Flood Spam Burst` to show per-peer throttling and local quarantine.
- **AI as local edge compute:** enable one persona per node to show multi-human, multi-AI decentralized chat.

## Tests

```bash
go test ./...
```

Current tests cover vector clock ordering, signature tamper prevention, sender ID forgery prevention, hash-chain behavior, malformed payload rejection, sync spoofing, forged sync responses, missing sender clocks, replay dedupe, clock regression blocking, equivocation blocking, and rate-limit flood protection.

## License

MIT

# DMAPC Quick Start

DMAPC is a decentralized multi-agent P2P chat demo. Each computer runs one peer node and can bring a local AI persona into the room.

## 1. Prerequisites

For the most reliable cross-device demo, install these first:

- Tailscale: https://tailscale.com/download
- Ollama: https://ollama.com/download

Ollama is required only if you want local AI persona replies. Normal P2P chat and Attack Lab can still be demonstrated without an active model.

## 2. Start The App

### macOS

Double-click `run-mac.command`.

If macOS blocks the app because it is unsigned, right-click `run-mac.command`, choose **Open**, then confirm.

### Windows

Double-click `run-windows.bat`.

If Windows SmartScreen appears, choose **More info** and **Run anyway**.

The app opens at:

```text
http://localhost:8080
```

## 3. Join A Room

1. Open `http://localhost:8080`.
2. Enter your display name.
3. Enter the same room code on all demo computers.
4. Choose or create an AI persona.
5. Click **Join Room**.

## 4. Connect Over Tailscale

Use this when peers are on different networks or mDNS does not discover them automatically.

1. Make sure both computers are logged in to Tailscale.
2. On Computer A, copy **My multiaddr** from the **Tailscale / Manual Peer** section.
3. Prefer an address that starts with the Tailscale IP:

```text
/ip4/100.x.x.x/tcp/9000/p2p/12D3...
```

4. On Computer B, paste it into **Remote multiaddr** and click **Connect Peer**.
5. Send a chat message to verify the connection.

## 5. Recommended Demo Flow

1. Normal P2P chat between two computers.
2. Mention an AI persona with `@AIName` or `@all`.
3. Run **Tamper Signed Content** in Attack Lab.
4. Run **Replay Last Valid Message**.
5. Run **Flood Spam Burst**.
6. Show **Diagnostics** and **Security Trace**.

## Notes

- This package is portable; it does not install anything system-wide.
- Tailscale is used as a private overlay network for the demo, not as a full public Internet NAT traversal solution.
- The app uses port `8080` for the web UI and fixed libp2p port `9000` for manual peer connection.

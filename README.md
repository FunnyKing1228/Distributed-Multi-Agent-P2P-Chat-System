# Distributed Multi-Agent P2P Chat System

A decentralized peer-to-peer chat system built with **Go** and **libp2p**. No central server required.

Each node integrates a local AI model via **Ollama** for on-device inference, enabling intelligent agent capabilities directly within the chat network.

## Features

- **Fully Decentralized** — Peer discovery and messaging powered by libp2p with no central server.
- **Local AI Inference** — Each peer runs an Ollama-backed agent for private, on-device reasoning.
- **Web UI** — Lightweight HTML frontend served from every node.

## Project Structure

```
cmd/          # Application entry point (main.go)
internal/p2p/ # libp2p networking logic
web/          # HTML/CSS/JS frontend
```

## Getting Started

```bash
go run ./cmd
```

## License

MIT

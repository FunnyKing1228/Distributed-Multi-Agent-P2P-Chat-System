#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

echo "Starting DMAPC..."
echo "Web UI: http://localhost:8080"
echo "P2P port: 9000"
echo ""
echo "Keep this terminal window open while using the app."
echo ""

./dmapc -port 8080 -p2p-port 9000

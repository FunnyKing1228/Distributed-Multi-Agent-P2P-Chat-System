@echo off
cd /d "%~dp0"

echo Starting DMAPC...
echo Web UI: http://localhost:8080
echo P2P port: 9000
echo.
echo Keep this window open while using the app.
echo.

dmapc.exe -port 8080 -p2p-port 9000

pause

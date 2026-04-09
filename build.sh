#!/bin/bash
# ============================================================
# Build script – Go Multiplayer (Linux)
# Run from the project root directory.
# ============================================================
set -e

PROJECT_ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$PROJECT_ROOT"

echo "=== Compilation du serveur ==="
cd server
go mod tidy
go build -o ../game-server .
cd "$PROJECT_ROOT"
echo "    OK: game-server"

echo ""
echo "=== Compilation du client natif (Linux) ==="
go mod tidy
go build -o game-client ./client/
echo "    OK: game-client"

echo ""
echo "==================================================="
echo " Build termine!"
echo "==================================================="
echo ""
echo " 1) Lancez le serveur:"
echo "    ./game-server"
echo ""
echo " 2) Dans un autre terminal, lancez le client:"
echo "    ./game-client"
echo "    (option: ./game-client -server monserveur.com:8080)"
echo ""
echo " Pour la version web, lancez: ./build_web.sh"
echo "==================================================="

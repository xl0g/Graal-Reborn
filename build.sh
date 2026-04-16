#!/bin/bash
# ============================================================
# Build script – Go Multiplayer (Linux + WASM)
# Run from the project root directory.
# ============================================================
set -e

PROJECT_ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$PROJECT_ROOT"

echo "=== Building server ==="
cd server
go mod tidy
go build -o ../game-server .
cd "$PROJECT_ROOT"
echo "    OK: game-server"

echo ""
echo "=== Converting NW maps to TMX ==="
cd tools/nw2tmx
go run . -tsx ../classiciphone_pics4.tsx 2>&1 | grep -v "^$" || true
cd "$PROJECT_ROOT"
echo "    OK: maps/tmx/"

echo ""
echo "=== Building native client (Linux) ==="
go mod tidy
go build -o game-client ./client/
echo "    OK: game-client"

echo ""
echo "=== Building WASM client ==="
STATIC_DIR="server/static"
mkdir -p "$STATIC_DIR"
GOOS=js GOARCH=wasm go build -o "$STATIC_DIR/game.wasm" ./client/
echo "    OK: $STATIC_DIR/game.wasm"

WASM_EXEC="$(go env GOROOT)/lib/wasm/wasm_exec.js"
if [ ! -f "$WASM_EXEC" ]; then
    WASM_EXEC="$(go env GOROOT)/misc/wasm/wasm_exec.js"
fi
if [ -f "$WASM_EXEC" ]; then
    cp "$WASM_EXEC" "$STATIC_DIR/"
    echo "    OK: $STATIC_DIR/wasm_exec.js"
else
    echo "    WARNING: wasm_exec.js not found — web client may not work"
fi

echo ""
echo "==================================================="
echo " Build complete!"
echo "==================================================="
echo ""
echo " 1) Start the server:"
echo "    ./game-server"
echo ""
echo " 2) In another terminal, start the native client:"
echo "    ./game-client"
echo "    (or: ./game-client -server myserver.com:8080)"
echo ""
echo " Web client: http://localhost:8080"
echo "==================================================="

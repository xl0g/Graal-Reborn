#!/bin/bash
# ============================================================
# Build script – Go Multiplayer (WebAssembly only)
# Run from the project root directory.
# ============================================================
set -e

PROJECT_ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$PROJECT_ROOT"

STATIC_DIR="server/static"
mkdir -p "$STATIC_DIR"

echo "=== Building WASM client ==="
go mod tidy
GOOS=js GOARCH=wasm go build -o "$STATIC_DIR/game.wasm" ./client/
echo "    OK: $STATIC_DIR/game.wasm"

echo ""
echo "=== Copying wasm_exec.js ==="
WASM_EXEC="$(go env GOROOT)/lib/wasm/wasm_exec.js"
if [ ! -f "$WASM_EXEC" ]; then
    WASM_EXEC="$(go env GOROOT)/misc/wasm/wasm_exec.js"
fi
if [ -f "$WASM_EXEC" ]; then
    cp "$WASM_EXEC" "$STATIC_DIR/"
    echo "    OK: $STATIC_DIR/wasm_exec.js"
else
    echo "    ERROR: wasm_exec.js not found in $(go env GOROOT)"
    echo "    Try: find \$(go env GOROOT) -name 'wasm_exec.js'"
    exit 1
fi

echo ""
echo "==================================================="
echo " WASM build complete!"
echo "==================================================="
echo ""
echo " Start the server (if not already running):"
echo "    ./game-server"
echo ""
echo " Then open in your browser:"
echo "    http://localhost:8080"
echo "==================================================="

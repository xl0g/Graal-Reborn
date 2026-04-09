#!/bin/bash
# ============================================================
# Build script – Go Multiplayer (WebAssembly)
# Compile le client en WASM pour jouer dans un navigateur.
# Run from the project root directory.
# ============================================================
set -e

PROJECT_ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$PROJECT_ROOT"

STATIC_DIR="server/static"
mkdir -p "$STATIC_DIR"

echo "=== Compilation WASM ==="
go mod tidy
GOOS=js GOARCH=wasm go build -o "$STATIC_DIR/game.wasm" ./client/
echo "    OK: $STATIC_DIR/game.wasm"

echo ""
echo "=== Copie de wasm_exec.js ==="
WASM_EXEC="$(go env GOROOT)/lib/wasm/wasm_exec.js"
# Fallback for older Go versions
if [ ! -f "$WASM_EXEC" ]; then
    WASM_EXEC="$(go env GOROOT)/misc/wasm/wasm_exec.js"
fi
if [ -f "$WASM_EXEC" ]; then
    cp "$WASM_EXEC" "$STATIC_DIR/"
    echo "    OK: $STATIC_DIR/wasm_exec.js"
else
    echo "    ERREUR: wasm_exec.js introuvable dans $(go env GOROOT)/misc/wasm/"
    echo "    Essayez: find \$(go env GOROOT) -name 'wasm_exec.js'"
    exit 1
fi

echo ""
echo "==================================================="
echo " Build WASM termine!"
echo "==================================================="
echo ""
echo " Demarrez le serveur (si pas encore fait):"
echo "    ./game-server"
echo ""
echo " Puis ouvrez dans votre navigateur:"
echo "    http://localhost:8080"
echo "==================================================="

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build Commands

```bash
# Build server + native Linux client
./build.sh
# Outputs: ./game-server, ./game-client

# Build WASM client (placed in server/static/)
./build_web.sh

# Run server (creates game.db automatically)
./game-server
PORT=9090 DB_PATH=/tmp/game.db ./game-server

# Run native client
./game-client
./game-client -server myserver.com:8080
```

The client and server are **separate Go modules** (`go.mod` at root for client, `server/go.mod` for server). Run `go mod tidy` in each directory independently.

## Architecture

This is a real-time multiplayer game with two separate Go programs sharing a JSON-over-WebSocket protocol.

### Client (root package `darkzone/MultiTest`)

Built with [Ebiten](https://ebitengine.org/). Implements `ebiten.Game` in `game.go` as a **state machine** with states `StateMainMenu`, `StateLogin`, `StateRegister`, `StatePlaying`.

Key flow:
1. `main.go` → loads assets, creates `Game`, hands off to Ebiten's loop (60 TPS)
2. `Game.Update()` dispatches to the active state; in `StatePlaying`, calls `updatePlaying()` which drains the network channel then processes input
3. After login/register, `startGame()` spawns a goroutine that dials the WebSocket; auth token is sent as the first message
4. Network messages arrive via `Connection.TryReceive()` (non-blocking channel poll) and are processed in `handleServerMsg()`

**Platform split:** `network_native.go` (`!js` build tag) uses gorilla/websocket with goroutines; `network_js.go` (WASM) uses `syscall/js` browser WebSocket API. Both produce the same `*Connection` interface defined in `network.go`.

**Client-side interpolation:** Remote entities (`Character.TargetX/Y`) are set from server state; display position (`Character.X/Y`) exponentially decays toward target each frame (`interpK = 20.0`). The local player skips interpolation and moves immediately.

**Cosmetics:** Body/head/hat images are loaded asynchronously in goroutines when filenames change (`SetCosmetics` → sets `cosDirty` → goroutine loads from `Assets/offline/levels/`). Cosmetic state is protected by `Character.cosmu` mutex.

Sprite sheet layout: body frames are 32×32 px, arranged as `[col=direction][row=animFrame]`; head frames are 32×32 px per direction; hat frames are 48×48 px per direction.

### Server (`server/` package `darkzone/MultiTestServer`)

Single-file server (`server/server.go`) combining:
- **SQLite** (via `modernc.org/sqlite`, CGo-free) — tables: `users`, `sessions`, `chat_history`
- **HTTP REST** — `POST /api/register`, `POST /api/login` return a session token
- **WebSocket hub** — `Hub` manages connected `Client`s under an `sync.RWMutex`
- **Game loop** — `Hub.runGameLoop()` ticks at 60 Hz, updates NPC AI, broadcasts full world state to all clients
- **NPC AI** — 7 hard-coded NPCs wander randomly within a radius around their home position

WebSocket lifecycle: first message must be `{"type":"auth","token":"..."}` within 10 s, then the client is registered in the hub. A writer goroutine drains `client.send` and sends WebSocket pings every 54 s. Read deadline resets on each message or pong.

The server also serves `server/static/` as the web root (WASM client after `./build_web.sh`).

### WebSocket Protocol (JSON)

| Direction | `type` | Key fields |
|-----------|--------|------------|
| C → S | `auth` | `token` |
| C → S | `move` | `x`, `y`, `dir`, `moving` |
| C → S | `chat` | `msg` |
| C → S | `cosmetic` | `body`, `head`, `hat` (filenames) |
| S → C | `auth_ok` | `id`, `name`, `x`, `y` |
| S → C | `auth_error` | `msg` |
| S → C | `state` | `players[]`, `npcs[]` (full world, 60 Hz) |
| S → C | `chat` | `from`, `msg` |
| S → C | `system` | `msg` |

### Gralat Currency System

`GralatPickup` is defined in `types.go` (client) and inline in `server/server.go`. The server owns the authoritative list of world pickups, broadcasting them inside every `state` message. The client auto-collects on overlap and sends `collect_gralat`; the server validates, removes, credits the DB, and sends back `gralat_update`. NPCs give gralats via `talk_npc` → `npc_dialog` (120 s cooldown per NPC per player). Gralats persist in the `users.gralats` DB column. Each player's count is included in `PlayerState` and displayed in the HUD (top-centre) and the profile panel (`P` key). Other players can see the count in name tags.

Sprite sheet `Assets/offline/levels/images/downloads/gralats.png` is 64×64 (2×2 grid of 32×32): top-left=1, top-right=5, bottom-left=30, bottom-right=100. Loading and sprite selection is in `gralat.go`.

### TMX Map System

`tilemap.go` parses `test2.tmx` (loaded in `NewGame()`). Tile IDs reference the tileset `Assets/offline/levels/tiles/classiciphone_pics4.png` (2048×512, 64 columns, 32×32 tiles, 1-based GID). Layer properties drive behavior:
- Layer property `collision=true` → non-zero tiles become solid (checked in `GameMap.IsBlocked()`)
- Layer property `panneau="<text>"` → non-zero tiles become readable signs (F key when adjacent)

`handleMovement()` in `game.go` splits X/Y movement and calls `IsBlocked()` independently for wall-sliding. World size is now 30×20 tiles × 32 px = **960×640**.

### Controls (in-game)

| Key | Action |
|-----|--------|
| `ZQSD` / Arrows | Move (with tile collision) |
| `F` | Read adjacent sign / talk to nearby NPC / close dialog |
| `P` | Toggle profile panel (gralat count) |
| `T` | Open chat |
| `C` | Open cosmetic picker |
| `Esc` | Close dialog / close profile / back to menu |

### Key Types

- `types.go` — `PlayerState`, `NPCState`, `ServerMessage` (client-side deserialization)
- `character.go` — `Character` (both local and remote entities, rendering + interpolation)
- `ui.go` — primitive drawing helpers (`DrawRect`, `DrawText`) and reusable widgets
- `menu.go` — login/register menus; perform HTTP calls to `/api/login` and `/api/register`
- `chat.go` — in-game chat overlay
- `cosmetic.go` — cosmetic picker menu (`CosmeticMenu`)

### Asset Paths

- `assets/character.png`, `assets/head.png`, `assets/tiles.png` — default sprites (missing files are tolerated)
- `Assets/offline/levels/bodies/`, `heads/`, `hats/` — cosmetic sprites loaded at runtime

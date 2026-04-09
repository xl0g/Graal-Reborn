# Go Multiplayer Template

A real-time multiplayer game written in Go with Ebiten. Runs natively on Linux and in the browser (WebAssembly).

---

## Features

- Account registration and login with persistent storage (SQLite)
- Real-time multiplayer — positions synchronized at 60 Hz
- Client-side interpolation — smooth movement without teleportation
- In-game chat broadcast to all connected players
- 10 NPCs (villagers, merchants, guards, horses) with wandering AI
- Sword combat system — hit NPCs, collect gralats
- Horse riding — mount and dismount horses
- Gani animation system (idle, walk, sword, ride, sit, push, dead)
- TMX tile map with collision and interactive signs
- Gralat currency — collect coins, earn rewards from NPCs
- Full player profile panel with character preview and playtime tracking
- Zelda-style UI — star background, gold-bordered panels
- Cosmetic picker — body, head, and hat customization
- Linux native and browser (WASM) support

---

## Quick Start

### 1. Build

```bash
./build.sh
```

Produces `game-server` and `game-client`.

### 2. Start the server

```bash
./game-server
```

The database `game.db` is created automatically.

### 3. Run the client

```bash
./game-client

# Remote server:
./game-client -server myserver.com:8080
```

---

## Web Version (WASM)

```bash
./build_web.sh   # compiles game.wasm and copies wasm_exec.js
./game-server    # also serves the web client on the same port
```

Open **http://localhost:8080** in your browser.

---

## Controls

| Key | Action |
|-----|--------|
| `WASD` / Arrows | Move |
| `X` | Sword swing |
| `R` | Mount / dismount horse |
| `F` | Interact with sign or NPC / close dialog |
| `T` | Open chat |
| `C` | Open cosmetic picker |
| `P` | Toggle own profile panel |
| `Click` player | Open that player's profile |
| `Enter` | Send message / confirm form |
| `Tab` | Switch field in menus |
| `Esc` | Close overlay / back to menu |

---

## Player Profile

Clicking on another player opens a full profile panel showing:

- **Name** — their account username
- **Fortune** — total gralats accumulated
- **Playtime** — total time played (live, updated each second)
- **Status** — Online badge
- **Character preview** — their cosmetic appearance rendered at 2.5× scale on the right side of the panel

Press **P** to open your own profile with the same information.

---

## Architecture

```
.
├── client/
│   ├── main.go              Client entry point
│   ├── game.go              Game loop, state machine
│   ├── character.go         Entity rendering, interpolation, DrawPreview
│   ├── chat.go              Chat overlay
│   ├── menu.go              Menus (main, login, register)
│   ├── cosmetic.go          Cosmetic picker
│   ├── ui.go                Widgets (TextInput, Button, DrawPanel…)
│   ├── gani.go              Gani animation system
│   ├── tilemap.go           TMX map parser and renderer
│   ├── gralat.go            Gralat pickup rendering
│   ├── network.go           WebSocket — shared interface
│   ├── network_native.go    Native WebSocket (gorilla, !js)
│   ├── network_js.go        WASM WebSocket (syscall/js)
│   └── types.go             JSON message types
└── server/
    ├── main.go              Server entry point
    ├── db.go                SQLite layer (users, sessions, playtime)
    ├── hub.go               Hub (connection manager + game loop)
    ├── client.go            WebSocket client handler
    ├── handlers.go          HTTP REST (register, login)
    ├── npc.go               NPC AI and dialog definitions
    ├── gralat.go            Gralat spawn definitions
    ├── collision.go         Tile collision map (TMX)
    ├── types.go             Shared data types
    └── static/              Web files (index.html, game.wasm…)
```

### WebSocket Protocol (JSON)

| Direction | Type | Key Fields |
|-----------|------|------------|
| C → S | `auth` | `token` |
| C → S | `move` | `x`, `y`, `dir`, `moving` |
| C → S | `chat` | `msg` |
| C → S | `cosmetic` | `body`, `head`, `hat` |
| C → S | `anim_state` | `anim`, `mounted` |
| C → S | `sword_hit` | `npc_id` |
| C → S | `talk_npc` | `npc_id` |
| C → S | `mount_npc` | `npc_id` |
| C → S | `dismount` | — |
| C → S | `collect_gralat` | `gralat_id` |
| S → C | `auth_ok` | `id`, `name`, `x`, `y`, `gralat_n`, `playtime` |
| S → C | `auth_error` | `msg` |
| S → C | `state` | `players[]`, `npcs[]`, `gralats[]` (60 Hz) |
| S → C | `chat` | `from`, `msg` |
| S → C | `system` | `msg` |
| S → C | `gralat_update` | `gralat_n` |
| S → C | `npc_dialog` | `msg`, `gralat_n` |
| S → C | `npc_damage` | `npc_id`, `hp`, `killed` |
| S → C | `mount_ok` | `npc_id` |
| S → C | `dismount_ok` | — |

---

## Server Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Listening port |
| `DB_PATH` | `game.db` | SQLite database path |

---

## Database Schema

| Table | Key Columns |
|-------|-------------|
| `users` | `id`, `username`, `password_hash`, `gralats`, `playtime`, `last_x`, `last_y` |
| `sessions` | `token`, `user_id`, `username` |
| `chat_history` | `username`, `message`, `sent_at` |

Playtime is accumulated in seconds. Each session's elapsed time is added to the total when the player disconnects.

---

## Dependencies

**Server** — `github.com/gorilla/websocket` · `modernc.org/sqlite` · `golang.org/x/crypto`

**Client** — `github.com/hajimehoshi/ebiten/v2` · `github.com/gorilla/websocket`

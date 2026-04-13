
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

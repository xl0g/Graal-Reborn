# Graal Reborn — Lua Resources

Each subdirectory inside `resources/` is an independent Lua resource.

## Resource structure

```
resources/
  my_resource/
    server.lua          ← loaded automatically if present
    __resource.lua      ← optional manifest (for multiple scripts)
```

### Optional manifest (`__resource.lua`)

```lua
server_scripts {
    'utils.lua',
    'server.lua',
}
```

Without a manifest the system looks for `server.lua` first, then all `*.lua` files in the folder (sorted).

## Admin commands (in-game chat)

```
/resources            — list running resources
/start  <name>        — start a resource
/stop   <name>        — stop a resource and remove its NPCs
/restart <name>        — hot-reload a resource
```

---

## Lua API

```
┌──────────────────────────────────────────────────┬────────────────────────────────────┐
│ Function                                         │ Description                        │
├──────────────────────────────────────────────────┼────────────────────────────────────┤
│ CreateNPC(name, x, y, type, dialog, gMin, gMax)  │ Spawn an NPC, returns its ID       │
│ DeleteNPC(id)                                    │ Remove an NPC                      │
│ SetNPCPosition(id, x, y)                         │ Teleport an NPC                    │
│ SetNPCDialog(id, msg, gMin, gMax)                │ Set a custom NPC dialog            │
│ GetNPCPosition(id) → x, y                        │ Get an NPC's position              │
├──────────────────────────────────────────────────┼────────────────────────────────────┤
│ GetPlayers() → table                             │ List of {id, name, x, y, gralats}  │
│ GetPlayerName(id)                                │ Get a player's name                │
│ GetPlayerPos(id) → x, y                          │ Get a player's position            │
│ GiveGralats(id, n)                               │ Give gralats to a player           │
│ TakeGralats(id, n) → bool                        │ Take gralats (false if not enough) │
├──────────────────────────────────────────────────┼────────────────────────────────────┤
│ SendMessage(id, msg)                             │ System message to one player       │
│ BroadcastMessage(msg)                            │ System message to all players      │
│ BroadcastChat(from, msg)                         │ Chat message attributed to from    │
├──────────────────────────────────────────────────┼────────────────────────────────────┤
│ SetTimeout(ms, fn) → id                          │ Run fn once after ms milliseconds  │
│ SetInterval(ms, fn) → id                         │ Run fn every ms milliseconds       │
│ ClearTimer(id)                                   │ Cancel a timer                     │
├──────────────────────────────────────────────────┼────────────────────────────────────┤
│ AddEventHandler(event, fn)                       │ Listen for an event                │
│ TriggerEvent(event, ...)                         │ Fire an event to all resources     │
├──────────────────────────────────────────────────┼────────────────────────────────────┤
│ GetResourceName() → string                       │ Name of the current resource       │
│ RandomInt(min, max) → int                        │ Inclusive random integer           │
│ print(...)                                       │ Log prefixed with [LUA:name]       │
└──────────────────────────────────────────────────┴────────────────────────────────────┘
```

## Built-in events

```
onServerStart                          — resource loaded and ready
onResourceStop                         — resource is being stopped
onPlayerConnect(playerId, name)        — player connected
onPlayerDisconnect(playerId, name)     — player disconnected
onPlayerChat(playerId, name, msg)      — player sent a chat message
```

---

## Minimal example

```lua
-- resources/hello/server.lua

AddEventHandler("onPlayerConnect", function(playerId, name)
    SetTimeout(1000, function()
        SendMessage(playerId, "Welcome " .. name .. "!")
    end)
end)

AddEventHandler("onPlayerChat", function(playerId, name, msg)
    if msg == "!pos" then
        local x, y = GetPlayerPos(playerId)
        SendMessage(playerId, string.format("Position: %.0f, %.0f", x, y))
    end
end)

local guard = CreateNPC("Guard", 300, 200, 2, "Welcome to the village!", 1, 3)

SetInterval(60 * 1000, function()
    BroadcastMessage("Server is running fine.")
end)
```

## NPC types

| Value | Type     |
|-------|----------|
| 0     | Villager |
| 1     | Merchant |
| 2     | Guard    |
| 3     | Traveler |
| 4     | Farmer   |
| 5     | Horse    |

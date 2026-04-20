package main

import (
	"darkzone/MultiTestServer/internal/db"
	"encoding/json"
	"fmt"
	"log"
	"math"
	mrand "math/rand"
	"sync"
	"time"
)

// Hub manages all connected clients and drives the server-side game loop.
type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]bool
	npcs    []*NPC

	// World collision — either a CollisionMap (TMX) or a GMapWorld (GMAP).
	worldColl        WorldCollider
	worldW, worldH   float64 // cached from worldColl.Bounds(); never changes after init

	// World gralat pickups
	gralats     []*GralatPickup
	gralatTimer map[string]time.Time // id → scheduled respawn time

	// Admin-spawned world items (persisted in world_items table)
	worldItems []*WorldSpawnItem
}

var globalHub *Hub

func newHub() *Hub {
	h := &Hub{
		clients:     make(map[*Client]bool),
		gralatTimer: make(map[string]time.Time),
	}

	// Load world collision from the same spawn map as the client (config.json → spawnMap).
	h.worldColl = loadWorldCollider("config.json")

	npcDefs := []struct {
		name    string
		x, y    float64
		npcType int
	}{
		// Regular NPCs
		{"Thibaut the Villager", 300, 200, NPCTypeVillager},
		{"Marceline the Merchant", 600, 280, NPCTypeMerchant},
		{"Eleanor the Traveller", 420, 480, NPCTypeTraveler},
		{"Baptiste the Farmer", 680, 520, NPCTypeFarmer},
		// Passive animals (flee from players)
		{"Lapin", 350, 350, NPCTypePassive},
		{"Biche", 750, 400, NPCTypePassive},
		{"Poulet", 500, 600, NPCTypePassive},
		// Aggressive monsters
		{"Slime Rouge", 200, 700, NPCTypeAggressive},
		{"Slime Vert", 800, 650, NPCTypeAggressive},
		{"Gobelin", 550, 800, NPCTypeAggressive},
		{"Bat", 900, 300, NPCTypeAggressive},
	}

	if h.worldColl != nil {
		h.worldW, h.worldH = h.worldColl.Bounds()
	} else {
		h.worldW, h.worldH = mapWidth, mapHeight
	}

	for i, def := range npcDefs {
		x, y := def.x, def.y
		if h.worldColl != nil && !h.worldColl.IsFreePoint(x+8, y+8) {
			x, y = findFreePos(h.worldColl, x, y, h.worldW, h.worldH)
		}
		n := newNPC(fmt.Sprintf("npc_%d", i), def.name, x, y, def.npcType)
		n.worldW, n.worldH = h.worldW, h.worldH
		h.npcs = append(h.npcs, n)
	}

	// Load persisted world items from DB.
	for _, w := range db.LoadWorldItems() {
		h.worldItems = append(h.worldItems, &WorldSpawnItem{
			ID: w.ID, Name: w.Name, SpritePath: w.SpritePath,
			X: w.X, Y: w.Y, Price: w.Price, ItemID: w.ItemID,
		})
	}

	for i := range gralatSpawnDefs {
		d := gralatSpawnDefs[i]
		x, y := d.x, d.y
		if h.worldColl != nil && !h.worldColl.IsFreePoint(x+8, y+8) {
			x, y = findFreePos(h.worldColl, x, y, h.worldW, h.worldH)
		}
		h.gralats = append(h.gralats, &GralatPickup{
			ID: d.id, X: x, Y: y, Value: d.value,
		})
	}

	return h
}

// register adds a client to the hub.
func (h *Hub) register(c *Client) {
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
	h.broadcastSystem(fmt.Sprintf("%s joined the world!", c.name))
	log.Printf("[HUB] %s connected (ID: %s)", c.name, c.playerID)
}

// unregister removes a client, frees any mount, saves position and playtime.
func (h *Hub) unregister(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	// Free any horse this player was riding
	for _, n := range h.npcs {
		if n.mountedBy == c.playerID {
			n.mountedBy = ""
			n.state.MountedBy = ""
			break
		}
	}
	h.mu.Unlock()
	elapsed := int(time.Since(c.sessionStart).Seconds())
	db.UpdatePosition(c.userID, c.state.X, c.state.Y)
	db.AddPlaytime(c.userID, elapsed)
	h.broadcastSystem(fmt.Sprintf("%s left the world.", c.name))
	log.Printf("[HUB] %s disconnected (session: %ds)", c.name, elapsed)
}

func (h *Hub) broadcastRaw(data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		select {
		case c.send <- data:
		default:
		}
	}
}

func (h *Hub) broadcast(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.broadcastRaw(data)
}

func (h *Hub) broadcastSystem(msg string) {
	h.broadcast(map[string]string{"type": "system", "msg": msg})
}

// sendPerClientState sends each client a state snapshot filtered by:
//  1. Same map as the receiver.
//  2. Within viewRadius world-px of the receiver (interest management).
//
// A tempGrid is rebuilt each tick — O(n) to construct, O(1) per cell lookup.
func (h *Hub) sendPerClientState() {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Build a flat snapshot of every online player.
	type playerSnap struct {
		state PlayerState
		mapID string
	}
	snaps := make([]playerSnap, 0, len(h.clients))
	for c := range h.clients {
		ps := c.state
		ps.Playtime = c.savedPlaytime + int(time.Since(c.sessionStart).Seconds())
		m := c.currentMap
		if m == "" {
			m = defaultMap
		}
		snaps = append(snaps, playerSnap{ps, m})
	}

	// Build spatial grid from snapshot positions.
	xs := make([]float64, len(snaps))
	ys := make([]float64, len(snaps))
	for i, s := range snaps {
		xs[i] = s.state.X
		ys[i] = s.state.Y
	}
	grid := buildSpatialGrid(xs, ys)

	// Snapshot NPCs grouped by map.
	npcsByMap := make(map[string][]NPCState)
	for _, n := range h.npcs {
		if n.combat.IsAlive() || n.combat.RecentlyDied() {
			mid := n.mapID
			if mid == "" {
				mid = defaultMap
			}
			npcsByMap[mid] = append(npcsByMap[mid], n.state)
		}
	}

	// Snapshot gralats / world items (main map only).
	gralats := make([]GralatPickup, len(h.gralats))
	for i, g := range h.gralats {
		gralats[i] = *g
	}
	worldItems := make([]WorldSpawnItem, len(h.worldItems))
	for i, wi := range h.worldItems {
		worldItems[i] = *wi
	}

	radiusSq := viewRadius * viewRadius

	for c := range h.clients {
		myMap := c.currentMap
		if myMap == "" {
			myMap = defaultMap
		}
		cx, cy := c.state.X, c.state.Y

		// Nearby players: same map + within viewRadius.
		nearIdx := grid.nearby(cx, cy)
		players := make([]PlayerState, 0, len(nearIdx))
		for _, idx := range nearIdx {
			if snaps[idx].mapID == myMap {
				players = append(players, snaps[idx].state)
			}
		}

		// NPCs: same map, radius-filtered.
		var sendNPCs []NPCState
		for _, n := range npcsByMap[myMap] {
			dx, dy := n.X-cx, n.Y-cy
			if dx*dx+dy*dy <= radiusSq {
				sendNPCs = append(sendNPCs, n)
			}
		}

		// Gralats / world items: main map only, radius-filtered.
		var sendGralats []GralatPickup
		var sendWorldItems []WorldSpawnItem
		if myMap == defaultMap {
			for _, g := range gralats {
				dx, dy := g.X-cx, g.Y-cy
				if dx*dx+dy*dy <= radiusSq {
					sendGralats = append(sendGralats, g)
				}
			}
			for _, wi := range worldItems {
				dx, dy := wi.X-cx, wi.Y-cy
				if dx*dx+dy*dy <= radiusSq {
					sendWorldItems = append(sendWorldItems, wi)
				}
			}
		}

		data, err := json.Marshal(map[string]interface{}{
			"type":        "state",
			"players":     players,
			"npcs":        sendNPCs,
			"gralats":     sendGralats,
			"world_items": sendWorldItems,
		})
		if err != nil {
			continue
		}
		select {
		case c.send <- data:
		default:
		}
	}
}

// ──────────────────────────────────────────────────────────────
// Combat
// ──────────────────────────────────────────────────────────────

// damageNPC reduces an NPC's HP by dmg.
// Returns (newHP, killed). Returns (-1, false) if the NPC is immune or on cooldown.
func (h *Hub) damageNPC(npcID string, dmg int) (newHP int, killed bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, n := range h.npcs {
		if n.state.ID != npcID {
			continue
		}
		newHP, killed = n.combat.Damage(dmg)
		if newHP >= 0 {
			n.syncState()
		}
		return
	}
	return -1, false
}

// ──────────────────────────────────────────────────────────────
// Mount
// ──────────────────────────────────────────────────────────────

// mountNPC marks the horse npcID as ridden by playerID.
// Returns false if the horse doesn't exist, is already ridden, or is the wrong type.
func (h *Hub) mountNPC(npcID, playerID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, n := range h.npcs {
		if n.state.ID == npcID && n.state.NPCType == NPCTypeHorse &&
			n.mountedBy == "" && n.combat.IsAlive() {
			n.mountedBy = playerID
			n.state.MountedBy = playerID
			return true
		}
	}
	return false
}

// dismountNPC frees the horse currently ridden by playerID.
func (h *Hub) dismountNPC(playerID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, n := range h.npcs {
		if n.mountedBy == playerID {
			n.mountedBy = ""
			n.state.MountedBy = ""
			return
		}
	}
}

// updateHorsePos moves the horse ridden by playerID to (x, y).
// Must be called while holding h.mu write lock.
func (h *Hub) updateHorsePos(playerID string, x, y float64) {
	for _, n := range h.npcs {
		if n.mountedBy == playerID {
			n.state.X = x
			n.state.Y = y
			return
		}
	}
}

// ──────────────────────────────────────────────────────────────
// Gralat respawn
// ──────────────────────────────────────────────────────────────

func (h *Hub) collectGralat(id string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, g := range h.gralats {
		if g.ID == id {
			value := g.Value
			h.gralats = append(h.gralats[:i], h.gralats[i+1:]...)
			h.gralatTimer[id] = time.Now().Add(respawnDelay)
			return value
		}
	}
	return 0
}

func (h *Hub) checkRespawns() {
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, t := range h.gralatTimer {
		if now.After(t) {
			for i := range gralatSpawnDefs {
				if gralatSpawnDefs[i].id == id {
					d := gralatSpawnDefs[i]
					h.gralats = append(h.gralats, &GralatPickup{
						ID: d.id, X: d.x, Y: d.y, Value: d.value,
					})
					delete(h.gralatTimer, id)
					break
				}
			}
		}
	}
}

// ──────────────────────────────────────────────────────────────
// World items (admin-spawned)
// ──────────────────────────────────────────────────────────────

func (h *Hub) addWorldItem(wi *WorldSpawnItem) {
	h.mu.Lock()
	h.worldItems = append(h.worldItems, wi)
	h.mu.Unlock()
}

func (h *Hub) removeWorldItem(id string) {
	h.mu.Lock()
	for i, wi := range h.worldItems {
		if wi.ID == id {
			h.worldItems = append(h.worldItems[:i], h.worldItems[i+1:]...)
			break
		}
	}
	h.mu.Unlock()
}

// ──────────────────────────────────────────────────────────────
// Game loop
// ──────────────────────────────────────────────────────────────

func (h *Hub) runGameLoop() {
	ticker := time.NewTicker(time.Second / 60)
	defer ticker.Stop()
	lastTime := time.Now()
	respawnTick := 0

	for range ticker.C {
		now := time.Now()
		dt := now.Sub(lastTime).Seconds()
		lastTime = now
		if dt > 0.1 {
			dt = 0.1
		}

		h.mu.Lock()
		// Build player snapshots per map for NPC AI.
		playersByMap := make(map[string][]playerPos, 4)
		for c := range h.clients {
			cm := c.currentMap
			if cm == "" {
				cm = defaultMap
			}
			playersByMap[cm] = append(playersByMap[cm], playerPos{
				id:    c.playerID,
				x:     c.state.X,
				y:     c.state.Y,
				alive: c.state.HP > 0,
			})
		}

		// Update NPCs; each NPC only sees players on its own map.
		type npcAttack struct {
			playerID string
			mapID    string
		}
		var attacks []npcAttack
		for _, n := range h.npcs {
			mid := n.mapID
			if mid == "" {
				mid = defaultMap
			}
			if attackedID := n.update(dt, h.worldColl, playersByMap[mid]); attackedID != "" {
				attacks = append(attacks, npcAttack{playerID: attackedID, mapID: mid})
			}
		}
		// Remove noRespawn NPCs that have died.
		for i := len(h.npcs) - 1; i >= 0; i-- {
			n := h.npcs[i]
			if !n.combat.IsAlive() && n.combat.noRespawn {
				h.npcs = append(h.npcs[:i], h.npcs[i+1:]...)
			}
		}

		// Apply NPC attacks using the shared CombatEntity — same rules as PvP.
		for _, atk := range attacks {
			for c := range h.clients {
				cm := c.currentMap
				if cm == "" {
					cm = defaultMap
				}
				if c.playerID != atk.playerID || cm != atk.mapID {
					continue
				}
				newHP, killed := c.combat.Damage(aggroDamage)
				if newHP < 0 {
					break // immune (hit cooldown)
				}
				c.state.HP = newHP
				msg := map[string]interface{}{"type": "pvp_damage", "hp": newHP}
				if killed {
					c.state.AnimState = "dead"
					msg["killed"] = true
				}
				data, _ := json.Marshal(msg)
				select {
				case c.send <- data:
				default:
				}
				break
			}
		}
		h.mu.Unlock()

		// Tick Lua resources (timers + queued events).
		if globalLuaManager != nil {
			globalLuaManager.Tick(dt)
		}

		respawnTick++
		if respawnTick >= 300 {
			respawnTick = 0
			h.checkRespawns()
		}

		h.sendPerClientState()
	}
}

// ── Spawned enemy (admin command) ─────────────────────────────────────────────

// spawnEnemyAt creates a temporary aggressive NPC near (x, y) on mapID.
// The enemy has 6 HP, moves at spawnedEnemySpeed, and is permanently removed on death.
func (h *Hub) spawnEnemyAt(name, mapID string, x, y float64) {
	id := fmt.Sprintf("enemy_%d", time.Now().UnixNano())

	// Spawn 100 px away in a random direction at a free (non-wall) position.
	angle := mrand.Float64() * 2 * math.Pi
	sx := x + math.Cos(angle)*100
	sy := y + math.Sin(angle)*100
	if h.worldColl != nil {
		sx, sy = findFreePos(h.worldColl, sx, sy, h.worldW, h.worldH)
	}
	sx = clamp(sx, 0, h.worldW-32)
	sy = clamp(sy, 0, h.worldH-32)

	npc := newNPC(id, name, sx, sy, NPCTypeSpawnedEnemy)
	npc.mapID = mapID
	npc.combat.noRespawn = true
	npc.worldW, npc.worldH = h.worldW, h.worldH
	npc.combat.atkCD = aggroAttackCD // no immediate first strike

	h.mu.Lock()
	h.npcs = append(h.npcs, npc)
	h.mu.Unlock()
}

// ── Lua NPC helpers ──────────────────────────────────────────────────────────

// addLuaNPC adds a Lua-spawned NPC to the hub.
func (h *Hub) addLuaNPC(npc *NPC) {
	h.mu.Lock()
	h.npcs = append(h.npcs, npc)
	h.mu.Unlock()
}

// removeLuaNPC removes an NPC by ID (used when a resource stops).
func (h *Hub) removeLuaNPC(id string) {
	h.mu.Lock()
	for i, n := range h.npcs {
		if n.state.ID == id {
			h.npcs = append(h.npcs[:i], h.npcs[i+1:]...)
			break
		}
	}
	h.mu.Unlock()
}

// setLuaNPCPos teleports an NPC to a new position.
func (h *Hub) setLuaNPCPos(id string, x, y float64) {
	h.mu.Lock()
	for _, n := range h.npcs {
		if n.state.ID == id {
			n.state.X = x
			n.state.Y = y
			n.homeX = x
			n.homeY = y
			n.targetX = x
			n.targetY = y
			break
		}
	}
	h.mu.Unlock()
}

// setLuaNPCDialog sets a custom dialog for an NPC.
func (h *Hub) setLuaNPCDialog(id, msg string, gMin, gMax int) {
	h.mu.Lock()
	for _, n := range h.npcs {
		if n.state.ID == id {
			n.customDialog = msg
			n.customGMin = gMin
			n.customGMax = gMax
			break
		}
	}
	h.mu.Unlock()
}

// getLuaNPCPos returns the current position of an NPC.
func (h *Hub) getLuaNPCPos(id string) (x, y float64, ok bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, n := range h.npcs {
		if n.state.ID == id {
			return n.state.X, n.state.Y, true
		}
	}
	return 0, 0, false
}

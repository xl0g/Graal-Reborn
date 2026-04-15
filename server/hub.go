package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// Hub manages all connected clients and drives the server-side game loop.
type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]bool
	npcs    []*NPC

	// Tile collision map (loaded from TMX at startup)
	collMap *CollisionMap

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

	// Load tile collision map (path relative to server working directory).
	if cm, err := LoadCollisionMap("maps/GraalRebornMap.tmx"); err == nil {
		h.collMap = cm
		log.Println("[MAP] Collision map loaded from maps/GraalRebornMap.tmx")
	} else {
		log.Printf("[MAP] Could not load collision map: %v — NPCs will ignore walls", err)
	}

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

	for i, def := range npcDefs {
		x, y := def.x, def.y
		if h.collMap != nil && !h.collMap.IsFreePoint(x+8, y+8) {
			x, y = findFreeGralatPos(h.collMap, x, y)
		}
		h.npcs = append(h.npcs, newNPC(
			fmt.Sprintf("npc_%d", i),
			def.name, x, y, def.npcType,
		))
	}

	// Load persisted world items from DB.
	for _, w := range dbLoadWorldItems() {
		h.worldItems = append(h.worldItems, &WorldSpawnItem{
			ID: w.ID, Name: w.Name, SpritePath: w.SpritePath,
			X: w.X, Y: w.Y, Price: w.Price, ItemID: w.ItemID,
		})
	}

	for i := range gralatSpawnDefs {
		d := gralatSpawnDefs[i]
		x, y := d.x, d.y
		// If the hardcoded position is in a wall, nudge it to a free tile.
		if h.collMap != nil && !h.collMap.IsFreePoint(x+8, y+8) {
			x, y = findFreeGralatPos(h.collMap, x, y)
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
	dbUpdatePosition(c.userID, c.state.X, c.state.Y)
	dbAddPlaytime(c.userID, elapsed)
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

// sendPerClientState sends each client a state snapshot containing only the
// players/NPCs/gralats that are on the same map as that client.
func (h *Hub) sendPerClientState() {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Snapshot all player states and their current maps.
	type playerEntry struct {
		state PlayerState
		cmap  string
	}
	allPlayers := make([]playerEntry, 0, len(h.clients))
	for c := range h.clients {
		ps := c.state
		ps.Playtime = c.savedPlaytime + int(time.Since(c.sessionStart).Seconds())
		m := c.currentMap
		if m == "" {
			m = defaultMap
		}
		allPlayers = append(allPlayers, playerEntry{ps, m})
	}

	// Snapshot NPCs (main map only).
	mainNPCs := make([]NPCState, 0, len(h.npcs))
	for _, n := range h.npcs {
		if n.alive || n.respawnTimer > npcRespawnTime-2.0 {
			mainNPCs = append(mainNPCs, n.state)
		}
	}

	// Snapshot gralats (main map only).
	gralats := make([]GralatPickup, len(h.gralats))
	for i, g := range h.gralats {
		gralats[i] = *g
	}

	// Snapshot world items (main map only).
	worldItems := make([]WorldSpawnItem, len(h.worldItems))
	for i, wi := range h.worldItems {
		worldItems[i] = *wi
	}

	for c := range h.clients {
		myMap := c.currentMap
		if myMap == "" {
			myMap = defaultMap
		}

		// Only include players on the same map.
		filtered := make([]PlayerState, 0)
		for _, pe := range allPlayers {
			if pe.cmap == myMap {
				filtered = append(filtered, pe.state)
			}
		}

		// NPCs, gralats and world items only exist on the main map.
		var sendNPCs []NPCState
		var sendGralats []GralatPickup
		var sendWorldItems []WorldSpawnItem
		if myMap == defaultMap {
			sendNPCs = mainNPCs
			sendGralats = gralats
			sendWorldItems = worldItems
		}

		data, err := json.Marshal(map[string]interface{}{
			"type":        "state",
			"players":     filtered,
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
		if !n.alive || n.state.MaxHP == 0 || n.hitCooldown > 0 {
			return -1, false // immortal, dead, or invulnerable
		}
		n.state.HP -= dmg
		n.hitCooldown = hitInvincibleDt
		if n.state.HP <= 0 {
			n.state.HP = 0
			n.alive = false
			n.respawnTimer = npcRespawnTime
			n.state.AnimState = "dead"
			return 0, true
		}
		return n.state.HP, false
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
			n.mountedBy == "" && n.alive {
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
		// Build player snapshot for NPC AI
		players := make([]playerPos, 0, len(h.clients))
		for c := range h.clients {
			players = append(players, playerPos{
				id:    c.playerID,
				x:     c.state.X,
				y:     c.state.Y,
				alive: c.state.HP > 0,
			})
		}
		// Update NPCs and collect attacks
		type npcAttack struct {
			playerID string
		}
		var attacks []npcAttack
		for _, n := range h.npcs {
			if attackedID := n.update(dt, h.collMap, players); attackedID != "" {
				attacks = append(attacks, npcAttack{playerID: attackedID})
			}
		}
		// Apply NPC attacks to players
		for _, atk := range attacks {
			for c := range h.clients {
				if c.playerID == atk.playerID && c.state.HP > 0 {
					c.state.HP -= aggroDamage
					if c.state.HP < 0 {
						c.state.HP = 0
					}
					// Notify attacked player
					data, _ := json.Marshal(map[string]interface{}{
						"type": "pvp_damage",
						"hp":   c.state.HP,
					})
					select {
					case c.send <- data:
					default:
					}
					break
				}
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

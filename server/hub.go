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

	// World gralat pickups
	gralats     []*GralatPickup
	gralatTimer map[string]time.Time // id → scheduled respawn time
}

var globalHub *Hub

func newHub() *Hub {
	h := &Hub{
		clients:     make(map[*Client]bool),
		gralatTimer: make(map[string]time.Time),
	}

	npcDefs := []struct {
		name    string
		x, y    float64
		npcType int
	}{
		// Regular NPCs
		{"Thibaut le Villageois", 300, 200, NPCTypeVillager},
		{"Marceline la Marchande", 600, 280, NPCTypeMerchant},
		{"Galaad le Garde", 800, 160, NPCTypeGuard},
		{"Eleonore la Voyageuse", 420, 480, NPCTypeTraveler},
		{"Baptiste le Fermier", 680, 520, NPCTypeFarmer},
		{"Sylvain l'Aubergiste", 180, 400, NPCTypeMerchant},
		{"Noemie la Sorcierre", 850, 550, NPCTypeVillager},
		// Horses (NPCTypeHorse = 5)
		{"Cheval de Guerre", 240, 360, NPCTypeHorse},
		{"Jument Grise", 700, 430, NPCTypeHorse},
		{"Poulain Rapide", 450, 570, NPCTypeHorse},
	}

	for i, def := range npcDefs {
		h.npcs = append(h.npcs, newNPC(
			fmt.Sprintf("npc_%d", i),
			def.name, def.x, def.y, def.npcType,
		))
	}

	for i := range gralatSpawnDefs {
		d := gralatSpawnDefs[i]
		h.gralats = append(h.gralats, &GralatPickup{
			ID: d.id, X: d.x, Y: d.y, Value: d.value,
		})
	}

	return h
}

// register adds a client to the hub.
func (h *Hub) register(c *Client) {
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
	h.broadcastSystem(fmt.Sprintf("%s a rejoint le monde!", c.name))
	log.Printf("[HUB] %s connecte (ID: %s)", c.name, c.playerID)
}

// unregister removes a client, frees any mount, saves position.
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
	dbUpdatePosition(c.userID, c.state.X, c.state.Y)
	h.broadcastSystem(fmt.Sprintf("%s a quitte le monde.", c.name))
	log.Printf("[HUB] %s deconnecte", c.name)
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

// getGameState snapshots current players, alive NPCs, and world gralats.
func (h *Hub) getGameState() ([]PlayerState, []NPCState, []GralatPickup) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	players := make([]PlayerState, 0, len(h.clients))
	for c := range h.clients {
		players = append(players, c.state)
	}

	// Only include alive NPCs in broadcast
	npcs := make([]NPCState, 0, len(h.npcs))
	for _, n := range h.npcs {
		if n.alive {
			npcs = append(npcs, n.state)
		}
	}

	gralats := make([]GralatPickup, len(h.gralats))
	for i, g := range h.gralats {
		gralats[i] = *g
	}
	return players, npcs, gralats
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
		for _, n := range h.npcs {
			n.update(dt)
		}
		h.mu.Unlock()

		respawnTick++
		if respawnTick >= 300 {
			respawnTick = 0
			h.checkRespawns()
		}

		players, npcs, gralats := h.getGameState()
		h.broadcast(map[string]interface{}{
			"type":    "state",
			"players": players,
			"npcs":    npcs,
			"gralats": gralats,
		})
	}
}

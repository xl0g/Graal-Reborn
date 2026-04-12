package main

import (
	"math"
	mrand "math/rand"
)

const (
	mapWidth  = 960.0 // 30 tiles × 32 px — matches client worldW
	mapHeight = 640.0 // 20 tiles × 32 px — matches client worldH

	// NPC type constants (must match client-side NPCTypeHorse)
	NPCTypeVillager = 0
	NPCTypeMerchant = 1
	NPCTypeGuard    = 2
	NPCTypeTraveler = 3
	NPCTypeFarmer   = 4
	NPCTypeHorse    = 5

	// Combat
	npcRespawnTime  = 15.0 // seconds before a dead NPC respawns
	hitInvincibleDt = 0.5  // seconds of invulnerability after a hit
)

// NPC is a server-side entity with wandering AI, optional HP, and mount support.
type NPC struct {
	state        NPCState
	homeX, homeY float64
	speed        float64
	targetX      float64
	targetY      float64
	timer        float64

	// Combat / lifecycle
	alive        bool
	respawnTimer float64 // counts down when dead
	hitCooldown  float64 // invulnerability window after being hit

	// Mounting
	mountedBy string // player ID currently riding this NPC

	// Anti-stuck: time spent continuously blocked against a wall
	stuckTimer float64
}

func newNPC(id, name string, x, y float64, npcType int) *NPC {
	maxHP := 5
	speed := 70.0 + mrand.Float64()*50.0

	if npcType == NPCTypeHorse {
		maxHP = 0   // horses cannot be damaged
		speed = 55.0 + mrand.Float64()*30.0
	}

	return &NPC{
		state: NPCState{
			ID:      id,
			Name:    name,
			X:       x,
			Y:       y,
			Dir:     mrand.Intn(4),
			Moving:  false,
			NPCType: npcType,
			HP:      maxHP,
			MaxHP:   maxHP,
		},
		homeX:   x,
		homeY:   y,
		speed:   speed,
		targetX: x,
		targetY: y,
		timer:   mrand.Float64() * 3.0,
		alive:   true,
	}
}

// update advances the NPC's AI by dt seconds.
// collMap may be nil (NPCs move freely when no map is loaded).
func (n *NPC) update(dt float64, collMap *CollisionMap) {
	// Respawn countdown when dead
	if !n.alive {
		n.respawnTimer -= dt
		if n.respawnTimer <= 0 {
			n.alive = true
			n.state.X = n.homeX
			n.state.Y = n.homeY
			n.state.HP = n.state.MaxHP
			n.state.AnimState = ""
			n.targetX = n.homeX
			n.targetY = n.homeY
			n.timer = 2.0
		}
		return
	}

	// Hit invulnerability cooldown
	if n.hitCooldown > 0 {
		n.hitCooldown -= dt
	}

	// When mounted, the horse position is driven by the player's move messages
	if n.mountedBy != "" {
		n.state.Moving = false
		return
	}

	// Standard wandering AI
	dx := n.targetX - n.state.X
	dy := n.targetY - n.state.Y
	dist := math.Sqrt(dx*dx + dy*dy)

	if dist < 4.0 {
		n.state.Moving = false
		n.timer -= dt
		if n.timer <= 0 {
			angle := mrand.Float64() * math.Pi * 2
			radius := 80.0 + mrand.Float64()*150.0
			n.targetX = clamp(n.homeX+math.Cos(angle)*radius, 50, mapWidth-50)
			n.targetY = clamp(n.homeY+math.Sin(angle)*radius, 50, mapHeight-50)
			n.timer = 1.5 + mrand.Float64()*4.0
			n.state.Moving = true
		}
	} else {
		n.state.Moving = true
		step := n.speed * dt

		// Try to move with wall-sliding collision (X then Y independently).
		const npcW, npcH = 28.0, 28.0 // slightly smaller than a tile for smoother sliding

		newX := clamp(n.state.X+(dx/dist)*step, 1, mapWidth-npcW-1)
		newY := clamp(n.state.Y+(dy/dist)*step, 1, mapHeight-npcH-1)

		blocked := false
		if !collMap.IsBlocked(newX, n.state.Y, npcW, npcH) {
			n.state.X = newX
		} else {
			blocked = true
		}
		if !collMap.IsBlocked(n.state.X, newY, npcW, npcH) {
			n.state.Y = newY
		} else {
			blocked = true
		}

		if blocked {
			n.stuckTimer += dt
			if n.stuckTimer >= 0.3 {
				n.stuckTimer = 0
				angle := mrand.Float64() * math.Pi * 2
				radius := 60.0 + mrand.Float64()*120.0
				n.targetX = clamp(n.homeX+math.Cos(angle)*radius, 50, mapWidth-50)
				n.targetY = clamp(n.homeY+math.Sin(angle)*radius, 50, mapHeight-50)
				n.timer = 1.0 + mrand.Float64()*3.0
			}
		} else {
			n.stuckTimer = 0
		}

		if math.Abs(dx) > math.Abs(dy) {
			if dx > 0 {
				n.state.Dir = 3
			} else {
				n.state.Dir = 1
			}
		} else {
			if dy > 0 {
				n.state.Dir = 2
			} else {
				n.state.Dir = 0
			}
		}
	}
}

// clamp constrains v to [lo, hi].
func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// npcDialogDefs maps NPC type index → conversation text and gralat reward range.
var npcDialogDefs = []struct {
	msg        string
	minG, maxG int
}{
	// type 0 — villager
	{"Greetings, traveller! The village is peaceful today. Take these gralats for your journey.", 1, 3},
	// type 1 — merchant
	{"Great deal! Special price just for you, friend.", 2, 5},
	// type 2 — guard
	{"Halt! Hmm... you seem harmless enough. Take these gralats and be on your way.", 1, 2},
	// type 3 — traveler
	{"I have returned from distant lands! I gladly share my findings with fellow travellers.", 1, 5},
	// type 4 — farmer
	{"What a harvest this season! Here is your share, friend.", 2, 4},
}

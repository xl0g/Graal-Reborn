package main

import (
	"math"
	mrand "math/rand"
)

const (
	mapWidth  = 1120.0 // 70 tiles × 16 px — matches GraalRebornMap.tmx
	mapHeight = 1120.0 // 70 tiles × 16 px — matches GraalRebornMap.tmx

	// NPC type constants (must match client-side NPCTypeHorse)
	NPCTypeVillager   = 0
	NPCTypeMerchant   = 1
	NPCTypeGuard      = 2
	NPCTypeTraveler   = 3
	NPCTypeFarmer     = 4
	NPCTypeHorse      = 5
	NPCTypeAggressive = 6 // chases players and attacks them
	NPCTypePassive    = 7 // flees from nearby players

	// Combat
	npcRespawnTime  = 15.0 // seconds before a dead NPC respawns
	hitInvincibleDt = 0.5  // seconds of invulnerability after a hit

	// Aggressive NPC constants
	aggroRange      = 160.0 // pixels — aggro activation distance
	aggroAttackDist = 28.0  // pixels — melee attack distance
	aggroAttackCD   = 1.2   // seconds between attacks
	aggroDamage     = 1     // HP removed per hit
	aggroSpeed      = 90.0  // chase speed (px/s)

	// Passive NPC constants
	passiveFleeRange = 100.0 // pixels — flee activation distance
	passiveFleeSpeed = 120.0 // flee speed (px/s)
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

	// Optional Lua-defined dialog override (empty = use npcDialogDefs[type]).
	customDialog string
	customGMin   int
	customGMax   int

	// Aggressive / Passive AI
	aggroTarget   string  // player ID being chased (aggressive NPCs)
	attackCooldown float64 // countdown until next attack
}

func newNPC(id, name string, x, y float64, npcType int) *NPC {
	maxHP := 5
	speed := 70.0 + mrand.Float64()*50.0

	switch npcType {
	case NPCTypeHorse:
		maxHP = 0 // horses cannot be damaged
		speed = 55.0 + mrand.Float64()*30.0
	case NPCTypeAggressive:
		maxHP = 8
		speed = aggroSpeed + mrand.Float64()*20.0
	case NPCTypePassive:
		maxHP = 3
		speed = passiveFleeSpeed + mrand.Float64()*20.0
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

// playerPos is a lightweight snapshot of a connected player's position.
type playerPos struct {
	id   string
	x, y float64
	alive bool
}

// update advances the NPC's AI by dt seconds.
// players is a snapshot of all connected players (used by aggressive/passive AI).
// collMap may be nil (NPCs move freely when no map is loaded).
// Returns a non-empty playerID if this NPC just attacked that player.
func (n *NPC) update(dt float64, collMap *CollisionMap, players []playerPos) (attackedID string) {
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
			n.aggroTarget = ""
			n.timer = 2.0
		}
		return ""
	}

	// Hit invulnerability cooldown
	if n.hitCooldown > 0 {
		n.hitCooldown -= dt
	}
	if n.attackCooldown > 0 {
		n.attackCooldown -= dt
	}

	// When mounted, the horse position is driven by the player's move messages
	if n.mountedBy != "" {
		n.state.Moving = false
		return ""
	}

	switch n.state.NPCType {
	case NPCTypeAggressive:
		return n.updateAggressive(dt, collMap, players)
	case NPCTypePassive:
		n.updatePassive(dt, collMap, players)
		return ""
	default:
		n.updateWander(dt, collMap)
		return ""
	}
}

// updateAggressive: chase nearest player; attack when close enough.
func (n *NPC) updateAggressive(dt float64, collMap *CollisionMap, players []playerPos) string {
	const npcW, npcH = 28.0, 28.0

	// Find nearest alive player
	var nearestID string
	nearestDist := math.MaxFloat64
	var nearestX, nearestY float64
	for _, p := range players {
		if !p.alive {
			continue
		}
		dx := p.x - n.state.X
		dy := p.y - n.state.Y
		d := math.Sqrt(dx*dx + dy*dy)
		if d < nearestDist {
			nearestDist = d
			nearestID = p.id
			nearestX = p.x
			nearestY = p.y
		}
	}

	if nearestID == "" || nearestDist > aggroRange {
		// No player in range — wander back home
		n.aggroTarget = ""
		n.updateWander(dt, collMap)
		return ""
	}

	n.aggroTarget = nearestID
	n.state.Moving = true

	// Attack if close enough
	if nearestDist <= aggroAttackDist && n.attackCooldown <= 0 {
		n.attackCooldown = aggroAttackCD
		return nearestID
	}

	// Chase player
	dx := nearestX - n.state.X
	dy := nearestY - n.state.Y
	dist := math.Sqrt(dx*dx + dy*dy)
	if dist < 1 {
		n.state.Moving = false
		return ""
	}
	step := n.speed * dt
	newX := clamp(n.state.X+(dx/dist)*step, 1, mapWidth-npcW-1)
	newY := clamp(n.state.Y+(dy/dist)*step, 1, mapHeight-npcH-1)

	if collMap == nil || !collMap.IsBlocked(newX, n.state.Y, npcW, npcH) {
		n.state.X = newX
	}
	if collMap == nil || !collMap.IsBlocked(n.state.X, newY, npcW, npcH) {
		n.state.Y = newY
	}
	n.setDirFromDelta(dx, dy)
	return ""
}

// updatePassive: flee from nearest player when too close.
func (n *NPC) updatePassive(dt float64, collMap *CollisionMap, players []playerPos) {
	const npcW, npcH = 28.0, 28.0

	// Find nearest player
	nearestDist := math.MaxFloat64
	var nearestX, nearestY float64
	for _, p := range players {
		dx := p.x - n.state.X
		dy := p.y - n.state.Y
		d := math.Sqrt(dx*dx + dy*dy)
		if d < nearestDist {
			nearestDist = d
			nearestX = p.x
			nearestY = p.y
		}
	}

	if nearestDist > passiveFleeRange {
		// No player nearby — wander normally
		n.updateWander(dt, collMap)
		return
	}

	// Flee away from player
	dx := n.state.X - nearestX
	dy := n.state.Y - nearestY
	dist := math.Sqrt(dx*dx + dy*dy)
	if dist < 1 {
		n.state.Moving = false
		return
	}
	n.state.Moving = true
	step := passiveFleeSpeed * dt
	newX := clamp(n.state.X+(dx/dist)*step, 1, mapWidth-npcW-1)
	newY := clamp(n.state.Y+(dy/dist)*step, 1, mapHeight-npcH-1)

	if collMap == nil || !collMap.IsBlocked(newX, n.state.Y, npcW, npcH) {
		n.state.X = newX
	}
	if collMap == nil || !collMap.IsBlocked(n.state.X, newY, npcW, npcH) {
		n.state.Y = newY
	}
	// Face away from player
	n.setDirFromDelta(dx, dy)
}

// updateWander is the standard random wandering AI (used by villagers, guards, etc.).
func (n *NPC) updateWander(dt float64, collMap *CollisionMap) {
	const npcW, npcH = 28.0, 28.0

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
		newX := clamp(n.state.X+(dx/dist)*step, 1, mapWidth-npcW-1)
		newY := clamp(n.state.Y+(dy/dist)*step, 1, mapHeight-npcH-1)

		blocked := false
		if collMap == nil || !collMap.IsBlocked(newX, n.state.Y, npcW, npcH) {
			n.state.X = newX
		} else {
			blocked = true
		}
		if collMap == nil || !collMap.IsBlocked(n.state.X, newY, npcW, npcH) {
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
		n.setDirFromDelta(dx, dy)
	}
}

// setDirFromDelta sets the NPC direction based on movement delta.
func (n *NPC) setDirFromDelta(dx, dy float64) {
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

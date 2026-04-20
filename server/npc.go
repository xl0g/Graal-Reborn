package main

import (
	"math"
	mrand "math/rand"
)

const (
	mapWidth  = 1120.0
	mapHeight = 1120.0

	// NPC type constants (must match client-side values)
	NPCTypeVillager     = 0
	NPCTypeMerchant     = 1
	NPCTypeGuard        = 2
	NPCTypeTraveler     = 3
	NPCTypeFarmer       = 4
	NPCTypeHorse        = 5
	NPCTypeAggressive   = 6
	NPCTypePassive      = 7
	NPCTypeSpawnedEnemy = 8

	// Aggressive NPC
	aggroRange      = 160.0
	aggroAttackDist = 32.0
	aggroAttackCD   = 1.2
	aggroDamage     = 1
	aggroSpeed      = 90.0

	// Admin-spawned enemy
	spawnedEnemySpeed      = 240.0
	spawnedEnemyAttackDist = 52.0
	spawnedEnemyAggroRange = 800.0
	spawnedEnemyHP         = 6

	// Passive NPC
	passiveFleeRange = 100.0
	passiveFleeSpeed = 120.0
)

// NPC is a server-side entity with shared combat logic (via CombatEntity) and AI.
type NPC struct {
	combat       CombatEntity // HP, damage, invulnerability, respawn — identical to player
	state        NPCState     // wire-format snapshot sent to clients; synced before broadcast
	homeX, homeY float64      // respawn / wander anchor position
	worldW, worldH float64    // world bounds for movement clamping
	speed        float64
	targetX      float64
	targetY      float64
	timer        float64 // wander: wait time before choosing next target

	mapID     string // map instance this NPC belongs to (empty = defaultMap)
	mountedBy string // player ID currently riding this NPC (horses only)

	stuckTimer float64 // time spent continuously blocked against a wall

	// Optional Lua-defined dialog
	customDialog         string
	customGMin, customGMax int

	aggroTarget string // player ID being chased (aggressive NPCs)
}

func newNPC(id, name string, x, y float64, npcType int) *NPC {
	maxHP := 5
	speed := 70.0 + mrand.Float64()*50.0

	switch npcType {
	case NPCTypeHorse:
		maxHP = 0 // immortal
		speed = 55.0 + mrand.Float64()*30.0
	case NPCTypeAggressive:
		maxHP = 8
		speed = aggroSpeed + mrand.Float64()*20.0
	case NPCTypePassive:
		maxHP = 3
		speed = passiveFleeSpeed + mrand.Float64()*20.0
	case NPCTypeSpawnedEnemy:
		maxHP = spawnedEnemyHP
		speed = spawnedEnemySpeed + mrand.Float64()*15.0
	}

	n := &NPC{
		combat: newCombat(maxHP),
		state: NPCState{
			ID:      id,
			Name:    name,
			X:       x,
			Y:       y,
			Dir:     mrand.Intn(4),
			NPCType: npcType,
		},
		homeX:   x,
		homeY:   y,
		worldW:  mapWidth,
		worldH:  mapHeight,
		speed:   speed,
		targetX: x,
		targetY: y,
		timer:   mrand.Float64() * 3.0,
	}
	n.syncState()
	return n
}

// syncState copies combat HP/alive status into the wire-format NPCState.
// Call after every combat update and before broadcasting.
func (n *NPC) syncState() {
	n.state.HP = n.combat.HP
	n.state.MaxHP = n.combat.MaxHP
	if !n.combat.alive {
		n.state.AnimState = "dead"
	} else if n.state.AnimState == "dead" {
		n.state.AnimState = ""
	}
}

// playerPos is a lightweight snapshot of a connected player used by AI.
type playerPos struct {
	id    string
	x, y  float64
	alive bool
}

// update advances the NPC's AI by dt seconds.
// Returns a non-empty playerID if this NPC just attacked that player.
func (n *NPC) update(dt float64, collMap WorldCollider, players []playerPos) (attackedID string) {
	// Tick shared combat (cooldowns + respawn).
	if respawned := n.combat.Tick(dt); respawned {
		n.state.X = n.homeX
		n.state.Y = n.homeY
		n.targetX = n.homeX
		n.targetY = n.homeY
		n.aggroTarget = ""
		n.timer = 2.0
		n.syncState()
		return ""
	}

	if !n.combat.alive {
		n.syncState()
		return ""
	}

	// Horses are driven by the rider's move messages.
	if n.mountedBy != "" {
		n.state.Moving = false
		return ""
	}

	switch n.state.NPCType {
	case NPCTypeAggressive, NPCTypeSpawnedEnemy:
		attackedID = n.updateAggressive(dt, collMap, players)
	case NPCTypePassive:
		n.updatePassive(dt, collMap, players)
	default:
		n.updateWander(dt, collMap)
	}
	n.syncState()
	return attackedID
}

func (n *NPC) updateAggressive(dt float64, collMap WorldCollider, players []playerPos) string {
	const npcW, npcH = 28.0, 28.0

	isSpawned := n.state.NPCType == NPCTypeSpawnedEnemy
	effectiveRange := aggroRange
	attackDist := aggroAttackDist
	if isSpawned {
		effectiveRange = spawnedEnemyAggroRange
		attackDist = spawnedEnemyAttackDist
	}

	// Find nearest alive player within aggro range.
	nearestDist := math.MaxFloat64
	var nearestID string
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

	if nearestID == "" || nearestDist > effectiveRange {
		n.aggroTarget = ""
		n.stuckTimer = 0
		n.updateWander(dt, collMap)
		return ""
	}

	n.aggroTarget = nearestID
	n.state.Moving = true

	// Attack if close enough and cooldown expired.
	if nearestDist <= attackDist && n.combat.CanAttack() {
		n.combat.atkCD = aggroAttackCD
		return nearestID
	}

	// Move toward the player.
	dx := nearestX - n.state.X
	dy := nearestY - n.state.Y
	dist := math.Sqrt(dx*dx + dy*dy)
	if dist < 1 {
		n.state.Moving = false
		return ""
	}

	step := n.speed * dt
	newX := clamp(n.state.X+(dx/dist)*step, 1, n.worldW-npcW-1)
	newY := clamp(n.state.Y+(dy/dist)*step, 1, n.worldH-npcH-1)

	movedX := canMove(collMap, newX, n.state.Y, npcW, npcH)
	movedY := canMove(collMap, n.state.X, newY, npcW, npcH)
	if movedX {
		n.state.X = newX
	}
	if movedY {
		n.state.Y = newY
	}

	if !movedX && !movedY {
		n.stuckTimer += dt
		if n.stuckTimer >= 0.4 {
			n.stuckTimer = 0
			angle := math.Atan2(dy, dx) + math.Pi/4
			slideX := clamp(n.state.X+math.Cos(angle)*step, 1, n.worldW-npcW-1)
			slideY := clamp(n.state.Y+math.Sin(angle)*step, 1, n.worldH-npcH-1)
			if canMove(collMap, slideX, n.state.Y, npcW, npcH) {
				n.state.X = slideX
			} else if canMove(collMap, n.state.X, slideY, npcW, npcH) {
				n.state.Y = slideY
			}
		}
	} else {
		n.stuckTimer = 0
	}

	n.setDirFromDelta(dx, dy)
	return ""
}

func (n *NPC) updatePassive(dt float64, collMap WorldCollider, players []playerPos) {
	const npcW, npcH = 28.0, 28.0

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
		n.updateWander(dt, collMap)
		return
	}

	dx := n.state.X - nearestX
	dy := n.state.Y - nearestY
	dist := math.Sqrt(dx*dx + dy*dy)
	if dist < 1 {
		n.state.Moving = false
		return
	}
	n.state.Moving = true
	step := passiveFleeSpeed * dt
	newX := clamp(n.state.X+(dx/dist)*step, 1, n.worldW-npcW-1)
	newY := clamp(n.state.Y+(dy/dist)*step, 1, n.worldH-npcH-1)
	if canMove(collMap, newX, n.state.Y, npcW, npcH) {
		n.state.X = newX
	}
	if canMove(collMap, n.state.X, newY, npcW, npcH) {
		n.state.Y = newY
	}
	n.setDirFromDelta(dx, dy)
}

func (n *NPC) updateWander(dt float64, collMap WorldCollider) {
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
			n.targetX = clamp(n.homeX+math.Cos(angle)*radius, 50, n.worldW-50)
			n.targetY = clamp(n.homeY+math.Sin(angle)*radius, 50, n.worldH-50)
			n.timer = 1.5 + mrand.Float64()*4.0
			n.state.Moving = true
		}
	} else {
		n.state.Moving = true
		step := n.speed * dt
		newX := clamp(n.state.X+(dx/dist)*step, 1, n.worldW-npcW-1)
		newY := clamp(n.state.Y+(dy/dist)*step, 1, n.worldH-npcH-1)

		blocked := false
		if canMove(collMap, newX, n.state.Y, npcW, npcH) {
			n.state.X = newX
		} else {
			blocked = true
		}
		if canMove(collMap, n.state.X, newY, npcW, npcH) {
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
				n.targetX = clamp(n.homeX+math.Cos(angle)*radius, 50, n.worldW-50)
				n.targetY = clamp(n.homeY+math.Sin(angle)*radius, 50, n.worldH-50)
				n.timer = 1.0 + mrand.Float64()*3.0
			}
		} else {
			n.stuckTimer = 0
		}
		n.setDirFromDelta(dx, dy)
	}
}

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

// canMove returns true when the bounding box at (x,y) is passable.
// A nil collider means the world has no collision — always passable.
func canMove(collMap WorldCollider, x, y, w, h float64) bool {
	return collMap == nil || !collMap.IsBlocked(x, y, w, h)
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

// npcDialogDefs maps NPC type → conversation text and gralat reward.
var npcDialogDefs = []struct {
	msg        string
	minG, maxG int
}{
	{"Greetings, traveller! The village is peaceful today. Take these gralats for your journey.", 1, 3},
	{"Great deal! Special price just for you, friend.", 2, 5},
	{"Halt! Hmm... you seem harmless enough. Take these gralats and be on your way.", 1, 2},
	{"I have returned from distant lands! I gladly share my findings with fellow travellers.", 1, 5},
	{"What a harvest this season! Here is your share, friend.", 2, 4},
}

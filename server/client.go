package main

import (
	"encoding/json"
	"fmt"
	"log"
	mrand "math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Client represents a single connected player.
type Client struct {
	conn         *websocket.Conn
	send         chan []byte
	hub          *Hub
	userID       int64
	name         string
	playerID     string
	state        PlayerState
	npcCooldowns map[string]time.Time
	mountedNPCID string // ID of the horse this client is riding, or ""
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// handleWebSocket upgrades the connection, authenticates, and runs read/write loops.
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("[WS] Upgrade error:", err)
		return
	}

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var authMsg struct {
		Type  string `json:"type"`
		Token string `json:"token"`
	}
	if err := conn.ReadJSON(&authMsg); err != nil || authMsg.Type != "auth" {
		conn.WriteJSON(map[string]string{"type": "auth_error", "msg": "Authentification requise"})
		conn.Close()
		return
	}
	conn.SetReadDeadline(time.Time{})

	user, err := dbValidateSession(authMsg.Token)
	if err != nil {
		conn.WriteJSON(map[string]string{"type": "auth_error", "msg": err.Error()})
		conn.Close()
		return
	}

	spawnX := clamp(user.LastX, 0, mapWidth-32)
	spawnY := clamp(user.LastY, 0, mapHeight-32)
	playerID := fmt.Sprintf("player_%d", user.ID)

	client := &Client{
		conn:         conn,
		send:         make(chan []byte, 512),
		hub:          globalHub,
		userID:       user.ID,
		name:         user.Name,
		playerID:     playerID,
		npcCooldowns: make(map[string]time.Time),
		state: PlayerState{
			ID:      playerID,
			Name:    user.Name,
			X:       spawnX,
			Y:       spawnY,
			Dir:     2,
			Gralats: user.Gralats,
		},
	}

	conn.WriteJSON(map[string]interface{}{
		"type":     "auth_ok",
		"id":       playerID,
		"name":     user.Name,
		"x":        spawnX,
		"y":        spawnY,
		"gralat_n": user.Gralats,
	})

	globalHub.register(client)
	defer func() {
		globalHub.unregister(client)
		conn.Close()
	}()

	// Writer goroutine
	go func() {
		pingTick := time.NewTicker(54 * time.Second)
		defer pingTick.Stop()
		for {
			select {
			case data := <-client.send:
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
					return
				}
			case <-pingTick.C:
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}()

	// Read loop
	conn.SetReadLimit(4096)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		var base struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &base); err != nil {
			continue
		}

		switch base.Type {
		case "move":
			handleMove(client, raw)
		case "cosmetic":
			handleCosmetic(client, raw)
		case "chat":
			handleChat(client, raw)
		case "collect_gralat":
			handleCollectGralat(client, raw)
		case "talk_npc":
			handleTalkNPC(client, raw)
		case "sword_hit":
			handleSwordHit(client, raw)
		case "mount_npc":
			handleMountNPC(client, raw)
		case "dismount":
			handleDismount(client, raw)
		case "anim_state":
			handleAnimState(client, raw)
		}
	}
}

// ──────────────────────────────────────────────────────────────
// Existing handlers
// ──────────────────────────────────────────────────────────────

func handleMove(c *Client, raw []byte) {
	var msg struct {
		X         float64 `json:"x"`
		Y         float64 `json:"y"`
		Dir       int     `json:"dir"`
		Moving    bool    `json:"moving"`
		AnimState string  `json:"anim,omitempty"`
		Mounted   bool    `json:"mounted,omitempty"`
	}
	if json.Unmarshal(raw, &msg) != nil {
		return
	}
	globalHub.mu.Lock()
	c.state.X = clamp(msg.X, 0, mapWidth-32)
	c.state.Y = clamp(msg.Y, 0, mapHeight-32)
	c.state.Dir = msg.Dir
	c.state.Moving = msg.Moving
	c.state.AnimState = msg.AnimState
	c.state.Mounted = msg.Mounted
	// Move the horse the player is riding
	if c.mountedNPCID != "" {
		globalHub.updateHorsePos(c.playerID, c.state.X, c.state.Y)
	}
	globalHub.mu.Unlock()
}

func handleCosmetic(c *Client, raw []byte) {
	var msg struct {
		Body string `json:"body"`
		Head string `json:"head"`
		Hat  string `json:"hat"`
	}
	if json.Unmarshal(raw, &msg) != nil {
		return
	}
	globalHub.mu.Lock()
	c.state.Body = msg.Body
	c.state.Head = msg.Head
	c.state.Hat = msg.Hat
	globalHub.mu.Unlock()
}

func handleChat(c *Client, raw []byte) {
	var msg struct {
		Msg string `json:"msg"`
	}
	if json.Unmarshal(raw, &msg) != nil {
		return
	}
	txt := strings.TrimSpace(msg.Msg)
	if len(txt) == 0 || len(txt) > 200 {
		return
	}
	dbSaveChat(c.name, txt)
	globalHub.broadcast(map[string]interface{}{
		"type": "chat",
		"from": c.name,
		"msg":  txt,
	})
}

func handleCollectGralat(c *Client, raw []byte) {
	var msg struct {
		GralatID string `json:"gralat_id"`
	}
	if json.Unmarshal(raw, &msg) != nil || msg.GralatID == "" {
		return
	}
	value := globalHub.collectGralat(msg.GralatID)
	if value <= 0 {
		return
	}
	newTotal, _ := dbAddGralats(c.userID, value)
	globalHub.mu.Lock()
	c.state.Gralats = newTotal
	globalHub.mu.Unlock()

	data, _ := json.Marshal(map[string]interface{}{
		"type":     "gralat_update",
		"gralat_n": newTotal,
	})
	select {
	case c.send <- data:
	default:
	}
	log.Printf("[GRALAT] %s collecte %s (+%d → %d)", c.name, msg.GralatID, value, newTotal)
}

func handleTalkNPC(c *Client, raw []byte) {
	var msg struct {
		NPCID string `json:"npc_id"`
	}
	if json.Unmarshal(raw, &msg) != nil || msg.NPCID == "" {
		return
	}

	var npc *NPC
	globalHub.mu.RLock()
	for _, n := range globalHub.npcs {
		if n.state.ID == msg.NPCID {
			npc = n
			break
		}
	}
	globalHub.mu.RUnlock()
	if npc == nil || npc.state.NPCType == NPCTypeHorse {
		return // horses don't talk
	}

	const cooldown = 120 * time.Second
	if last, ok := c.npcCooldowns[msg.NPCID]; ok && time.Since(last) < cooldown {
		remaining := int((cooldown - time.Since(last)).Seconds())
		data, _ := json.Marshal(map[string]interface{}{
			"type":     "npc_dialog",
			"msg":      fmt.Sprintf("%s: Je n'ai rien de plus a dire pour l'instant... (%ds)", npc.state.Name, remaining),
			"gralat_n": 0,
		})
		select {
		case c.send <- data:
		default:
		}
		return
	}

	def := npcDialogDefs[npc.state.NPCType%len(npcDialogDefs)]
	gralatN := def.minG + mrand.Intn(def.maxG-def.minG+1)
	newTotal, _ := dbAddGralats(c.userID, gralatN)

	globalHub.mu.Lock()
	c.state.Gralats = newTotal
	globalHub.mu.Unlock()

	c.npcCooldowns[msg.NPCID] = time.Now()

	dialog := fmt.Sprintf("%s: %s", npc.state.Name, def.msg)
	data, _ := json.Marshal(map[string]interface{}{
		"type":     "npc_dialog",
		"msg":      dialog,
		"gralat_n": gralatN,
	})
	select {
	case c.send <- data:
	default:
	}
	log.Printf("[NPC] %s -> %s: +%d gralats (%d total)", npc.state.Name, c.name, gralatN, newTotal)
}

// ──────────────────────────────────────────────────────────────
// Combat
// ──────────────────────────────────────────────────────────────

func handleSwordHit(c *Client, raw []byte) {
	var msg struct {
		NPCID string `json:"npc_id"`
	}
	if json.Unmarshal(raw, &msg) != nil || msg.NPCID == "" {
		return
	}

	// Server-side proximity validation (within 100 px)
	const maxReach = 100.0
	var npcX, npcY float64
	var found bool
	globalHub.mu.RLock()
	for _, n := range globalHub.npcs {
		if n.state.ID == msg.NPCID && n.alive {
			npcX, npcY = n.state.X, n.state.Y
			found = true
			break
		}
	}
	globalHub.mu.RUnlock()

	if !found {
		return
	}

	dx := npcX - c.state.X
	dy := npcY - c.state.Y
	if dx*dx+dy*dy > maxReach*maxReach {
		return // cheating or network lag — ignore
	}

	newHP, killed := globalHub.damageNPC(msg.NPCID, 1)
	if newHP < 0 {
		return // NPC on cooldown or immortal
	}

	data, _ := json.Marshal(map[string]interface{}{
		"type":   "npc_damage",
		"npc_id": msg.NPCID,
		"hp":     newHP,
		"killed": killed,
	})
	select {
	case c.send <- data:
	default:
	}

	if killed {
		log.Printf("[COMBAT] %s a tue %s", c.name, msg.NPCID)
	} else {
		log.Printf("[COMBAT] %s frappe %s → HP %d", c.name, msg.NPCID, newHP)
	}
}

// ──────────────────────────────────────────────────────────────
// Mount
// ──────────────────────────────────────────────────────────────

func handleMountNPC(c *Client, raw []byte) {
	var msg struct {
		NPCID string `json:"npc_id"`
	}
	if json.Unmarshal(raw, &msg) != nil || msg.NPCID == "" {
		return
	}

	// Proximity check (within 64 px)
	const maxDist = 64.0
	var npcX, npcY float64
	globalHub.mu.RLock()
	for _, n := range globalHub.npcs {
		if n.state.ID == msg.NPCID {
			npcX, npcY = n.state.X, n.state.Y
			break
		}
	}
	globalHub.mu.RUnlock()

	dx := npcX - c.state.X
	dy := npcY - c.state.Y
	if dx*dx+dy*dy > maxDist*maxDist {
		return
	}

	if !globalHub.mountNPC(msg.NPCID, c.playerID) {
		return // horse already taken or wrong type
	}

	c.mountedNPCID = msg.NPCID

	globalHub.mu.Lock()
	c.state.Mounted = true
	c.state.AnimState = "ride"
	globalHub.mu.Unlock()

	data, _ := json.Marshal(map[string]interface{}{
		"type":   "mount_ok",
		"npc_id": msg.NPCID,
	})
	select {
	case c.send <- data:
	default:
	}
	log.Printf("[MOUNT] %s monte sur %s", c.name, msg.NPCID)
}

func handleDismount(c *Client, raw []byte) {
	if c.mountedNPCID == "" {
		return
	}
	globalHub.dismountNPC(c.playerID)
	c.mountedNPCID = ""

	globalHub.mu.Lock()
	c.state.Mounted = false
	c.state.AnimState = ""
	globalHub.mu.Unlock()

	data, _ := json.Marshal(map[string]string{"type": "dismount_ok"})
	select {
	case c.send <- data:
	default:
	}
}

func handleAnimState(c *Client, raw []byte) {
	var msg struct {
		Anim    string `json:"anim"`
		Mounted bool   `json:"mounted,omitempty"`
	}
	if json.Unmarshal(raw, &msg) != nil {
		return
	}
	globalHub.mu.Lock()
	c.state.AnimState = msg.Anim
	globalHub.mu.Unlock()
}

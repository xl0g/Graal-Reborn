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

const playerMaxHP = 6

// Client represents a single connected player.
type Client struct {
	conn          *websocket.Conn
	send          chan []byte
	hub           *Hub
	userID        int64
	name          string
	playerID      string
	state         PlayerState
	npcCooldowns  map[string]time.Time
	mountedNPCID  string    // ID of the horse this client is riding, or ""
	sessionStart  time.Time // when the player connected
	savedPlaytime int       // playtime seconds accumulated before this session
	currentMap    string    // which map this client is currently on
	isAdmin       bool      // whether this player has admin privileges
}

const defaultMap = "GraalRebornMap.tmx"

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
		conn.WriteJSON(map[string]string{"type": "auth_error", "msg": "Authentication required"})
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
		conn:          conn,
		send:          make(chan []byte, 512),
		hub:           globalHub,
		userID:        user.ID,
		name:          user.Name,
		playerID:      playerID,
		npcCooldowns:  make(map[string]time.Time),
		sessionStart:  time.Now(),
		savedPlaytime: user.Playtime,
		currentMap:    defaultMap,
		isAdmin:       dbIsAdmin(user.ID),
		state: PlayerState{
			ID:       playerID,
			Name:     user.Name,
			X:        spawnX,
			Y:        spawnY,
			Dir:      2,
			Gralats:  user.Gralats,
			Playtime: user.Playtime,
			Body:     user.Body,
			Head:     user.Head,
			Hat:      user.Hat,
			Shield:   user.Shield,
			Sword:    user.Sword,
			HP:       playerMaxHP,
			MaxHP:    playerMaxHP,
		},
	}

	conn.WriteJSON(map[string]interface{}{
		"type":      "auth_ok",
		"id":        playerID,
		"name":      user.Name,
		"x":         spawnX,
		"y":         spawnY,
		"gralat_n":  user.Gralats,
		"playtime":  user.Playtime,
		"body":      user.Body,
		"head":      user.Head,
		"hat":       user.Hat,
		"shield":    user.Shield,
		"sword":     user.Sword,
		"is_admin":  client.isAdmin,
	})

	globalHub.register(client)
	// Send current inventory after registration.
	go sendInventoryTo(client)
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
		case "change_map":
			handleChangeMap(client, raw)
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
		case "pvp_hit":
			handlePvPHit(client, raw)
		case "mount_npc":
			handleMountNPC(client, raw)
		case "dismount":
			handleDismount(client, raw)
		case "anim_state":
			handleAnimState(client, raw)
		// ── New handlers ──────────────────────────────────────
		case "use_item":
			handleUseItem(client, raw)
		case "buy_world_item":
			handleBuyWorldItem(client, raw)
		case "admin_spawn_world_item":
			handleAdminSpawnWorldItem(client, raw)
		case "admin_remove_world_item":
			handleAdminRemoveWorldItem(client, raw)
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

func handleChangeMap(c *Client, raw []byte) {
	var msg struct {
		Map string `json:"map"`
	}
	if json.Unmarshal(raw, &msg) != nil || msg.Map == "" {
		return
	}
	globalHub.mu.Lock()
	c.currentMap = msg.Map
	globalHub.mu.Unlock()
}

func handleCosmetic(c *Client, raw []byte) {
	var msg struct {
		Body   string `json:"body"`
		Head   string `json:"head"`
		Hat    string `json:"hat"`
		Shield string `json:"shield"`
		Sword  string `json:"sword"`
	}
	if json.Unmarshal(raw, &msg) != nil {
		return
	}
	globalHub.mu.Lock()
	c.state.Body = msg.Body
	c.state.Head = msg.Head
	c.state.Hat = msg.Hat
	c.state.Shield = msg.Shield
	c.state.Sword = msg.Sword
	globalHub.mu.Unlock()
	dbSaveCosmetics(c.userID, msg.Body, msg.Head, msg.Hat, msg.Shield, msg.Sword)
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

	// Server-side commands (not broadcast as chat).
	lower := strings.ToLower(txt)
	if strings.HasPrefix(lower, "/giveitem ") {
		handleGiveItemCommand(c, txt)
		return
	}
	if strings.HasPrefix(lower, "/removeitem ") {
		handleRemoveItemCommand(c, txt)
		return
	}
	if lower == "/itemlist" {
		var names []string
		for id := range allItemDefs {
			names = append(names, id)
		}
		sendDirectMsg(c, "Items disponibles: "+strings.Join(names, ", "))
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
			"msg":      fmt.Sprintf("%s: I have nothing more to say for now... (%ds)", npc.state.Name, remaining),
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
		log.Printf("[COMBAT] %s killed %s", c.name, msg.NPCID)
	} else {
		log.Printf("[COMBAT] %s hit %s → HP %d", c.name, msg.NPCID, newHP)
	}
}

func handlePvPHit(attacker *Client, raw []byte) {
	var msg struct {
		TargetID string `json:"target_id"`
	}
	if json.Unmarshal(raw, &msg) != nil || msg.TargetID == "" {
		return
	}

	// Proximity validation (within 120px)
	const maxReach = 120.0
	var target *Client
	globalHub.mu.RLock()
	for c := range globalHub.clients {
		if c.playerID == msg.TargetID {
			dx := c.state.X - attacker.state.X
			dy := c.state.Y - attacker.state.Y
			if dx*dx+dy*dy <= maxReach*maxReach {
				target = c
			}
			break
		}
	}
	globalHub.mu.RUnlock()

	if target == nil {
		return
	}

	// Apply damage server-side.
	globalHub.mu.Lock()
	if target.state.HP > 0 {
		target.state.HP--
	}
	targetHP := target.state.HP
	globalHub.mu.Unlock()

	data, _ := json.Marshal(map[string]interface{}{
		"type":   "pvp_damage",
		"from":   attacker.name,
		"damage": 1,
		"atk_x":  attacker.state.X,
		"atk_y":  attacker.state.Y,
		"hp":     targetHP,
	})
	select {
	case target.send <- data:
	default:
	}
	log.Printf("[PVP] %s hit %s → HP %d", attacker.name, target.name, targetHP)
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
	log.Printf("[MOUNT] %s mounted %s", c.name, msg.NPCID)
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
	// Respawn: restore HP when transitioning out of dead state.
	if c.state.AnimState == "dead" && msg.Anim != "dead" {
		c.state.HP = playerMaxHP
	}
	c.state.AnimState = msg.Anim
	globalHub.mu.Unlock()
}

// ──────────────────────────────────────────────────────────────
// Inventory / Item helpers
// ──────────────────────────────────────────────────────────────

// sendDirectMsg sends a system message only to one client.
func sendDirectMsg(c *Client, msg string) {
	data, _ := json.Marshal(map[string]string{"type": "system", "msg": msg})
	select {
	case c.send <- data:
	default:
	}
}

// sendInventoryTo builds and sends the player's current inventory.
func sendInventoryTo(c *Client) {
	rows := dbGetInventory(c.userID)
	items := make([]InventoryItem, 0, len(rows))
	for _, r := range rows {
		if def, ok := allItemDefs[r.ItemID]; ok {
			items = append(items, inventoryItemFromDef(def, r.Quantity))
		}
	}
	data, _ := json.Marshal(map[string]interface{}{
		"type":      "inventory_data",
		"inventory": items,
	})
	select {
	case c.send <- data:
	default:
	}
}

// handleGiveItemCommand processes "/giveitem <player> <item_id>" (admin only).
func handleGiveItemCommand(c *Client, txt string) {
	if !c.isAdmin {
		sendDirectMsg(c, "Permission refusée.")
		return
	}
	parts := strings.Fields(txt)
	if len(parts) < 3 {
		sendDirectMsg(c, "Usage: /giveitem <joueur> <item_id>")
		return
	}
	targetName := parts[1]
	itemID := parts[2]

	def, ok := allItemDefs[itemID]
	if !ok {
		sendDirectMsg(c, "Item inconnu: "+itemID)
		return
	}
	targetUserID, err := dbGetPlayerIDByName(targetName)
	if err != nil {
		sendDirectMsg(c, "Joueur introuvable: "+targetName)
		return
	}
	dbGiveItem(targetUserID, itemID, 1)

	// Refresh inventory for target if online.
	globalHub.mu.RLock()
	for cl := range globalHub.clients {
		if strings.EqualFold(cl.name, targetName) {
			go sendInventoryTo(cl)
			break
		}
	}
	globalHub.mu.RUnlock()

	sendDirectMsg(c, fmt.Sprintf("Item '%s' donné à %s.", def.Name, targetName))
	globalHub.broadcastSystem(fmt.Sprintf("%s a reçu %s !", targetName, def.Name))
	log.Printf("[ADMIN] %s gave '%s' to %s", c.name, itemID, targetName)
}

// handleRemoveItemCommand processes "/removeitem <player> <item_id>" (admin only).
func handleRemoveItemCommand(c *Client, txt string) {
	if !c.isAdmin {
		sendDirectMsg(c, "Permission denied.")
		return
	}
	parts := strings.Fields(txt)
	if len(parts) < 3 {
		sendDirectMsg(c, "Usage: /removeitem <player> <item_id>")
		return
	}
	targetName := parts[1]
	itemID := parts[2]

	if _, ok := allItemDefs[itemID]; !ok {
		sendDirectMsg(c, "Unknown item: "+itemID)
		return
	}
	targetUserID, err := dbGetPlayerIDByName(targetName)
	if err != nil {
		sendDirectMsg(c, "Player not found: "+targetName)
		return
	}
	if !dbRemoveItem(targetUserID, itemID) {
		sendDirectMsg(c, targetName+" does not have item: "+itemID)
		return
	}

	// Refresh inventory for target if online.
	globalHub.mu.RLock()
	for cl := range globalHub.clients {
		if strings.EqualFold(cl.name, targetName) {
			go sendInventoryTo(cl)
			break
		}
	}
	globalHub.mu.RUnlock()

	sendDirectMsg(c, fmt.Sprintf("Removed '%s' from %s.", itemID, targetName))
	log.Printf("[ADMIN] %s removed '%s' from %s", c.name, itemID, targetName)
}

// ──────────────────────────────────────────────────────────────
// Item use
// ──────────────────────────────────────────────────────────────

func handleUseItem(c *Client, raw []byte) {
	var msg struct {
		ItemID string `json:"item_id"`
	}
	if json.Unmarshal(raw, &msg) != nil || msg.ItemID == "" {
		return
	}
	def, ok := allItemDefs[msg.ItemID]
	if !ok {
		return
	}
	// Verify the player actually owns the item.
	rows := dbGetInventory(c.userID)
	hasItem := false
	for _, r := range rows {
		if r.ItemID == msg.ItemID && r.Quantity > 0 {
			hasItem = true
			break
		}
	}
	if !hasItem {
		return
	}
	// Update server anim state.
	globalHub.mu.Lock()
	c.state.AnimState = def.AnimState
	globalHub.mu.Unlock()

	// Broadcast immediately so other clients react without waiting for the
	// next 60 Hz state tick.
	globalHub.broadcast(map[string]interface{}{
		"type":      "use_item_ok",
		"player_id": c.playerID,
		"item_id":   msg.ItemID,
		"anim":      def.AnimState,
	})
	log.Printf("[ITEM] %s uses %s", c.name, def.Name)
}

// ──────────────────────────────────────────────────────────────
// Shop / world items
// ──────────────────────────────────────────────────────────────

func handleBuyWorldItem(c *Client, raw []byte) {
	var msg struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &msg) != nil || msg.ID == "" {
		return
	}

	globalHub.mu.RLock()
	var item *WorldSpawnItem
	for _, wi := range globalHub.worldItems {
		if wi.ID == msg.ID {
			item = wi
			break
		}
	}
	globalHub.mu.RUnlock()

	if item == nil || item.Price <= 0 {
		return
	}

	newTotal, err := dbDeductGralats(c.userID, item.Price)
	if err != nil {
		data, _ := json.Marshal(map[string]interface{}{
			"type":    "buy_result",
			"success": false,
			"msg":     "Pas assez de gralats !",
		})
		select {
		case c.send <- data:
		default:
		}
		return
	}

	globalHub.mu.Lock()
	c.state.Gralats = newTotal
	globalHub.mu.Unlock()

	if item.ItemID != "" {
		dbGiveItem(c.userID, item.ItemID, 1)
		go sendInventoryTo(c)
	}

	data, _ := json.Marshal(map[string]interface{}{
		"type":     "buy_result",
		"success":  true,
		"msg":      fmt.Sprintf("Acheté '%s' pour %d gralats !", item.Name, item.Price),
		"gralat_n": newTotal,
		"item_id":  item.ItemID,
	})
	select {
	case c.send <- data:
	default:
	}
	log.Printf("[SHOP] %s bought '%s' for %d G (balance: %d)", c.name, item.Name, item.Price, newTotal)
}

func handleAdminSpawnWorldItem(c *Client, raw []byte) {
	if !c.isAdmin {
		return
	}
	var msg struct {
		Name       string  `json:"name"`
		SpritePath string  `json:"sprite"`
		X          float64 `json:"x"`
		Y          float64 `json:"y"`
		Price      int     `json:"price"`
		ItemID     string  `json:"item_id"`
	}
	if json.Unmarshal(raw, &msg) != nil || strings.TrimSpace(msg.Name) == "" {
		return
	}

	id := fmt.Sprintf("wi_%d", time.Now().UnixMilli())
	wi := &WorldSpawnItem{
		ID:         id,
		Name:       strings.TrimSpace(msg.Name),
		SpritePath: msg.SpritePath,
		X:          msg.X,
		Y:          msg.Y,
		Price:      msg.Price,
		ItemID:     msg.ItemID,
	}
	globalHub.addWorldItem(wi)
	dbSaveWorldItem(worldItemDB{
		ID: id, Name: wi.Name, SpritePath: wi.SpritePath,
		X: wi.X, Y: wi.Y, Price: wi.Price, ItemID: wi.ItemID,
		MapName: defaultMap,
	})
	log.Printf("[ADMIN] %s spawned world item '%s' at (%.0f,%.0f)", c.name, wi.Name, wi.X, wi.Y)
}

func handleAdminRemoveWorldItem(c *Client, raw []byte) {
	if !c.isAdmin {
		return
	}
	var msg struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &msg) != nil || msg.ID == "" {
		return
	}
	globalHub.removeWorldItem(msg.ID)
	dbRemoveWorldItem(msg.ID)
	log.Printf("[ADMIN] %s removed world item %s", c.name, msg.ID)
}

package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text"
	"golang.org/x/image/font/basicfont"
)

const (
	screenW = 800
	screenH = 600

	tileSize  = 32
	mapTilesW = 30
	mapTilesH = 20

	worldW = mapTilesW * tileSize // 960
	worldH = mapTilesH * tileSize // 640
)

// Game implements ebiten.Game and acts as the top-level state machine.
type Game struct {
	state GameState

	// Assets
	bodyImg *ebiten.Image
	headImg *ebiten.Image

	// Menus
	mainMenu     *MainMenu
	loginMenu    *LoginMenu
	regMenu      *RegisterMenu
	cosmeticMenu *CosmeticMenu

	// Network (nil while disconnected)
	conn *Connection

	// Local player info
	localID      string
	localName    string
	localGralats int

	// World state
	localChar    *Character
	otherPlayers map[string]*Character
	npcs         map[string]*Character
	mu           sync.Mutex

	// Chat
	chat *Chat

	// TMX map
	gameMap        *GameMap
	currentMapName string
	prevMapName    string
	mapSwitchCooldown float64 // seconds before next map switch allowed

	// World gralat pickups (replicated from server)
	worldGralats []GralatPickup
	grMu         sync.Mutex

	// UI state
	signDialog   string     // text shown when reading a panneau
	npcDialog    string     // text from NPC conversation
	npcGralatN   int        // gralats received in NPC dialog
	nearNPCID    string     // ID of closest NPC within talk range
	nearNPCType  int        // NPCType of nearest NPC (-1 if none)
	profileOpen  bool       // local player profile (P key)
	viewedPlayer *Character // non-nil when inspecting another player's profile

	// Profile
	localPlaytime int
	sessionStart  time.Time
	previewImg    *ebiten.Image // 96×96 offscreen used for character previews

	// Push animation delay
	pushTimer float64

	// Noclip mode (disables tile collision for local player)
	noclip bool

	// Main panel menu (slides from top)
	panelMenu *PanelMenu

	// Cached last-sent state to reduce message rate
	lastSentX, lastSentY   float64
	lastSentDir            int
	lastSentMoving         bool
	lastSentAnim           string
	lastSentMounted        bool

	// Sword hit tracking (reset each new swing)
	swordHitSent bool

	lastUpdate time.Time
}

// NewGame initialises the Game. Assets may be nil (graceful fallback).
func NewGame(bodyImg, headImg, tilesImg *ebiten.Image) *Game {
	g := &Game{
		state:        StateMainMenu,
		bodyImg:      bodyImg,
		headImg:      headImg,
		mainMenu:     NewMainMenu(),
		loginMenu:    NewLoginMenu(),
		regMenu:      NewRegisterMenu(),
		cosmeticMenu: NewCosmeticMenu(),
		otherPlayers: make(map[string]*Character),
		npcs:         make(map[string]*Character),
		chat:         NewChat(),
		lastUpdate:   time.Now(),
	}

	// Load TMX map
	g.loadMap("GraalRebornMap.tmx", false)

	// Panel menu
	g.panelMenu = NewPanelMenu()

	// Emoji images
	loadEmojiImages()

	// Load gralat sprites
	loadGralatImage()

	// Offscreen image for character previews (reused each frame).
	g.previewImg = ebiten.NewImage(96, 96)

	return g
}

// ──────────────────────────────────────────────────────────────
// Ebiten interface
// ──────────────────────────────────────────────────────────────

func (g *Game) Update() error {
	now := time.Now()
	dt := now.Sub(g.lastUpdate).Seconds()
	g.lastUpdate = now
	if dt > 0.05 {
		dt = 0.05
	}

	switch g.state {
	case StateMainMenu:
		ns := g.mainMenu.Update()
		if g.mainMenu.WantsQuit() {
			return ebiten.Termination
		}
		if ns != StateMainMenu {
			g.state = ns
		}

	case StateLogin:
		ns := g.loginMenu.Update()
		if ns == StatePlaying {
			g.startGame(g.loginMenu.Token, g.loginMenu.Name)
			g.state = StatePlaying
		} else if ns != StateLogin {
			g.state = ns
		}

	case StateRegister:
		ns := g.regMenu.Update()
		if ns == StatePlaying {
			g.startGame(g.regMenu.Token, g.regMenu.Name)
			g.state = StatePlaying
		} else if ns != StateRegister {
			g.state = ns
		}

	case StatePlaying:
		if err := g.updatePlaying(dt); err != nil {
			return err
		}
	}
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	switch g.state {
	case StateMainMenu:
		g.mainMenu.Draw(screen)
	case StateLogin:
		g.loginMenu.Draw(screen)
	case StateRegister:
		g.regMenu.Draw(screen)
	case StatePlaying:
		g.drawPlaying(screen)
		g.cosmeticMenu.Draw(screen)
	}
}

func (g *Game) Layout(_, _ int) (int, int) {
	return screenW, screenH
}

// ──────────────────────────────────────────────────────────────
// Map helpers
// ──────────────────────────────────────────────────────────────

// worldSize returns current map dimensions in pixels.
func (g *Game) worldSize() (int, int) {
	if g.gameMap != nil {
		return g.gameMap.WorldW(), g.gameMap.WorldH()
	}
	return worldW, worldH
}

// loadMap loads a TMX file by name. If spawnAtExit is true and the map has
// exitmap tiles, the local player is teleported there; otherwise the player
// is placed at the world centre.
func (g *Game) loadMap(name string, spawnAtExit bool) {
	filename := name
	// append .tmx if missing
	if len(filename) < 4 || filename[len(filename)-4:] != ".tmx" {
		filename += ".tmx"
	}
	gm, err := LoadTMX(filename)
	if err != nil {
		fmt.Println("[MAP] failed to load", filename, ":", err)
		return
	}
	g.prevMapName = g.currentMapName
	g.currentMapName = filename
	g.gameMap = gm
	g.mapSwitchCooldown = 1.0 // 1 second grace period

	// Notify the server which map we're on so it can filter the state broadcast.
	if g.conn != nil {
		g.conn.SendJSON(map[string]string{"type": "change_map", "map": filename})
	}

	if g.localChar == nil {
		return
	}
	ww, wh := g.worldSize()
	if spawnAtExit {
		if ex, ey, ok := gm.ExitPos(); ok {
			g.localChar.X = ex - float64(frameW)/2
			g.localChar.Y = ey - float64(frameH)/2
			return
		}
	}
	g.localChar.X = float64(ww)/2 - float64(frameW)/2
	g.localChar.Y = float64(wh)/2 - float64(frameH)/2
}

// ──────────────────────────────────────────────────────────────
// Game start / stop
// ──────────────────────────────────────────────────────────────

func (g *Game) startGame(token, name string) {
	g.localName = name
	g.localID = ""
	g.localGralats = 0
	g.signDialog = ""
	g.npcDialog = ""
	g.profileOpen = false
	g.viewedPlayer = nil
	g.mu.Lock()
	g.otherPlayers = make(map[string]*Character)
	g.npcs = make(map[string]*Character)
	g.mu.Unlock()
	g.grMu.Lock()
	g.worldGralats = nil
	g.grMu.Unlock()

	ww, wh := g.worldSize()
	g.localChar = NewCharacter(g.bodyImg, g.headImg,
		float64(ww)/2, float64(wh)/2,
		name, false, 0)
	g.localChar.IsLocal = true

	g.chat = NewChat()
	g.chat.AddMessage("", "Welcome! [WASD] move  [X] sword  [R] mount  [F] interact  [P] profile", true)

	go func() {
		conn, err := Dial(getWSURL())
		if err != nil {
			g.chat.AddMessage("", "Connection error: "+err.Error(), true)
			return
		}
		g.conn = conn
		conn.SendJSON(map[string]string{"type": "auth", "token": token})
	}()
}

func (g *Game) disconnect() {
	if g.conn != nil {
		g.conn.Close()
		g.conn = nil
	}
}

func (g *Game) sendCosmetics() {
	if g.conn == nil {
		return
	}
	cm := g.cosmeticMenu
	if g.localChar != nil {
		g.localChar.SetCosmetics(cm.BodyFile(), cm.HeadFile(), cm.HatFile())
	}
	g.conn.SendJSON(map[string]string{
		"type": "cosmetic",
		"body": cm.BodyFile(),
		"head": cm.HeadFile(),
		"hat":  cm.HatFile(),
	})
}

// ──────────────────────────────────────────────────────────────
// Playing update
// ──────────────────────────────────────────────────────────────

func (g *Game) updatePlaying(dt float64) error {
	if g.localChar == nil {
		return nil
	}

	// Panel menu update — consumes mouse clicks when active.
	g.panelMenu.Update(dt)

	g.processNetwork()

	// Escape: close overlays in order before returning to menu.
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) && !g.chat.IsOpen {
		if g.signDialog != "" || g.npcDialog != "" {
			g.signDialog = ""
			g.npcDialog = ""
			return nil
		}
		if g.viewedPlayer != nil {
			g.viewedPlayer = nil
			return nil
		}
		if g.profileOpen {
			g.profileOpen = false
			return nil
		}
		g.disconnect()
		g.state = StateMainMenu
		g.mainMenu = NewMainMenu()
		g.loginMenu = NewLoginMenu()
		g.regMenu = NewRegisterMenu()
		return nil
	}

	// Profile toggle (P = own profile; clears viewed-player panel).
	if inpututil.IsKeyJustPressed(ebiten.KeyP) && !g.chat.IsOpen {
		g.profileOpen = !g.profileOpen
		g.viewedPlayer = nil
	}

	// Left-click: select another player to view their profile.
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) &&
		!g.chat.IsOpen && !g.cosmeticMenu.IsVisible() &&
		g.signDialog == "" && g.npcDialog == "" {
		g.handlePlayerClick()
	}

	// F key: interact with sign or NPC (or close dialog)
	if inpututil.IsKeyJustPressed(ebiten.KeyF) && !g.chat.IsOpen {
		if g.signDialog != "" || g.npcDialog != "" {
			g.signDialog = ""
			g.npcDialog = ""
		} else if g.gameMap != nil {
			if sign := g.gameMap.NearbySign(g.localChar.X, g.localChar.Y); sign != "" {
				g.signDialog = sign
			} else if g.nearNPCID != "" && g.nearNPCType != NPCTypeHorse && g.conn != nil {
				g.conn.SendJSON(map[string]string{
					"type":   "talk_npc",
					"npc_id": g.nearNPCID,
				})
			}
		}
	}

	// X key: sword swing
	if inpututil.IsKeyJustPressed(ebiten.KeyX) && !g.chat.IsOpen {
		if g.localChar.StartSword() {
			g.swordHitSent = false
			if g.conn != nil {
				g.conn.SendJSON(map[string]interface{}{
					"type": "anim_state",
					"anim": AnimSword,
				})
			}
		}
	}

	// R key: mount / dismount
	if inpututil.IsKeyJustPressed(ebiten.KeyR) && !g.chat.IsOpen {
		if g.localChar.Mounted {
			// Dismount
			if g.conn != nil {
				g.conn.SendJSON(map[string]string{"type": "dismount"})
			}
			g.localChar.Mounted = false
			g.localChar.AnimState = AnimIdle
		} else if g.nearNPCType == NPCTypeHorse && g.nearNPCID != "" && g.conn != nil {
			// Request mount
			g.conn.SendJSON(map[string]string{
				"type":   "mount_npc",
				"npc_id": g.nearNPCID,
			})
		}
	}

	// Cosmetic menu
	if g.cosmeticMenu.IsVisible() {
		g.cosmeticMenu.Update()
		if g.cosmeticMenu.TakeChanged() {
			g.sendCosmetics()
		}
		return nil
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyC) && !g.chat.IsOpen {
		g.cosmeticMenu.Open()
		return nil
	}

	// Chat
	if inpututil.IsKeyJustPressed(ebiten.KeyT) && !g.chat.IsOpen {
		g.chat.IsOpen = true
	}
	if msg, ok := g.chat.Update(); ok {
		g.handleChatMessage(msg)
	}

	// Movement (blocked while dialogs / chat / sitting are open)
	if !g.chat.IsOpen && g.signDialog == "" && g.npcDialog == "" {
		g.handleMovement(dt)
	}

	// Animate entities
	g.localChar.Update(dt)
	g.mu.Lock()
	for _, p := range g.otherPlayers {
		p.Update(dt)
	}
	for _, n := range g.npcs {
		n.Update(dt)
	}
	g.mu.Unlock()

	// Stale-pointer guard: clear viewedPlayer if they disconnected.
	if g.viewedPlayer != nil {
		g.mu.Lock()
		found := false
		for _, p := range g.otherPlayers {
			if p == g.viewedPlayer {
				found = true
				break
			}
		}
		g.mu.Unlock()
		if !found {
			g.viewedPlayer = nil
		}
	}

	// Detect nearest NPC only on the main map
	if g.currentMapName == "GraalRebornMap.tmx" || g.currentMapName == "" {
		g.nearNPCID, g.nearNPCType = g.nearestNPC()
	} else {
		g.nearNPCID, g.nearNPCType = "", -1
	}

	// Sword hit detection: fires once when the active phase begins
	if !g.swordHitSent && g.localChar.SwordJustActivated() {
		g.localChar.MarkSwordHitDone()
		g.swordHitSent = true
		g.checkSwordHit()
	}

	// Auto-collect gralats by walking over them
	g.checkGralatPickup()

	// Map transition cooldown
	if g.mapSwitchCooldown > 0 {
		g.mapSwitchCooldown -= dt
	}

	// Map switch triggers
	if g.mapSwitchCooldown <= 0 && g.gameMap != nil && g.localChar != nil {
		c := g.localChar
		if target := g.gameMap.SwitchmapAt(c.X, c.Y, float64(frameW), float64(frameH)); target != "" {
			g.loadMap(target, true)
		} else if g.gameMap.OnExitTile(c.X, c.Y, float64(frameW), float64(frameH)) && g.prevMapName != "" {
			prev := g.prevMapName
			g.loadMap(prev, true)
		}
	}

	// Sync AnimState changes to server (e.g. sword swing while standing still)
	if g.conn != nil && g.localChar.AnimState != g.lastSentAnim {
		g.lastSentAnim = g.localChar.AnimState
		g.conn.SendJSON(map[string]interface{}{
			"type":    "anim_state",
			"anim":    g.localChar.AnimState,
			"mounted": g.localChar.Mounted,
		})
	}

	return nil
}

func (g *Game) handleMovement(dt float64) {
	c := g.localChar
	// Block all movement while swinging sword or dead.
	if c.AnimState == AnimSword || c.AnimState == AnimDead {
		c.Moving = false
		return
	}

	anyKey := ebiten.IsKeyPressed(ebiten.KeyUp) || ebiten.IsKeyPressed(ebiten.KeyW) ||
		ebiten.IsKeyPressed(ebiten.KeyDown) || ebiten.IsKeyPressed(ebiten.KeyS) ||
		ebiten.IsKeyPressed(ebiten.KeyLeft) || ebiten.IsKeyPressed(ebiten.KeyA) ||
		ebiten.IsKeyPressed(ebiten.KeyRight) || ebiten.IsKeyPressed(ebiten.KeyD)

	// Cancel sit when any movement key is pressed.
	if c.AnimState == AnimSit && anyKey {
		c.AnimState = AnimIdle
	}
	// Sitting blocks movement.
	if c.AnimState == AnimSit {
		c.Moving = false
		return
	}

	c.Moving = false
	dx, dy := 0.0, 0.0

	if ebiten.IsKeyPressed(ebiten.KeyUp) || ebiten.IsKeyPressed(ebiten.KeyW) {
		dy = -1; c.Dir = 0; c.Moving = true
	}
	if ebiten.IsKeyPressed(ebiten.KeyDown) || ebiten.IsKeyPressed(ebiten.KeyS) {
		dy = 1; c.Dir = 2; c.Moving = true
	}
	if ebiten.IsKeyPressed(ebiten.KeyLeft) || ebiten.IsKeyPressed(ebiten.KeyA) {
		dx = -1; c.Dir = 1; c.Moving = true
	}
	if ebiten.IsKeyPressed(ebiten.KeyRight) || ebiten.IsKeyPressed(ebiten.KeyD) {
		dx = 1; c.Dir = 3; c.Moving = true
	}

	if dx != 0 && dy != 0 {
		dx *= 0.7071
		dy *= 0.7071
	}

	// Mounted players move faster
	speed := moveSpeed
	if c.Mounted {
		speed = mountedMoveSpeed
	}

	newX := c.X + dx*speed*dt
	newY := c.Y + dy*speed*dt

	// Tile collision: move axes independently (wall sliding).
	// Track whether the primary movement direction is blocked for push anim.
	pushedIntoWall := false
	if g.gameMap != nil && !g.noclip {
		blockedX := dx != 0 && g.gameMap.IsBlocked(newX, c.Y, float64(frameW), float64(frameH))
		blockedY := dy != 0 && g.gameMap.IsBlocked(c.X, newY, float64(frameW), float64(frameH))

		if !blockedX {
			c.X = newX
		}
		if !blockedY {
			c.Y = newY
		}

		// Push animation: primary direction is blocked.
		// Primary direction is whichever axis has more input (or the only one).
		if c.Moving {
			if math.Abs(dx) >= math.Abs(dy) {
				pushedIntoWall = blockedX
			} else {
				pushedIntoWall = blockedY
			}
		}
	} else {
		c.X = newX
		c.Y = newY
	}

	// Update push/walk state (don't touch AnimSword or AnimRide).
	if c.AnimState != AnimRide {
		if pushedIntoWall {
			g.pushTimer += dt
			if g.pushTimer >= 1.5 {
				c.AnimState = AnimPush
			}
		} else {
			g.pushTimer = 0
			if c.AnimState == AnimPush {
				c.AnimState = AnimIdle
			}
		}
	}

	// Clamp to world bounds
	if c.X < 0 {
		c.X = 0
	}
	if c.Y < 0 {
		c.Y = 0
	}
	ww, wh := g.worldSize()
	if c.X > float64(ww-frameW) {
		c.X = float64(ww - frameW)
	}
	if c.Y > float64(wh-frameH) {
		c.Y = float64(wh - frameH)
	}

	if g.conn != nil && (c.X != g.lastSentX || c.Y != g.lastSentY ||
		c.Dir != g.lastSentDir || c.Moving != g.lastSentMoving ||
		c.Mounted != g.lastSentMounted) {
		g.conn.SendJSON(map[string]interface{}{
			"type":    "move",
			"x":       c.X,
			"y":       c.Y,
			"dir":     c.Dir,
			"moving":  c.Moving,
			"anim":    c.AnimState,
			"mounted": c.Mounted,
		})
		g.lastSentX, g.lastSentY = c.X, c.Y
		g.lastSentDir = c.Dir
		g.lastSentMoving = c.Moving
		g.lastSentMounted = c.Mounted
	}
}

// handlePlayerClick converts the cursor to world coords and selects the clicked player.
// Clicking own character opens own profile; clicking another player opens their profile.
func (g *Game) handlePlayerClick() {
	if g.localChar == nil {
		return
	}
	mx, my := ebiten.CursorPosition()
	camX, camY := g.camera()
	wx := float64(mx) + camX
	wy := float64(my) + camY

	// Hitbox is slightly larger than the sprite for easier clicking.
	const pad = 8.0

	// Check own character first.
	lx, ly := g.localChar.X, g.localChar.Y
	if wx >= lx-pad && wx < lx+float64(frameW)+pad &&
		wy >= ly-pad && wy < ly+float64(frameH)+pad {
		g.profileOpen = true
		g.viewedPlayer = nil
		return
	}

	// Check other players.
	g.mu.Lock()
	var hit *Character
	for _, p := range g.otherPlayers {
		if wx >= p.X-pad && wx < p.X+float64(frameW)+pad &&
			wy >= p.Y-pad && wy < p.Y+float64(frameH)+pad {
			hit = p
			break
		}
	}
	g.mu.Unlock()

	if hit != nil {
		g.viewedPlayer = hit
		g.profileOpen = false // close own profile when viewing another's
	} else {
		g.viewedPlayer = nil
	}
}

// nearestNPC returns the ID and NPCType of the closest NPC within 48px.
func (g *Game) nearestNPC() (id string, npcType int) {
	if g.localChar == nil {
		return "", -1
	}
	px := g.localChar.X + float64(frameW)/2
	py := g.localChar.Y + float64(frameH)/2
	const radius = 48.0
	g.mu.Lock()
	defer g.mu.Unlock()
	bestDist := radius * radius
	bestID := ""
	bestType := -1
	for nid, npc := range g.npcs {
		dx := npc.X + float64(frameW)/2 - px
		dy := npc.Y + float64(frameH)/2 - py
		d2 := dx*dx + dy*dy
		if d2 <= bestDist {
			bestDist = d2
			bestID = nid
			bestType = npc.NPCType
		}
	}
	return bestID, bestType
}

// checkSwordHit tests the local player's sword hitbox against all NPCs and sends
// a sword_hit message to the server for each overlapping target.
func (g *Game) checkSwordHit() {
	if g.conn == nil || g.localChar == nil {
		return
	}
	hitbox := g.localChar.SwordHitbox()
	if hitbox.Empty() {
		return
	}

	g.mu.Lock()
	var hits []string
	for id, npc := range g.npcs {
		if npc.NPCType == NPCTypeHorse || npc.HP <= 0 {
			continue
		}
		npcRect := image.Rect(int(npc.X), int(npc.Y), int(npc.X)+frameW, int(npc.Y)+frameH)
		if hitbox.Overlaps(npcRect) {
			hits = append(hits, id)
		}
	}
	g.mu.Unlock()

	for _, id := range hits {
		g.conn.SendJSON(map[string]string{
			"type":   "sword_hit",
			"npc_id": id,
		})
	}
}

// checkGralatPickup auto-collects any gralat the player stands on.
func (g *Game) checkGralatPickup() {
	if g.localChar == nil || g.conn == nil {
		return
	}
	px := g.localChar.X + float64(frameW)/2
	py := g.localChar.Y + float64(frameH)/2
	const radius = 18.0

	g.grMu.Lock()
	remaining := g.worldGralats[:0]
	toCollect := ""
	for _, gr := range g.worldGralats {
		dx := gr.X - px
		dy := gr.Y - py
		if toCollect == "" && dx*dx+dy*dy <= radius*radius {
			toCollect = gr.ID
		} else {
			remaining = append(remaining, gr)
		}
	}
	if toCollect != "" {
		g.worldGralats = remaining
	}
	g.grMu.Unlock()

	if toCollect != "" {
		g.conn.SendJSON(map[string]string{
			"type":      "collect_gralat",
			"gralat_id": toCollect,
		})
	}
}

// ──────────────────────────────────────────────────────────────
// Network message processing
// ──────────────────────────────────────────────────────────────

func (g *Game) processNetwork() {
	if g.conn == nil {
		return
	}
	for {
		data, ok := g.conn.TryReceive()
		if !ok {
			break
		}
		g.handleServerMsg(data)
	}
}

func (g *Game) handleServerMsg(data []byte) {
	var msg ServerMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	switch msg.Type {
	case "auth_ok":
		g.localID = msg.ID
		g.localGralats = msg.GralatN
		g.localPlaytime = msg.Playtime
		g.sessionStart = time.Now()
		if g.localChar != nil {
			g.localChar.X, g.localChar.Y = msg.X, msg.Y
			g.localChar.TargetX, g.localChar.TargetY = msg.X, msg.Y
		}
		// Restore saved cosmetics from server, then broadcast.
		if msg.Body != "" || msg.Head != "" || msg.Hat != "" {
			g.cosmeticMenu.SetByFilenames(msg.Body, msg.Head, msg.Hat)
		}
		g.sendCosmetics()
		// Tell the server which map we're currently on.
		if g.currentMapName != "" && g.conn != nil {
			g.conn.SendJSON(map[string]string{"type": "change_map", "map": g.currentMapName})
		}
		g.chat.AddMessage("", fmt.Sprintf("Connected as %s", msg.Name), true)

	case "auth_error":
		g.chat.AddMessage("", "Auth error: "+msg.Msg, true)
		g.disconnect()
		g.state = StateMainMenu

	case "state":
		g.mu.Lock()
		// Remote players
		seen := make(map[string]bool)
		for _, p := range msg.Players {
			if p.ID == g.localID {
				continue
			}
			seen[p.ID] = true
			if ch, ok := g.otherPlayers[p.ID]; ok {
				ch.TargetX, ch.TargetY = p.X, p.Y
				ch.Dir = p.Dir
				ch.Moving = p.Moving
				ch.Gralats = p.Gralats
				ch.Playtime = p.Playtime
				ch.SetCosmetics(p.Body, p.Head, p.Hat)
				// Sync mounted / ride state
				ch.Mounted = p.Mounted
				if p.Mounted || p.AnimState == AnimRide {
					ch.AnimState = AnimRide
					ch.Mounted = true
				} else if ch.AnimState == AnimRide && !p.Mounted {
					ch.AnimState = AnimIdle
					ch.Mounted = false
				}
				// Trigger sword animation on remote player
				if p.AnimState == AnimSword && ch.AnimState != AnimSword {
					ch.StartSword()
				}
				// Sync sit state
				if p.AnimState == AnimSit {
					ch.AnimState = AnimSit
				} else if ch.AnimState == AnimSit && p.AnimState != AnimSit {
					ch.AnimState = AnimIdle
				}
			} else {
				ch := NewCharacter(g.bodyImg, g.headImg, p.X, p.Y, p.Name, false, 0)
				ch.Gralats = p.Gralats
				ch.Playtime = p.Playtime
				ch.Mounted = p.Mounted
				if p.Mounted {
					ch.AnimState = AnimRide
				}
				ch.SetCosmetics(p.Body, p.Head, p.Hat)
				g.otherPlayers[p.ID] = ch
			}
		}
		for id := range g.otherPlayers {
			if !seen[id] {
				delete(g.otherPlayers, id)
			}
		}
		// NPCs
		seenNPC := make(map[string]bool)
		for _, n := range msg.NPCs {
			seenNPC[n.ID] = true
			if ch, ok := g.npcs[n.ID]; ok {
				ch.TargetX, ch.TargetY = n.X, n.Y
				ch.Dir = n.Dir
				ch.Moving = n.Moving
				ch.HP = n.HP
				ch.MaxHP = n.MaxHP
				if n.AnimState == "dead" && ch.AnimState != AnimDead {
					ch.AnimState = AnimDead
				} else if n.AnimState == "" && ch.AnimState == AnimDead {
					// Respawned — reset to idle.
					ch.AnimState = AnimIdle
				}
			} else {
				ch := NewCharacter(g.bodyImg, g.headImg, n.X, n.Y, n.Name, true, n.NPCType)
				ch.HP = n.HP
				ch.MaxHP = n.MaxHP
				if n.AnimState == "dead" {
					ch.AnimState = AnimDead
				}
				g.npcs[n.ID] = ch
			}
		}
		for id := range g.npcs {
			if !seenNPC[id] {
				delete(g.npcs, id)
			}
		}
		g.mu.Unlock()

		// World gralats
		g.grMu.Lock()
		g.worldGralats = msg.Gralats
		g.grMu.Unlock()

	case "chat":
		g.chat.AddMessage(msg.From, msg.Msg, false)
		// Show chat + emoji bubble above the sender's head.
		g.mu.Lock()
		for _, p := range g.otherPlayers {
			if p.Name == msg.From {
				p.SetChatMsg(msg.Msg)
				if code := containsEmoji(msg.Msg); code != "" {
					if img := emojiImageFor(code); img != nil {
						p.SetEmoji(img)
					}
				}
				break
			}
		}
		g.mu.Unlock()

	case "system":
		g.chat.AddMessage("", msg.Msg, true)

	case "gralat_update":
		g.localGralats = msg.GralatN

	case "npc_dialog":
		g.npcDialog = msg.Msg
		g.npcGralatN = msg.GralatN
		if msg.GralatN > 0 {
			g.localGralats += msg.GralatN
		}

	case "npc_damage":
		// Update the local NPC HP display immediately (before next state broadcast)
		g.mu.Lock()
		if npc, ok := g.npcs[msg.NPCID]; ok {
			npc.HP = msg.HP
			if msg.Killed {
				npc.AnimState = AnimDead
			}
		}
		g.mu.Unlock()
		if msg.Killed {
			g.chat.AddMessage("", "You defeated an enemy!", true)
		}

	case "mount_ok":
		if g.localChar != nil {
			g.localChar.Mounted = true
			g.localChar.AnimState = AnimRide
		}

	case "dismount_ok":
		if g.localChar != nil {
			g.localChar.Mounted = false
			g.localChar.AnimState = AnimIdle
		}
	}
}

// ──────────────────────────────────────────────────────────────
// Rendering
// ──────────────────────────────────────────────────────────────

func (g *Game) drawPlaying(screen *ebiten.Image) {
	if g.localChar == nil {
		screen.Fill(color.RGBA{12, 12, 22, 255})
		text.Draw(screen, "Connecting to server...", basicfont.Face7x13, 300, 300, color.White)
		return
	}

	camX, camY := g.camera()

	// Map
	if g.gameMap != nil {
		g.gameMap.Draw(screen, camX, camY)
	} else {
		screen.Fill(color.RGBA{40, 70, 40, 255})
	}

	// World gralat pickups
	g.drawWorldGralats(screen, camX, camY)

	// NPCs only exist on the main world map.
	onMainMap := g.currentMapName == "GraalRebornMap.tmx" || g.currentMapName == ""

	// Entities
	g.mu.Lock()
	if onMainMap {
		for _, n := range g.npcs {
			n.Draw(screen, camX, camY)
		}
	}
	for _, p := range g.otherPlayers {
		p.Draw(screen, camX, camY)
	}
	g.mu.Unlock()

	g.localChar.Draw(screen, camX, camY)

	// Interaction prompts (only when no dialog is open)
	if g.signDialog == "" && g.npcDialog == "" && !g.profileOpen {
		if g.gameMap != nil {
			g.gameMap.DrawSignPrompts(screen, camX, camY)
		}
		if onMainMap {
			g.drawNPCPrompt(screen, camX, camY)
		}
	}

	// HUD
	g.drawHUD(screen)
	g.chat.Draw(screen)
	g.panelMenu.Draw(screen)

	// Overlays
	if g.signDialog != "" {
		g.drawDialog(screen, g.signDialog, 0)
	}
	if g.npcDialog != "" {
		g.drawDialog(screen, g.npcDialog, g.npcGralatN)
	}
	if g.profileOpen {
		g.drawProfile(screen)
	}
	if g.viewedPlayer != nil {
		g.drawViewedProfile(screen)
	}
}

// drawViewedProfile shows a rich read-only profile panel for another player.
func (g *Game) drawViewedProfile(screen *ebiten.Image) {
	p := g.viewedPlayer
	if p == nil {
		return
	}
	const (
		pw = 480
		ph = 300
		px = screenW/2 - pw/2
		py = screenH/2 - ph/2
	)
	DrawPanel(screen, px, py, pw, ph)

	title := "PLAYER PROFILE"
	DrawBigText(screen, title, px+(pw-BigTextW(title))/2+2, py+16, colGoldDim)
	DrawBigText(screen, title, px+(pw-BigTextW(title))/2, py+14, colGold)
	DrawHDivider(screen, px+10, py+44, pw-20)

	// ── Left: text info (px+16 … px+240) ────────────────────────
	infoX := px + 18
	row := py + 66

	// Name
	DrawText(screen, "Name", infoX, row, colGoldDim)
	DrawText(screen, p.Name, infoX, row+fontH+3, colTextWhite)
	row += fontH*2 + 14

	// Gralats with icon
	DrawText(screen, "Fortune", infoX, row, colGoldDim)
	row += fontH + 3
	spr := gralatSprite(1)
	if spr != nil {
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(0.75, 0.75)
		op.GeoM.Translate(float64(infoX), float64(row-fontH+1))
		screen.DrawImage(spr, op)
	}
	DrawText(screen, fmt.Sprintf("%d gralats", p.Gralats), infoX+26, row, colGold)
	row += fontH + 14

	// Playtime
	DrawText(screen, "Playtime", infoX, row, colGoldDim)
	DrawText(screen, formatPlaytime(p.Playtime), infoX, row+fontH+3, colTextWhite)
	row += fontH*2 + 14

	// Status: Online
	DrawText(screen, "Status", infoX, row, colGoldDim)
	DrawRect(screen, infoX, row+fontH+3, 8, 8, color.RGBA{40, 220, 80, 255})
	DrawText(screen, "Online", infoX+12, row+fontH+11, colTextOK)

	// ── Vertical divider ─────────────────────────────────────────
	DrawRect(screen, px+248, py+50, 1, ph-70, colBorderMid)
	DrawRect(screen, px+249, py+50, 1, ph-70, colBorderHL)

	// ── Right: character preview (px+258 … px+476) ───────────────
	previewScale := 2.5
	previewW := int(96 * previewScale)
	previewH := int(96 * previewScale)
	previewAreaW := pw - 260
	previewAreaH := ph - 70
	previewX := float64(px+258) + float64(previewAreaW-previewW)/2
	previewY := float64(py+54) + float64(previewAreaH-previewH)/2
	p.DrawPreview(screen, g.previewImg, previewX, previewY, previewScale)

	hint := "[Esc] Close"
	DrawText(screen, hint, px+(pw-len(hint)*fontW)/2, py+ph-10, colTextDim)
}

func (g *Game) camera() (camX, camY float64) {
	cww, cwh := g.worldSize()

	// If the map is narrower/shorter than the screen, centre it.
	if cww <= screenW {
		camX = -(float64(screenW) - float64(cww)) / 2
	} else {
		camX = g.localChar.X + float64(frameW)/2 - float64(screenW)/2
		if camX < 0 {
			camX = 0
		}
		if camX > float64(cww-screenW) {
			camX = float64(cww - screenW)
		}
	}
	if cwh <= screenH {
		camY = -(float64(screenH) - float64(cwh)) / 2
	} else {
		camY = g.localChar.Y + float64(frameH)/2 - float64(screenH)/2
		if camY < 0 {
			camY = 0
		}
		if camY > float64(cwh-screenH) {
			camY = float64(cwh - screenH)
		}
	}
	return
}

// drawWorldGralats renders each active gralat pickup.
func (g *Game) drawWorldGralats(screen *ebiten.Image, camX, camY float64) {
	g.grMu.Lock()
	gralats := make([]GralatPickup, len(g.worldGralats))
	copy(gralats, g.worldGralats)
	g.grMu.Unlock()

	for _, gr := range gralats {
		sx := gr.X - camX
		sy := gr.Y - camY
		if sx < -32 || sx > float64(screenW)+32 || sy < -32 || sy > float64(screenH)+32 {
			continue
		}
		// Gentle bob animation
		bob := math.Sin(float64(time.Now().UnixMilli())/400.0+gr.X/60.0) * 3.0
		sy += bob

		spr := gralatSprite(gr.Value)
		if spr != nil {
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(sx-float64(spr.Bounds().Dx())/2, sy-float64(spr.Bounds().Dy())/2)
			screen.DrawImage(spr, op)
		} else {
			// Fallback: gold dot
			DrawRect(screen, int(sx)-6, int(sy)-6, 12, 12, colGold)
		}
	}
}

// drawNPCPrompt shows an interaction hint above the nearest NPC if in range.
func (g *Game) drawNPCPrompt(screen *ebiten.Image, camX, camY float64) {
	// Mounted player: show dismount hint in HUD area instead
	if g.localChar != nil && g.localChar.Mounted {
		lbl := "[R] Dismount"
		bw := len(lbl)*fontW + 12
		bx := screenW/2 - bw/2
		DrawRect(screen, bx, screenH-44, bw, 16, color.RGBA{0, 0, 0, 160})
		DrawText(screen, lbl, bx+6, screenH-30, colGold)
		return
	}

	if g.nearNPCID == "" {
		return
	}
	g.mu.Lock()
	npc, ok := g.npcs[g.nearNPCID]
	if !ok {
		g.mu.Unlock()
		return
	}
	sx := npc.X - camX
	sy := npc.Y - camY
	npcType := npc.NPCType
	g.mu.Unlock()

	var lbl string
	if npcType == NPCTypeHorse {
		lbl = "[R] Mount"
	} else {
		lbl = "[F] Talk"
	}
	DrawText(screen, lbl,
		int(sx)+frameW/2-len(lbl)*fontW/2,
		int(sy)-30, colGold)
}

// drawDialog draws a Zelda-style dialog box at the bottom of the screen.
func (g *Game) drawDialog(screen *ebiten.Image, msg string, gralatN int) {
	const (
		px = 30
		py = screenH - 130
		pw = screenW - 60
		ph = 110
	)
	DrawPanel(screen, px, py, pw, ph)

	// Word-wrap the message
	maxChars := (pw - 24) / fontW
	lines := wordWrap(msg, maxChars)
	for i, line := range lines {
		if i >= 4 {
			break
		}
		DrawText(screen, line, px+12, py+22+i*(fontH+4), colTextWhite)
	}

	// Gralat reward
	if gralatN > 0 {
		reward := fmt.Sprintf("+%d gralat(s)!", gralatN)
		DrawText(screen, reward, px+12, py+ph-22, colGold)
	}

	hint := "[F] or [Esc] to close"
	DrawText(screen, hint,
		px+pw-len(hint)*fontW-10,
		py+ph-8, colTextDim)
}

// drawProfile draws the local player's own profile overlay.
func (g *Game) drawProfile(screen *ebiten.Image) {
	const (
		pw = 480
		ph = 300
		px = screenW/2 - pw/2
		py = screenH/2 - ph/2
	)
	DrawPanel(screen, px, py, pw, ph)

	title := "MY PROFILE"
	DrawBigText(screen, title, px+(pw-BigTextW(title))/2+2, py+16, colGoldDim)
	DrawBigText(screen, title, px+(pw-BigTextW(title))/2, py+14, colGold)
	DrawHDivider(screen, px+10, py+44, pw-20)

	// ── Left: text info ───────────────────────────────────────────
	infoX := px + 18
	row := py + 66

	// Name
	DrawText(screen, "Name", infoX, row, colGoldDim)
	DrawText(screen, g.localName, infoX, row+fontH+3, colTextWhite)
	row += fontH*2 + 14

	// Gralats
	DrawText(screen, "Fortune", infoX, row, colGoldDim)
	row += fontH + 3
	spr := gralatSprite(1)
	if spr != nil {
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(0.75, 0.75)
		op.GeoM.Translate(float64(infoX), float64(row-fontH+1))
		screen.DrawImage(spr, op)
	}
	DrawText(screen, fmt.Sprintf("%d gralats", g.localGralats), infoX+26, row, colGold)
	row += fontH + 14

	// Live playtime (saved + current session)
	totalPlaytime := g.localPlaytime + int(time.Since(g.sessionStart).Seconds())
	DrawText(screen, "Playtime", infoX, row, colGoldDim)
	DrawText(screen, formatPlaytime(totalPlaytime), infoX, row+fontH+3, colTextWhite)
	row += fontH*2 + 14

	// Status
	DrawText(screen, "Status", infoX, row, colGoldDim)
	DrawRect(screen, infoX, row+fontH+3, 8, 8, color.RGBA{40, 220, 80, 255})
	DrawText(screen, "Online", infoX+12, row+fontH+11, colTextOK)

	// ── Vertical divider ─────────────────────────────────────────
	DrawRect(screen, px+248, py+50, 1, ph-70, colBorderMid)
	DrawRect(screen, px+249, py+50, 1, ph-70, colBorderHL)

	// ── Right: character preview ──────────────────────────────────
	if g.localChar != nil {
		previewScale := 2.5
		previewW := int(96 * previewScale)
		previewH := int(96 * previewScale)
		previewAreaW := pw - 260
		previewAreaH := ph - 70
		previewX := float64(px+258) + float64(previewAreaW-previewW)/2
		previewY := float64(py+54) + float64(previewAreaH-previewH)/2
		g.localChar.DrawPreview(screen, g.previewImg, previewX, previewY, previewScale)
	}

	hint := "[P] or [Esc] Close"
	DrawText(screen, hint, px+(pw-len(hint)*fontW)/2, py+ph-10, colTextDim)
}

// formatPlaytime formats a duration in seconds as "Xh Ym Zs".
func formatPlaytime(seconds int) string {
	if seconds <= 0 {
		return "0s"
	}
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	if h > 0 {
		return fmt.Sprintf("%dh %02dm %02ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func (g *Game) drawHUD(screen *ebiten.Image) {
	// Top-left: player name
	DrawRect(screen, 0, 0, 230, 20, color.RGBA{0, 0, 0, 150})
	DrawText(screen, "Player: "+g.localName, 5, 14, color.RGBA{200, 220, 255, 255})

	// Top-right: player/NPC count
	g.mu.Lock()
	pCount := len(g.otherPlayers) + 1
	nCount := len(g.npcs)
	g.mu.Unlock()
	info := fmt.Sprintf("Players: %d  NPCs: %d", pCount, nCount)
	DrawRect(screen, screenW-len(info)*fontW-12, 0, len(info)*fontW+12, 20, color.RGBA{0, 0, 0, 150})
	DrawText(screen, info, screenW-len(info)*fontW-7, 14, color.RGBA{200, 220, 255, 255})

	// Gralat count (top-centre)
	gralatStr := fmt.Sprintf("G: %d", g.localGralats)
	gw := len(gralatStr)*fontW + 28
	gx := screenW/2 - gw/2
	DrawRect(screen, gx, 0, gw, 20, color.RGBA{0, 0, 0, 150})
	spr := gralatSprite(1)
	if spr != nil {
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(0.55, 0.55)
		op.GeoM.Translate(float64(gx+4), 1)
		screen.DrawImage(spr, op)
	}
	DrawText(screen, gralatStr, gx+20, 14, colGold)

	// Bottom-left: position
	if g.localChar != nil {
		posStr := fmt.Sprintf("X:%.0f Y:%.0f", g.localChar.X, g.localChar.Y)
		DrawText(screen, posStr, 5, screenH-8, colTextDim)
	}

	// Connection indicator
	if g.conn == nil || g.conn.IsClosed() {
		DrawRect(screen, screenW/2-60, 0, 120, 20, color.RGBA{180, 0, 0, 200})
		DrawText(screen, "DISCONNECTED", screenW/2-6*fontW, 14, color.RGBA{255, 200, 200, 255})
	}
}

// ──────────────────────────────────────────────────────────────
// Asset loader
// ──────────────────────────────────────────────────────────────

func loadAssets() (body, head, tiles *ebiten.Image) {
	var err error
	from := func(path string) *ebiten.Image {
		img, _, e := ebitenutil.NewImageFromFile(path)
		err = e
		if e != nil {
			fmt.Println("[ASSETS]", path, "not found, fallback rendering active")
		}
		return img
	}
	body = from("assets/character.png")
	head = from("assets/head.png")
	tiles = from("assets/tiles.png")
	_ = err
	return
}

// ──────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────

// handleChatMessage processes a submitted chat line.
// Commands start with "/" or ":"; everything else is sent to the server.
func (g *Game) handleChatMessage(msg string) {
	lower := strings.ToLower(strings.TrimSpace(msg))

	// /noclip — toggle collision
	if lower == "/noclip" {
		g.noclip = !g.noclip
		status := "OFF"
		if g.noclip {
			status = "ON"
		}
		g.chat.AddMessage("", "Noclip "+status, true)
		return
	}

	// /sit or :sit — toggle sit animation
	if lower == "/sit" || lower == ":sit" {
		if g.localChar == nil {
			return
		}
		if g.localChar.AnimState == AnimSit {
			g.localChar.AnimState = AnimIdle
		} else {
			g.localChar.AnimState = AnimSit
		}
		if g.conn != nil {
			g.conn.SendJSON(map[string]interface{}{
				"type": "anim_state",
				"anim": g.localChar.AnimState,
			})
		}
		return
	}

	// Regular chat message
	if g.conn == nil {
		return
	}
	g.conn.SendJSON(map[string]string{"type": "chat", "msg": msg})
	if g.localChar != nil {
		g.localChar.SetChatMsg(msg)
		// Trigger emoji bubble if the message contains an emoji shortcode.
		if code := containsEmoji(msg); code != "" {
			if img := emojiImageFor(code); img != nil {
				g.localChar.SetEmoji(img)
			}
		}
	}
}

// wordWrap splits text into lines of at most maxChars characters.
func wordWrap(s string, maxChars int) []string {
	words := strings.Fields(s)
	var lines []string
	current := ""
	for _, w := range words {
		if current == "" {
			current = w
		} else if utf8.RuneCountInString(current)+1+utf8.RuneCountInString(w) <= maxChars {
			current += " " + w
		} else {
			lines = append(lines, current)
			current = w
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

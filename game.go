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
	gameMap *GameMap

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
	if gm, err := LoadTMX("test2.tmx"); err == nil {
		g.gameMap = gm
	} else {
		fmt.Println("[MAP] test2.tmx:", err)
	}

	// Load gralat sprites
	loadGralatImage()

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

	g.localChar = NewCharacter(g.bodyImg, g.headImg,
		float64(worldW)/2, float64(worldH)/2,
		name, false, 0)
	g.localChar.IsLocal = true

	g.chat = NewChat()
	g.chat.AddMessage("", "Bienvenue! [ZQSD] deplacer  [X] epee  [R] monture  [F] interagir  [P] profil", true)

	go func() {
		conn, err := Dial(getWSURL())
		if err != nil {
			g.chat.AddMessage("", "Erreur connexion: "+err.Error(), true)
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
		if g.conn != nil {
			g.conn.SendJSON(map[string]string{"type": "chat", "msg": msg})
		}
	}

	// Movement (blocked while dialogs / chat are open)
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

	// Detect nearest NPC for interaction prompt
	g.nearNPCID, g.nearNPCType = g.nearestNPC()

	// Sword hit detection: fires once when the active phase begins
	if !g.swordHitSent && g.localChar.SwordJustActivated() {
		g.localChar.MarkSwordHitDone()
		g.swordHitSent = true
		g.checkSwordHit()
	}

	// Auto-collect gralats by walking over them
	g.checkGralatPickup()

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
	// Don't change moving state while swinging sword
	if c.AnimState != AnimSword {
		c.Moving = false
	}
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
	if g.gameMap != nil {
		if !g.gameMap.IsBlocked(newX, c.Y, float64(frameW), float64(frameH)) {
			c.X = newX
		}
		if !g.gameMap.IsBlocked(c.X, newY, float64(frameW), float64(frameH)) {
			c.Y = newY
		}
	} else {
		c.X = newX
		c.Y = newY
	}

	// Clamp to world bounds
	if c.X < 0 {
		c.X = 0
	}
	if c.Y < 0 {
		c.Y = 0
	}
	if c.X > float64(worldW-frameW) {
		c.X = float64(worldW - frameW)
	}
	if c.Y > float64(worldH-frameH) {
		c.Y = float64(worldH - frameH)
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
func (g *Game) handlePlayerClick() {
	if g.localChar == nil {
		return
	}
	mx, my := ebiten.CursorPosition()
	camX, camY := g.camera()
	wx := float64(mx) + camX
	wy := float64(my) + camY

	g.mu.Lock()
	var hit *Character
	for _, p := range g.otherPlayers {
		if wx >= p.X && wx < p.X+float64(frameW) &&
			wy >= p.Y && wy < p.Y+float64(frameH) {
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
		if g.localChar != nil {
			g.localChar.X, g.localChar.Y = msg.X, msg.Y
			g.localChar.TargetX, g.localChar.TargetY = msg.X, msg.Y
		}
		g.chat.AddMessage("", fmt.Sprintf("Connecte en tant que %s", msg.Name), true)

	case "auth_error":
		g.chat.AddMessage("", "Erreur d'auth: "+msg.Msg, true)
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
			} else {
				ch := NewCharacter(g.bodyImg, g.headImg, p.X, p.Y, p.Name, false, 0)
				ch.Gralats = p.Gralats
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
			} else {
				ch := NewCharacter(g.bodyImg, g.headImg, n.X, n.Y, n.Name, true, n.NPCType)
				ch.HP = n.HP
				ch.MaxHP = n.MaxHP
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
		}
		g.mu.Unlock()
		if msg.Killed {
			g.chat.AddMessage("", fmt.Sprintf("Vous avez vaincu un ennemi!", ), true)
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
		text.Draw(screen, "Connexion au serveur...", basicfont.Face7x13, 300, 300, color.White)
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

	// Entities
	g.mu.Lock()
	for _, n := range g.npcs {
		n.Draw(screen, camX, camY)
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
		g.drawNPCPrompt(screen, camX, camY)
	}

	// HUD
	g.drawHUD(screen)
	g.chat.Draw(screen)

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

// drawViewedProfile shows a read-only profile panel for another player.
func (g *Game) drawViewedProfile(screen *ebiten.Image) {
	p := g.viewedPlayer
	if p == nil {
		return
	}
	const (
		pw = 280
		ph = 160
		px = screenW/2 - pw/2
		py = screenH/2 - ph/2
	)
	DrawPanel(screen, px, py, pw, ph)

	title := "PROFIL JOUEUR"
	DrawBigText(screen, title, px+(pw-BigTextW(title))/2+2, py+16, colGoldDim)
	DrawBigText(screen, title, px+(pw-BigTextW(title))/2, py+14, colGold)
	DrawHDivider(screen, px+10, py+42, pw-20)

	DrawText(screen, "Joueur: "+p.Name, px+16, py+62, colTextWhite)

	spr := gralatSprite(1)
	if spr != nil {
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(0.8, 0.8)
		op.GeoM.Translate(float64(px+16), float64(py+78))
		screen.DrawImage(spr, op)
	}
	DrawText(screen, fmt.Sprintf("Gralats: %d", p.Gralats), px+44, py+94, colGold)

	hint := "[Echap] ou clic ailleurs Fermer"
	DrawText(screen, hint, px+(pw-len(hint)*fontW)/2, py+ph-10, colTextDim)
}

func (g *Game) camera() (camX, camY float64) {
	camX = g.localChar.X + float64(frameW)/2 - float64(screenW)/2
	camY = g.localChar.Y + float64(frameH)/2 - float64(screenH)/2

	maxX := float64(worldW - screenW)
	maxY := float64(worldH - screenH)
	if camX < 0 {
		camX = 0
	}
	if camY < 0 {
		camY = 0
	}
	if camX > maxX {
		camX = maxX
	}
	if camY > maxY {
		camY = maxY
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
		lbl := "[R] Descendre"
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
		lbl = "[R] Monter"
	} else {
		lbl = "[F] Parler"
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

	hint := "[F] ou [Echap] pour fermer"
	DrawText(screen, hint,
		px+pw-len(hint)*fontW-10,
		py+ph-8, colTextDim)
}

// drawProfile draws the local player profile overlay.
func (g *Game) drawProfile(screen *ebiten.Image) {
	const (
		pw = 280
		ph = 160
		px = screenW/2 - pw/2
		py = screenH/2 - ph/2
	)
	DrawPanel(screen, px, py, pw, ph)

	title := "PROFIL"
	DrawBigText(screen, title, px+(pw-BigTextW(title))/2+2, py+16, colGoldDim)
	DrawBigText(screen, title, px+(pw-BigTextW(title))/2, py+14, colGold)
	DrawHDivider(screen, px+10, py+42, pw-20)

	DrawText(screen, "Joueur: "+g.localName, px+16, py+62, colTextWhite)

	// Gralat count with sprite
	spr := gralatSprite(1)
	if spr != nil {
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(0.8, 0.8)
		op.GeoM.Translate(float64(px+16), float64(py+78))
		screen.DrawImage(spr, op)
	}
	DrawText(screen, fmt.Sprintf("Gralats: %d", g.localGralats), px+44, py+94, colGold)

	hint := "[P] ou [Echap] Fermer"
	DrawText(screen, hint, px+(pw-len(hint)*fontW)/2, py+ph-10, colTextDim)
}

func (g *Game) drawHUD(screen *ebiten.Image) {
	// Top-left: player name
	DrawRect(screen, 0, 0, 230, 20, color.RGBA{0, 0, 0, 150})
	DrawText(screen, "Joueur: "+g.localName, 5, 14, color.RGBA{200, 220, 255, 255})

	// Top-right: player/NPC count
	g.mu.Lock()
	pCount := len(g.otherPlayers) + 1
	nCount := len(g.npcs)
	g.mu.Unlock()
	info := fmt.Sprintf("Joueurs: %d  NPCs: %d", pCount, nCount)
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
		DrawText(screen, posStr, 5, screenH-22, colTextDim)
	}

	// Bottom centre: controls
	hint := "[ZQSD] Deplacer  [X] Epee  [R] Monture  [T] Chat  [F] Interagir  [P] Profil  [C] Look  [Echap] Menu"
	DrawText(screen, hint, screenW/2-utf8.RuneCountInString(hint)*fontW/2, screenH-8,
		color.RGBA{90, 95, 130, 160})

	// Connection indicator
	if g.conn == nil || g.conn.IsClosed() {
		DrawRect(screen, screenW/2-60, 0, 120, 20, color.RGBA{180, 0, 0, 200})
		DrawText(screen, "DECONNECTE", screenW/2-5*fontW, 14, color.RGBA{255, 200, 200, 255})
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
			fmt.Println("[ASSETS]", path, "non trouve, rendu de secours actif")
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

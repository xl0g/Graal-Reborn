package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
)

const (
	screenW = 800
	screenH = 600

	tileSize  = 16 // GraalRebornMap.tmx tile size in pixels
	mapTilesW = 70 // map width in tiles
	mapTilesH = 70 // map height in tiles

	worldW = mapTilesW * tileSize // 1120
	worldH = mapTilesH * tileSize // 1120
)

// Game implements ebiten.Game and acts as the top-level state machine.
type Game struct {
	state GameState

	// Assets
	bodyImg *ebiten.Image
	headImg *ebiten.Image

	// Menus
	mainMenu      *MainMenu
	loginMenu     *LoginMenu
	regMenu       *RegisterMenu
	cosmeticMenu  *CosmeticMenu
	inventoryMenu *InventoryMenu
	adminMenu     *AdminMenu

	// Network (nil while disconnected)
	conn *Connection

	// Local player info
	localID        string
	localName      string
	localGralats   int
	localInventory []InventoryItem
	isAdmin        bool

	// World state
	localChar    *Character
	otherPlayers map[string]*Character
	npcs         map[string]*Character
	mu           sync.Mutex

	// Chat
	chat *Chat

	// TMX map
	gameMap           *GameMap
	currentMapName    string
	prevMapName       string
	mapSwitchCooldown float64

	// World gralat pickups (replicated from server)
	worldGralats []GralatPickup
	grMu         sync.Mutex

	// World items (admin-spawned, replicated from server)
	worldItems      []WorldSpawnItem
	nearWorldItem   *WorldSpawnItem
	worldItemDialog *WorldSpawnItem // item currently shown in the click popup

	// Sprite cache for world-item sprites
	wiSpriteCache  map[string]*ebiten.Image
	tapToBuyImg    *ebiten.Image

	// UI state
	signDialog   string
	npcDialog    string
	npcGralatN   int
	nearNPCID    string
	nearNPCType  int
	profileOpen  bool
	viewedPlayer *Character

	// Profile
	localPlaytime int
	sessionStart  time.Time
	previewImg    *ebiten.Image

	// Push animation delay
	pushTimer float64

	// Noclip mode (disables tile collision for local player)
	noclip bool

	// Main panel menu (slides from top)
	panelMenu *PanelMenu

	// Cached last-sent state to reduce message rate
	lastSentX, lastSentY float64
	lastSentDir          int
	lastSentMoving       bool
	lastSentAnim         string
	lastSentMounted      bool

	// Sword hit tracking (reset each new swing)
	swordHitSent bool

	// Grab state
	grabBtnHeld bool // true while the virtual grab button is held by mouse

	// Player HP (PvP)
	localHP    int
	localMaxHP int
	hpCircles  [3]*ebiten.Image

	// Virtual action button images
	grabKeyImg  *ebiten.Image
	swordKeyImg *ebiten.Image

	lastUpdate time.Time
}

// NewGame initialises the Game. Assets may be nil (graceful fallback).
func NewGame(bodyImg, headImg, tilesImg *ebiten.Image) *Game {
	g := &Game{
		state:         StateMainMenu,
		bodyImg:       bodyImg,
		headImg:       headImg,
		mainMenu:      NewMainMenu(),
		loginMenu:     NewLoginMenu(),
		regMenu:       NewRegisterMenu(),
		cosmeticMenu:  NewCosmeticMenu(),
		inventoryMenu: NewInventoryMenu(),
		adminMenu:     NewAdminMenu(),
		otherPlayers:  make(map[string]*Character),
		npcs:          make(map[string]*Character),
		chat:          NewChat(),
		lastUpdate:    time.Now(),
		wiSpriteCache: make(map[string]*ebiten.Image),
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

	// Pre-render HP circle sprites (full, half, empty).
	g.hpCircles = makeHPCircles()

	// Tap-to-buy icon (shown above priced world items when nearby)
	g.tapToBuyImg, _, _ = ebitenutil.NewImageFromFile(
		"Assets/offline/levels/images/dcicon/dcicon_taptobuy0.png")

	// Virtual action button images
	g.grabKeyImg, _, _ = ebitenutil.NewImageFromFile(
		"Assets/offline/levels/images/classiciphone/classiciphone_virtualkey_glove_blue.png")
	g.swordKeyImg, _, _ = ebitenutil.NewImageFromFile(
		"Assets/offline/levels/images/classiciphone/classiciphone_virtualkey_sword_blue.png")

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

func (g *Game) worldSize() (int, int) {
	if g.gameMap != nil {
		return g.gameMap.WorldW(), g.gameMap.WorldH()
	}
	return worldW, worldH
}

func (g *Game) loadMap(name string, spawnAtExit bool) {
	filename := name
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
	g.mapSwitchCooldown = 1.0

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
	g.localInventory = nil
	g.isAdmin = false
	g.signDialog = ""
	g.npcDialog = ""
	g.profileOpen = false
	g.viewedPlayer = nil
	g.nearWorldItem = nil
	g.mu.Lock()
	g.otherPlayers = make(map[string]*Character)
	g.npcs = make(map[string]*Character)
	g.worldItems = nil
	g.mu.Unlock()
	g.grMu.Lock()
	g.worldGralats = nil
	g.grMu.Unlock()
	if g.inventoryMenu != nil {
		g.inventoryMenu.Close()
	}
	if g.adminMenu != nil {
		g.adminMenu.Close()
	}

	ww, wh := g.worldSize()
	g.localChar = NewCharacter(g.bodyImg, g.headImg,
		float64(ww)/2, float64(wh)/2,
		name, false, 0)
	g.localChar.IsLocal = true

	g.localHP = 6
	g.localMaxHP = 6

	g.chat = NewChat()
	g.chat.AddMessage("", "Welcome! [WASD/ZQSD] move  [X] sword  [A] grab  [R] mount  [F] interact  [P] profile", true)

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
		g.localChar.SetCosmetics(cm.BodyFile(), cm.HeadFile(), cm.HatFile(), cm.ShieldFile(), cm.SwordFile())
	}
	g.conn.SendJSON(map[string]string{
		"type":   "cosmetic",
		"body":   cm.BodyFile(),
		"head":   cm.HeadFile(),
		"hat":    cm.HatFile(),
		"shield": cm.ShieldFile(),
		"sword":  cm.SwordFile(),
	})
}

// ──────────────────────────────────────────────────────────────
// Asset loader (called from main.go)
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


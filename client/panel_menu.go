package main

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// ── Layout constants ──────────────────────────────────────────────────────────

const (
	pmW      = screenW     // full screen width
	pmH      = screenH / 3 // top third of screen = 200px
	pmX      = 0
	pmY      = 0
	pmTitleH = 22
	pmBotH   = 22

	pmCols     = 10                    // 10 icons in one row
	pmMarginH  = 20                    // left/right margin inside panel
	pmSlotW    = (pmW - 2*pmMarginH) / pmCols // ~76px per slot
	pmIconSize = 46                    // icon image size
	pmAnimSpd  = 10.0
)

// menuEntry defines one slot in the grid.
type menuEntry struct {
	label   string
	imgPath string
}

var menuEntries = []menuEntry{
	{"Maps", "Assets/offline/levels/images/dc/dc_menuicons_zoom.png"},
	{"News", "Assets/offline/levels/images/dc/dc_menuicons_news.png"},
	{"Shop", "Assets/offline/levels/images/dc/dc_menuicons_shop.png"},
	{"Friends", "Assets/offline/levels/images/dc/dc_menuicons_friends.png"},
	{"Guilds", "Assets/offline/levels/images/dc/dc_menuicons_guilds.png"},
	{"Housing", "Assets/offline/levels/images/dc/dc_menuicons_housing.png"},
	{"Scores", "Assets/offline/levels/images/dc/dc_menuicons_leaderboards.png"},
	{"PM", "Assets/offline/levels/images/dc/dc_menuicons_pmhistory.png"},
	{"Inventory", ""},
	{"Keys", ""},
}

// ── Map entries ──────────────────────────────────────────────────────────────

type mapEntry struct {
	name  string
	file  string
	desc  string
	isGMap bool
}

var mapEntries = []mapEntry{
	// ── TMX maps ──
	{"GraalReborn City", "GraalRebornMap.tmx", "Main map", false},
	{"City Entry", "GraalCityEntry.tmx", "Entry", false},
	{"Interior", "interior1.tmx", "Interior", false},
	// ── GMAP worlds ──
	{"Balamb Island", "balambisland.gmap", "World", true},
	{"Balamb Island 2", "balambisland2.gmap", "World", true},
	{"Classic iPhone", "classiciphone.gmap", "World", true},
	{"Newcastle", "newcastle_main.gmap", "World", true},
	{"Underground Caves", "destiny_undergroundcaves.gmap", "World", true},
	{"Rail Cave", "railsystemcave.gmap", "World", true},
	{"Test Fort", "testfort4.gmap", "World", true},
	{"Cliff Climber", "event_cliffclimber.gmap", "Event", true},
}

// ── PanelMenu ─────────────────────────────────────────────────────────────────

// PanelMenu is the GraalOnline-style slide-down overlay (full width, top third).
type PanelMenu struct {
	progress float64 // 0 = hidden, 1 = fully open
	isOpen   bool

	openBtnImg *ebiten.Image
	icons      []*ebiten.Image // one per menuEntries slot

	// Active sub-panel: "" | "News" | "Keys" | "Maps" | "Inventory" | ...
	activeSub string

	// RequestMap is set when the player selects a TMX map.
	RequestMap string

	// RequestGMap is set when the player selects a GMAP world.
	RequestGMap string

	// RequestInventory is set when the player clicks the Inventory icon.
	RequestInventory bool

	// Maps panel scroll offset (in entries)
	mapsScroll int
}

func NewPanelMenu() *PanelMenu {
	m := &PanelMenu{
		icons: make([]*ebiten.Image, len(menuEntries)),
	}
	m.openBtnImg, _, _ = ebitenutil.NewImageFromFile(
		"Assets/offline/levels/images/classiciphone/classiciphone_friendsbutton_blue.png")

	for i, e := range menuEntries {
		if e.imgPath != "" {
			img, _, _ := ebitenutil.NewImageFromFile(e.imgPath)
			m.icons[i] = img
		}
	}
	return m
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m *PanelMenu) Update(dt float64) {
	// Animate slide
	target := 0.0
	if m.isOpen {
		target = 1.0
	}
	diff := target - m.progress
	if math.Abs(diff) < 0.005 {
		m.progress = target
	} else {
		m.progress += diff * pmAnimSpd * dt
		if m.progress < 0 {
			m.progress = 0
		}
		if m.progress > 1 {
			m.progress = 1
		}
	}

	if !inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		return
	}
	mx, my := ebiten.CursorPosition()

	// ── Open/close button (always active) ────────────────────────
	obx, oby, obw, obh := m.openBtnRect()
	if mx >= obx && mx < obx+obw && my >= oby && my < oby+obh {
		m.isOpen = !m.isOpen
		if !m.isOpen {
			m.activeSub = ""
		}
		return
	}

	if m.progress < 0.5 {
		return
	}

	panelTop := m.panelTop()
	panelBot := panelTop + pmH

	// ── Sub-panel clicks (BELOW main panel) ──────────────────────
	// Must be checked before the main-panel bounds guard.
	if m.activeSub != "" && my >= panelBot {
		m.handleSubClick(mx, my, panelBot)
		return
	}

	// ── Main panel bounds guard ───────────────────────────────────
	if my < panelTop || my >= panelBot {
		return
	}

	// ── Back / close button ───────────────────────────────────────
	backX := pmX + 10
	backY := panelBot - pmBotH + 3
	if mx >= backX && mx < backX+70 && my >= backY && my < backY+pmBotH-6 {
		m.isOpen = false
		m.activeSub = ""
		return
	}

	// ── Icon grid ─────────────────────────────────────────────────
	for i := range menuEntries {
		ix, iy := m.iconPos(i)
		if mx >= ix && mx < ix+pmIconSize && my >= iy && my < iy+pmIconSize {
			label := menuEntries[i].label
			if label == "Inventory" {
				m.RequestInventory = true
				m.isOpen = false
				m.activeSub = ""
				return
			}
			if m.activeSub == label {
				m.activeSub = ""
			} else {
				m.activeSub = label
			}
			return
		}
	}
}

func (m *PanelMenu) handleSubClick(mx, my, subY int) {
	if m.activeSub != "Maps" {
		return
	}

	// Scroll arrows
	upX, upY, upW, upH := mapsScrollUpRect(subY)
	if mx >= upX && mx < upX+upW && my >= upY && my < upY+upH {
		if m.mapsScroll > 0 {
			m.mapsScroll--
		}
		return
	}
	dnX, dnY, dnW, dnH := mapsScrollDownRect(subY)
	if mx >= dnX && mx < dnX+dnW && my >= dnY && my < dnY+dnH {
		if m.mapsScroll < len(mapEntries)-mapsVisible {
			m.mapsScroll++
		}
		return
	}

	for slot := 0; slot < mapsVisible; slot++ {
		idx := m.mapsScroll + slot
		if idx >= len(mapEntries) {
			break
		}
		bx, by, bw, bh := mapBtnRect(subY, slot)
		if mx >= bx && mx < bx+bw && my >= by && my < by+bh {
			e := mapEntries[idx]
			if e.isGMap {
				m.RequestGMap = e.file
			} else {
				m.RequestMap = e.file
			}
			m.isOpen = false
			m.activeSub = ""
			return
		}
	}
}

// ── Draw ──────────────────────────────────────────────────────────────────────

func (m *PanelMenu) Draw(screen *ebiten.Image) {
	m.drawOpenButton(screen)

	if m.progress <= 0.01 {
		return
	}

	panelTop := m.panelTop()
	alpha := uint8(math.Min(255, m.progress*400))

	// Shadow
	DrawRect(screen, pmX+3, panelTop+3, pmW, pmH, color.RGBA{0, 0, 0, uint8(float64(alpha) * 0.35)})

	// Background
	DrawRect(screen, pmX, panelTop, pmW, pmH, color.RGBA{195, 215, 240, alpha})
	DrawRect(screen, pmX, panelTop+pmTitleH, pmW, pmH/3, color.RGBA{215, 232, 252, alpha})

	// Title bar
	DrawRect(screen, pmX, panelTop, pmW, pmTitleH, color.RGBA{55, 95, 175, alpha})
	DrawRect(screen, pmX, panelTop, pmW, 2, color.RGBA{120, 165, 230, alpha})
	DrawText(screen, "GraalReborn",
		pmX+10, panelTop+pmTitleH-6,
		color.RGBA{255, 255, 255, alpha})

	if m.progress < 0.5 {
		return
	}

	// Icons
	for i, e := range menuEntries {
		ix, iy := m.iconPos(i)
		active := m.activeSub == e.label

		bg := color.RGBA{90, 130, 195, 200}
		if active {
			bg = color.RGBA{50, 95, 215, 230}
		}
		DrawRect(screen, ix+2, iy, pmIconSize-4, pmIconSize, bg)
		DrawRect(screen, ix, iy+2, pmIconSize, pmIconSize-4, bg)
		DrawRect(screen, ix+2, iy, pmIconSize-4, 2, color.RGBA{170, 205, 255, 200})

		if m.icons[i] != nil {
			iw := m.icons[i].Bounds().Dx()
			ih := m.icons[i].Bounds().Dy()
			scale := float64(pmIconSize-8) / float64(max(iw, ih))
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Scale(scale, scale)
			op.GeoM.Translate(
				float64(ix)+float64(pmIconSize)/2-float64(iw)*scale/2,
				float64(iy)+float64(pmIconSize)/2-float64(ih)*scale/2,
			)
			op.ColorScale.ScaleAlpha(float32(alpha) / 255)
			screen.DrawImage(m.icons[i], op)
		} else {
			DrawText(screen, "?",
				ix+pmIconSize/2-fontW/2, iy+pmIconSize/2+fontH/2,
				color.RGBA{255, 255, 255, alpha})
		}

		lbl := e.label
		lblX := ix + pmSlotW/2 - len(lbl)*fontW/2
		lblY := iy + pmIconSize + fontH
		DrawText(screen, lbl, lblX, lblY, color.RGBA{25, 35, 75, alpha})
	}

	// Bottom bar
	botY := panelTop + pmH - pmBotH
	DrawRect(screen, pmX, botY, pmW, pmBotH, color.RGBA{45, 75, 155, alpha})
	DrawRect(screen, pmX, botY, pmW, 2, color.RGBA{100, 145, 215, alpha})
	DrawRect(screen, pmX+10, botY+8, 8, 8, color.RGBA{220, 55, 55, alpha})
	DrawText(screen, "Close", pmX+22, botY+pmBotH-6, color.RGBA{255, 255, 255, alpha})

	// Sub-panel
	subY := panelTop + pmH
	switch m.activeSub {
	case "Keys":
		m.drawSubPanel(screen, subY, "Controls", []string{
			"ZQSD / Arrows    Move",
			"X                Sword",
			"A (AZERTY)       Grab (hold)",
			"R                Mount / Dismount",
			"F                Interact / Read sign / Buy",
			"T                Chat",
			"C                Change look",
			"P                Profile",
			"I                Inventory",
			"Tab              Admin menu (admins only)",
			"Esc              Close / Main menu",
			"/sit             Sit down",
			"/noclip          Noclip",
			"/giveitem        Give item (admin)",
			"/itemlist        List items",
			"/removeitem      Remove item from player (admin)",
		})
	case "News":
		m.drawSubPanel(screen, subY, "News", []string{
			"Welcome to GraalReborn!",
			"",
			"No updates yet.",
		})
	case "Maps":
		m.drawMapsPanel(screen, subY, alpha)
	default:
		if m.activeSub != "" {
			m.drawSubPanel(screen, subY, m.activeSub, []string{"Coming soon..."})
		}
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// panelTop returns the Y coordinate of the panel top based on slide progress.
// Slides from -pmH (above screen) to 0 (top of screen).
func (m *PanelMenu) panelTop() int {
	return int(float64(-pmH) * (1 - m.progress))
}

// iconPos returns the top-left pixel of icon slot i (absolute screen coords).
func (m *PanelMenu) iconPos(i int) (x, y int) {
	col := i % pmCols
	row := i / pmCols

	panelTop := m.panelTop()
	contentTop := panelTop + pmTitleH + 6

	// Centre icon within its slot
	slotX := pmX + pmMarginH + col*pmSlotW
	iconOffX := (pmSlotW - pmIconSize) / 2

	labelH := fontH + 3
	rowH := pmIconSize + labelH + 6
	return slotX + iconOffX, contentTop + row*rowH
}

func (m *PanelMenu) openBtnRect() (x, y, w, h int) {
	bw, bh := 40, 40
	if m.openBtnImg != nil {
		bw = m.openBtnImg.Bounds().Dx()
		bh = m.openBtnImg.Bounds().Dy()
		if bw > 48 {
			bh = bh * 48 / bw
			bw = 48
		}
	}
	return screenW - bw - 8, screenH - bh - 8, bw, bh
}

func (m *PanelMenu) drawOpenButton(screen *ebiten.Image) {
	bx, by, bw, _ := m.openBtnRect()
	if m.openBtnImg != nil {
		origW := m.openBtnImg.Bounds().Dx()
		scale := float64(bw) / float64(origW)
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(scale, scale)
		op.GeoM.Translate(float64(bx), float64(by))
		screen.DrawImage(m.openBtnImg, op)
	} else {
		DrawRect(screen, bx, by, 40, 40, color.RGBA{40, 70, 160, 220})
		DrawText(screen, "M", bx+16, by+24, colTextWhite)
	}
}

func (m *PanelMenu) drawSubPanel(screen *ebiten.Image, topY int, title string, lines []string) {
	pw := pmW
	ph := 18 + len(lines)*(fontH+4) + 10
	DrawRect(screen, 0, topY, pw, ph, color.RGBA{175, 198, 238, 245})
	DrawRect(screen, 0, topY, pw, 18, color.RGBA{55, 95, 175, 245})
	DrawRect(screen, 0, topY, pw, 2, color.RGBA{120, 165, 230, 245})
	DrawText(screen, title, pw/2-len(title)*fontW/2, topY+13, color.RGBA{255, 255, 255, 255})
	for i, l := range lines {
		DrawText(screen, l, 14, topY+18+i*(fontH+4)+fontH, color.RGBA{25, 35, 75, 255})
	}
}

// ── Maps sub-panel ────────────────────────────────────────────────────────────

const (
	mapBtnW    = 440
	mapBtnH    = 34
	mapBtnGap  = 5
	mapTitleH  = 22
	mapArrowH  = 18
	mapsVisible = 5 // max entries shown at once
)

func mapBtnRect(subY, slot int) (x, y, w, h int) {
	bx := screenW/2 - mapBtnW/2
	by := subY + mapTitleH + 4 + slot*(mapBtnH+mapBtnGap)
	return bx, by, mapBtnW, mapBtnH
}

func mapsScrollUpRect(subY int) (x, y, w, h int) {
	_, lastBy, _, lastBh := mapBtnRect(subY, mapsVisible-1)
	return screenW/2 - mapBtnW/2, lastBy + lastBh + 6, mapBtnW/2 - 4, mapArrowH
}

func mapsScrollDownRect(subY int) (x, y, w, h int) {
	_, lastBy, _, lastBh := mapBtnRect(subY, mapsVisible-1)
	return screenW/2 + 4, lastBy + lastBh + 6, mapBtnW/2 - 4, mapArrowH
}

func mapsSubPanelH(subY int) int {
	_, upY, _, upH := mapsScrollUpRect(subY)
	return upY + upH + 8 - subY
}

func (m *PanelMenu) drawMapsPanel(screen *ebiten.Image, subY int, alpha uint8) {
	ph := mapsSubPanelH(subY)
	DrawRect(screen, 0, subY, pmW, ph, color.RGBA{175, 198, 238, alpha})
	DrawRect(screen, 0, subY, pmW, mapTitleH, color.RGBA{55, 95, 175, alpha})
	DrawRect(screen, 0, subY, pmW, 2, color.RGBA{120, 165, 230, alpha})
	title := "Choose a map"
	DrawText(screen, title, pmW/2-len(title)*fontW/2, subY+mapTitleH-5, color.RGBA{255, 255, 255, alpha})

	// Entries
	for slot := 0; slot < mapsVisible; slot++ {
		idx := m.mapsScroll + slot
		if idx >= len(mapEntries) {
			break
		}
		e := mapEntries[idx]
		bx, by, bw, bh := mapBtnRect(subY, slot)

		// Button background — blue for TMX, teal for GMAP
		var btnCol, topCol, arrowCol color.RGBA
		if e.isGMap {
			btnCol = color.RGBA{35, 110, 115, alpha}
			topCol = color.RGBA{80, 185, 195, alpha}
			arrowCol = color.RGBA{120, 240, 250, alpha}
		} else {
			btnCol = color.RGBA{75, 115, 195, alpha}
			topCol = color.RGBA{145, 185, 240, alpha}
			arrowCol = color.RGBA{255, 215, 70, alpha}
		}
		DrawRect(screen, bx, by, bw, bh, btnCol)
		DrawRect(screen, bx, by, bw, 2, topCol)

		// Badge: [TMX] or [GMAP]
		badge := "[TMX]"
		if e.isGMap {
			badge = "[GMAP]"
		}
		DrawText(screen, badge, bx+8, by+bh-7, arrowCol)

		nameX := bx + len(badge)*fontW + 14
		DrawText(screen, e.name, nameX, by+bh-7, color.RGBA{255, 255, 255, alpha})

		descX := bx + bw - len(e.desc)*fontW - 8
		DrawText(screen, e.desc, descX, by+bh-7, color.RGBA{195, 215, 255, alpha})
	}

	// Scroll arrows (only when needed)
	if len(mapEntries) > mapsVisible {
		upX, upY, upW, upH := mapsScrollUpRect(subY)
		dnX, dnY, dnW, dnH := mapsScrollDownRect(subY)

		upActive := m.mapsScroll > 0
		dnActive := m.mapsScroll < len(mapEntries)-mapsVisible

		upCol := color.RGBA{60, 60, 80, alpha}
		if upActive {
			upCol = color.RGBA{75, 115, 195, alpha}
		}
		DrawRect(screen, upX, upY, upW, upH, upCol)
		DrawText(screen, "▲ Up", upX+upW/2-2*fontW, upY+upH-4, color.RGBA{255, 255, 255, alpha})

		dnCol := color.RGBA{60, 60, 80, alpha}
		if dnActive {
			dnCol = color.RGBA{75, 115, 195, alpha}
		}
		DrawRect(screen, dnX, dnY, dnW, dnH, dnCol)
		DrawText(screen, "▼ Down", dnX+dnW/2-3*fontW, dnY+dnH-4, color.RGBA{255, 255, 255, alpha})

		// Counter
		counter := fmt.Sprintf("%d/%d", m.mapsScroll+1, len(mapEntries)-mapsVisible+1)
		DrawText(screen, counter,
			pmW/2-len(counter)*fontW/2, upY+upH-4,
			color.RGBA{180, 200, 230, alpha})
	}
}

func inBtn(mx, my, bx, by, size int) bool {
	return mx >= bx && mx < bx+size && my >= by && my < by+size
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

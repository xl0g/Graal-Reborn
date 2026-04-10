package main

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// ── Layout constants ──────────────────────────────────────────────────────────

const (
	pmW        = 700 // panel width
	pmH        = 300 // panel height
	pmTitleH   = 30  // title bar height
	pmBotH     = 28  // bottom bar height
	pmCols     = 6   // icon columns
	pmIconSize = 64  // icon image size (scaled)
	pmIconPad  = 16  // horizontal gap between icons
	pmAnimSpd  = 6.0 // slide animation speed

	// derived
	pmX = (screenW - pmW) / 2
	pmY = (screenH - pmH) / 2
)

// menuEntry defines one slot in the grid.
type menuEntry struct {
	label   string
	imgPath string
	action  func(*PanelMenu) // nil = placeholder
}

var menuEntries = []menuEntry{
	{"News", "Assets/offline/levels/images/dc/dc_virtualkey_news_blue.png", nil},
	{"Shop", "Assets/offline/levels/images/dc/dc_virtualkey_shop_blue.png", nil},
	{"Friends", "Assets/offline/levels/images/dc/dc_virtualkey_friends_blue.png", nil},
	{"Guilds", "Assets/offline/levels/images/dc/dc_virtualkey_guilds_blue.png", nil},
	{"Housing", "Assets/offline/levels/images/dc/dc_virtualkey_housing_blue.png", nil},
	{"Scores", "Assets/offline/levels/images/dc/dc_virtualkey_leaderboards_blue.png", nil},
	{"PM History", "Assets/offline/levels/images/dc/dc_virtualkey_pmhistory_blue.png", nil},
	{"Feedback", "Assets/offline/levels/images/dc/dc_virtualkey_feedback_blue.png", nil},
	{"Keys", "", nil}, // shortcuts — no icon, drawn as text button
}

// ── PanelMenu ─────────────────────────────────────────────────────────────────

// PanelMenu is the GraalOnline-style slide-down overlay menu.
type PanelMenu struct {
	progress float64 // 0 = hidden, 1 = fully open
	isOpen   bool

	openBtnImg *ebiten.Image
	icons      []*ebiten.Image // one per menuEntries slot (nil = not loaded)

	// Active sub-panel: "" | "News" | "Keys"
	activeSub string
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

// Update handles animation and mouse input.
func (m *PanelMenu) Update(dt float64) {
	// Animate slide
	target := 0.0
	if m.isOpen {
		target = 1.0
	}
	diff := target - m.progress
	if math.Abs(diff) < 0.01 {
		m.progress = target
	} else {
		m.progress += diff * dt * pmAnimSpd * 60 * dt
		// Clamp
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

	// Open button
	obx, oby, obw, obh := m.openBtnRect()
	if mx >= obx && mx < obx+obw && my >= oby && my < oby+obh {
		m.isOpen = !m.isOpen
		if !m.isOpen {
			m.activeSub = ""
		}
		return
	}

	if m.progress < 0.8 {
		return
	}

	// Translate my into panel-relative coords
	panelTop := m.panelTop()
	rel := my - panelTop

	// Title bar click: drag (ignore) / close on double — skip for now
	if rel < 0 || rel > pmH {
		return
	}

	// Bottom bar: Back button
	backX := pmX + 10
	backY := panelTop + pmH - pmBotH + 4
	if mx >= backX && mx < backX+80 && my >= backY && my < backY+20 {
		m.isOpen = false
		m.activeSub = ""
		return
	}

	// Icon grid
	contentTop := panelTop + pmTitleH
	for i := range menuEntries {
		bx, by := m.iconPos(i, contentTop)
		if mx >= bx && mx < bx+pmIconSize && my >= by && my < by+pmIconSize {
			m.handleClick(i)
			return
		}
	}
}

func (m *PanelMenu) handleClick(i int) {
	label := menuEntries[i].label
	switch label {
	case "Keys":
		if m.activeSub == "Keys" {
			m.activeSub = ""
		} else {
			m.activeSub = "Keys"
		}
	case "News":
		if m.activeSub == "News" {
			m.activeSub = ""
		} else {
			m.activeSub = "News"
		}
	default:
		// Placeholder: toggle a generic info sub-panel
		if m.activeSub == label {
			m.activeSub = ""
		} else {
			m.activeSub = label
		}
	}
}

func (m *PanelMenu) Draw(screen *ebiten.Image) {
	m.drawOpenButton(screen)

	if m.progress <= 0 {
		return
	}

	panelTop := m.panelTop()
	contentTop := panelTop + pmTitleH
	alpha := uint8(math.Min(255, m.progress*300))

	// ── Shadow ────────────────────────────────────────────────────
	DrawRect(screen, pmX+4, panelTop+4, pmW, pmH, color.RGBA{0, 0, 0, uint8(float64(alpha) * 0.4)})

	// ── Background (light blue-white gradient simulated in two bands) ─
	DrawRect(screen, pmX, panelTop, pmW, pmH, color.RGBA{200, 218, 240, alpha})
	// slightly lighter upper third
	DrawRect(screen, pmX, panelTop+pmTitleH, pmW, pmH/3, color.RGBA{220, 235, 250, alpha})

	// ── Title bar ─────────────────────────────────────────────────
	DrawRect(screen, pmX, panelTop, pmW, pmTitleH, color.RGBA{60, 100, 180, alpha})
	// highlight line at top of title bar
	DrawRect(screen, pmX, panelTop, pmW, 2, color.RGBA{130, 170, 230, alpha})
	DrawText(screen, "GraalReborn",
		pmX+10, panelTop+pmTitleH-8,
		color.RGBA{255, 255, 255, alpha})

	if m.progress < 0.8 {
		return
	}

	// ── Icon grid ─────────────────────────────────────────────────
	for i, e := range menuEntries {
		bx, by := m.iconPos(i, contentTop)
		active := m.activeSub == e.label

		// Icon background (rounded-square look)
		bg := color.RGBA{100, 140, 200, 200}
		if active {
			bg = color.RGBA{60, 100, 220, 230}
		}
		// draw a slightly inset rounded rect (approximated with 2 rects)
		DrawRect(screen, bx+2, by, pmIconSize-4, pmIconSize, bg)
		DrawRect(screen, bx, by+2, pmIconSize, pmIconSize-4, bg)
		// highlight edge
		DrawRect(screen, bx+2, by, pmIconSize-4, 2, color.RGBA{180, 210, 255, 200})

		// Icon image
		if m.icons[i] != nil {
			iw := m.icons[i].Bounds().Dx()
			ih := m.icons[i].Bounds().Dy()
			scale := float64(pmIconSize-8) / float64(max(iw, ih))
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Scale(scale, scale)
			op.GeoM.Translate(
				float64(bx)+float64(pmIconSize)/2-float64(iw)*scale/2,
				float64(by)+float64(pmIconSize)/2-float64(ih)*scale/2,
			)
			op.ColorScale.ScaleAlpha(float32(alpha) / 255)
			screen.DrawImage(m.icons[i], op)
		} else {
			// Text fallback (Keys button)
			DrawText(screen, "?",
				bx+pmIconSize/2-fontW/2,
				by+pmIconSize/2+fontH/2,
				color.RGBA{255, 255, 255, alpha})
		}

		// Label below icon
		lbl := e.label
		lblX := bx + pmIconSize/2 - len(lbl)*fontW/2
		lblY := by + pmIconSize + fontH + 2
		DrawText(screen, lbl, lblX, lblY, color.RGBA{30, 40, 80, alpha})
	}

	// ── Bottom bar ────────────────────────────────────────────────
	botY := panelTop + pmH - pmBotH
	DrawRect(screen, pmX, botY, pmW, pmBotH, color.RGBA{50, 80, 160, alpha})
	DrawRect(screen, pmX, botY, pmW, 2, color.RGBA{110, 150, 220, alpha})
	// Back bullet
	DrawRect(screen, pmX+10, botY+9, 8, 8, color.RGBA{220, 60, 60, alpha})
	DrawText(screen, "Back", pmX+22, botY+pmBotH-8, color.RGBA{255, 255, 255, alpha})

	// ── Active sub-panel ─────────────────────────────────────────
	subY := panelTop + pmH
	switch m.activeSub {
	case "Keys":
		m.drawSubPanel(screen, subY, "Controls", []string{
			"ZQSD / Arrows   Move",
			"X               Sword",
			"R               Mount / Dismount",
			"F               Interact / Read sign",
			"T               Chat",
			"C               Change look",
			"P               Profile",
			"Esc             Menu",
			"/sit            Sit down",
			"/noclip         Toggle noclip",
		})
	case "News":
		m.drawSubPanel(screen, subY, "News", []string{
			"Welcome to GraalReborn!",
			"",
			"No updates yet — check back later.",
		})
	default:
		if m.activeSub != "" {
			m.drawSubPanel(screen, subY, m.activeSub, []string{
				"Coming soon...",
			})
		}
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

// panelTop returns the Y of the panel top based on slide progress.
// The panel slides in from the top of the screen.
func (m *PanelMenu) panelTop() int {
	// Fully closed: top edge is at -(pmH) above screen.
	// Fully open: panel is centred vertically.
	startY := -pmH
	endY := pmY
	return startY + int(float64(endY-startY)*m.progress)
}

// iconPos returns the top-left pixel of icon slot i.
func (m *PanelMenu) iconPos(i, contentTop int) (x, y int) {
	col := i % pmCols
	row := i / pmCols

	contentW := pmCols*pmIconSize + (pmCols-1)*pmIconPad
	startX := pmX + (pmW-contentW)/2

	labelH := fontH + 4
	rowH := pmIconSize + labelH + 10
	startY := contentTop + 8

	return startX + col*(pmIconSize+pmIconPad), startY + row*rowH
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
	pw := 380
	ph := 18 + len(lines)*(fontH+4) + 12
	px := screenW/2 - pw/2

	DrawRect(screen, px, topY, pw, ph, color.RGBA{180, 200, 240, 240})
	DrawRect(screen, px, topY, pw, 18, color.RGBA{60, 100, 180, 240})
	DrawRect(screen, px, topY, pw, 2, color.RGBA{130, 170, 230, 240})
	DrawText(screen, title, px+pw/2-len(title)*fontW/2, topY+13, color.RGBA{255, 255, 255, 255})

	for i, l := range lines {
		DrawText(screen, l, px+10, topY+18+i*(fontH+4)+fontH, color.RGBA{30, 40, 80, 255})
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

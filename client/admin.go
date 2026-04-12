package main

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// AdminMenu is the admin control panel for spawning world items / shop entries.
// Open with Tab (admin only).
type AdminMenu struct {
	visible bool

	// Form fields (use TextInput from ui.go)
	fieldName   *TextInput
	fieldSprite *TextInput
	fieldItemID *TextInput
	fieldPrice  *TextInput
	spawnBtn    *Button

	// World items list (synced from server via state broadcast)
	worldItems []WorldSpawnItem

	// Signals consumed by game.go each frame
	SpawnReq *AdminSpawnReq
	RemoveID string
}

// AdminSpawnReq carries the data for a world-item spawn request.
type AdminSpawnReq struct {
	Name       string
	SpritePath string
	ItemID     string
	Price      int
	X, Y       float64 // filled by game.go with localChar position
}

const (
	adminPW = 560
	adminPH = 500
)

func adminPX() int { return screenW/2 - adminPW/2 }
func adminPY() int { return screenH/2 - adminPH/2 }

func NewAdminMenu() *AdminMenu {
	px := adminPX()
	py := adminPY()
	fi := func(y int, label string) *TextInput {
		return NewTextInput(px+130, py+y, adminPW-145, 22, label, false)
	}
	m := &AdminMenu{
		fieldName:   fi(72, "Name:"),
		fieldSprite: fi(128, "Sprite:"),
		fieldItemID: fi(184, "Item ID (optional):"),
		fieldPrice:  fi(240, "Price (0=free):"),
		spawnBtn:    NewButton(px+adminPW/2-55, py+274, 110, 26, "Spawn here"),
	}
	m.fieldSprite.MaxLen = 128
	m.fieldPrice.Value = "0"
	return m
}

func (m *AdminMenu) IsVisible() bool { return m.visible }
func (m *AdminMenu) Open()           { m.visible = true }
func (m *AdminMenu) Close()          { m.visible = false }
func (m *AdminMenu) Toggle()         { m.visible = !m.visible }

// HasFocus returns true when any text field inside the menu has keyboard focus.
func (m *AdminMenu) HasFocus() bool {
	if !m.visible {
		return false
	}
	for _, f := range []*TextInput{m.fieldName, m.fieldSprite, m.fieldItemID, m.fieldPrice} {
		if f.IsFocused {
			return true
		}
	}
	return false
}

func (m *AdminMenu) SetWorldItems(items []WorldSpawnItem) { m.worldItems = items }

// reflow repositions widgets when the screen is first drawn (positions are
// computed from adminPX/adminPY which depend on screen layout constants).
func (m *AdminMenu) reflow() {
	px := adminPX()
	py := adminPY()
	set := func(ti *TextInput, y int) {
		ti.X = px + 130
		ti.Y = py + y
		ti.W = adminPW - 145
	}
	set(m.fieldName, 72)
	set(m.fieldSprite, 128)
	set(m.fieldItemID, 184)
	set(m.fieldPrice, 240)
	m.fieldPrice.W = 100
	m.spawnBtn.X = px + adminPW/2 - 55
	m.spawnBtn.Y = py + 274
}

func (m *AdminMenu) Update() {
	if !m.visible {
		return
	}
	m.reflow()

	mx, my := ebiten.CursorPosition()

	// Focus management: click on a field to focus it.
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		fields := []*TextInput{m.fieldName, m.fieldSprite, m.fieldItemID, m.fieldPrice}
		anyFocused := false
		for _, f := range fields {
			if f.ContainsPoint(mx, my) {
				f.IsFocused = true
				anyFocused = true
			} else {
				f.IsFocused = false
			}
		}
		_ = anyFocused

		// Spawn button
		if m.spawnBtn.IsClicked() {
			m.trySpawn()
		}

		// Remove buttons in world-items list
		listY := adminPY() + 336
		px := adminPX()
		for i, wi := range m.worldItems {
			iy := listY + i*22
			rbx := px + adminPW - 65
			if mx >= rbx && mx < rbx+58 && my >= iy && my < iy+18 {
				m.RemoveID = wi.ID
				break
			}
		}
	}

	// Keyboard input for focused fields.
	for _, f := range []*TextInput{m.fieldName, m.fieldSprite, m.fieldItemID, m.fieldPrice} {
		f.Update()
	}
}

func (m *AdminMenu) trySpawn() {
	name := strings.TrimSpace(m.fieldName.Value)
	if name == "" {
		return
	}
	price, _ := strconv.Atoi(strings.TrimSpace(m.fieldPrice.Value))
	m.SpawnReq = &AdminSpawnReq{
		Name:       name,
		SpritePath: strings.TrimSpace(m.fieldSprite.Value),
		ItemID:     strings.TrimSpace(m.fieldItemID.Value),
		Price:      price,
	}
}

func (m *AdminMenu) Draw(screen *ebiten.Image) {
	if !m.visible {
		return
	}
	m.reflow()
	px := adminPX()
	py := adminPY()

	DrawPanel(screen, px, py, adminPW, adminPH)

	title := "ADMIN — WORLD MANAGEMENT"
	DrawBigText(screen, title, px+(adminPW-BigTextW(title))/2+2, py+16, colGoldDim)
	DrawBigText(screen, title, px+(adminPW-BigTextW(title))/2, py+14, colGold)
	DrawHDivider(screen, px+10, py+44, adminPW-20)

	// ── Spawn form ───────────────────────────────────────────────
	subTitle := "New world item"
	DrawText(screen, subTitle, px+12, py+58, colGoldDim)

	for _, f := range []*TextInput{m.fieldName, m.fieldSprite, m.fieldItemID, m.fieldPrice} {
		f.Draw(screen)
	}

	// Hint below price
	DrawText(screen, "Position: current player coordinates",
		px+12, py+262, colTextDim)

	m.spawnBtn.Draw(screen)

	DrawHDivider(screen, px+10, py+306, adminPW-20)

	// ── World items list ─────────────────────────────────────────
	DrawText(screen, "World items:", px+12, py+320, colGoldDim)

	listY := py + 336
	maxRows := (adminPH - 348) / 22

	if len(m.worldItems) == 0 {
		DrawText(screen, "No items spawned", px+12, listY+fontH, colTextDim)
	} else {
		for i, wi := range m.worldItems {
			if i >= maxRows {
				DrawText(screen, "…", px+12, listY+i*22+fontH, colTextDim)
				break
			}
			iy := listY + i*22

			label := wi.Name
			if wi.Price > 0 {
				label = fmt.Sprintf("%s — %dG", wi.Name, wi.Price)
			}
			DrawText(screen, label, px+12, iy+fontH, colTextWhite)

			// Cursor blink for "active" indication — just draw the position.
			posStr := fmt.Sprintf("(%.0f,%.0f)", wi.X, wi.Y)
			DrawText(screen, posStr, px+260, iy+fontH, colTextDim)

			// Blinking removal timestamp so admin knows it was acknowledged
			blink := int(time.Now().UnixMilli()/400)%2 == 0
			rbCol := color.RGBA{180, 40, 40, 220}
			if blink {
				rbCol = color.RGBA{220, 60, 60, 255}
			}
			rbx := px + adminPW - 65
			DrawRect(screen, rbx, iy, 58, 18, rbCol)
			DrawRect(screen, rbx, iy, 58, 1, color.RGBA{255, 100, 100, 200})
			DrawText(screen, "Remove", rbx+6, iy+14, colTextWhite)
		}
	}

	hint := "[Tab/Esc] Close"
	DrawText(screen, hint, px+(adminPW-len(hint)*fontW)/2, py+adminPH-10, colTextDim)
}

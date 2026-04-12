package main

import (
	"fmt"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

const (
	invCols = 5
	invSlot = 64
	invGap  = 6
)

// InventoryMenu shows the player's usable items.
type InventoryMenu struct {
	visible  bool
	items    []InventoryItem
	selected int
	// UsedItem is set to the item_id the player wants to use; consumed by game.go.
	UsedItem string
}

func NewInventoryMenu() *InventoryMenu {
	return &InventoryMenu{selected: -1}
}

func (m *InventoryMenu) IsVisible() bool { return m.visible }
func (m *InventoryMenu) Open()           { m.visible = true }
func (m *InventoryMenu) Close()          { m.visible = false }
func (m *InventoryMenu) Toggle()         { m.visible = !m.visible }

func (m *InventoryMenu) SetItems(items []InventoryItem) { m.items = items }

// contentRect returns the top-left of the item grid area.
func (m *InventoryMenu) gridOrigin() (px, py, pw, ph int) {
	rows := (len(m.items) + invCols - 1) / invCols
	if rows < 1 {
		rows = 1
	}
	pw = invCols*(invSlot+invGap) + invGap + 24
	ph = 54 + rows*(invSlot+invGap) + invGap + 28
	px = screenW/2 - pw/2
	py = screenH/2 - ph/2
	return
}

// Update handles mouse input.
func (m *InventoryMenu) Update() {
	if !m.visible {
		return
	}
	if !inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		return
	}
	mx, my := ebiten.CursorPosition()
	px, py, _, _ := m.gridOrigin()
	gridX := px + 12
	gridY := py + 54

	for i, item := range m.items {
		col := i % invCols
		row := i / invCols
		bx := gridX + col*(invSlot+invGap)
		by := gridY + row*(invSlot+invGap)
		if mx >= bx && mx < bx+invSlot && my >= by && my < by+invSlot {
			m.selected = i
			m.UsedItem = item.ID
			return
		}
	}
}

// TakeUsed returns the used item_id and clears it (one-shot).
func (m *InventoryMenu) TakeUsed() string {
	id := m.UsedItem
	m.UsedItem = ""
	return id
}

func (m *InventoryMenu) Draw(screen *ebiten.Image) {
	if !m.visible {
		return
	}
	px, py, pw, ph := m.gridOrigin()
	DrawPanel(screen, px, py, pw, ph)

	title := "INVENTORY"
	DrawBigText(screen, title, px+(pw-BigTextW(title))/2+2, py+16, colGoldDim)
	DrawBigText(screen, title, px+(pw-BigTextW(title))/2, py+14, colGold)
	DrawHDivider(screen, px+10, py+44, pw-20)

	if len(m.items) == 0 {
		msg := "Empty inventory  — use /giveitem"
		DrawText(screen, msg, px+pw/2-len(msg)*fontW/2, py+ph/2, colTextDim)
	} else {
		gridX := px + 12
		gridY := py + 54
		for i, item := range m.items {
			col := i % invCols
			row := i / invCols
			bx := gridX + col*(invSlot+invGap)
			by := gridY + row*(invSlot+invGap)

			bgCol := color.RGBA{35, 50, 95, 220}
			borderCol := color.RGBA{70, 90, 160, 200}
			if i == m.selected {
				bgCol = color.RGBA{60, 100, 200, 240}
				borderCol = colGold
			}
			DrawRect(screen, bx, by, invSlot, invSlot, bgCol)
			DrawRect(screen, bx, by, invSlot, 1, borderCol)
			DrawRect(screen, bx, by+invSlot-1, invSlot, 1, borderCol)
			DrawRect(screen, bx, by, 1, invSlot, borderCol)
			DrawRect(screen, bx+invSlot-1, by, 1, invSlot, borderCol)

			// Item name (2 lines max)
			name := item.Name
			line1, line2 := name, ""
			if len(name) > 9 {
				line1 = name[:8]
				line2 = name[8:]
				if len(line2) > 9 {
					line2 = line2[:8] + "."
				}
			}
			lx1 := bx + invSlot/2 - len(line1)*fontW/2
			DrawText(screen, line1, lx1, by+invSlot/2, colTextWhite)
			if line2 != "" {
				lx2 := bx + invSlot/2 - len(line2)*fontW/2
				DrawText(screen, line2, lx2, by+invSlot/2+fontH+2, colTextDim)
			}

			// Quantity badge
			qty := fmt.Sprintf("x%d", item.Quantity)
			DrawText(screen, qty, bx+invSlot-len(qty)*fontW-3, by+fontH, colGold)
		}
	}

	hint := "[I/Esc] Close  [Click] Use item"
	DrawText(screen, hint, px+(pw-len(hint)*fontW)/2, py+ph-10, colTextDim)
}

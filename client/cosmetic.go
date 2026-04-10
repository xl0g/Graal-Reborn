package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

const (
	cosBase     = "Assets/offline/levels"
	cosItemSize = 52 // grid cell size in pixels
	cosCols     = 10 // thumbnails per row
	cosRows     = 5  // visible rows
	cosPageSize = cosCols * cosRows
	cosThumbSz  = 40 // displayed thumbnail size
)

// CosmeticTab selects which category is shown.
type CosmeticTab int

const (
	TabBody CosmeticTab = iota
	TabHead
	TabHat
)

// thumbKey uniquely identifies a thumbnail.
type thumbKey struct {
	tab CosmeticTab
	idx int
}

// CosmeticMenu is the in-game sprite-selection overlay.
type CosmeticMenu struct {
	visible bool
	tab     CosmeticTab
	page    int

	bodyFiles []string
	headFiles []string
	hatFiles  []string

	// Selected indices (survive tab switches)
	BodyIdx int
	HeadIdx int
	HatIdx  int

	// Thumbnail cache
	mu      sync.Mutex
	thumbs  map[thumbKey]*ebiten.Image
	pending map[thumbKey]bool

	changed bool // true after a selection change — read with TakeChanged
}

// noHatSentinel is the special first entry meaning "no hat equipped".
const noHatSentinel = ""

func NewCosmeticMenu() *CosmeticMenu {
	m := &CosmeticMenu{
		thumbs:  make(map[thumbKey]*ebiten.Image),
		pending: make(map[thumbKey]bool),
	}
	m.bodyFiles = listPNGs(filepath.Join(cosBase, "bodies"))
	m.headFiles = listPNGs(filepath.Join(cosBase, "heads"))
	// Prepend empty string = "no hat"
	m.hatFiles = append([]string{noHatSentinel}, listPNGs(filepath.Join(cosBase, "hats"))...)
	return m
}

func listPNGs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".png" {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	return files
}

// Open shows the menu, jumping to the page that contains the current selection.
func (m *CosmeticMenu) Open() {
	m.visible = true
	m.changed = false
	m.page = m.currentIdx() / cosPageSize
}

func (m *CosmeticMenu) Close()      { m.visible = false }
func (m *CosmeticMenu) IsVisible() bool { return m.visible }

// TakeChanged returns and clears the changed flag.
func (m *CosmeticMenu) TakeChanged() bool {
	c := m.changed
	m.changed = false
	return c
}

func (m *CosmeticMenu) BodyFile() string { return safeFile(m.bodyFiles, m.BodyIdx) }
func (m *CosmeticMenu) HeadFile() string { return safeFile(m.headFiles, m.HeadIdx) }
func (m *CosmeticMenu) HatFile() string  { return safeFile(m.hatFiles, m.HatIdx) }

// SetByFilenames restores the selected indices from saved filenames.
// Unknown filenames are silently ignored (index stays at 0).
func (m *CosmeticMenu) SetByFilenames(body, head, hat string) {
	if i := indexOfFile(m.bodyFiles, body); i >= 0 {
		m.BodyIdx = i
	}
	if i := indexOfFile(m.headFiles, head); i >= 0 {
		m.HeadIdx = i
	}
	if i := indexOfFile(m.hatFiles, hat); i >= 0 {
		m.HatIdx = i
	}
}

func indexOfFile(files []string, name string) int {
	for i, f := range files {
		if f == name {
			return i
		}
	}
	return -1
}

func safeFile(files []string, idx int) string {
	if idx < 0 || idx >= len(files) {
		return ""
	}
	return files[idx]
}

func (m *CosmeticMenu) currentFiles() []string {
	switch m.tab {
	case TabBody:
		return m.bodyFiles
	case TabHead:
		return m.headFiles
	case TabHat:
		return m.hatFiles
	}
	return nil
}

func (m *CosmeticMenu) currentIdx() int {
	switch m.tab {
	case TabBody:
		return m.BodyIdx
	case TabHead:
		return m.HeadIdx
	case TabHat:
		return m.HatIdx
	}
	return 0
}

func (m *CosmeticMenu) setIdx(idx int) {
	switch m.tab {
	case TabBody:
		m.BodyIdx = idx
	case TabHead:
		m.HeadIdx = idx
	case TabHat:
		m.HatIdx = idx
	}
}

// getThumb returns a cached thumbnail or triggers async loading.
func (m *CosmeticMenu) getThumb(tab CosmeticTab, idx int) *ebiten.Image {
	key := thumbKey{tab, idx}
	m.mu.Lock()
	img, ok := m.thumbs[key]
	if ok {
		m.mu.Unlock()
		return img
	}
	if m.pending[key] {
		m.mu.Unlock()
		return nil
	}
	m.pending[key] = true
	m.mu.Unlock()

	// Determine file path
	var subdir string
	var files []string
	switch tab {
	case TabBody:
		subdir, files = "bodies", m.bodyFiles
	case TabHead:
		subdir, files = "heads", m.headFiles
	case TabHat:
		subdir, files = "hats", m.hatFiles
	}
	if idx >= len(files) {
		return nil
	}
	// "no hat" sentinel — no image to load, cache nil permanently
	if files[idx] == noHatSentinel {
		m.mu.Lock()
		m.thumbs[key] = nil
		m.mu.Unlock()
		return nil
	}
	path := filepath.Join(cosBase, subdir, files[idx])

	go func() {
		src, _, err := ebitenutil.NewImageFromFile(path)
		var thumb *ebiten.Image
		if err == nil {
			thumb = extractThumb(src, tab)
		}
		m.mu.Lock()
		m.thumbs[key] = thumb // nil on error → shows placeholder forever
		m.mu.Unlock()
	}()
	return nil
}

// extractThumb crops the representative "down-facing, standing" frame.
func extractThumb(img *ebiten.Image, tab CosmeticTab) *ebiten.Image {
	b := img.Bounds()
	var r image.Rectangle
	switch tab {
	case TabBody:
		// col=2 (down dir), row=0 (stand): x=64, y=0
		r = image.Rect(64, 0, 96, 32)
	case TabHead:
		// dir=2 (down): x=0, y=64
		r = image.Rect(0, 64, 32, 96)
	case TabHat:
		// HatThumbRect uses row=1 (down), col=0 (stand)
		r = HatThumbRect()
	}
	// Safety clamp
	if r.Max.X > b.Max.X {
		r.Max.X = b.Max.X
	}
	if r.Max.Y > b.Max.Y {
		r.Max.Y = b.Max.Y
	}
	if r.Empty() {
		return nil
	}
	return img.SubImage(r).(*ebiten.Image)
}

// ──────────────────────────────────────────────────────────────
// Input / Update
// ──────────────────────────────────────────────────────────────

func (m *CosmeticMenu) Update() bool {
	if !m.visible {
		return false
	}

	// Close
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) ||
		inpututil.IsKeyJustPressed(ebiten.KeyC) {
		m.visible = false
		return false
	}

	files := m.currentFiles()
	totalPages := max(1, (len(files)+cosPageSize-1)/cosPageSize)

	// Tab switching with Q/E
	if inpututil.IsKeyJustPressed(ebiten.KeyQ) && m.tab > 0 {
		m.tab--
		m.page = m.currentIdx() / cosPageSize
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyE) && int(m.tab) < 2 {
		m.tab++
		m.page = m.currentIdx() / cosPageSize
	}

	// Page navigation with Up/Down
	if inpututil.IsKeyJustPressed(ebiten.KeyUp) && m.page > 0 {
		m.page--
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyDown) && m.page < totalPages-1 {
		m.page++
	}

	// Mouse
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		mx, my := ebiten.CursorPosition()

		// Tab clicks
		for i, tx := range cosTabRects() {
			if mx >= tx && mx < tx+tabW && my >= tabY && my < tabY+tabH {
				newTab := CosmeticTab(i)
				if newTab != m.tab {
					m.tab = newTab
					m.page = m.currentIdx() / cosPageSize
				}
			}
		}

		// Thumbnail clicks
		gx, gy := cosGridOrigin()
		col := (mx - gx) / cosItemSize
		row := (my - gy) / cosItemSize
		if col >= 0 && col < cosCols && row >= 0 && row < cosRows {
			idx := m.page*cosPageSize + row*cosCols + col
			files = m.currentFiles()
			if idx < len(files) {
				m.setIdx(idx)
				m.changed = true
			}
		}

		// Prev / Next page buttons
		if my >= pageNavY && my < pageNavY+pageNavH {
			if mx >= prevBtnX && mx < prevBtnX+pageNavW && m.page > 0 {
				m.page--
			}
			if mx >= nextBtnX && mx < nextBtnX+pageNavW && m.page < totalPages-1 {
				m.page++
			}
		}
	}

	return true
}

// ──────────────────────────────────────────────────────────────
// Layout constants
// ──────────────────────────────────────────────────────────────

const (
	tabY    = 62
	tabH    = 28
	tabW    = 160
	tabGap  = 20

	pageNavY = 0 // set in Draw based on grid height
	pageNavH = 28
	pageNavW = 80
	prevBtnX = 0
	nextBtnX = 0
)

func cosTabRects() [3]int {
	total := 3*tabW + 2*tabGap
	startX := (screenW - total) / 2
	return [3]int{startX, startX + tabW + tabGap, startX + 2*(tabW+tabGap)}
}

func cosGridOrigin() (int, int) {
	gw := cosCols * cosItemSize
	gx := (screenW - gw) / 2
	gy := tabY + tabH + 14
	return gx, gy
}

// ──────────────────────────────────────────────────────────────
// Draw
// ──────────────────────────────────────────────────────────────

func (m *CosmeticMenu) Draw(screen *ebiten.Image) {
	if !m.visible {
		return
	}

	// Dim overlay
	DrawRect(screen, 0, 0, screenW, screenH, color.RGBA{0, 0, 0, 200})

	panelX, panelY := 20, 20
	panelW, panelH := screenW-40, screenH-40
	DrawPanel(screen, panelX, panelY, panelW, panelH)

	// Title
	title := "CUSTOMIZE"
	tx := panelX + (panelW-BigTextW(title))/2
	DrawBigText(screen, title, tx+2, panelY+16, colGoldDim)
	DrawBigText(screen, title, tx, panelY+14, colGold)

	// Tabs
	tabs := []string{"BODY", "HEAD", "HAT"}
	txPositions := cosTabRects()
	for i, label := range tabs {
		selected := CosmeticTab(i) == m.tab
		bgClr := colPanelBg
		bdrClr := colBorderMid
		if selected {
			bgClr = colSelBg
			bdrClr = colBorderHL
		}
		DrawRectBorder(screen, txPositions[i], tabY, tabW, tabH, bgClr, bdrClr)
		lw := len(label) * fontW
		lblClr := colTextDim
		if selected {
			lblClr = colGoldBright
		}
		DrawText(screen, label, txPositions[i]+(tabW-lw)/2, tabY+tabH-6, lblClr)
	}

	files := m.currentFiles()
	if len(files) == 0 {
		msg := "No files found in Assets/offline/levels/"
		DrawText(screen, msg, screenW/2-len(msg)*fontW/2, 200, colTextErr)
		hint := "[C / Esc] Close"
		DrawText(screen, hint, screenW/2-len(hint)*fontW/2, screenH-40, colTextDim)
		return
	}

	gx, gy := cosGridOrigin()
	selIdx := m.currentIdx()
	startIdx := m.page * cosPageSize
	totalPages := max(1, (len(files)+cosPageSize-1)/cosPageSize)

	// Draw thumbnails
	for i := 0; i < cosPageSize; i++ {
		fileIdx := startIdx + i
		if fileIdx >= len(files) {
			break
		}
		col := i % cosCols
		row := i / cosCols
		cx := gx + col*cosItemSize
		cy := gy + row*cosItemSize

		selected := fileIdx == selIdx
		bgClr := color.RGBA{18, 18, 36, 220}
		bdrClr := color.RGBA{55, 55, 90, 255}
		if selected {
			bgClr = colSelBg
			bdrClr = colBorderHL
		}
		DrawRectBorder(screen, cx+1, cy+1, cosItemSize-3, cosItemSize-3, bgClr, bdrClr)

		thumb := m.getThumb(m.tab, fileIdx)
		// "no hat" slot
		isNoHat := m.tab == TabHat && files[fileIdx] == noHatSentinel
		if isNoHat {
			noHatClr := colTextDim
			if selected {
				noHatClr = colGoldBright
			}
			label := "NONE"
			DrawText(screen, label, cx+(cosItemSize-len(label)*fontW)/2, cy+cosItemSize/2+fontH/2, noHatClr)
		} else if thumb != nil {
			op := &ebiten.DrawImageOptions{}
			tw := thumb.Bounds().Dx()
			th := thumb.Bounds().Dy()
			if tw > 0 && th > 0 {
				scale := float64(cosThumbSz) / float64(max(tw, th))
				op.GeoM.Scale(scale, scale)
				offsetX := float64(cx) + float64(cosItemSize-cosThumbSz)/2
				offsetY := float64(cy) + float64(cosItemSize-cosThumbSz)/2
				op.GeoM.Translate(offsetX, offsetY)
				screen.DrawImage(thumb, op)
			}
		} else {
			DrawText(screen, ".", cx+cosItemSize/2-fontW/2, cy+cosItemSize/2+fontH/2, color.RGBA{80, 80, 120, 200})
		}
	}

	// Grid bottom y
	gridBottom := gy + cosRows*cosItemSize

	// Page info + navigation
	pageInfo := fmt.Sprintf("%d / %d  (%d sprites)", m.page+1, totalPages, len(files))
	piW := len(pageInfo) * fontW
	DrawText(screen, pageInfo, screenW/2-piW/2, gridBottom+14, colTextDim)

	const btnW, btnH = 70, 22
	prevX := screenW/2 - 100 - btnW
	nextX := screenW/2 + 100

	if m.page > 0 {
		DrawRectBorder(screen, prevX, gridBottom+6, btnW, btnH, colPanelBg, colBorderMid)
		DrawText(screen, "< PREV", prevX+8, gridBottom+6+btnH-5, colGoldBright)
	}
	if m.page < totalPages-1 {
		DrawRectBorder(screen, nextX, gridBottom+6, btnW, btnH, colPanelBg, colBorderMid)
		DrawText(screen, "NEXT >", nextX+8, gridBottom+6+btnH-5, colGoldBright)
	}

	// Footer hint
	hint := "[Q/E] Switch category   [Up/Down] Page   [Click] Select   [C/Esc] Close"
	DrawText(screen, hint, screenW/2-len(hint)*fontW/2, screenH-26, colTextDim)
}

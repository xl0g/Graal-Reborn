package main

import (
	"image/color"
	"math"
	"math/rand"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
)

// ── Font metrics ──────────────────────────────────────────────
const (
	fontW = 7  // pixels per character (1×)
	fontH = 13 // pixels per character height (1×)
)

var uiFont font.Face = basicfont.Face7x13

// ── RPG color palette (Zelda-like) ────────────────────────────
var (
	colBg        = color.RGBA{8, 8, 20, 255}
	colPanelBg   = color.RGBA{10, 10, 28, 255}
	colPanelBg2  = color.RGBA{16, 14, 40, 255}
	colBorderOut = color.RGBA{72, 80, 120, 255}
	colBorderMid = color.RGBA{40, 44, 72, 255}
	colBorderHL  = color.RGBA{128, 136, 200, 255}
	colBorderSH  = color.RGBA{20, 22, 38, 255}
	colGold      = color.RGBA{255, 210, 50, 255}
	colGoldDim   = color.RGBA{180, 140, 30, 255}
	colGoldBright = color.RGBA{255, 240, 130, 255}
	colTextWhite  = color.RGBA{240, 242, 255, 255}
	colTextDim    = color.RGBA{130, 134, 170, 255}
	colTextErr    = color.RGBA{255, 85, 85, 255}
	colTextOK     = color.RGBA{80, 215, 100, 255}
	colSelBg      = color.RGBA{18, 20, 58, 255}
	colInputBg    = color.RGBA{7, 7, 22, 255}
	colInputBdr   = color.RGBA{48, 52, 84, 255}
	colInputFocus = color.RGBA{200, 160, 40, 255}
)

// ── Primitive helpers ─────────────────────────────────────────

func DrawRect(dst *ebiten.Image, x, y, w, h int, clr color.RGBA) {
	if w <= 0 || h <= 0 {
		return
	}
	vector.DrawFilledRect(dst, float32(x), float32(y), float32(w), float32(h), clr, false)
}

// DrawRectBorder draws a filled rect with a 1-px outer border.
func DrawRectBorder(dst *ebiten.Image, x, y, w, h int, fill, border color.RGBA) {
	DrawRect(dst, x-1, y-1, w+2, h+2, border)
	DrawRect(dst, x, y, w, h, fill)
}

// DrawText draws a string at the given position (baseline y) with 1× font.
func DrawText(dst *ebiten.Image, s string, x, y int, clr color.RGBA) {
	text.Draw(dst, s, uiFont, x, y, clr)
}

// DrawBigText draws s at 2× scale. (x, y) is the TOP-LEFT corner of the text.
// Visual size: BigTextW(s) × 26.
func DrawBigText(dst *ebiten.Image, s string, x, y int, clr color.RGBA) {
	if s == "" {
		return
	}
	runes := []rune(s)
	imgW := len(runes)*fontW + fontW
	imgH := fontH + 4
	tmp := ebiten.NewImage(imgW, imgH)
	text.Draw(tmp, s, basicfont.Face7x13, 0, fontH, clr)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(2, 2)
	op.GeoM.Translate(float64(x), float64(y))
	dst.DrawImage(tmp, op)
}

// BigTextW returns the visual width of s at 2× scale.
func BigTextW(s string) int { return utf8.RuneCountInString(s) * fontW * 2 }

// BigTextH is the visual height of DrawBigText output.
const BigTextH = (fontH + 4) * 2 // ≈ 34 px

// DrawPanel renders a Zelda-style thick-bordered window.
//
//	Layers (outside→in): shadow / outer border / mid border / highlight / background
func DrawPanel(dst *ebiten.Image, x, y, w, h int) {
	// Drop shadow
	DrawRect(dst, x+4, y+5, w, h, color.RGBA{0, 0, 0, 110})
	// Outer border
	DrawRect(dst, x, y, w, h, colBorderOut)
	// Mid border
	DrawRect(dst, x+2, y+2, w-4, h-4, colBorderMid)
	// Top-left highlight
	DrawRect(dst, x+3, y+3, w-6, 1, colBorderHL) // top edge
	DrawRect(dst, x+3, y+3, 1, h-6, colBorderHL) // left edge
	// Bottom-right shadow
	DrawRect(dst, x+3, y+h-4, w-6, 1, colBorderSH)
	DrawRect(dst, x+w-4, y+3, 1, h-6, colBorderSH)
	// Background
	DrawRect(dst, x+4, y+4, w-8, h-8, colPanelBg)
}

// DrawHDivider draws a 2-layer horizontal divider line.
func DrawHDivider(dst *ebiten.Image, x, y, w int) {
	DrawRect(dst, x, y, w, 1, colBorderMid)
	DrawRect(dst, x, y+1, w, 1, colBorderHL)
}

// ── Star background ───────────────────────────────────────────

type menuStar struct {
	x, y  int
	alpha uint8
	phase float64
}

var menuStars []menuStar

func initStars() {
	if len(menuStars) > 0 {
		return
	}
	r := rand.New(rand.NewSource(0xDEADBEEF))
	for i := 0; i < 130; i++ {
		menuStars = append(menuStars, menuStar{
			x:     r.Intn(screenW),
			y:     r.Intn(screenH),
			alpha: uint8(50 + r.Intn(150)),
			phase: r.Float64() * math.Pi * 2,
		})
	}
}

// DrawStarBg fills the screen with a dark color and twinkling stars.
func DrawStarBg(dst *ebiten.Image) {
	dst.Fill(colBg)
	initStars()
	t := float64(time.Now().UnixMilli()) / 1800.0
	for i, s := range menuStars {
		twinkle := 0.65 + 0.35*math.Sin(t+s.phase+float64(i)*0.31)
		v := uint8(float64(s.alpha) * twinkle)
		clr := color.RGBA{v, v, v + 20, 255}
		vector.DrawFilledRect(dst, float32(s.x), float32(s.y), 1, 1, clr, false)
		if s.alpha > 160 {
			faint := color.RGBA{v / 3, v / 3, v/3 + 8, 200}
			vector.DrawFilledRect(dst, float32(s.x+1), float32(s.y), 1, 1, faint, false)
		}
	}
}

// ── TextInput ─────────────────────────────────────────────────

const (
	labelGap = 10 // pixels between label baseline and box top
)

// TextInput is a single-line keyboard input field.
// Layout: label drawn above the box (baseline at Y-labelGap).
// ContainsPoint covers both the label and the box so clicks anywhere
// on the visible field activate focus.
type TextInput struct {
	X, Y, W, H int
	Label       string
	IsPassword  bool
	IsFocused   bool
	Value       string
	MaxLen      int

	bsHeld  bool
	bsTimer time.Time
}

// NewTextInput creates a TextInput with the box at (x,y) of size (w×h).
func NewTextInput(x, y, w, h int, label string, isPassword bool) *TextInput {
	return &TextInput{
		X: x, Y: y, W: w, H: h,
		Label: label, IsPassword: isPassword,
		MaxLen: 32,
	}
}

// ContainsPoint returns true when (px,py) is anywhere over the label+box area.
func (ti *TextInput) ContainsPoint(px, py int) bool {
	topY := ti.Y - labelGap - fontH // top of label text
	return px >= ti.X && px <= ti.X+ti.W && py >= topY && py <= ti.Y+ti.H
}

// Update processes keyboard input when focused.
func (ti *TextInput) Update() {
	if !ti.IsFocused {
		ti.bsHeld = false
		return
	}
	for _, c := range ebiten.AppendInputChars(nil) {
		if utf8.RuneCountInString(ti.Value) < ti.MaxLen {
			ti.Value += string(c)
		}
	}
	if ebiten.IsKeyPressed(ebiten.KeyBackspace) {
		now := time.Now()
		if !ti.bsHeld {
			deleteLastRune(&ti.Value)
			ti.bsHeld = true
			ti.bsTimer = now
		} else if now.Sub(ti.bsTimer) > 75*time.Millisecond {
			deleteLastRune(&ti.Value)
			ti.bsTimer = now
		}
	} else {
		ti.bsHeld = false
	}
}

func deleteLastRune(s *string) {
	r := []rune(*s)
	if len(r) > 0 {
		*s = string(r[:len(r)-1])
	}
}

// Draw renders the label and input box onto dst.
func (ti *TextInput) Draw(dst *ebiten.Image) {
	labelClr := colGoldDim
	bdrClr := colInputBdr
	if ti.IsFocused {
		labelClr = colGold
		bdrClr = colInputFocus
	}

	// Label (1× font, baseline at Y-labelGap)
	text.Draw(dst, ti.Label, uiFont, ti.X+2, ti.Y-labelGap, labelClr)

	// Box
	DrawRectBorder(dst, ti.X, ti.Y, ti.W, ti.H, colInputBg, bdrClr)

	// Value text
	display := ti.Value
	if ti.IsPassword {
		display = strings.Repeat("*", utf8.RuneCountInString(ti.Value))
	}
	if ti.IsFocused && (time.Now().UnixMilli()/500)%2 == 0 {
		display += "_"
	}
	textY := ti.Y + ti.H/2 + fontH/2 - 1
	text.Draw(dst, display, uiFont, ti.X+8, textY, colTextWhite)
}

// ── Button ────────────────────────────────────────────────────

// Button is a clickable Zelda-style button.
type Button struct {
	X, Y, W, H int
	Label       string
}

func NewButton(x, y, w, h int, label string) *Button {
	return &Button{X: x, Y: y, W: w, H: h, Label: label}
}

func (b *Button) IsHovered() bool {
	mx, my := ebiten.CursorPosition()
	return mx >= b.X && mx < b.X+b.W && my >= b.Y && my < b.Y+b.H
}

func (b *Button) IsClicked() bool {
	return b.IsHovered() && inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft)
}

func (b *Button) Draw(dst *ebiten.Image) {
	bg := colPanelBg2
	bdr := colBorderMid
	lblClr := colTextDim
	if b.IsHovered() {
		bg = colSelBg
		bdr = colBorderHL
		lblClr = colGoldBright
	}
	// Outer glow border
	DrawRect(dst, b.X-2, b.Y-2, b.W+4, b.H+4, colBorderSH)
	DrawRectBorder(dst, b.X, b.Y, b.W, b.H, bg, bdr)
	// Highlight top edge
	DrawRect(dst, b.X+1, b.Y+1, b.W-2, 1, color.RGBA{bdr.R, bdr.G, bdr.B, 80})

	// Centered label
	tw := utf8.RuneCountInString(b.Label) * fontW
	tx := b.X + (b.W-tw)/2
	ty := b.Y + (b.H+fontH)/2 - 1
	text.Draw(dst, b.Label, uiFont, tx, ty, lblClr)
}

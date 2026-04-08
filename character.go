package main

import (
	"image"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"golang.org/x/image/font/basicfont"
)

const (
	frameW     = 32
	frameH     = 32
	animSpeed  = 0.1  // seconds per frame
	animFrames = 5
	moveSpeed  = 200.0 // px/s
)

// npcTints maps NPCType → color tint applied when rendering.
var npcTints = []color.RGBA{
	{160, 230, 160, 255}, // 0 villager : green
	{255, 210, 100, 255}, // 1 merchant : gold
	{140, 160, 255, 255}, // 2 guard    : blue
	{255, 160, 200, 255}, // 3 traveler : pink
	{210, 170, 120, 255}, // 4 farmer   : brown
}

// interpK is the exponential-decay constant for position interpolation.
// Higher = snappier but more jitter; lower = smoother but more lag.
// At 60 fps: factor per frame ≈ 1 - exp(-20/60) ≈ 28 % of remaining distance.
const interpK = 20.0

// Character represents any moving entity (local player, remote player, or NPC).
type Character struct {
	// X, Y are the smoothly-interpolated display position.
	X, Y float64
	// TargetX/Y is the authoritative server position (lerp destination).
	TargetX, TargetY float64

	Dir     int  // 0=up 1=left 2=down 3=right
	Moving  bool
	Name    string
	IsNPC   bool
	NPCType int
	IsLocal bool // true only for the locally-controlled player

	frame    float64
	frameIdx int

	bodyImg *ebiten.Image
	headImg *ebiten.Image
}

// NewCharacter allocates a Character. X/Y and TargetX/Y are both set to (x,y)
// so there is no initial lerp jump.
func NewCharacter(bodyImg, headImg *ebiten.Image, x, y float64, name string, isNPC bool, npcType int) *Character {
	return &Character{
		X: x, Y: y,
		TargetX: x, TargetY: y,
		Dir: 2, Name: name,
		IsNPC: isNPC, NPCType: npcType,
		bodyImg: bodyImg, headImg: headImg,
	}
}

// Update advances the animation and, for remote entities, smoothly interpolates
// the display position toward the server-authoritative target.
func (c *Character) Update(dt float64) {
	// Position interpolation (skip for the local player — it moves directly).
	if !c.IsLocal {
		factor := 1 - math.Exp(-interpK*dt)
		c.X += (c.TargetX - c.X) * factor
		c.Y += (c.TargetY - c.Y) * factor
	}

	if c.Moving {
		c.frame += dt
		if c.frame >= animSpeed {
			c.frame -= animSpeed
			c.frameIdx = (c.frameIdx + 1) % animFrames
		}
	} else {
		c.frameIdx = 0
		c.frame = 0
	}
}

// Draw renders the character at its world position, offset by the camera.
func (c *Character) Draw(screen *ebiten.Image, camX, camY float64) {
	sx := c.X - camX
	sy := c.Y - camY

	// Cull off-screen characters
	if sx < -64 || sx > float64(screenW)+64 || sy < -64 || sy > float64(screenH)+64 {
		return
	}

	// Choose body row based on animation state
	bodyRow := 0
	if c.Moving {
		bodyRow = c.frameIdx + 1
	}

	bodyRect := image.Rect(frameW*c.Dir, frameH*bodyRow, frameW*(c.Dir+1), frameH*(bodyRow+1))
	headRect := image.Rect(0, frameH*c.Dir, frameW, frameH*(c.Dir+1))

	bodyOp := &ebiten.DrawImageOptions{}
	headOp := &ebiten.DrawImageOptions{}
	bodyOp.GeoM.Translate(sx, sy)
	headOp.GeoM.Translate(sx, sy-16)

	// NPC tint
	if c.IsNPC {
		t := npcTints[c.NPCType%len(npcTints)]
		r := float32(t.R) / 255
		g := float32(t.G) / 255
		b := float32(t.B) / 255
		bodyOp.ColorScale.Scale(r, g, b, 1)
		headOp.ColorScale.Scale(r, g, b, 1)
	}

	if c.bodyImg != nil {
		screen.DrawImage(c.bodyImg.SubImage(bodyRect).(*ebiten.Image), bodyOp)
	}
	if c.headImg != nil {
		screen.DrawImage(c.headImg.SubImage(headRect).(*ebiten.Image), headOp)
	}

	// Name tag
	nameClr := color.RGBA{240, 240, 255, 220}
	if c.IsNPC {
		nameClr = color.RGBA{255, 215, 70, 220}
	}
	nameX := int(sx) + frameW/2 - len([]rune(c.Name))*fontW/2
	nameY := int(sy) - 22
	DrawText(screen, c.Name, nameX, nameY, nameClr)
	_ = basicfont.Face7x13 // ensure import used
}

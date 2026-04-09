package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"path/filepath"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"golang.org/x/image/font/basicfont"
)

// ──────────────────────────────────────────────────────────────
// Constants
// ──────────────────────────────────────────────────────────────

const (
	frameW     = 32
	frameH     = 32
	animSpeed  = 0.1 // seconds per walk frame
	animFrames = 5   // walk cycle length
	moveSpeed  = 200.0

	// Sword animation (Gani-style: 4 frames, ~70 ms each)
	swordTotalFrames = 4
	swordFrameTime   = 0.07

	// Mount sprite (ride.png: 96 px wide, 32 px per row, 4 frames per direction)
	rideFrameW       = 96
	rideFrameH       = 32
	rideFramesPerDir = 4

	// Mounted movement speed bonus
	mountedMoveSpeed = 320.0

	// NPC type for horse (must match server constant)
	NPCTypeHorse = 5
)

// Gani animation states
const (
	AnimIdle  = "idle"
	AnimWalk  = "walk"
	AnimSword = "sword"
	AnimRide  = "ride"
)

// npcTints maps NPCType → rendering tint.
var npcTints = []color.RGBA{
	{160, 230, 160, 255}, // 0 villager
	{255, 210, 100, 255}, // 1 merchant
	{140, 160, 255, 255}, // 2 guard
	{255, 160, 200, 255}, // 3 traveler
	{210, 170, 120, 255}, // 4 farmer
	{220, 190, 130, 255}, // 5 horse (fallback body tint)
}

const interpK = 20.0

// ──────────────────────────────────────────────────────────────
// Mount sprite (loaded once, shared)
// ──────────────────────────────────────────────────────────────

var (
	rideImg     *ebiten.Image
	rideImgOnce sync.Once
)

func ensureRideImg() *ebiten.Image {
	rideImgOnce.Do(func() {
		img, _, err := ebitenutil.NewImageFromFile(
			"Assets/offline/levels/images/downloads/ride.png")
		if err != nil {
			fmt.Println("[ASSETS] ride.png:", err)
			return
		}
		rideImg = img
	})
	return rideImg
}

// ──────────────────────────────────────────────────────────────
// Cosmetic image bundle
// ──────────────────────────────────────────────────────────────

type cosmeticImgs struct {
	body *ebiten.Image
	head *ebiten.Image
	hat  *ebiten.Image
}

// ──────────────────────────────────────────────────────────────
// Character
// ──────────────────────────────────────────────────────────────

// Character represents any moving entity (local player, remote player, or NPC).
type Character struct {
	// Display position (interpolated toward Target for remote entities).
	X, Y float64
	// Authoritative server position.
	TargetX, TargetY float64

	Dir     int
	Moving  bool
	Name    string
	IsNPC   bool
	NPCType int
	IsLocal bool
	Gralats int

	// Gani animation state machine
	AnimState string // AnimIdle | AnimWalk | AnimSword | AnimRide

	// Sword animation sub-state
	swordFrame   int     // 0-3
	swordTimer   float64 // accumulator
	swordHitDone bool    // true once hit detection ran this swing

	// Mount state
	Mounted bool

	// HP / MaxHP for NPCs (0 = immortal)
	HP    int
	MaxHP int

	// Walk animation accumulator
	frame    float64
	frameIdx int

	// Default sprites (used when no cosmetic is set)
	bodyImg *ebiten.Image
	headImg *ebiten.Image

	// Cosmetic filenames
	BodyFile string
	HeadFile string
	HatFile  string

	// Loaded cosmetic images (swapped under mutex)
	cosmu    sync.Mutex
	cosImgs  cosmeticImgs
	cosDirty bool
}

// NewCharacter allocates a Character. X/Y and TargetX/Y are both set to (x,y).
func NewCharacter(bodyImg, headImg *ebiten.Image, x, y float64, name string, isNPC bool, npcType int) *Character {
	return &Character{
		X: x, Y: y,
		TargetX: x, TargetY: y,
		Dir:     2,
		Name:    name,
		IsNPC:   isNPC, NPCType: npcType,
		bodyImg:   bodyImg,
		headImg:   headImg,
		AnimState: AnimIdle,
	}
}

// SetCosmetics updates filenames and triggers async image loading.
func (c *Character) SetCosmetics(bodyFile, headFile, hatFile string) {
	if c.BodyFile == bodyFile && c.HeadFile == headFile && c.HatFile == hatFile {
		return
	}
	c.BodyFile = bodyFile
	c.HeadFile = headFile
	c.HatFile = hatFile
	c.cosDirty = true
}

// StartSword begins the sword swing animation.
// Returns false if already swinging or mounted.
func (c *Character) StartSword() bool {
	if c.AnimState == AnimSword || c.AnimState == AnimRide {
		return false
	}
	c.AnimState = AnimSword
	c.swordFrame = 0
	c.swordTimer = 0
	c.swordHitDone = false
	return true
}

// SwordHitbox returns the world-space rectangle that can hit enemies during
// the active sword frames (1 and 2).  Returns an empty rectangle otherwise.
func (c *Character) SwordHitbox() image.Rectangle {
	if c.AnimState != AnimSword || c.swordFrame < 1 || c.swordFrame > 2 {
		return image.Rectangle{}
	}
	x, y := int(c.X), int(c.Y)
	const reach = 44
	switch c.Dir {
	case 0: // up
		return image.Rect(x+2, y-reach, x+30, y)
	case 1: // left
		return image.Rect(x-reach, y+2, x, y+30)
	case 2: // down
		return image.Rect(x+2, y+32, x+30, y+32+reach)
	case 3: // right
		return image.Rect(x+32, y+2, x+32+reach, y+30)
	}
	return image.Rectangle{}
}

// SwordJustActivated reports whether the sword entered its active phase this
// tick AND hit detection has not yet been processed.
func (c *Character) SwordJustActivated() bool {
	return c.AnimState == AnimSword && c.swordFrame >= 1 && !c.swordHitDone
}

// MarkSwordHitDone prevents duplicate hit detection for the current swing.
func (c *Character) MarkSwordHitDone() { c.swordHitDone = true }

// ──────────────────────────────────────────────────────────────
// Update
// ──────────────────────────────────────────────────────────────

// Update advances the animation state machine and interpolates position.
func (c *Character) Update(dt float64) {
	// Async cosmetic loading
	if c.cosDirty {
		c.cosDirty = false
		body, head, hat := c.BodyFile, c.HeadFile, c.HatFile
		go func() {
			var imgs cosmeticImgs
			if body != "" {
				imgs.body, _, _ = ebitenutil.NewImageFromFile(
					filepath.Join("Assets/offline/levels/bodies", body))
			}
			if head != "" {
				imgs.head, _, _ = ebitenutil.NewImageFromFile(
					filepath.Join("Assets/offline/levels/heads", head))
			}
			if hat != "" {
				imgs.hat, _, _ = ebitenutil.NewImageFromFile(
					filepath.Join("Assets/offline/levels/hats", hat))
			}
			c.cosmu.Lock()
			c.cosImgs = imgs
			c.cosmu.Unlock()
		}()
	}

	// Position interpolation (remote entities only)
	if !c.IsLocal {
		factor := 1 - math.Exp(-interpK*dt)
		c.X += (c.TargetX - c.X) * factor
		c.Y += (c.TargetY - c.Y) * factor
	}

	// Walk animation accumulator (used by walk and ride states)
	if c.Moving {
		c.frame += dt
		if c.frame >= animSpeed {
			c.frame -= animSpeed
			c.frameIdx = (c.frameIdx + 1) % animFrames
		}
	} else if c.AnimState != AnimSword {
		c.frameIdx = 0
		c.frame = 0
	}

	// Gani-style animation state machine
	switch c.AnimState {
	case AnimSword:
		c.swordTimer += dt
		c.swordFrame = int(c.swordTimer / swordFrameTime)
		if c.swordFrame >= swordTotalFrames {
			// Swing finished — return to idle or walk
			c.swordFrame = 0
			c.swordTimer = 0
			c.swordHitDone = false
			if c.Moving {
				c.AnimState = AnimWalk
			} else {
				c.AnimState = AnimIdle
			}
		}
	case AnimRide:
		// Mounted: keep ride state until explicitly dismounted
		if !c.Moving {
			c.frameIdx = 0
			c.frame = 0
		}
	default:
		// Idle / walk
		if c.Moving {
			c.AnimState = AnimWalk
		} else {
			c.AnimState = AnimIdle
		}
	}
}

// ──────────────────────────────────────────────────────────────
// Draw
// ──────────────────────────────────────────────────────────────

// Draw renders the character at its world position, offset by the camera.
func (c *Character) Draw(screen *ebiten.Image, camX, camY float64) {
	sx := c.X - camX
	sy := c.Y - camY

	// Frustum cull with generous margin for wide mount sprite
	if sx < -96 || sx > float64(screenW)+96 || sy < -96 || sy > float64(screenH)+96 {
		return
	}

	isHorse := c.IsNPC && c.NPCType == NPCTypeHorse

	// Draw mount sprite below the character (horses use only this sprite)
	if isHorse || c.AnimState == AnimRide || c.Mounted {
		c.drawMount(screen, sx, sy)
	}

	// Horse NPCs are rendered purely as the mount sprite — skip body/head
	if isHorse {
		c.drawNameTag(screen, sx, sy)
		return
	}

	// Resolve cosmetic images
	c.cosmu.Lock()
	imgs := c.cosImgs
	c.cosmu.Unlock()

	activeBody := c.bodyImg
	if imgs.body != nil {
		activeBody = imgs.body
	}
	activeHead := c.headImg
	if imgs.head != nil {
		activeHead = imgs.head
	}

	// Body animation row:
	//   row 0   = idle / sword (standing pose)
	//   rows 1-5 = walk cycle
	bodyRow := 0
	if c.Moving && c.AnimState != AnimSword {
		bodyRow = c.frameIdx + 1
	}

	bodyRect := image.Rect(frameW*c.Dir, frameH*bodyRow, frameW*(c.Dir+1), frameH*(bodyRow+1))
	headRect := image.Rect(0, frameH*c.Dir, frameW, frameH*(c.Dir+1))

	// When mounted shift the rider up so they appear to sit on the horse
	yOff := 0.0
	if c.AnimState == AnimRide || c.Mounted {
		yOff = -8.0
	}

	bodyOp := &ebiten.DrawImageOptions{}
	headOp := &ebiten.DrawImageOptions{}
	bodyOp.GeoM.Translate(sx, sy+yOff)
	headOp.GeoM.Translate(sx, sy-16+yOff)

	// NPC tint
	if c.IsNPC {
		t := npcTints[c.NPCType%len(npcTints)]
		r, g, b := float32(t.R)/255, float32(t.G)/255, float32(t.B)/255
		bodyOp.ColorScale.Scale(r, g, b, 1)
		headOp.ColorScale.Scale(r, g, b, 1)
	}

	if activeBody != nil {
		screen.DrawImage(activeBody.SubImage(bodyRect).(*ebiten.Image), bodyOp)
	}
	if activeHead != nil {
		screen.DrawImage(activeHead.SubImage(headRect).(*ebiten.Image), headOp)
	}

	// Hat (48×48 frames, row 0, col = direction)
	if imgs.hat != nil {
		const hatSz = 48
		hatRect := image.Rect(c.Dir*hatSz, 0, (c.Dir+1)*hatSz, hatSz)
		hatBounds := imgs.hat.Bounds()
		if hatRect.Max.X <= hatBounds.Max.X && hatRect.Max.Y <= hatBounds.Max.Y {
			hatOp := &ebiten.DrawImageOptions{}
			hatOp.GeoM.Translate(sx-float64(hatSz-frameW)/2, sy-32+yOff)
			screen.DrawImage(imgs.hat.SubImage(hatRect).(*ebiten.Image), hatOp)
		}
	}

	// Sword overlay — drawn on top of body, visible during frames 1-3
	if c.AnimState == AnimSword && c.swordFrame >= 1 {
		c.drawSword(screen, sx, sy)
	}

	// HP bar for damageable NPCs
	if c.IsNPC && c.MaxHP > 0 && c.HP > 0 {
		c.drawHPBar(screen, sx, sy)
	}

	c.drawNameTag(screen, sx, sy)
}

// drawMount renders ride.png below the character.
// Layout assumption: each row is 96×32 px; direction groups of rideFramesPerDir rows.
//
//	row = dir * rideFramesPerDir + animFrame
func (c *Character) drawMount(screen *ebiten.Image, sx, sy float64) {
	mount := ensureRideImg()
	if mount == nil {
		return
	}
	animFrame := c.frameIdx % rideFramesPerDir
	srcRow := c.Dir*rideFramesPerDir + animFrame
	srcY := srcRow * rideFrameH

	bounds := mount.Bounds()
	if srcY+rideFrameH > bounds.Max.Y {
		srcY = 0 // clamp to first row if layout differs
	}

	srcRect := image.Rect(0, srcY, rideFrameW, srcY+rideFrameH)

	mOp := &ebiten.DrawImageOptions{}
	// Centre the 96 px wide mount over the 32 px character
	mOp.GeoM.Translate(sx-float64(rideFrameW-frameW)/2, sy+4)
	screen.DrawImage(mount.SubImage(srcRect).(*ebiten.Image), mOp)
}

// drawSword renders a procedural sword shape in the character's facing direction.
// Lengths vary per frame for a swing-arc feel.
func (c *Character) drawSword(screen *ebiten.Image, sx, sy float64) {
	// Sword reach per frame (0=windup 1=strike 2=follow 3=recover)
	lengths := [4]int{10, 28, 26, 12}
	reach := lengths[c.swordFrame]

	blade := color.RGBA{255, 240, 80, 240}
	guard := color.RGBA{140, 100, 30, 255}
	handle := color.RGBA{100, 60, 10, 255}

	var bx, by, bw, bh int
	var gx, gy, gw, gh int
	var hx, hy, hw, hh int

	switch c.Dir {
	case 0: // up
		bx, by, bw, bh = int(sx)+14, int(sy)-reach, 4, reach
		gx, gy, gw, gh = int(sx)+6, int(sy)-4, 20, 4
		hx, hy, hw, hh = int(sx)+14, int(sy)-1, 4, 8
	case 2: // down
		bx, by, bw, bh = int(sx)+14, int(sy)+32, 4, reach
		gx, gy, gw, gh = int(sx)+6, int(sy)+32, 20, 4
		hx, hy, hw, hh = int(sx)+14, int(sy)+24, 4, 8
	case 1: // left
		bx, by, bw, bh = int(sx)-reach, int(sy)+14, reach, 4
		gx, gy, gw, gh = int(sx)-4, int(sy)+6, 4, 20
		hx, hy, hw, hh = int(sx)-1, int(sy)+14, 8, 4
	case 3: // right
		bx, by, bw, bh = int(sx)+32, int(sy)+14, reach, 4
		gx, gy, gw, gh = int(sx)+32, int(sy)+6, 4, 20
		hx, hy, hw, hh = int(sx)+24, int(sy)+14, 8, 4
	}

	DrawRect(screen, bx, by, bw, bh, blade)
	DrawRect(screen, gx, gy, gw, gh, guard)
	DrawRect(screen, hx, hy, hw, hh, handle)
}

// drawHPBar renders a small health bar above the character.
func (c *Character) drawHPBar(screen *ebiten.Image, sx, sy float64) {
	const (
		barW = 30
		barH = 4
	)
	barX := int(sx) + (frameW-barW)/2
	barY := int(sy) - 42

	// Border
	DrawRect(screen, barX-1, barY-1, barW+2, barH+2, color.RGBA{0, 0, 0, 180})
	// Empty (red)
	DrawRect(screen, barX, barY, barW, barH, color.RGBA{180, 20, 20, 220})
	// Fill (green)
	if fillW := barW * c.HP / c.MaxHP; fillW > 0 {
		DrawRect(screen, barX, barY, fillW, barH, color.RGBA{40, 200, 40, 220})
	}
}

// drawNameTag renders the name label (and gralat count) above the character.
func (c *Character) drawNameTag(screen *ebiten.Image, sx, sy float64) {
	nameClr := color.RGBA{240, 240, 255, 220}
	if c.IsNPC {
		nameClr = color.RGBA{255, 215, 70, 220}
	}
	nameX := int(sx) + frameW/2 - len([]rune(c.Name))*fontW/2
	nameY := int(sy) - 26
	DrawText(screen, c.Name, nameX, nameY, nameClr)

	if !c.IsNPC && c.Gralats > 0 {
		gs := fmt.Sprintf("G:%d", c.Gralats)
		gx := int(sx) + frameW/2 - len(gs)*fontW/2
		DrawText(screen, gs, gx, nameY+fontH+1, color.RGBA{255, 200, 60, 180})
	}
	_ = basicfont.Face7x13
}

// HatThumbRect returns the source rect for a hat thumbnail (down/front-facing).
func HatThumbRect() image.Rectangle {
	const hatSz = 48
	return image.Rect(2*hatSz, 0, 3*hatSz, hatSz)
}

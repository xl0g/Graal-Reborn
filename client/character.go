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
	frameW  = 32
	frameH  = 32
	moveSpeed = 200.0

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

// ganiFile maps animation state → .gani filename.
var ganiFile = map[string]string{
	AnimIdle:  "idle.gani",
	AnimWalk:  "walk.gani",
	AnimSword: "sword.gani",
	AnimRide:  "mount.gani",
}

// npcTints maps NPCType → rendering tint.
var npcTints = []color.RGBA{
	{160, 230, 160, 255}, // 0 villager
	{255, 210, 100, 255}, // 1 merchant
	{140, 160, 255, 255}, // 2 guard
	{255, 160, 200, 255}, // 3 traveler
	{210, 170, 120, 255}, // 4 farmer
	{220, 190, 130, 255}, // 5 horse
}

const interpK = 20.0

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

	// Sword hit detection flag
	swordHitDone bool

	// Mount state
	Mounted bool

	// HP / MaxHP for damageable NPCs (0 = immortal)
	HP    int
	MaxHP int

	// Cosmetic filenames
	BodyFile string
	HeadFile string
	HatFile  string

	// Loaded cosmetic images (swapped under mutex)
	cosmu    sync.Mutex
	cosImgs  cosmeticImgs
	cosDirty bool

	// Gani players — one per gani file, lazily loaded.
	players map[string]*GaniPlayer
	curAnim string // currently active gani name (e.g. "walk.gani")
}

// NewCharacter allocates a Character.  bodyImg / headImg are ignored; the gani
// system loads its own default images from GANITEMPLATE/res/images/.
func NewCharacter(_ *ebiten.Image, _ *ebiten.Image, x, y float64, name string, isNPC bool, npcType int) *Character {
	return &Character{
		X: x, Y: y,
		TargetX: x, TargetY: y,
		Dir:       2,
		Name:      name,
		IsNPC:     isNPC,
		NPCType:   npcType,
		AnimState: AnimIdle,
		players:   make(map[string]*GaniPlayer),
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

// StartSword begins (or restarts) the sword swing animation.
// Returns false only when mounted (sword is disabled on horse).
func (c *Character) StartSword() bool {
	if c.AnimState == AnimRide {
		return false
	}
	c.AnimState = AnimSword
	c.swordHitDone = false
	// Always reset so spamming restarts the swing from frame 0.
	if p := c.getPlayer("sword.gani"); p != nil {
		p.Reset()
	}
	return true
}

// SwordHitbox returns the world-space rectangle that can hit enemies during
// the active sword frames.  Returns an empty rectangle otherwise.
func (c *Character) SwordHitbox() image.Rectangle {
	if c.AnimState != AnimSword {
		return image.Rectangle{}
	}
	p := c.getPlayer("sword.gani")
	if p == nil {
		return image.Rectangle{}
	}
	// Frames 1-3 are the active swing in sword.gani (0=windup, 5=recovery).
	if p.Frame < 1 || p.Frame > 3 {
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
	if c.AnimState != AnimSword || c.swordHitDone {
		return false
	}
	p := c.getPlayer("sword.gani")
	return p != nil && p.Frame >= 1
}

// MarkSwordHitDone prevents duplicate hit detection for the current swing.
func (c *Character) MarkSwordHitDone() { c.swordHitDone = true }

// ──────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────

// getPlayer returns (and lazily creates) the GaniPlayer for a .gani file.
func (c *Character) getPlayer(name string) *GaniPlayer {
	if p, ok := c.players[name]; ok {
		return p
	}
	p := newGaniPlayer(name)
	c.players[name] = p
	return p
}

// activateAnim switches to the given gani name, resetting if it changed.
func (c *Character) activateAnim(name string) *GaniPlayer {
	if c.curAnim != name {
		c.curAnim = name
		if p := c.getPlayer(name); p != nil {
			p.Reset()
		}
	}
	return c.getPlayer(name)
}

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

	// Determine target gani based on animation state
	var targetGani string
	switch c.AnimState {
	case AnimSword:
		targetGani = "sword.gani"
	case AnimRide:
		if c.Moving {
			targetGani = "mount.gani"
		} else {
			targetGani = "mount.gani" // mount.gani handles both still and moving
		}
	default:
		if c.Moving {
			targetGani = "walk.gani"
		} else {
			targetGani = "idle.gani"
		}
	}

	// Activate gani (resets if switched)
	p := c.activateAnim(targetGani)
	if p == nil {
		return
	}

	// Advance the gani player.
	// When mounted and standing still, freeze the animation on frame 0
	// so the horse shows a still pose rather than cycling walk frames.
	if targetGani == "mount.gani" && !c.Moving {
		p.Frame = 0
		p.Timer = 0
	} else {
		p.Update(dt)
	}

	// Sword completion: when the sword gani finishes, return to idle/walk
	if c.AnimState == AnimSword && p.Done {
		c.swordHitDone = false
		if c.Moving {
			c.AnimState = AnimWalk
		} else {
			c.AnimState = AnimIdle
		}
	}

	// Idle/walk transitions (outside sword/ride)
	if c.AnimState != AnimSword && c.AnimState != AnimRide {
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

	// Frustum cull with generous margin
	if sx < -96 || sx > float64(screenW)+96 || sy < -96 || sy > float64(screenH)+96 {
		return
	}

	// Resolve cosmetic images
	c.cosmu.Lock()
	imgs := c.cosImgs
	c.cosmu.Unlock()

	// Build GaniImages for this draw call
	gi := &GaniImages{
		def: GaniDefaultImages(),
	}

	// Horse NPCs: hide the rider sprites, only the HORSE layer shows.
	isHorse := c.IsNPC && c.NPCType == NPCTypeHorse
	if isHorse {
		gi.NoBody = true
		gi.NoHead = true
		gi.NoAttr1 = true
	} else {
		if imgs.body != nil {
			gi.Body = imgs.body
		}
		if imgs.head != nil {
			gi.Head = imgs.head
		}
		// No hat selected (empty string) → suppress ATTR1 entirely.
		if c.HatFile == "" {
			gi.NoAttr1 = true
		} else if imgs.hat != nil {
			gi.Attr1 = imgs.hat
		}
	}

	// Determine which gani player to draw
	ganiName := c.curAnim
	if ganiName == "" {
		ganiName = "idle.gani"
	}
	p := c.getPlayer(ganiName)
	if p == nil || p.Anim == nil {
		c.drawFallback(screen, camX, camY, imgs)
		c.drawNameTag(screen, sx, sy)
		return
	}

	// Draw gani frame at character body position
	p.Draw(screen, gi, c.Dir, c.X, c.Y, camX, camY)

	// HP bar for damageable NPCs
	if c.IsNPC && c.MaxHP > 0 && c.HP > 0 {
		c.drawHPBar(screen, sx, sy)
	}

	c.drawNameTag(screen, sx, sy)
}

// drawFallback draws a plain coloured rectangle when no gani is available.
func (c *Character) drawFallback(screen *ebiten.Image, camX, camY float64, imgs cosmeticImgs) {
	sx := c.X - camX
	sy := c.Y - camY
	if imgs.body != nil {
		bodyRect := image.Rect(frameW*c.Dir, 0, frameW*(c.Dir+1), frameH)
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(sx, sy)
		screen.DrawImage(imgs.body.SubImage(bodyRect).(*ebiten.Image), op)
	}
}

// drawHPBar renders a small health bar above the character.
func (c *Character) drawHPBar(screen *ebiten.Image, sx, sy float64) {
	const (
		barW = 30
		barH = 4
	)
	barX := int(sx) + (frameW-barW)/2
	barY := int(sy) - 42

	DrawRect(screen, barX-1, barY-1, barW+2, barH+2, color.RGBA{0, 0, 0, 180})
	DrawRect(screen, barX, barY, barW, barH, color.RGBA{180, 20, 20, 220})
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
// Hat sheets are 48×48 per direction, 4 columns: up/left/down/right.
func HatThumbRect() image.Rectangle {
	const hatSz = 48
	return image.Rect(2*hatSz, 0, 3*hatSz, hatSz) // col 2 = down direction
}

package main

// ──────────────────────────────────────────────────────────────
// Gani animation system
//
// Parses the .gani format used by Graal Online / GANITEMPLATE and
// plays back the resulting sprite-composition animations via Ebiten.
//
// Key facts extracted from GANITEMPLATE/main.js :
//   • Base framerate  : 20 fps  (0.05 s per tick)
//   • WAIT N          : frame holds for (1+N)*0.05 seconds
//   • ANI section     : 4 consecutive non-blank lines = 1 frame
//                       (direction order: up / left / down / right)
//   • Each dir-line   : comma-separated "spriteIdx dx dy" tokens
//   • ATTACHSPRITE    : after drawing a parent sprite, also draw child
//   • ATTACHSPRITE2   : draw child BEFORE parent (drawunder)
//   • Gani origin     : top-left of the 48×48 bounding box
//                       → body sprite at gani offset (8,16)
//                       → c.X,c.Y  (body top-left) = origin + (8,16)
//                       → origin_screen = (c.X-8-camX, c.Y-16-camY)
// ──────────────────────────────────────────────────────────────

import (
	"bufio"
	"fmt"
	"image"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
)

const (
	ganiImageDir = "GANITEMPLATE/res/images/"
	ganiDir      = "GANITEMPLATE/res/ganis/"

	// Gani bounding-box offsets (body is at 8,16 inside the 48×48 box).
	ganiOriginDX = 8
	ganiOriginDY = 16
)

// ──────────────────────────────────────────────────────────────
// Source types
// ──────────────────────────────────────────────────────────────

type ganiSource int

const (
	srcUnknown ganiSource = iota
	srcSprites            // sprites.png  (shadow…)
	srcBody               // body.png / custom
	srcHead               // head.png / custom
	srcAttr1              // hat.png  / custom
	srcSword              // sword1.png
	srcHorse              // ride.png
	srcShield             // shield1.png
	srcAttr4              // accessory
	srcFile               // inline filename
)

func parseSrc(s string) (ganiSource, string) {
	switch strings.ToUpper(s) {
	case "SPRITES":
		return srcSprites, ""
	case "BODY":
		return srcBody, ""
	case "HEAD":
		return srcHead, ""
	case "ATTR1":
		return srcAttr1, ""
	case "SWORD", "PARAM1":
		return srcSword, ""
	case "HORSE", "ATTR2":
		return srcHorse, ""
	case "SHIELD":
		return srcShield, ""
	case "ATTR4":
		return srcAttr4, ""
	default:
		return srcFile, s // treat token as a filename
	}
}

// ──────────────────────────────────────────────────────────────
// Data types
// ──────────────────────────────────────────────────────────────

// ganiSprite is one SPRITE definition.
type ganiSprite struct {
	source  ganiSource
	fileSrc string // non-empty when source==srcFile
	sx, sy  int
	sw, sh  int
	under   [][3]int // ATTACHSPRITE2: [childIdx, dx, dy] drawn before
	over    [][3]int // ATTACHSPRITE:  [childIdx, dx, dy] drawn after
}

// ganiEntry is one draw call inside a frame direction.
type ganiEntry struct {
	idx  int // sprite index
	x, y int // pixel offset from gani origin
}

// ganiFrame is one animation frame (4 directions).
type ganiFrame struct {
	dirs      [4][]ganiEntry
	frameTime float64 // seconds to hold this frame
}

// GaniAnim is a fully parsed .gani file, ready for playback.
type GaniAnim struct {
	sprites   map[int]*ganiSprite
	frames    []ganiFrame
	loop      bool
	setBackTo string // e.g. "idle.gani"

	// Default image filenames (resolved from ganiImageDir)
	defBody  string
	defHead  string
	defAttr1 string
	defSword string
}

// ──────────────────────────────────────────────────────────────
// Parser
// ──────────────────────────────────────────────────────────────

// ParseGani reads and parses a .gani file from disk.
func ParseGani(path string) (*GaniAnim, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	anim := &GaniAnim{
		sprites: make(map[int]*ganiSprite),
		defBody:  "body.png",
		defHead:  "head0.png",
		defAttr1: "hat0.png",
		defSword: "sword1.png",
	}

	var (
		parsingAni bool
		newFrame   [][]ganiEntry // accumulates 4 dir-lines → 1 frame
	)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		origLine := scanner.Text()
		line := strings.TrimSpace(origLine)

		// Skip blank lines and comments
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		if line == "ANI" {
			parsingAni = true
			newFrame = nil
			continue
		}
		if line == "ANIEND" {
			parsingAni = false
			if len(newFrame) > 0 {
				// flush partial frame
				pushFrame(anim, newFrame)
				newFrame = nil
			}
			continue
		}

		if parsingAni {
			if strings.HasPrefix(line, "WAIT") {
				n, _ := strconv.Atoi(strings.TrimSpace(line[4:]))
				dt := float64(1+n) * 0.05
				if len(anim.frames) > 0 {
					anim.frames[len(anim.frames)-1].frameTime = dt
				}
				continue
			}
			if strings.HasPrefix(line, "PLAYSOUND") {
				continue
			}
			// Direction lines must start with a space in the original text
			if !strings.HasPrefix(origLine, " ") {
				continue
			}
			entries := parseAniLine(line)
			if entries != nil {
				newFrame = append(newFrame, entries)
				if len(newFrame) >= 4 {
					pushFrame(anim, newFrame)
					newFrame = nil
				}
			}
			continue
		}

		// Outside ANI — parse header directives
		tokens := strings.Fields(line)
		if len(tokens) == 0 {
			continue
		}

		switch tokens[0] {
		case "SPRITE":
			if len(tokens) < 7 {
				continue
			}
			idx, _ := strconv.Atoi(tokens[1])
			src, fileSrc := parseSrc(tokens[2])
			sx, _ := strconv.Atoi(tokens[3])
			sy, _ := strconv.Atoi(tokens[4])
			sw, _ := strconv.Atoi(tokens[5])
			sh, _ := strconv.Atoi(tokens[6])
			anim.sprites[idx] = &ganiSprite{
				source: src, fileSrc: fileSrc,
				sx: sx, sy: sy, sw: sw, sh: sh,
			}

		case "ATTACHSPRITE":
			if len(tokens) < 5 {
				continue
			}
			parent, _ := strconv.Atoi(tokens[1])
			child, _ := strconv.Atoi(tokens[2])
			dx, _ := strconv.Atoi(tokens[3])
			dy, _ := strconv.Atoi(tokens[4])
			if sp, ok := anim.sprites[parent]; ok {
				sp.over = append(sp.over, [3]int{child, dx, dy})
			}

		case "ATTACHSPRITE2":
			if len(tokens) < 5 {
				continue
			}
			parent, _ := strconv.Atoi(tokens[1])
			child, _ := strconv.Atoi(tokens[2])
			dx, _ := strconv.Atoi(tokens[3])
			dy, _ := strconv.Atoi(tokens[4])
			if sp, ok := anim.sprites[parent]; ok {
				sp.under = append(sp.under, [3]int{child, dx, dy})
			}

		case "LOOP":
			anim.loop = true
		case "SETBACKTO":
			sb := strings.TrimSpace(strings.Join(tokens[1:], " "))
			if sb == "iidle" {
				sb = "idle.gani"
			}
			if !strings.HasSuffix(sb, ".gani") {
				sb += ".gani"
			}
			anim.setBackTo = sb

		case "DEFAULTBODY":
			if len(tokens) > 1 {
				anim.defBody = tokens[1]
			}
		case "DEFAULTHEAD":
			if len(tokens) > 1 {
				anim.defHead = tokens[1]
			}
		case "DEFAULTATTR1":
			if len(tokens) > 1 {
				anim.defAttr1 = tokens[1]
			}
		case "DEFAULTPARAM1":
			if len(tokens) > 1 {
				anim.defSword = tokens[1]
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(anim.frames) == 0 {
		return nil, fmt.Errorf("no animation frames found in %s", path)
	}
	return anim, nil
}

// pushFrame appends a full 4-direction ganiFrame to anim.
func pushFrame(anim *GaniAnim, dirs [][]ganiEntry) {
	var f ganiFrame
	f.frameTime = 0.05 // default 20 fps
	for d := 0; d < 4 && d < len(dirs); d++ {
		f.dirs[d] = dirs[d]
	}
	anim.frames = append(anim.frames, f)
}

// parseAniLine parses one direction-line of the ANI section.
// Format: "spriteIdx dx dy , spriteIdx dx dy , ..."
func parseAniLine(line string) []ganiEntry {
	var out []ganiEntry
	for _, part := range strings.Split(line, ",") {
		// collapse multiple spaces
		fields := strings.Fields(part)
		if len(fields) < 3 {
			continue
		}
		idx, err1 := strconv.Atoi(fields[0])
		x, err2 := strconv.Atoi(fields[1])
		y, err3 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		out = append(out, ganiEntry{idx: idx, x: x, y: y})
	}
	return out
}

// ──────────────────────────────────────────────────────────────
// Global image cache
// ──────────────────────────────────────────────────────────────

var (
	ganiImgCache   = make(map[string]*ebiten.Image)
	ganiImgCacheMu sync.Mutex
)

// ganiLoadImg loads an image from ganiImageDir, caching the result.
// Returns nil on error (graceful fallback).
func ganiLoadImg(filename string) *ebiten.Image {
	if filename == "" {
		return nil
	}
	path := ganiImageDir + filename

	ganiImgCacheMu.Lock()
	if img, ok := ganiImgCache[path]; ok {
		ganiImgCacheMu.Unlock()
		return img
	}
	ganiImgCacheMu.Unlock()

	img, _, err := ebitenutil.NewImageFromFile(path)
	if err != nil {
		img = nil
	}

	ganiImgCacheMu.Lock()
	ganiImgCache[path] = img
	ganiImgCacheMu.Unlock()
	return img
}

// ──────────────────────────────────────────────────────────────
// GaniImages — per-character image bundle
// ──────────────────────────────────────────────────────────────

// GaniImages holds the resolved source images for one draw call.
// Callers override Body/Head/Attr1 with the character's cosmetics.
type GaniImages struct {
	Body   *ebiten.Image // overrides BODY source
	Head   *ebiten.Image // overrides HEAD source
	Attr1  *ebiten.Image // overrides ATTR1 source
	// Shared defaults (singletons loaded by GaniDefaults)
	def *ganiDefaults
}

type ganiDefaults struct {
	sprites *ebiten.Image
	body    *ebiten.Image
	head    *ebiten.Image
	attr1   *ebiten.Image
	sword   *ebiten.Image
	horse   *ebiten.Image
	shield  *ebiten.Image
}

var (
	globalGaniDef     *ganiDefaults
	globalGaniDefOnce sync.Once
)

// GaniDefaultImages returns the shared singleton default images.
func GaniDefaultImages() *ganiDefaults {
	globalGaniDefOnce.Do(func() {
		globalGaniDef = &ganiDefaults{
			sprites: ganiLoadImg("sprites.png"),
			body:    ganiLoadImg("body.png"),
			head:    ganiLoadImg("head0.png"),
			attr1:   ganiLoadImg("hat0.png"),
			sword:   ganiLoadImg("sword1.png"),
			horse:   ganiLoadImg("ride.png"),
			shield:  ganiLoadImg("shield1.png"),
		}
	})
	return globalGaniDef
}

// resolve returns the image for a given ganiSource.
func (gi *GaniImages) resolve(src ganiSource, fileSrc string, anim *GaniAnim) *ebiten.Image {
	d := gi.def
	switch src {
	case srcSprites:
		return d.sprites
	case srcBody:
		if gi.Body != nil {
			return gi.Body
		}
		return d.body
	case srcHead:
		if gi.Head != nil {
			return gi.Head
		}
		return d.head
	case srcAttr1:
		if gi.Attr1 != nil {
			return gi.Attr1
		}
		return d.attr1
	case srcSword:
		return d.sword
	case srcHorse:
		return d.horse
	case srcShield:
		return d.shield
	case srcAttr4:
		return nil // accessories not implemented
	case srcFile:
		return ganiLoadImg(fileSrc)
	}
	return nil
}

// ──────────────────────────────────────────────────────────────
// GaniPlayer — plays back a GaniAnim
// ──────────────────────────────────────────────────────────────

// GaniPlayer tracks playback state for one GaniAnim.
type GaniPlayer struct {
	Anim  *GaniAnim
	Frame int     // current frame index
	Timer float64 // time accumulator
	Done  bool    // true when non-looping anim has finished its last frame
}

// Reset restarts the animation from the beginning.
func (p *GaniPlayer) Reset() {
	p.Frame = 0
	p.Timer = 0
	p.Done = false
}

// Update advances the animation by dt seconds.
func (p *GaniPlayer) Update(dt float64) {
	if p.Anim == nil || p.Done {
		return
	}
	nf := len(p.Anim.frames)
	if nf == 0 {
		return
	}
	p.Timer += dt
	ft := p.Anim.frames[p.Frame].frameTime
	if ft <= 0 {
		ft = 0.05
	}
	for p.Timer >= ft {
		p.Timer -= ft
		if p.Anim.loop && p.Anim.setBackTo == "" {
			// Pure loop
			p.Frame = (p.Frame + 1) % nf
		} else {
			if p.Frame < nf-1 {
				p.Frame++
			} else {
				p.Done = true
				return
			}
		}
		if p.Frame < nf {
			ft = p.Anim.frames[p.Frame].frameTime
			if ft <= 0 {
				ft = 0.05
			}
		}
	}
}

// Draw renders the current frame at world position (wx, wy) minus camera.
// dir: 0=up 1=left 2=down 3=right
func (p *GaniPlayer) Draw(screen *ebiten.Image, gi *GaniImages, dir int, wx, wy, camX, camY float64) {
	if p.Anim == nil || len(p.Anim.frames) == 0 {
		return
	}
	frame := p.Anim.frames[p.Frame]
	entries := frame.dirs[dir]

	// Gani origin on screen: body is at gani offset (8,16), so
	//   gani_origin = character_body_pos - (8,16)
	ox := wx - camX - float64(ganiOriginDX)
	oy := wy - camY - float64(ganiOriginDY)

	for _, e := range entries {
		p.drawSprite(screen, gi, e.idx, int(ox)+e.x, int(oy)+e.y, 0)
	}
}

// drawSprite draws sprite idx at screen position (sx, sy).
// depth guards against recursive ATTACHSPRITE chains.
func (p *GaniPlayer) drawSprite(screen *ebiten.Image, gi *GaniImages, idx, sx, sy, depth int) {
	if depth > 3 {
		return
	}
	sp := p.Anim.sprites[idx]
	if sp == nil {
		return
	}

	// Draw UNDER attachments first (ATTACHSPRITE2)
	for _, att := range sp.under {
		p.drawSprite(screen, gi, att[0], sx+att[1], sy+att[2], depth+1)
	}

	// Draw the sprite itself
	img := gi.resolve(sp.source, sp.fileSrc, p.Anim)
	if img != nil {
		bounds := img.Bounds()
		srcRect := image.Rect(sp.sx, sp.sy, sp.sx+sp.sw, sp.sy+sp.sh)
		// Clip to image bounds to avoid panics with out-of-range rects
		if srcRect.Max.X <= bounds.Max.X && srcRect.Max.Y <= bounds.Max.Y &&
			srcRect.Min.X >= 0 && srcRect.Min.Y >= 0 {
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(float64(sx), float64(sy))
			screen.DrawImage(img.SubImage(srcRect).(*ebiten.Image), op)
		}
	}

	// Draw OVER attachments (ATTACHSPRITE)
	for _, att := range sp.over {
		p.drawSprite(screen, gi, att[0], sx+att[1], sy+att[2], depth+1)
	}
}

// ──────────────────────────────────────────────────────────────
// Global gani cache (parsed once per file)
// ──────────────────────────────────────────────────────────────

var (
	ganiCache   = make(map[string]*GaniAnim)
	ganiCacheMu sync.Mutex
)

// LoadGani loads and caches a .gani file by name (e.g. "walk.gani").
// Returns nil on error.
func LoadGani(name string) *GaniAnim {
	ganiCacheMu.Lock()
	if a, ok := ganiCache[name]; ok {
		ganiCacheMu.Unlock()
		return a
	}
	ganiCacheMu.Unlock()

	a, err := ParseGani(ganiDir + name)
	if err != nil {
		fmt.Printf("[GANI] %s: %v\n", name, err)
		a = nil
	}

	ganiCacheMu.Lock()
	ganiCache[name] = a
	ganiCacheMu.Unlock()
	return a
}

// newGaniPlayer creates a GaniPlayer for the named gani (or nil on error).
func newGaniPlayer(name string) *GaniPlayer {
	a := LoadGani(name)
	if a == nil {
		return nil
	}
	return &GaniPlayer{Anim: a}
}

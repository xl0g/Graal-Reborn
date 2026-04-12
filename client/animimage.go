package main

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/png"
	"path/filepath"
	"strings"

	gifpkg "image/gif"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
)

// AnimImage holds either a static image or an animated GIF sequence.
// rawFrames are decoded in a background goroutine (no ebiten calls).
// frames (ebiten) are lazily allocated on the main thread the first time
// Frame() is called for a given index — this is safe because Frame() is only
// ever called from the Ebiten draw/update loop (main thread).
type AnimImage struct {
	rawFrames []image.Image  // always set; used for lazy ebiten conversion
	frames    []*ebiten.Image // lazily populated on main thread
	delays    []float64      // per-frame duration in seconds
	total     float64        // total loop duration
}

// Frame returns the frame that should be displayed at time t (in seconds).
// t is expected to increase monotonically; the animation loops.
// Safe to call only from the main (Ebiten) thread.
func (a *AnimImage) Frame(t float64) *ebiten.Image {
	if a == nil || len(a.rawFrames) == 0 {
		return nil
	}

	idx := 0
	if len(a.rawFrames) > 1 && a.total > 0 {
		// Loop
		t = t - float64(int(t/a.total))*a.total
		if t < 0 {
			t += a.total
		}
		var acc float64
		for i, d := range a.delays {
			acc += d
			if t < acc {
				idx = i
				break
			} else {
				idx = len(a.rawFrames) - 1
			}
		}
	}

	// Lazily allocate ebiten image on main thread.
	if a.frames[idx] == nil {
		a.frames[idx] = ebiten.NewImageFromImage(a.rawFrames[idx])
	}
	return a.frames[idx]
}

// Static returns the first frame (useful for thumbnails / non-animated use).
func (a *AnimImage) Static() *ebiten.Image {
	if a == nil || len(a.rawFrames) == 0 {
		return nil
	}
	if a.frames[0] == nil {
		a.frames[0] = ebiten.NewImageFromImage(a.rawFrames[0])
	}
	return a.frames[0]
}

// loadAnimImage loads a .png or .gif file and wraps it in an AnimImage.
// May be called from any goroutine — no ebiten calls are made here.
// Returns nil on error (caller should treat nil as "no image").
func loadAnimImage(path string) *AnimImage {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".gif" {
		a, err := loadGIFAnimImage(path)
		if err == nil {
			return a
		}
		// Fall through to single-frame loader
	}

	data, err := ReadGameFile(path)
	if err != nil {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		// Try ebitenutil as fallback (only safe from main thread, but this path
		// is for PNG which is always single-frame anyway).
		eimg, _, e2 := ebitenutil.NewImageFromFile(path)
		if e2 != nil {
			return nil
		}
		return &AnimImage{
			rawFrames: []image.Image{eimg},
			frames:    make([]*ebiten.Image, 1),
			delays:    []float64{1e9},
			total:     1e9,
		}
	}
	return &AnimImage{
		rawFrames: []image.Image{img},
		frames:    make([]*ebiten.Image, 1),
		delays:    []float64{1e9},
		total:     1e9,
	}
}

// loadGIFAnimImage decodes an animated GIF into an AnimImage.
// All image.Image compositing is done here (goroutine-safe).
// ebiten.NewImageFromImage is deferred to Frame() on the main thread.
func loadGIFAnimImage(path string) (*AnimImage, error) {
	data, err := ReadGameFile(path)
	if err != nil {
		return nil, err
	}
	g, err := gifpkg.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if len(g.Image) == 0 {
		return nil, image.ErrFormat
	}

	w := g.Config.Width
	h := g.Config.Height
	canvas := image.NewNRGBA(image.Rect(0, 0, w, h))
	prev := image.NewNRGBA(image.Rect(0, 0, w, h))

	rawFrames := make([]image.Image, len(g.Image))
	delays := make([]float64, len(g.Image))

	for i, paletted := range g.Image {
		// Save current canvas in case DisposalPrevious is used next frame.
		copy(prev.Pix, canvas.Pix)

		// Composite frame onto canvas.
		draw.Draw(canvas, paletted.Bounds(), paletted, paletted.Bounds().Min, draw.Over)

		// Snapshot this frame as a plain NRGBA image (safe outside main thread).
		snap := image.NewNRGBA(image.Rect(0, 0, w, h))
		copy(snap.Pix, canvas.Pix)
		rawFrames[i] = snap

		// Apply disposal method for next frame.
		if i < len(g.Disposal) {
			switch g.Disposal[i] {
			case gifpkg.DisposalBackground:
				var bg color.Color = color.Transparent
				if g.BackgroundIndex < uint8(len(g.Image[i].Palette)) {
					bg = g.Image[i].Palette[g.BackgroundIndex]
				}
				draw.Draw(canvas, paletted.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)
			case gifpkg.DisposalPrevious:
				copy(canvas.Pix, prev.Pix)
			}
		}

		d := 2 // fallback: 20ms (~50fps) for delay=0 or missing
		if i < len(g.Delay) && g.Delay[i] > 0 {
			d = g.Delay[i]
		}
		delays[i] = float64(d) / 100.0 // centiseconds → seconds
	}

	var total float64
	for _, d := range delays {
		total += d
	}

	return &AnimImage{
		rawFrames: rawFrames,
		frames:    make([]*ebiten.Image, len(rawFrames)),
		delays:    delays,
		total:     total,
	}, nil
}

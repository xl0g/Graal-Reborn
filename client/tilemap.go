package main

import (
	"encoding/xml"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
)

// ── TMX XML structures ────────────────────────────────────────

type tmxMap struct {
	TileW    int          `xml:"tilewidth,attr"`
	TileH    int          `xml:"tileheight,attr"`
	Cols     int          `xml:"width,attr"`
	Rows     int          `xml:"height,attr"`
	Tilesets []tmxTileset `xml:"tileset"`
	Layers   []tmxLayer   `xml:"layer"`
}

type tmxTileset struct {
	FirstGID int    `xml:"firstgid,attr"`
	Source   string `xml:"source,attr"` // external .tsx path, empty for inline
}

type tmxLayer struct {
	Name  string        `xml:"name,attr"`
	Cols  int           `xml:"width,attr"`
	Rows  int           `xml:"height,attr"`
	Props []tmxProperty `xml:"properties>property"`
	Data  struct {
		Raw string `xml:",chardata"`
	} `xml:"data"`
}

type tmxProperty struct {
	Name  string `xml:"name,attr"`
	Type  string `xml:"type,attr"`
	Value string `xml:"value,attr"`
}

// ── TSX (image-collection or spritesheet tileset) structures ──

type tsxFile struct {
	Columns int       `xml:"columns,attr"`
	TileW   int       `xml:"tilewidth,attr"`
	TileH   int       `xml:"tileheight,attr"`
	Image   *tsxImage `xml:"image"` // set for spritesheet tilesets
	Tiles   []tsxTile `xml:"tile"`  // set for image-collection tilesets
}

type tsxImage struct {
	Source string `xml:"source,attr"`
	Width  int    `xml:"width,attr"`
	Height int    `xml:"height,attr"`
}

type tsxTile struct {
	ID    int `xml:"id,attr"`
	Image struct {
		Source string `xml:"source,attr"`
		Width  int    `xml:"width,attr"`
		Height int    `xml:"height,attr"`
	} `xml:"image"`
}

// ── GameMap ───────────────────────────────────────────────────

// GameMap holds the parsed TMX map and provides collision/sign queries.
type GameMap struct {
	TileW, TileH int
	Cols, Rows   int

	layers    [][][]int         // all layers [layer][row][col]
	collision [][]bool          // solid tiles
	signs     map[[2]int]string // (col,row) → panneau text
	switchmap map[[2]int]string // (col,row) → target map filename
	exitTiles [][2]int          // tiles marked exitmap=true

	// Spritesheet tileset
	tileImg     *ebiten.Image
	tileCols    int
	firstGID    int

	// Image-collection tileset (e.g. Test.tsx)
	imageTiles    map[int]*AnimImage // GID → animated image
	imageFirstGID int               // first GID of the image-collection tileset

	// Elapsed time for animated tiles (updated by Update)
	mapTime float64
}

// Update advances animated tile timers. Call once per game frame.
func (gm *GameMap) Update(dt float64) {
	gm.mapTime += dt
}

// LoadTMX parses a .tmx file (external TSX assumed to be classiciphone_pics4).
func LoadTMX(path string) (*GameMap, error) {
	raw, err := ReadGameFile(path)
	if err != nil {
		return nil, err
	}
	var mx tmxMap
	if err := xml.Unmarshal(raw, &mx); err != nil {
		return nil, err
	}

	gm := &GameMap{
		TileW: mx.TileW, TileH: mx.TileH,
		Cols: mx.Cols, Rows: mx.Rows,
		signs:     make(map[[2]int]string),
		switchmap: make(map[[2]int]string),
	}

	for _, ts := range mx.Tilesets {
		if ts.Source == "" {
			// Inline spritesheet tileset — use classiciphone_pics4.
			gm.firstGID = ts.FirstGID
			img, _, _ := ebitenutil.NewImageFromFile(
				"assets/offline/levels/tiles/classiciphone_pics4.png")
			gm.tileImg = img
			if img != nil && gm.TileW > 0 {
				gm.tileCols = img.Bounds().Dx() / gm.TileW
			}
		} else {
			// External TSX — parse to determine type.
			data, err := ReadGameFile(ts.Source)
			if err == nil {
				var tsx tsxFile
				if xml.Unmarshal(data, &tsx) == nil {
					if tsx.Image != nil && tsx.Image.Source != "" {
						// Spritesheet TSX: single image with columns.
						gm.firstGID = ts.FirstGID
						imgPath := tsx.Image.Source
						img, _, _ := ebitenutil.NewImageFromFile(imgPath)
						if img == nil {
							// Try by basename in asset dirs.
							if found := findAssetFile(filepath.Base(imgPath)); found != "" {
								img, _, _ = ebitenutil.NewImageFromFile(found)
							}
						}
						gm.tileImg = img
						tw := tsx.TileW
						if tw == 0 {
							tw = gm.TileW
						}
						if tw == 0 {
							tw = 16
						}
						if tsx.Columns > 0 {
							gm.tileCols = tsx.Columns
						} else if img != nil && tw > 0 {
							gm.tileCols = img.Bounds().Dx() / tw
						}
						// Override tile dimensions from TSX if map default is 0.
						if gm.TileW == 0 {
							gm.TileW = tw
						}
						if gm.TileH == 0 && tsx.TileH > 0 {
							gm.TileH = tsx.TileH
						}
					} else if len(tsx.Tiles) > 0 {
						// Image-collection TSX: per-tile images.
						gm.imageFirstGID = ts.FirstGID
						tiles := make(map[int]*AnimImage)
						for _, t := range tsx.Tiles {
							if t.Image.Source == "" {
								continue
							}
							fname := filepath.Base(t.Image.Source)
							assetPath := findAssetFile(fname)
							if assetPath == "" {
								continue
							}
							anim := loadAnimImage(assetPath)
							if anim == nil {
								continue
							}
							tiles[ts.FirstGID+t.ID] = anim
						}
						if gm.imageTiles == nil {
							gm.imageTiles = tiles
						} else {
							for k, v := range tiles {
								gm.imageTiles[k] = v
							}
						}
					}
				}
			}
		}
	}

	gm.collision = make([][]bool, mx.Rows)
	for i := range gm.collision {
		gm.collision[i] = make([]bool, mx.Cols)
	}

	for _, layer := range mx.Layers {
		tiles := parseTileCSV(layer.Data.Raw, layer.Cols, layer.Rows)
		gm.layers = append(gm.layers, tiles)

		isCollision := false
		signText := ""
		switchTarget := ""
		isExitmap := false
		for _, p := range layer.Props {
			if p.Name == "collision" && p.Value == "true" {
				isCollision = true
			}
			if p.Name == "panneau" {
				signText = p.Value
			}
			if p.Name == "switchmap" {
				switchTarget = p.Value
			}
			if p.Name == "exitmap" && p.Value == "true" {
				isExitmap = true
			}
		}
		if isCollision {
			for r := 0; r < layer.Rows; r++ {
				for c := 0; c < layer.Cols; c++ {
					rawID := tiles[r][c] & tileGIDMask
					if rawID != 0 {
						gm.collision[r][c] = true
					}
				}
			}
		}
		if signText != "" {
			for r := 0; r < layer.Rows; r++ {
				for c := 0; c < layer.Cols; c++ {
					if tiles[r][c] != 0 {
						gm.signs[[2]int{c, r}] = signText
					}
				}
			}
		}
		if switchTarget != "" {
			for r := 0; r < layer.Rows; r++ {
				for c := 0; c < layer.Cols; c++ {
					if tiles[r][c] != 0 {
						gm.switchmap[[2]int{c, r}] = switchTarget
					}
				}
			}
		}
		if isExitmap {
			for r := 0; r < layer.Rows; r++ {
				for c := 0; c < layer.Cols; c++ {
					if tiles[r][c] != 0 {
						gm.exitTiles = append(gm.exitTiles, [2]int{c, r})
					}
				}
			}
		}
	}
	return gm, nil
}


// findAssetFile searches for a file by base name across common asset dirs.
func findAssetFile(name string) string {
	dirs := []string{
		"assets/offline/levels/images/classic",
		"assets/offline/levels/images/classiciphone",
		"assets/offline/levels/images/dc",
		"assets/offline/levels/images/downloads",
		"assets/offline/levels/images/ce",
		"assets/offline/levels/images/light4",
		"assets/offline/levels/images/dcvip",
		"assets/offline/levels/images",
		"assets/offline/levels/tiles",
	}
	for _, d := range dirs {
		p := filepath.Join(d, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func parseTileCSV(raw string, cols, rows int) [][]int {
	grid := make([][]int, rows)
	for i := range grid {
		grid[i] = make([]int, cols)
	}
	fields := strings.FieldsFunc(strings.TrimSpace(raw), func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	i := 0
	for r := 0; r < rows && i < len(fields); r++ {
		for c := 0; c < cols && i < len(fields); c++ {
			v, _ := strconv.ParseInt(strings.TrimSpace(fields[i]), 10, 64)
			grid[r][c] = int(v)
			i++
		}
	}
	return grid
}

// IsBlocked reports whether a world-space rect (x,y,w,h) overlaps a solid tile.
// A 2-pixel inset is applied for a smoother feel.
func (gm *GameMap) IsBlocked(x, y, w, h float64) bool {
	const margin = 2.0
	x1 := int(x+margin) / gm.TileW
	y1 := int(y+margin) / gm.TileH
	x2 := int(x+w-margin-1) / gm.TileW
	y2 := int(y+h-margin-1) / gm.TileH
	for row := y1; row <= y2; row++ {
		for col := x1; col <= x2; col++ {
			if row < 0 || row >= gm.Rows || col < 0 || col >= gm.Cols {
				return true
			}
			if gm.collision[row][col] {
				return true
			}
		}
	}
	return false
}

// WorldW returns the total world width in pixels.
func (gm *GameMap) WorldW() int { return gm.Cols * gm.TileW }

// WorldH returns the total world height in pixels.
func (gm *GameMap) WorldH() int { return gm.Rows * gm.TileH }

// ExitPos returns the centre of the exitmap tiles as a spawn position.
// Returns (0,0,false) if no exitmap tiles are defined.
func (gm *GameMap) ExitPos() (float64, float64, bool) {
	if len(gm.exitTiles) == 0 {
		return 0, 0, false
	}
	var sumX, sumY float64
	for _, t := range gm.exitTiles {
		sumX += float64(t[0])*float64(gm.TileW) + float64(gm.TileW)/2
		sumY += float64(t[1])*float64(gm.TileH) + float64(gm.TileH)/2
	}
	n := float64(len(gm.exitTiles))
	return sumX / n, sumY / n, true
}

// SwitchmapAt returns the target map name if the rect (x,y,w,h) overlaps a switchmap tile,
// or "" if there is no trigger.
func (gm *GameMap) SwitchmapAt(x, y, w, h float64) string {
	cx := int(x+w/2) / gm.TileW
	cy := int(y+h/2) / gm.TileH
	if target, ok := gm.switchmap[[2]int{cx, cy}]; ok {
		return target
	}
	return ""
}

// OnExitTile reports whether the rect centre is on an exitmap tile.
func (gm *GameMap) OnExitTile(x, y, w, h float64) bool {
	cx := int(x+w/2) / gm.TileW
	cy := int(y+h/2) / gm.TileH
	for _, t := range gm.exitTiles {
		if t[0] == cx && t[1] == cy {
			return true
		}
	}
	return false
}

// NearbySign returns the panneau text if the player centre is within 1 tile of a sign.
func (gm *GameMap) NearbySign(px, py float64) string {
	cx := int(px+float64(frameW)/2) / gm.TileW
	cy := int(py+float64(frameH)/2) / gm.TileH
	for dr := -1; dr <= 1; dr++ {
		for dc := -1; dc <= 1; dc++ {
			if t, ok := gm.signs[[2]int{cx + dc, cy + dr}]; ok {
				return t
			}
		}
	}
	return ""
}

// Draw renders all tile layers (frustum-culled).
// Empty tiles (GID=0 across all layers) are filled with a procedural grass pattern.
func (gm *GameMap) Draw(screen *ebiten.Image, camX, camY float64) {
	// First pass: fill "hole" tiles with grass
	gm.drawGrassFill(screen, camX, camY)

	// Second pass: draw actual tiles
	for _, layer := range gm.layers {
		for row := 0; row < gm.Rows; row++ {
			for col := 0; col < gm.Cols; col++ {
				id := layer[row][col]
				if id == 0 {
					continue
				}
				sx := float64(col*gm.TileW) - camX
				sy := float64(row*gm.TileH) - camY
				rawID := id & tileGIDMask
				// Frustum cull: account for potentially large image tiles
				cullW := float64(gm.TileW * 16)
				cullH := float64(gm.TileH * 16)
				if sx+cullW < 0 || sx-cullW > float64(screenW) ||
					sy+cullH < 0 || sy-cullH > float64(screenH) {
					continue
				}
				// Image-collection tile?
				if gm.imageTiles != nil {
					if anim, ok := gm.imageTiles[rawID]; ok {
						gm.drawImageTile(screen, id, anim, sx, sy)
						continue
					}
				}
				// Regular spritesheet tile.
				if sx+float64(gm.TileW) < 0 || sx > float64(screenW) ||
					sy+float64(gm.TileH) < 0 || sy > float64(screenH) {
					continue
				}
				gm.drawTile(screen, id, sx, sy)
			}
		}
	}
}

// drawGrassFill fills empty tile positions (all-layers GID=0) with a grass color pattern.
func (gm *GameMap) drawGrassFill(screen *ebiten.Image, camX, camY float64) {
	if gm.Cols == 0 || gm.Rows == 0 || gm.TileW == 0 || gm.TileH == 0 {
		return
	}
	tw := float64(gm.TileW)
	th := float64(gm.TileH)

	// Determine visible tile range
	colMin := int(camX/tw) - 1
	colMax := int((camX+float64(screenW))/tw) + 2
	rowMin := int(camY/th) - 1
	rowMax := int((camY+float64(screenH))/th) + 2
	if colMin < 0 {
		colMin = 0
	}
	if rowMin < 0 {
		rowMin = 0
	}
	if colMax > gm.Cols {
		colMax = gm.Cols
	}
	if rowMax > gm.Rows {
		rowMax = gm.Rows
	}

	for row := rowMin; row < rowMax; row++ {
		for col := colMin; col < colMax; col++ {
			// Check if all layers are empty at this cell
			allEmpty := true
			for _, layer := range gm.layers {
				if len(layer) > row && len(layer[row]) > col && layer[row][col] != 0 {
					allEmpty = false
					break
				}
			}
			if !allEmpty {
				continue
			}

			sx := float64(col)*tw - camX
			sy := float64(row)*th - camY

			// Checkerboard-style grass with variation
			variation := (col*3 + row*7) % 5
			var grassClr color.RGBA
			switch variation {
			case 0:
				grassClr = color.RGBA{58, 110, 42, 255}
			case 1:
				grassClr = color.RGBA{52, 100, 38, 255}
			case 2:
				grassClr = color.RGBA{65, 120, 48, 255}
			case 3:
				grassClr = color.RGBA{55, 105, 40, 255}
			default:
				grassClr = color.RGBA{60, 115, 44, 255}
			}

			DrawRect(screen, int(sx), int(sy), int(tw), int(th), grassClr)

			// Subtle darker border to give texture
			if (col+row)%2 == 0 {
				DrawRect(screen, int(sx), int(sy), int(tw), 1,
					color.RGBA{40, 80, 28, 180})
				DrawRect(screen, int(sx), int(sy), 1, int(th),
					color.RGBA{40, 80, 28, 180})
			}

			// Occasional small detail (blade of grass dot)
			if (col*13+row*17)%7 == 0 {
				DrawRect(screen, int(sx)+int(tw)/3, int(sy)+int(th)/4, 1, 3,
					color.RGBA{80, 150, 60, 200})
			}
			if (col*11+row*19)%9 == 1 {
				DrawRect(screen, int(sx)+2*int(tw)/3, int(sy)+int(th)/3, 1, 2,
					color.RGBA{90, 160, 65, 200})
			}
		}
	}
}

// DrawSignPrompts draws a [F] hint above each sign that is visible on screen.
func (gm *GameMap) DrawSignPrompts(screen *ebiten.Image, camX, camY float64) {
	for pos := range gm.signs {
		col, row := pos[0], pos[1]
		sx := float64(col*gm.TileW) - camX
		sy := float64(row*gm.TileH) - camY
		if sx < -48 || sx > float64(screenW)+48 || sy < -48 || sy > float64(screenH)+48 {
			continue
		}
		lbl := "[F]"
		DrawText(screen, lbl,
			int(sx)+gm.TileW/2-len(lbl)*fontW/2,
			int(sy)-4, colGold)
	}
}

// tileFlipH, tileFlipV, tileFlipD are the Tiled flip flag bits in a GID.
const (
	tileFlipH   = 0x80000000
	tileFlipV   = 0x40000000
	tileFlipD   = 0x20000000
	tileGIDMask = 0x1FFFFFFF
)

func (gm *GameMap) drawTile(screen *ebiten.Image, tileID int, sx, sy float64) {
	// Strip Tiled flip flags from the GID.
	flipH := (tileID & tileFlipH) != 0
	flipV := (tileID & tileFlipV) != 0
	flipD := (tileID & tileFlipD) != 0
	rawID := tileID & tileGIDMask

	if rawID == 0 {
		return
	}

	if gm.tileImg == nil {
		DrawRect(screen, int(sx), int(sy), gm.TileW, gm.TileH,
			color.RGBA{60, 60, 80, 255})
		return
	}
	idx := rawID - gm.firstGID
	if idx < 0 {
		return
	}
	col := idx % gm.tileCols
	row := idx / gm.tileCols
	src := image.Rect(col*gm.TileW, row*gm.TileH,
		(col+1)*gm.TileW, (row+1)*gm.TileH)
	b := gm.tileImg.Bounds()
	if src.Max.X > b.Max.X || src.Max.Y > b.Max.Y {
		return
	}

	op := &ebiten.DrawImageOptions{}
	tw := float64(gm.TileW)
	th := float64(gm.TileH)

	// Apply flip transformations around the tile centre.
	if flipD {
		// Anti-diagonal flip = transpose then flip horizontally.
		op.GeoM.Scale(1, -1)
		op.GeoM.Rotate(math.Pi / 2)
	}
	if flipH {
		op.GeoM.Scale(-1, 1)
		op.GeoM.Translate(tw, 0)
	}
	if flipV {
		op.GeoM.Scale(1, -1)
		op.GeoM.Translate(0, th)
	}
	op.GeoM.Translate(sx, sy)
	screen.DrawImage(gm.tileImg.SubImage(src).(*ebiten.Image), op)
}

// drawImageTile draws a tile from an image-collection tileset at its natural size.
func (gm *GameMap) drawImageTile(screen *ebiten.Image, tileID int, anim *AnimImage, sx, sy float64) {
	flipH := (tileID & tileFlipH) != 0
	flipV := (tileID & tileFlipV) != 0
	flipD := (tileID & tileFlipD) != 0

	frame := anim.Frame(gm.mapTime)
	if frame == nil {
		return
	}
	b := frame.Bounds()
	tw := float64(b.Dx())
	th := float64(b.Dy())

	op := &ebiten.DrawImageOptions{}
	if flipD {
		op.GeoM.Scale(1, -1)
		op.GeoM.Rotate(math.Pi / 2)
	}
	if flipH {
		op.GeoM.Scale(-1, 1)
		op.GeoM.Translate(tw, 0)
	}
	if flipV {
		op.GeoM.Scale(1, -1)
		op.GeoM.Translate(0, th)
	}
	op.GeoM.Translate(sx, sy)
	screen.DrawImage(frame, op)
}

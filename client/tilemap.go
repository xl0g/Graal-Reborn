package main

import (
	"encoding/xml"
	"image"
	"image/color"
	"os"
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
	FirstGID int `xml:"firstgid,attr"`
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

// ── GameMap ───────────────────────────────────────────────────

// GameMap holds the parsed TMX map and provides collision/sign queries.
type GameMap struct {
	TileW, TileH int
	Cols, Rows   int

	layers    [][][]int         // all layers [layer][row][col]
	collision [][]bool          // solid tiles
	signs     map[[2]int]string // (col,row) → panneau text

	tileImg  *ebiten.Image
	tileCols int
	firstGID int
}

// LoadTMX parses a .tmx file (external TSX assumed to be classiciphone_pics4).
func LoadTMX(path string) (*GameMap, error) {
	raw, err := os.ReadFile(path)
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
		signs: make(map[[2]int]string),
	}

	if len(mx.Tilesets) > 0 {
		gm.firstGID = mx.Tilesets[0].FirstGID
		gm.tileCols = 64
		img, _, _ := ebitenutil.NewImageFromFile(
			"Assets/offline/levels/tiles/classiciphone_pics4.png")
		gm.tileImg = img
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
		for _, p := range layer.Props {
			if p.Name == "collision" && p.Value == "true" {
				isCollision = true
			}
			if p.Name == "panneau" {
				signText = p.Value
			}
		}
		if isCollision {
			for r := 0; r < layer.Rows; r++ {
				for c := 0; c < layer.Cols; c++ {
					if tiles[r][c] != 0 {
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
	}
	return gm, nil
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
			v, _ := strconv.Atoi(strings.TrimSpace(fields[i]))
			grid[r][c] = v
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
func (gm *GameMap) Draw(screen *ebiten.Image, camX, camY float64) {
	for _, layer := range gm.layers {
		for row := 0; row < gm.Rows; row++ {
			for col := 0; col < gm.Cols; col++ {
				id := layer[row][col]
				if id == 0 {
					continue
				}
				sx := float64(col*gm.TileW) - camX
				sy := float64(row*gm.TileH) - camY
				if sx+float64(gm.TileW) < 0 || sx > float64(screenW) ||
					sy+float64(gm.TileH) < 0 || sy > float64(screenH) {
					continue
				}
				gm.drawTile(screen, id, sx, sy)
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

func (gm *GameMap) drawTile(screen *ebiten.Image, tileID int, sx, sy float64) {
	if gm.tileImg == nil {
		DrawRect(screen, int(sx), int(sy), gm.TileW, gm.TileH,
			color.RGBA{60, 60, 80, 255})
		return
	}
	idx := tileID - gm.firstGID
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
	op.GeoM.Translate(sx, sy)
	screen.DrawImage(gm.tileImg.SubImage(src).(*ebiten.Image), op)
}

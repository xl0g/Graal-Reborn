package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ── NW tile encoding (base64) ────────────────────────────────────────────────

const nwEmptyTile = 2047 // "f/" — rendered as transparent in NW format

// nwDecodeChar converts a base64 ASCII character to its 6-bit value.
func nwDecodeChar(c byte) int {
	switch {
	case c >= 'A' && c <= 'Z':
		return int(c - 'A')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 26
	case c >= '0' && c <= '9':
		return int(c-'0') + 52
	case c == '+':
		return 62
	case c == '/':
		return 63
	}
	return 0
}

// nwDecodeTile decodes two base64 characters into a tile index (0-4095).
func nwDecodeTile(c1, c2 byte) int {
	return nwDecodeChar(c1)*64 + nwDecodeChar(c2)
}

// nwTilesetCols is the number of tile columns in classiciphone_pics4.png (2048/16).
const nwTilesetCols = 128

// NWTileToGID converts a raw NW 12-bit tile value to a 1-based Tiled GID.
//
// The NW encoding is NOT a linear index.  Each 12-bit value encodes a
// (section, row, col) triple that must be unpacked before mapping to a
// linear position in the 128-column tileset:
//
//	ty      = (g >> 4) % 32          — row in the tileset (0-31)
//	section = (g >> 4) / 32          — which 16-wide column bank (0-7)
//	tx      = (g & 0xF) + 16*section — absolute column in the tileset (0-127)
//	GID     = ty*128 + tx + 1        — 1-based Tiled GID
//
// The transparent tile (raw value 2047, "f/") maps to GID 0 (empty cell).
func NWTileToGID(g int) int {
	if g == nwEmptyTile {
		return 0
	}
	ty := (g >> 4) % 32
	section := (g >> 4) / 32
	tx := (g & 0xF) + 16*section
	return ty*nwTilesetCols + tx + 1
}

// ── NW level types ───────────────────────────────────────────────────────────

// NWBoard represents one BOARD row in an NW file.
type NWBoard struct {
	X, Y, Width, Layer int
	Tiles              []int // decoded tile indices, len = Width
}

// NWNPC represents a NPC entry in an NW file.
type NWNPC struct {
	Image  string  // gani/image filename, or "" for none
	X, Y   float64 // position in tile units
	Script string  // raw GraalScript body
}

// NWLevel holds the parsed content of one .nw file.
type NWLevel struct {
	Name   string
	Boards []*NWBoard
	NPCs   []*NWNPC

	// Derived: per-layer 64×64 grid (layer index → [row][col] GID)
	Layers [][][]int
	// Derived: terrain grids built from tile-type data.
	Collision [][]bool
	Water     [][]bool
	Lava      [][]bool
}

// NWCols / NWRows: standard dimensions of one NW level.
const (
	NWCols = 64
	NWRows = 64
)

// ParseNWFile reads an .nw file and returns a fully decoded NWLevel.
// page selects the tile-type lookup (0 = TYPE0, 1 = TYPE1).
func ParseNWFile(path string, page int) (*NWLevel, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	lv := &NWLevel{
		Name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
	}

	scanner := bufio.NewScanner(f)
	// Check header.
	if scanner.Scan() {
		hdr := strings.TrimSpace(scanner.Text())
		if hdr != "GLEVNW01" {
			return nil, fmt.Errorf("not an NW file (header %q)", hdr)
		}
	}

	var (
		inNPC      bool
		npcImg     string
		npcX, npcY float64
		npcLines   []string
	)

	for scanner.Scan() {
		line := scanner.Text()
		// ── NPC parsing ──────────────────────────────────────────────────
		if inNPC {
			if strings.TrimSpace(line) == "NPCEND" {
				lv.NPCs = append(lv.NPCs, &NWNPC{
					Image:  npcImg,
					X:      npcX,
					Y:      npcY,
					Script: strings.Join(npcLines, "\n"),
				})
				inNPC = false
				npcLines = nil
			} else {
				npcLines = append(npcLines, line)
			}
			continue
		}
		if strings.HasPrefix(line, "NPC ") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				inNPC = true
				npcImg = parts[1]
				if npcImg == "-" {
					npcImg = ""
				}
				npcX, _ = strconv.ParseFloat(parts[2], 64)
				npcY, _ = strconv.ParseFloat(parts[3], 64)
			}
			continue
		}
		// ── BOARD parsing ────────────────────────────────────────────────
		if strings.HasPrefix(line, "BOARD ") {
			b := parseBOARD(line)
			if b != nil {
				lv.Boards = append(lv.Boards, b)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	lv.buildLayers(page)
	return lv, nil
}

// parseBOARD parses a single "BOARD x y w l tiledata" line.
func parseBOARD(line string) *NWBoard {
	// Split on first 5 spaces; rest is tile data.
	parts := strings.SplitN(strings.TrimPrefix(line, "BOARD "), " ", 5)
	if len(parts) < 5 {
		return nil
	}
	x, _ := strconv.Atoi(parts[0])
	y, _ := strconv.Atoi(parts[1])
	w, _ := strconv.Atoi(parts[2])
	l, _ := strconv.Atoi(parts[3])
	data := parts[4]

	b := &NWBoard{X: x, Y: y, Width: w, Layer: l}
	// Each tile is encoded as 2 base64 characters.
	for i := 0; i+1 < len(data) && len(b.Tiles) < w; i += 2 {
		b.Tiles = append(b.Tiles, nwDecodeTile(data[i], data[i+1]))
	}
	return b
}

// buildLayers stitches all BOARDs into per-layer 2D grids and builds collision.
func (lv *NWLevel) buildLayers(page int) {
	// Find max layer index.
	maxL := 0
	for _, b := range lv.Boards {
		if b.Layer > maxL {
			maxL = b.Layer
		}
	}

	lv.Layers = make([][][]int, maxL+1)
	for i := range lv.Layers {
		lv.Layers[i] = make([][]int, NWRows)
		for r := range lv.Layers[i] {
			lv.Layers[i][r] = make([]int, NWCols)
		}
	}

	lv.Collision = make([][]bool, NWRows)
	lv.Water = make([][]bool, NWRows)
	lv.Lava = make([][]bool, NWRows)
	for r := range lv.Collision {
		lv.Collision[r] = make([]bool, NWCols)
		lv.Water[r] = make([]bool, NWCols)
		lv.Lava[r] = make([]bool, NWCols)
	}

	for _, b := range lv.Boards {
		if b.Y < 0 || b.Y >= NWRows || b.Layer >= len(lv.Layers) {
			continue
		}
		row := lv.Layers[b.Layer][b.Y]
		for i, idx := range b.Tiles {
			col := b.X + i
			if col < 0 || col >= NWCols {
				continue
			}
			gid := NWTileToGID(idx)
			row[col] = gid
			tt := TileTypeOf(idx, page)
			if gid > 0 && IsSolid(tt) {
				lv.Collision[b.Y][col] = true
			}
			if gid > 0 && IsWater(tt) {
				lv.Water[b.Y][col] = true
			}
			if gid > 0 && IsLava(tt) {
				lv.Lava[b.Y][col] = true
			}
		}
	}
}

// CollisionGIDs returns a flat slice of GIDs for the collision layer:
// non-zero means solid, zero means passable.
// We use GID 1 (first tile) as a sentinel — the actual visual doesn't matter
// since this layer is typically invisible in-game.
func (lv *NWLevel) CollisionGIDs() []int {
	gids := make([]int, NWRows*NWCols)
	for r := 0; r < NWRows; r++ {
		for c := 0; c < NWCols; c++ {
			if lv.Collision[r][c] {
				gids[r*NWCols+c] = 1
			}
		}
	}
	return gids
}

// TerrainGIDs returns a flat slice indicating terrain tiles.
// Pass lavaMode=false for water, lavaMode=true for lava.
// Returns nil when the grid is entirely empty (saves JSON bandwidth).
func (lv *NWLevel) TerrainGIDs(lavaMode bool) []int {
	grid := lv.Water
	if lavaMode {
		grid = lv.Lava
	}
	gids := make([]int, NWRows*NWCols)
	any := false
	for r := 0; r < NWRows; r++ {
		for c := 0; c < NWCols; c++ {
			if grid[r][c] {
				gids[r*NWCols+c] = 1
				any = true
			}
		}
	}
	if !any {
		return nil
	}
	return gids
}

// ── GMAP ────────────────────────────────────────────────────────────────────

// GMap holds the parsed content of a .gmap file.
type GMap struct {
	Name        string
	Width       int
	Height      int
	LevelNames  [][]string // [row][col] → .nw filename
}

// ParseGMapFile reads a .gmap file.
func ParseGMapFile(path string) (*GMap, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gm := &GMap{
		Name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
	}

	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		hdr := strings.TrimSpace(scanner.Text())
		if hdr != "GRMAP001" {
			return nil, fmt.Errorf("not a GMAP file (header %q)", hdr)
		}
	}

	inNames := false
	row := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "LEVELNAMESEND" {
			inNames = false
			continue
		}
		if strings.HasPrefix(line, "WIDTH ") {
			gm.Width, _ = strconv.Atoi(strings.TrimPrefix(line, "WIDTH "))
			continue
		}
		if strings.HasPrefix(line, "HEIGHT ") {
			gm.Height, _ = strconv.Atoi(strings.TrimPrefix(line, "HEIGHT "))
			continue
		}
		if line == "LEVELNAMES" {
			inNames = true
			gm.LevelNames = make([][]string, 0, gm.Height)
			row = 0
			continue
		}
		if inNames {
			names := parseQuotedCSV(line)
			gm.LevelNames = append(gm.LevelNames, names)
			row++
			_ = row
		}
	}
	return gm, scanner.Err()
}

// parseQuotedCSV splits a line of the form "a.nw","b.nw","c.nw" into names.
func parseQuotedCSV(line string) []string {
	var names []string
	for _, part := range strings.Split(line, ",") {
		s := strings.TrimSpace(part)
		s = strings.Trim(s, `"`)
		names = append(names, s)
	}
	return names
}

// ── GraalScript → Lua conversion ─────────────────────────────────────────────

// GraalScriptToLua performs a best-effort conversion of a GraalScript NPC
// body to a Lua script suitable for the server's resource system.
//
// Supported conversions:
//   join <resource>;         → joinResource("<resource>")
//   this.<prop> = <val>;     → self.<prop> = <val>
//   if (event == "<e>"){     → if event == "<e>" then
//   dontblock;               → self.dontblock = true
//   setcoloreffect r,g,b,a;  → self:setColorEffect(r,g,b,a)
//   drawaslight;             → self.drawaslight = true
//   //#CLIENTSIDE            → -- (client-side scripts are no-ops on server)
func GraalScriptToLua(script string) string {
	lines := strings.Split(script, "\n")
	var out []string
	clientSide := false

	out = append(out, "-- Auto-converted from GraalScript")

	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)

		// Client-side section marker.
		if trimmed == "//#CLIENTSIDE" {
			clientSide = true
			out = append(out, "-- [CLIENT-SIDE block below — server no-op]")
			continue
		}
		if clientSide {
			out = append(out, "-- "+trimmed)
			continue
		}

		// Strip trailing semicolons for comparison.
		stmt := strings.TrimSuffix(trimmed, ";")

		switch {
		case stmt == "":
			out = append(out, "")

		case strings.HasPrefix(stmt, "join "):
			res := strings.TrimPrefix(stmt, "join ")
			out = append(out, fmt.Sprintf("joinResource(%q)", res))

		case stmt == "dontblock":
			out = append(out, "self.dontblock = true")

		case stmt == "drawaslight":
			out = append(out, "self.drawaslight = true")

		case strings.HasPrefix(stmt, "setcoloreffect "):
			args := strings.TrimPrefix(stmt, "setcoloreffect ")
			out = append(out, "self:setColorEffect("+args+")")

		case strings.HasPrefix(stmt, "this."):
			// this.prop = value  →  self.prop = value
			lua := strings.Replace(stmt, "this.", "self.", 1)
			lua = strings.TrimSuffix(lua, ";")
			out = append(out, lua)

		case strings.HasPrefix(stmt, "if (event == "):
			// if (event == "X"){  →  if event == "X" then
			inner := strings.TrimPrefix(stmt, "if (")
			inner = strings.TrimSuffix(inner, "){")
			inner = strings.TrimSuffix(inner, ")")
			out = append(out, "if "+inner+" then")

		case trimmed == "}" || trimmed == "}{":
			out = append(out, "end")

		default:
			// Emit as a comment so original intent is preserved.
			out = append(out, "-- "+trimmed)
		}
	}
	return strings.Join(out, "\n")
}

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

// NWSign represents a readable sign in an NW file.
type NWSign struct {
	X, Y int    // tile position
	Text string // display text (may contain #l/#r for left/right alignment)
}

// NWLink represents a warp link in an NW file.
// When the player enters the area (X, Y, Width, Height), they warp to
// (DestX, DestY) in DestMap.
type NWLink struct {
	DestMap        string
	X, Y           int     // top-left tile of the trigger zone
	Width, Height  int     // size of the trigger zone (tiles)
	DestX, DestY   float64 // spawn position in the destination map (tile units)
}

// NWLevel holds the parsed content of one .nw file.
type NWLevel struct {
	Name   string
	Boards []*NWBoard
	NPCs   []*NWNPC
	Signs  []*NWSign
	Links  []*NWLink

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

		inSign    bool
		signX     int
		signY     int
		signLines []string
	)

	for scanner.Scan() {
		line := scanner.Text()

		// ── SIGN parsing ─────────────────────────────────────────────────
		if inSign {
			if strings.TrimSpace(line) == "SIGNEND" {
				text := strings.Join(signLines, "\n")
				// Strip Graal alignment markers (#l #r)
				text = strings.ReplaceAll(text, "#l", "")
				text = strings.ReplaceAll(text, "#r", "")
				text = strings.TrimSpace(text)
				if text != "" {
					lv.Signs = append(lv.Signs, &NWSign{X: signX, Y: signY, Text: text})
				}
				inSign = false
				signLines = nil
			} else {
				signLines = append(signLines, line)
			}
			continue
		}
		if strings.HasPrefix(line, "SIGN ") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				inSign = true
				signX, _ = strconv.Atoi(parts[1])
				signY, _ = strconv.Atoi(parts[2])
			}
			continue
		}

		// ── LINK parsing ─────────────────────────────────────────────────
		// Format: LINK destmap x y w h destx desty
		if strings.HasPrefix(line, "LINK ") {
			parts := strings.Fields(line)
			if len(parts) >= 8 {
				lnk := &NWLink{
					DestMap: parts[1],
				}
				lnk.X, _ = strconv.Atoi(parts[2])
				lnk.Y, _ = strconv.Atoi(parts[3])
				lnk.Width, _ = strconv.Atoi(parts[4])
				lnk.Height, _ = strconv.Atoi(parts[5])
				lnk.DestX, _ = strconv.ParseFloat(parts[6], 64)
				lnk.DestY, _ = strconv.ParseFloat(parts[7], 64)
				lv.Links = append(lv.Links, lnk)
			}
			continue
		}

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
// The most common GraalScript constructs are handled:
//
//	join <res>;                → joinResource("<res>")
//	dontblock;                 → self.dontblock = true
//	drawoverplayer;            → self.drawoverplayer = true
//	drawunderplayer;           → self.drawunderplayer = true
//	drawaslight;               → self.drawaslight = true
//	canbecarried;              → self.canbecarried = true
//	block;                     → self.block = true
//	this.<p> = <v>;            → self.<p> = <v>
//	timeout = N;               → self.timeout = N
//	say "text";                → self.chat = "text"
//	if (created) { ... }       → AddEventHandler("onCreated", function(self) ... end)
//	if (playerenters) { ... }  → AddEventHandler("onPlayerEnters", function(self, player) ... end)
//	if (timeout) { ... }       → AddEventHandler("onTimeout", function(self) ... end)
//	if (playertouchsme) { ... }→ AddEventHandler("onPlayerTouch", function(self, player) ... end)
//	if (playersays <x>)        → AddEventHandler("onPlayerSays", function(self,player,msg) ... end)
//	function f(args) { ... }   → function f(args) ... end
//	//#CLIENTSIDE              → everything below becomes a Lua comment
func GraalScriptToLua(script string) string {
	if strings.TrimSpace(script) == "" {
		return ""
	}

	// Pre-process: expand compound single-line event blocks like
	// "if (created) { dontblock; drawoverplayer; }" into multiple lines.
	expanded := expandInlineBlocks(script)

	lines := strings.Split(expanded, "\n")
	var out []string
	clientSide := false
	depth := 0 // brace nesting depth inside an event block

	out = append(out, "-- Auto-converted from GraalScript")

	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)

		// //#CLIENTSIDE marks the start of a client-only section.
		if strings.HasPrefix(trimmed, "//#CLIENTSIDE") {
			clientSide = true
			out = append(out, "-- [CLIENT-SIDE block — server no-op]")
			continue
		}
		if clientSide {
			if trimmed != "" {
				out = append(out, "-- "+trimmed)
			}
			continue
		}

		if trimmed == "" {
			out = append(out, "")
			continue
		}

		// Strip C-style line comment.
		if strings.HasPrefix(trimmed, "//") {
			out = append(out, "--"+trimmed[2:])
			continue
		}

		// Strip trailing semicolons.
		stmt := strings.TrimSuffix(trimmed, ";")
		stmt = strings.TrimSpace(stmt)

		// Closing braces (may also open a new block: "}{").
		if stmt == "}" || stmt == "};" {
			if depth > 0 {
				depth--
			}
			out = append(out, "end")
			continue
		}
		if stmt == "}{" {
			// Close previous block, open new one (rare in NPC scripts).
			if depth > 0 {
				depth--
			}
			out = append(out, "end")
			depth++
			out = append(out, "do")
			continue
		}
		// Lone opening brace.
		if stmt == "{" {
			depth++
			out = append(out, "do")
			continue
		}

		// ── Event blocks ──────────────────────────────────────────────────────
		if lua, ok := convertEventBlock(stmt); ok {
			depth++
			out = append(out, lua)
			continue
		}

		// ── function declaration ──────────────────────────────────────────────
		// function name(args) {  →  function name(args)
		if strings.HasPrefix(stmt, "function ") {
			body := strings.TrimPrefix(stmt, "function ")
			body = strings.TrimSuffix(body, "{")
			body = strings.TrimSpace(body)
			out = append(out, "function "+body)
			if strings.HasSuffix(stmt, "{") {
				depth++
			}
			continue
		}

		// ── Simple statement conversions ──────────────────────────────────────
		if lua, ok := convertSimpleStmt(stmt); ok {
			out = append(out, lua)
			continue
		}

		// Unknown — keep as comment so intent is preserved.
		out = append(out, "-- "+trimmed)
	}

	return strings.Join(out, "\n")
}

// expandInlineBlocks rewrites compact single-line GraalScript event bodies to
// multi-line form so the line-by-line converter can handle them.
// e.g.  "if (created) { dontblock; drawoverplayer; }"
//
//	→  "if (created) {\ndontblock;\ndrawoverplayer;\n}"
func expandInlineBlocks(s string) string {
	var sb strings.Builder
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		// Detect patterns like "if (created) { ... }" or "if(created) stmt;"
		// where the whole block is on one line.
		openIdx := strings.Index(t, "{")
		closeIdx := strings.LastIndex(t, "}")
		if openIdx >= 0 && closeIdx > openIdx {
			// There's a { ... } on this line — split it up.
			before := t[:openIdx+1]
			inner := t[openIdx+1 : closeIdx]
			after := t[closeIdx+1:]
			sb.WriteString(before + "\n")
			// Split inner by semicolons to get individual statements.
			for _, part := range strings.Split(inner, ";") {
				part = strings.TrimSpace(part)
				if part != "" {
					sb.WriteString(part + ";\n")
				}
			}
			sb.WriteString("}" + after + "\n")
			continue
		}
		sb.WriteString(line + "\n")
	}
	return sb.String()
}

// convertEventBlock matches GraalScript event-if patterns and returns the
// equivalent Lua AddEventHandler call opening line, plus true.
func convertEventBlock(stmt string) (string, bool) {
	// Normalise: remove spaces around parens and strip trailing `{` or `){`.
	clean := strings.TrimSuffix(stmt, "{")
	clean = strings.TrimSuffix(strings.TrimSpace(clean), ")")
	clean = strings.TrimSpace(clean)

	// Map of GraalScript event keyword → (Lua event name, parameter list).
	type evDef struct {
		keyword string
		event   string
		params  string
	}
	events := []evDef{
		{"if (created)", "onCreated", "self"},
		{"if(created)", "onCreated", "self"},
		{"if (playerenters)", "onPlayerEnters", "self, player"},
		{"if(playerenters)", "onPlayerEnters", "self, player"},
		{"if (playertouchsme)", "onPlayerTouch", "self, player"},
		{"if(playertouchsme)", "onPlayerTouch", "self, player"},
		{"if (timeout)", "onTimeout", "self"},
		{"if(timeout)", "onTimeout", "self"},
		{"if (activate)", "onActivate", "self, player"},
		{"if(activate)", "onActivate", "self, player"},
		{"if (playerleaves)", "onPlayerLeaves", "self, player"},
		{"if(playerleaves)", "onPlayerLeaves", "self, player"},
	}
	for _, ev := range events {
		if strings.EqualFold(clean, ev.keyword) || strings.HasPrefix(strings.ToLower(clean), strings.ToLower(ev.keyword)) {
			return fmt.Sprintf("AddEventHandler(%q, function(%s)", ev.event, ev.params), true
		}
	}

	// if (playersays "text") or if (playersays text)
	if strings.HasPrefix(strings.ToLower(clean), "if (playersays ") || strings.HasPrefix(strings.ToLower(clean), "if(playersays ") {
		return `AddEventHandler("onPlayerSays", function(self, player, msg)`, true
	}

	// Generic if (event == "X") pattern.
	if strings.HasPrefix(clean, "if (event == ") || strings.HasPrefix(clean, "if(event == ") {
		inner := clean
		inner = strings.TrimPrefix(inner, "if (")
		inner = strings.TrimPrefix(inner, "if(")
		inner = strings.TrimSpace(inner)
		return "if " + inner + " then", true
	}

	return "", false
}

// convertSimpleStmt maps a single GraalScript statement to Lua.
// Returns ("", false) when no match is found.
func convertSimpleStmt(stmt string) (string, bool) {
	lower := strings.ToLower(stmt)

	// Boolean flags.
	flags := map[string]string{
		"dontblock":       "self.dontblock = true",
		"block":           "self.block = true",
		"drawoverplayer":  "self.drawoverplayer = true",
		"drawunderplayer": "self.drawunderplayer = true",
		"drawaslight":     "self.drawaslight = true",
		"canbecarried":    "self.canbecarried = true",
		"nocontrols":      "self.nocontrols = true",
		"fixedposition":   "self.fixedposition = true",
	}
	if lua, ok := flags[lower]; ok {
		return lua, true
	}

	// join resource  or  join("resource")
	if strings.HasPrefix(lower, "join ") {
		res := strings.TrimPrefix(stmt, "join ")
		res = strings.Trim(res, `"'()`)
		return fmt.Sprintf("joinResource(%q)", res), true
	}
	if strings.HasPrefix(lower, `join("`) || strings.HasPrefix(lower, `join('`) {
		res := stmt[5:]
		res = strings.Trim(res, `"'()`)
		return fmt.Sprintf("joinResource(%q)", res), true
	}

	// this.<prop> = <val>  →  self.<prop> = <val>
	if strings.HasPrefix(stmt, "this.") {
		return "self." + stmt[5:], true
	}

	// timeout = N  →  self.timeout = N
	if strings.HasPrefix(lower, "timeout = ") || strings.HasPrefix(lower, "timeout=") {
		val := stmt[strings.Index(stmt, "=")+1:]
		val = strings.TrimSpace(val)
		return "self.timeout = " + val, true
	}

	// say "text"  →  self.chat = "text"
	if strings.HasPrefix(lower, "say ") {
		txt := strings.TrimPrefix(stmt, "say ")
		txt = strings.TrimPrefix(stmt, "say ")
		txt = stmt[4:]
		// Wrap bare text in quotes if not already quoted.
		if !strings.HasPrefix(txt, `"`) && !strings.HasPrefix(txt, `'`) {
			txt = `"` + txt + `"`
		}
		return "self.chat = " + txt, true
	}

	// setcoloreffect r,g,b,a
	if strings.HasPrefix(lower, "setcoloreffect ") {
		args := stmt[15:]
		return "self:setColorEffect(" + args + ")", true
	}

	// setimgpart img,x,y,w,h
	if strings.HasPrefix(lower, "setimgpart ") {
		args := stmt[11:]
		parts := strings.SplitN(args, ",", 2)
		if len(parts) == 2 {
			return fmt.Sprintf("self:setImgPart(%q, %s)", strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])), true
		}
		return "self:setImgPart(" + args + ")", true
	}

	// warp map x y  →  warpPlayer(player, "map", x, y)
	if strings.HasPrefix(lower, "warp ") {
		parts := strings.Fields(stmt[5:])
		if len(parts) >= 3 {
			return fmt.Sprintf("warpPlayer(player, %q, %s, %s)", parts[0], parts[1], parts[2]), true
		}
	}

	// setmap map x y  →  setMap("map", x, y)
	if strings.HasPrefix(lower, "setmap ") {
		parts := strings.Fields(stmt[7:])
		if len(parts) >= 3 {
			return fmt.Sprintf("setMap(%q, %s, %s)", parts[0], parts[1], parts[2]), true
		}
	}

	return "", false
}

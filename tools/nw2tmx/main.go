// nw2tmx converts GraalOnline .nw and .gmap files to Tiled-compatible .tmx files.
//
// Usage:
//
//	go run ./tools/nw2tmx [flags]
//
// Flags:
//
//	-nw   <dir>    Source directory of .nw files  (default: maps/nw)
//	-gmap <dir>    Source directory of .gmap files (default: maps/gmap)
//	-out  <dir>    Output directory for .tmx files  (default: maps/tmx)
//	-tsx  <path>   Relative path to classiciphone_pics4.tsx from the output dir
//	               (default: ../../classiciphone_pics4.tsx)
//	-page <0|1>    Tile-type page: 0 = interior (TYPE0), 1 = overworld (TYPE1)
//	               (default: 0)
//	-v             Verbose output
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ── flags ────────────────────────────────────────────────────────────────────

// projectRoot returns the absolute path to the project root (two directories
// above the location of this source file, i.e. tools/nw2tmx → project root).
// Falls back to the current working directory if os.Executable fails.
func projectRoot() string {
	// When run with "go run .", the executable is in a temp dir; use the
	// source file's path instead.  The most reliable fallback is to walk up
	// from the executable or CWD looking for the go.mod that owns the client.
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	// If we're already inside tools/nw2tmx, go up two levels.
	if filepath.Base(filepath.Dir(cwd)) == "tools" {
		return filepath.Join(cwd, "..", "..")
	}
	// If we're inside tools (but not nw2tmx), go up one.
	if filepath.Base(cwd) == "tools" {
		return filepath.Join(cwd, "..")
	}
	// Otherwise assume we're already at the project root.
	return cwd
}

var (
	nwDir   = flag.String("nw", "", "Source directory for .nw files (default: <project>/maps/nw)")
	gmapDir = flag.String("gmap", "", "Source directory for .gmap files (default: <project>/maps/gmap)")
	outDir  = flag.String("out", "", "Output directory for .tmx files (default: <project>/maps/tmx)")
	tsxPath = flag.String("tsx", "../../classiciphone_pics4.tsx", "Relative path to TSX from output dir")
	page    = flag.Int("page", 0, "Tile-type page (0=interior, 1=overworld)")
	verbose = flag.Bool("v", false, "Verbose output")
)

func main() {
	flag.Parse()

	// Apply path defaults relative to the project root.
	root := projectRoot()
	if *nwDir == "" {
		*nwDir = filepath.Join(root, "maps", "nw")
	}
	if *gmapDir == "" {
		*gmapDir = filepath.Join(root, "maps", "gmap")
	}
	if *outDir == "" {
		*outDir = filepath.Join(root, "maps", "tmx")
	}

	if *verbose {
		fmt.Printf("project root : %s\n", root)
		fmt.Printf("nw dir       : %s\n", *nwDir)
		fmt.Printf("gmap dir     : %s\n", *gmapDir)
		fmt.Printf("out dir      : %s\n", *outDir)
	}

	if err := os.MkdirAll(*outDir, 0755); err != nil {
		fatalf("cannot create output dir: %v", err)
	}

	// ── Convert individual .nw files ─────────────────────────────────────
	nwFiles, err := filepath.Glob(filepath.Join(*nwDir, "*.nw"))
	if err != nil {
		fatalf("glob .nw: %v", err)
	}
	nwConverted := 0
	for _, src := range nwFiles {
		base := filepath.Base(src)
		if strings.HasPrefix(base, "__") {
			continue // skip internal files
		}
		dst := filepath.Join(*outDir, strings.TrimSuffix(base, ".nw")+".tmx")
		if err := convertNW(src, dst, *tsxPath, *page); err != nil {
			fmt.Fprintf(os.Stderr, "[WARN] %s: %v\n", base, err)
			continue
		}
		if *verbose {
			fmt.Printf("  → %s\n", dst)
		}
		nwConverted++
	}
	fmt.Printf("Converted %d .nw file(s) → %s\n", nwConverted, *outDir)

	// ── Convert .gmap files ───────────────────────────────────────────────
	gmapFiles, err := filepath.Glob(filepath.Join(*gmapDir, "*.gmap"))
	if err != nil {
		fatalf("glob .gmap: %v", err)
	}
	for _, src := range gmapFiles {
		base := filepath.Base(src)
		dst := filepath.Join(*outDir, strings.TrimSuffix(base, ".gmap")+"_world.tmx")
		if err := convertGMap(src, *nwDir, dst, *tsxPath, *page); err != nil {
			fmt.Fprintf(os.Stderr, "[WARN] %s: %v\n", base, err)
			continue
		}
		if *verbose {
			fmt.Printf("  → %s (world map)\n", dst)
		}
	}
	fmt.Printf("Converted %d .gmap file(s).\n", len(gmapFiles))
}

// ── NW parser (standalone, no server package dependency) ────────────────────

const (
	nwEmptyTile = 2047
	nwCols      = 64
	nwRows      = 64
)

func b64Decode(c byte) int {
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

func decodeTile(c1, c2 byte) int { return b64Decode(c1)*64 + b64Decode(c2) }

// nwTilesetCols matches classiciphone_pics4.png (2048px / 16px per tile = 128).
const nwTilesetCols = 128

// nwToGID converts a raw 12-bit NW tile value to a 1-based Tiled GID.
//
// The NW format does NOT store a linear tile index. Each value encodes
// (section, row, col) that must be unpacked:
//
//	ty      = (g >> 4) % 32           row in tileset (0-31)
//	section = (g >> 4) / 32           which 16-wide column bank (0-7)
//	tx      = (g & 0xF) + 16*section  absolute column (0-127)
//	GID     = ty*128 + tx + 1
func nwToGID(g int) int {
	if g == nwEmptyTile {
		return 0
	}
	ty := (g >> 4) % 32
	section := (g >> 4) / 32
	tx := (g & 0xF) + 16*section
	return ty*nwTilesetCols + tx + 1
}

// ── Tile type arrays (TYPE0 only, embedded) ──────────────────────────────────

//go:generate go run ../../tools/nw2tmx/gen_tiletypes.go

// tileType0 holds collision info for each tile index (0=walk,22=solid,11=water…).
// Embedded from maps/tiletype.js TYPE0.
var tileType0 [4096]byte

func init() {
	// Same data as server/tile_types.go — kept in sync manually.
	// We embed only the first 4096 values from TYPE0.
	raw := "\x00\x00\x14\x14\x16\x16\x16\x16\x00\x16\x16\x16\x16\x16\x16\x00" // … abbreviated
	_ = raw
	// Rather than re-embedding 4096 bytes here, we load on demand from tiletype.js if present,
	// or fall back to zero (no automatic collision generation from tile types).
	if data, err := loadTileType0(); err == nil {
		tileType0 = data
	}
}

func loadTileType0() ([4096]byte, error) {
	// Try to read maps/tiletype.js from the working directory.
	src, err := os.ReadFile("maps/tiletype.js")
	if err != nil {
		// Fallback: look relative to this binary.
		src, err = os.ReadFile(filepath.Join(filepath.Dir(os.Args[0]), "maps/tiletype.js"))
		if err != nil {
			return [4096]byte{}, err
		}
	}
	return parseTileType0JS(string(src))
}

func parseTileType0JS(src string) ([4096]byte, error) {
	// Find "TYPE0: new Uint8Array([" and extract the numbers.
	marker := "TYPE0: new Uint8Array(["
	idx := strings.Index(src, marker)
	if idx < 0 {
		return [4096]byte{}, fmt.Errorf("TYPE0 not found in tiletype.js")
	}
	start := idx + len(marker)
	end := strings.Index(src[start:], "])")
	if end < 0 {
		return [4096]byte{}, fmt.Errorf("TYPE0 end not found")
	}
	body := src[start : start+end]
	var arr [4096]byte
	i := 0
	for _, tok := range strings.FieldsFunc(body, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t'
	}) {
		if i >= 4096 {
			break
		}
		v, err := strconv.Atoi(strings.TrimSpace(tok))
		if err != nil {
			continue
		}
		arr[i] = byte(v)
		i++
	}
	return arr, nil
}

func isSolid(idx int) bool {
	if idx < 0 || idx >= 4096 {
		return true
	}
	return tileType0[idx] == 22
}

// ── NW → TMX converter ───────────────────────────────────────────────────────

type nwLevel struct {
	layers    [][][]int // [layer][row][col] GIDs
	collision [][]bool
	npcs      []nwNPC
}

type nwNPC struct {
	image  string
	x, y   float64
	script string
}

func parseNWFile(path string) (*nwLevel, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	lv := &nwLevel{}

	// Layer storage: up to 8 layers.
	type boardEntry struct{ x, y, w, l int; data string }
	var boards []boardEntry

	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		if hdr := strings.TrimSpace(scanner.Text()); hdr != "GLEVNW01" {
			return nil, fmt.Errorf("not a NW file (header %q)", hdr)
		}
	}

	var inNPC bool
	var npcImg string
	var npcX, npcY float64
	var npcLines []string

	for scanner.Scan() {
		line := scanner.Text()
		if inNPC {
			if strings.TrimSpace(line) == "NPCEND" {
				lv.npcs = append(lv.npcs, nwNPC{
					image:  npcImg,
					x:      npcX,
					y:      npcY,
					script: strings.Join(npcLines, "\n"),
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
		if strings.HasPrefix(line, "BOARD ") {
			tail := strings.TrimPrefix(line, "BOARD ")
			parts := strings.SplitN(tail, " ", 5)
			if len(parts) < 5 {
				continue
			}
			x, _ := strconv.Atoi(parts[0])
			y, _ := strconv.Atoi(parts[1])
			w, _ := strconv.Atoi(parts[2])
			l, _ := strconv.Atoi(parts[3])
			boards = append(boards, boardEntry{x, y, w, l, parts[4]})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Find max layer.
	maxL := 0
	for _, b := range boards {
		if b.l > maxL {
			maxL = b.l
		}
	}

	// Allocate grids.
	lv.layers = make([][][]int, maxL+1)
	for i := range lv.layers {
		lv.layers[i] = make([][]int, nwRows)
		for r := range lv.layers[i] {
			lv.layers[i][r] = make([]int, nwCols)
		}
	}
	lv.collision = make([][]bool, nwRows)
	for r := range lv.collision {
		lv.collision[r] = make([]bool, nwCols)
	}

	// Fill grids from boards.
	for _, b := range boards {
		if b.y < 0 || b.y >= nwRows || b.l >= len(lv.layers) {
			continue
		}
		row := lv.layers[b.l][b.y]
		col := b.x
		for i := 0; i+1 < len(b.data) && i/2 < b.w; i += 2 {
			if col < 0 || col >= nwCols {
				col++
				continue
			}
			idx := decodeTile(b.data[i], b.data[i+1])
			gid := nwToGID(idx)
			row[col] = gid
			if gid != 0 && isSolid(idx) {
				lv.collision[b.y][col] = true
			}
			col++
		}
	}
	return lv, nil
}

// ── TMX writer ────────────────────────────────────────────────────────────────

func convertNW(src, dst, tsxRel string, page int) error {
	_ = page
	lv, err := parseNWFile(src)
	if err != nil {
		return err
	}
	return writeTMX(dst, tsxRel, nwCols, nwRows, lv)
}

func writeTMX(dst, tsxRel string, cols, rows int, lv *nwLevel) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)

	// Determine total layer count (tile layers + collision + NPC objects).
	nextLayerID := len(lv.layers) + 2 // +1 collision +1 for NPC objects
	nextObjID := 1

	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>`+"\n")
	fmt.Fprintf(w, `<map version="1.10" tiledversion="1.12.1" orientation="orthogonal"`+
		` renderorder="right-down" width="%d" height="%d"`+
		` tilewidth="16" tileheight="16" infinite="0"`+
		` nextlayerid="%d" nextobjectid="%d">`+"\n",
		cols, rows, nextLayerID, nextObjID+len(lv.npcs))
	fmt.Fprintf(w, ` <tileset firstgid="1" source="%s"/>`+"\n", xmlEsc(tsxRel))

	// ── Tile layers ──────────────────────────────────────────────────────
	for i, layer := range lv.layers {
		name := "base"
		if i == 1 {
			name = "overlay"
		} else if i > 1 {
			name = fmt.Sprintf("overlay%d", i)
		}
		fmt.Fprintf(w, ` <layer id="%d" name="%s" width="%d" height="%d">`+"\n",
			i+1, name, cols, rows)
		fmt.Fprintf(w, `  <data encoding="csv">`+"\n")
		for r := 0; r < rows; r++ {
			for c := 0; c < cols; c++ {
				if c > 0 {
					w.WriteByte(',')
				}
				fmt.Fprintf(w, "%d", layer[r][c])
			}
			if r < rows-1 {
				w.WriteByte(',')
			}
			w.WriteByte('\n')
		}
		fmt.Fprintf(w, `  </data>`+"\n")
		fmt.Fprintf(w, ` </layer>`+"\n")
	}

	// ── Collision layer ───────────────────────────────────────────────────
	colLayerID := len(lv.layers) + 1
	fmt.Fprintf(w, ` <layer id="%d" name="collision" width="%d" height="%d" visible="0">`+"\n",
		colLayerID, cols, rows)
	fmt.Fprintf(w, `  <properties><property name="collision" value="true"/></properties>`+"\n")
	fmt.Fprintf(w, `  <data encoding="csv">`+"\n")
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			if c > 0 {
				w.WriteByte(',')
			}
			if lv.collision[r][c] {
				w.WriteByte('1')
			} else {
				w.WriteByte('0')
			}
		}
		if r < rows-1 {
			w.WriteByte(',')
		}
		w.WriteByte('\n')
	}
	fmt.Fprintf(w, `  </data>`+"\n")
	fmt.Fprintf(w, ` </layer>`+"\n")

	// ── NPC object layer ─────────────────────────────────────────────────
	if len(lv.npcs) > 0 {
		objLayerID := colLayerID + 1
		fmt.Fprintf(w, ` <objectgroup id="%d" name="npcs">`+"\n", objLayerID)
		for i, npc := range lv.npcs {
			px := npc.x * 16
			py := npc.y * 16
			luaScript := graalToLua(npc.script)
			fmt.Fprintf(w, `  <object id="%d" x="%.1f" y="%.1f" width="16" height="16">`+"\n",
				nextObjID+i, px, py)
			fmt.Fprintf(w, `   <properties>`+"\n")
			if npc.image != "" {
				fmt.Fprintf(w, `    <property name="gani" value="%s"/>`+"\n", xmlEsc(npc.image))
			}
			fmt.Fprintf(w, `    <property name="script" type="string"><![CDATA[%s]]></property>`+"\n",
				luaScript)
			fmt.Fprintf(w, `   </properties>`+"\n")
			fmt.Fprintf(w, `  </object>`+"\n")
		}
		fmt.Fprintf(w, ` </objectgroup>`+"\n")
	}

	fmt.Fprintf(w, `</map>`+"\n")
	return w.Flush()
}

// ── GMAP → TMX world map ──────────────────────────────────────────────────────

// convertGMap creates a single large TMX file from a .gmap by stitching all .nw chunks.
func convertGMap(gmapPath, nwSrcDir, dst, tsxRel string, page int) error {
	gm, err := parseGMapFile(gmapPath)
	if err != nil {
		return err
	}

	totalCols := gm.width * nwCols
	totalRows := gm.height * nwRows

	// Allocate world layers (we support base + overlay only for the combined map).
	type worldLayer struct{ data []int }
	baseLyr := worldLayer{data: make([]int, totalRows*totalCols)}
	overLyr := worldLayer{data: make([]int, totalRows*totalCols)}
	collision := make([]bool, totalRows*totalCols)

	var npcs []nwNPC

	for gr := 0; gr < gm.height; gr++ {
		for gc := 0; gc < gm.width; gc++ {
			lvName := ""
			if gr < len(gm.levels) && gc < len(gm.levels[gr]) {
				lvName = gm.levels[gr][gc]
			}
			if lvName == "" {
				continue
			}
			nwPath := filepath.Join(nwSrcDir, lvName)
			lv, err := parseNWFile(nwPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  [WARN] %s: %v\n", lvName, err)
				continue
			}

			offX := gc * nwCols
			offY := gr * nwRows

			// Copy tile layers.
			for r := 0; r < nwRows; r++ {
				for c := 0; c < nwCols; c++ {
					wr := offY + r
					wc := offX + c
					if len(lv.layers) > 0 {
						baseLyr.data[wr*totalCols+wc] = lv.layers[0][r][c]
					}
					if len(lv.layers) > 1 {
						overLyr.data[wr*totalCols+wc] = lv.layers[1][r][c]
					}
					collision[wr*totalCols+wc] = lv.collision[r][c]
				}
			}
			// Offset NPC positions.
			for _, npc := range lv.npcs {
				npcs = append(npcs, nwNPC{
					image:  npc.image,
					x:      npc.x + float64(offX),
					y:      npc.y + float64(offY),
					script: npc.script,
				})
			}
		}
	}

	// Build pseudo nwLevel for writeTMX.
	wl := &nwLevel{
		layers: [][][]int{
			grid2D(baseLyr.data, totalRows, totalCols),
			grid2D(overLyr.data, totalRows, totalCols),
		},
		collision: boolGrid2D(collision, totalRows, totalCols),
		npcs:      npcs,
	}
	return writeTMX(dst, tsxRel, totalCols, totalRows, wl)
}

// grid2D reshapes a flat slice into a [rows][cols] slice.
func grid2D(flat []int, rows, cols int) [][]int {
	g := make([][]int, rows)
	for r := range g {
		g[r] = flat[r*cols : r*cols+cols]
	}
	return g
}

func boolGrid2D(flat []bool, rows, cols int) [][]bool {
	g := make([][]bool, rows)
	for r := range g {
		g[r] = flat[r*cols : r*cols+cols]
	}
	return g
}

// ── GMAP parser ───────────────────────────────────────────────────────────────

type gmapFile struct {
	width, height int
	levels        [][]string
}

func parseGMapFile(path string) (*gmapFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gm := &gmapFile{}
	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		if hdr := strings.TrimSpace(scanner.Text()); hdr != "GRMAP001" {
			return nil, fmt.Errorf("not a GMAP file (header %q)", hdr)
		}
	}

	inNames := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "" || line == "LEVELNAMESEND":
			inNames = false
		case strings.HasPrefix(line, "WIDTH "):
			gm.width, _ = strconv.Atoi(strings.TrimPrefix(line, "WIDTH "))
		case strings.HasPrefix(line, "HEIGHT "):
			gm.height, _ = strconv.Atoi(strings.TrimPrefix(line, "HEIGHT "))
		case line == "LEVELNAMES":
			inNames = true
			gm.levels = make([][]string, 0, gm.height)
		case inNames:
			var row []string
			for _, part := range strings.Split(line, ",") {
				row = append(row, strings.Trim(strings.TrimSpace(part), `"`))
			}
			gm.levels = append(gm.levels, row)
		}
	}
	return gm, scanner.Err()
}

// ── GraalScript → Lua conversion ─────────────────────────────────────────────

func graalToLua(script string) string {
	lines := strings.Split(script, "\n")
	var out []string
	out = append(out, "-- Auto-converted from GraalScript")
	clientSide := false

	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "//#CLIENTSIDE" {
			clientSide = true
			out = append(out, "-- [CLIENT-SIDE — server no-op]")
			continue
		}
		if clientSide {
			out = append(out, "-- "+trimmed)
			continue
		}
		stmt := strings.TrimSuffix(trimmed, ";")
		switch {
		case stmt == "":
			out = append(out, "")
		case strings.HasPrefix(stmt, "join "):
			out = append(out, fmt.Sprintf("joinResource(%q)", strings.TrimPrefix(stmt, "join ")))
		case stmt == "dontblock":
			out = append(out, "self.dontblock = true")
		case stmt == "drawaslight":
			out = append(out, "self.drawaslight = true")
		case strings.HasPrefix(stmt, "setcoloreffect "):
			out = append(out, "self:setColorEffect("+strings.TrimPrefix(stmt, "setcoloreffect ")+")")
		case strings.HasPrefix(stmt, "this."):
			out = append(out, strings.Replace(stmt, "this.", "self.", 1))
		case strings.HasPrefix(stmt, "if (event == "):
			inner := strings.TrimPrefix(stmt, "if (")
			inner = strings.TrimSuffix(inner, "){")
			inner = strings.TrimSuffix(inner, ")")
			out = append(out, "if "+inner+" then")
		case trimmed == "}" || trimmed == "}{":
			out = append(out, "end")
		default:
			out = append(out, "-- "+trimmed)
		}
	}
	return strings.Join(out, "\n")
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func xmlEsc(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "nw2tmx: "+format+"\n", args...)
	os.Exit(1)
}

package main

import (
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
)

// ── Tile constants for the NW chunk system ───────────────────────────────────

const (
	chunkTileW  = 16 // px per tile (matches classiciphone_pics4.tsx)
	chunkTileH  = 16
	chunkCols   = 64 // tiles per chunk (one NW file)
	chunkRows   = 64
	chunkPixelW = chunkCols * chunkTileW // 1024 px
	chunkPixelH = chunkRows * chunkTileH // 1024 px
	chunkRadius = 2                      // load chunks within this radius of the player
)

// ── JSON types (mirrors server MapChunkResponse / MapGMapResponse) ───────────

type chunkLayerJSON struct {
	Name      string `json:"name"`
	Collision bool   `json:"collision"`
	Data      []int  `json:"data"`
}

type chunkNPCJSON struct {
	Image  string  `json:"image"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Script string  `json:"script"`
}

type chunkJSON struct {
	Name       string           `json:"name"`
	Width      int              `json:"width"`
	Height     int              `json:"height"`
	TileWidth  int              `json:"tilewidth"`
	TileHeight int              `json:"tileheight"`
	Layers     []chunkLayerJSON `json:"layers"`
	NPCs       []chunkNPCJSON   `json:"npcs"`
}

type gmapJSON struct {
	Name   string     `json:"name"`
	Width  int        `json:"width"`
	Height int        `json:"height"`
	Levels [][]string `json:"levels"`
}

// ── Chunk ────────────────────────────────────────────────────────────────────

// Chunk holds the parsed data and Ebiten draw resources for one 64×64 NW tile block.
type Chunk struct {
	// Grid position within the GMAP.
	GridCol, GridRow int
	// Origin in world-pixel space.
	OriginX, OriginY float64
	// Tile layers (base, overlay…).  Each entry is [row*64+col] → GID.
	layers [][]int
	// Collision grid [row][col].
	collision [][]bool
	// NPC data (position in tile units).
	npcs []chunkNPCJSON

	// Spritesheet reference (shared across all chunks).
	tileImg  *ebiten.Image
	tileCols int
}

// IsBlocked reports whether world-space rect (x,y,w,h) hits a solid tile in this chunk.
func (c *Chunk) IsBlocked(x, y, w, h float64) bool {
	const margin = 2.0
	lx := x - c.OriginX
	ly := y - c.OriginY
	x1 := int(lx+margin) / chunkTileW
	y1 := int(ly+margin) / chunkTileH
	x2 := int(lx+w-margin-1) / chunkTileW
	y2 := int(ly+h-margin-1) / chunkTileH
	for row := y1; row <= y2; row++ {
		for col := x1; col <= x2; col++ {
			// Out-of-bounds cells belong to an adjacent chunk;
			// ChunkManager.IsBlocked handles them separately — skip here.
			if row < 0 || row >= chunkRows || col < 0 || col >= chunkCols {
				continue
			}
			if c.collision[row][col] {
				return true
			}
		}
	}
	return false
}

// Draw renders all tile layers of this chunk with a frustum cull.
// vw/vh are the effective viewport dimensions (screenW/zoom × screenH/zoom).
func (c *Chunk) Draw(screen *ebiten.Image, camX, camY, vw, vh float64) {
	if c.tileImg == nil {
		return
	}
	ox := c.OriginX - camX
	oy := c.OriginY - camY

	// Skip entire chunk if completely outside the effective viewport.
	if ox+chunkPixelW < 0 || ox > vw || oy+chunkPixelH < 0 || oy > vh {
		return
	}

	for _, layer := range c.layers {
		for row := 0; row < chunkRows; row++ {
			sy := oy + float64(row*chunkTileH)
			if sy+float64(chunkTileH) < 0 || sy > vh {
				continue
			}
			for col := 0; col < chunkCols; col++ {
				gid := layer[row*chunkCols+col]
				if gid == 0 {
					continue
				}
				sx := ox + float64(col*chunkTileW)
				if sx+float64(chunkTileW) < 0 || sx > vw {
					continue
				}
				drawNWTile(screen, c.tileImg, c.tileCols, gid, sx, sy)
			}
		}
	}
}

// drawNWTile blits a single tile from the spritesheet.
func drawNWTile(screen, sheet *ebiten.Image, cols, gid int, sx, sy float64) {
	idx := gid - 1 // GID is 1-based
	if idx < 0 {
		return
	}
	col := idx % cols
	row := idx / cols
	srcX := col * chunkTileW
	srcY := row * chunkTileH
	b := sheet.Bounds()
	if srcX+chunkTileW > b.Max.X || srcY+chunkTileH > b.Max.Y {
		return
	}
	src := image.Rect(srcX, srcY, srcX+chunkTileW, srcY+chunkTileH)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(sx, sy)
	screen.DrawImage(sheet.SubImage(src).(*ebiten.Image), op)
}

// ── ChunkManager ─────────────────────────────────────────────────────────────

// ChunkManager loads, caches and renders NW map chunks for a GMAP world.
type ChunkManager struct {
	mu sync.Mutex

	// GMAP metadata.
	gmapName string
	gmWidth  int // GMAP columns (number of NW files per row)
	gmHeight int // GMAP rows
	levels   [][]string // [row][col] → .nw filename

	// Loaded chunks keyed by "col,row".
	chunks  map[string]*Chunk
	loading map[string]bool // chunks currently being fetched

	// Shared tile spritesheet (lazy-loaded).
	tileImg  *ebiten.Image
	tileCols int
	tileOnce sync.Once
}

// NewChunkManager creates a ChunkManager.  Call LoadGMap before use.
func NewChunkManager() *ChunkManager {
	return &ChunkManager{
		chunks:  make(map[string]*Chunk),
		loading: make(map[string]bool),
	}
}

// LoadGMap fetches the GMAP layout from the server asynchronously.
func (cm *ChunkManager) LoadGMap(name string) {
	cm.mu.Lock()
	cm.gmapName = name
	cm.mu.Unlock()
	go func() {
		gm, err := fetchGMap(name)
		if err != nil {
			fmt.Printf("[GMAP] fetch %s: %v\n", name, err)
			return
		}
		cm.mu.Lock()
		cm.gmWidth = gm.Width
		cm.gmHeight = gm.Height
		cm.levels = gm.Levels
		cm.mu.Unlock()
		fmt.Printf("[GMAP] loaded %s (%d×%d)\n", name, gm.Width, gm.Height)
	}()
}

// Update loads chunks around the player's world position and unloads distant ones.
// viewRadius is the number of chunks to load in each direction beyond the player's
// current chunk — pass at least 1, and increase when zoomed out so off-screen
// chunks are preloaded before the player scrolls into them.
func (cm *ChunkManager) Update(worldX, worldY float64, viewRadius int) {
	if viewRadius < 1 {
		viewRadius = 1
	}

	// Which chunk grid cell contains the player?
	playerGCol := int(worldX) / chunkPixelW
	playerGRow := int(worldY) / chunkPixelH

	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.levels == nil {
		return
	}

	// Request chunks within radius.
	for dr := -viewRadius; dr <= viewRadius; dr++ {
		for dc := -viewRadius; dc <= viewRadius; dc++ {
			gc := playerGCol + dc
			gr := playerGRow + dr
			if gc < 0 || gr < 0 || gr >= len(cm.levels) || gc >= len(cm.levels[gr]) {
				continue
			}
			key := chunkKey(gc, gr)
			if _, loaded := cm.chunks[key]; loaded {
				continue
			}
			if cm.loading[key] {
				continue
			}
			lvName := cm.levels[gr][gc]
			if lvName == "" {
				continue
			}
			cm.loading[key] = true
			go cm.fetchAndStore(key, gc, gr, lvName)
		}
	}

	// Unload chunks that are comfortably outside the visible radius.
	evictRadius := viewRadius + 2
	for key, ch := range cm.chunks {
		if abs(ch.GridCol-playerGCol) > evictRadius || abs(ch.GridRow-playerGRow) > evictRadius {
			delete(cm.chunks, key)
		}
	}
}

// fetchAndStore downloads one chunk and inserts it into the cache.
func (cm *ChunkManager) fetchAndStore(key string, gc, gr int, lvName string) {
	data, err := fetchChunk(lvName)
	if err != nil {
		fmt.Printf("[CHUNK] fetch %s: %v\n", lvName, err)
		cm.mu.Lock()
		delete(cm.loading, key)
		cm.mu.Unlock()
		return
	}

	ch := cm.buildChunk(data, gc, gr)

	cm.mu.Lock()
	cm.chunks[key] = ch
	delete(cm.loading, key)
	cm.mu.Unlock()
}

// buildChunk converts the server JSON into a Chunk.
func (cm *ChunkManager) buildChunk(data *chunkJSON, gc, gr int) *Chunk {
	ch := &Chunk{
		GridCol: gc,
		GridRow: gr,
		OriginX: float64(gc * chunkPixelW),
		OriginY: float64(gr * chunkPixelH),
		tileImg:  cm.tileImage(),
		tileCols: cm.tileImageCols(),
	}

	// Collision grid.
	ch.collision = make([][]bool, chunkRows)
	for r := range ch.collision {
		ch.collision[r] = make([]bool, chunkCols)
	}

	for _, l := range data.Layers {
		if l.Collision {
			for i, gid := range l.Data {
				if gid != 0 {
					r := i / chunkCols
					c := i % chunkCols
					if r < chunkRows && c < chunkCols {
						ch.collision[r][c] = true
					}
				}
			}
		} else {
			cp := make([]int, len(l.Data))
			copy(cp, l.Data)
			ch.layers = append(ch.layers, cp)
		}
	}

	ch.npcs = data.NPCs
	return ch
}

// Draw renders all loaded chunks.
// vw/vh are the effective viewport dimensions (screenW/zoom × screenH/zoom).
func (cm *ChunkManager) Draw(screen *ebiten.Image, camX, camY, vw, vh float64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for _, ch := range cm.chunks {
		ch.Draw(screen, camX, camY, vw, vh)
	}
}

// IsBlocked checks collision across all loaded chunks.
func (cm *ChunkManager) IsBlocked(x, y, w, h float64) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Which chunks could overlap this rect?
	x1Chunk := int(x) / chunkPixelW
	y1Chunk := int(y) / chunkPixelH
	x2Chunk := int(x+w) / chunkPixelW
	y2Chunk := int(y+h) / chunkPixelH

	for gr := y1Chunk; gr <= y2Chunk; gr++ {
		for gc := x1Chunk; gc <= x2Chunk; gc++ {
			ch, ok := cm.chunks[chunkKey(gc, gr)]
			if !ok {
				continue
			}
			if ch.IsBlocked(x, y, w, h) {
				return true
			}
		}
	}
	return false
}

// WorldW / WorldH return the full world size in pixels.
func (cm *ChunkManager) WorldW() int {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.gmWidth * chunkPixelW
}
func (cm *ChunkManager) WorldH() int {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.gmHeight * chunkPixelH
}

// HasGMap reports whether a GMAP has been loaded.
func (cm *ChunkManager) HasGMap() bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.levels != nil
}

// ── Tile image (lazy) ────────────────────────────────────────────────────────

func (cm *ChunkManager) tileImage() *ebiten.Image {
	cm.tileOnce.Do(func() {
		img, _, err := ebitenutil.NewImageFromFile(
			"Assets/offline/levels/tiles/classiciphone_pics4.png")
		if err != nil {
			fmt.Println("[CHUNK] could not load tileset:", err)
			return
		}
		cm.tileImg = img
		cm.tileCols = img.Bounds().Dx() / chunkTileW // 128
	})
	return cm.tileImg
}

func (cm *ChunkManager) tileImageCols() int {
	cm.tileOnce.Do(func() { cm.tileImage() })
	return cm.tileCols
}

// ── HTTP fetchers ─────────────────────────────────────────────────────────────

func fetchChunk(name string) (*chunkJSON, error) {
	apiURL := getAPIURL() + "/api/maps/chunk?name=" + url.QueryEscape(name)
	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var data chunkJSON
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func fetchGMap(name string) (*gmapJSON, error) {
	apiURL := getAPIURL() + "/api/maps/gmap?name=" + url.QueryEscape(name)
	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var gm gmapJSON
	if err := json.Unmarshal(body, &gm); err != nil {
		return nil, err
	}
	return &gm, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func chunkKey(col, row int) string {
	return fmt.Sprintf("%d,%d", col, row)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// IsNWFile reports whether name ends with .nw (case-insensitive).
func IsNWFile(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".nw")
}

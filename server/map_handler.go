package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// mapsNWDir is the root directory of all .nw level files.
const mapsNWDir = "maps/nw"

// mapsTMXDir is the root directory of all converted .tmx chunk files.
const mapsTMXDir = "maps/tmx"

// ── JSON response types ──────────────────────────────────────────────────────

// MapLayer is one tile layer inside a chunk response.
type MapLayer struct {
	Name      string `json:"name"`
	Collision bool   `json:"collision,omitempty"`
	// Terrain type for non-visual layers: "water" or "lava".
	Terrain string `json:"terrain,omitempty"`
	// Flat row-major array of TMX GIDs (0 = empty, 1-based otherwise).
	Data []int `json:"data"`
}

// MapNPC describes a single NPC in a chunk.
type MapNPC struct {
	Image  string  `json:"image,omitempty"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Script string  `json:"script"` // Lua source
}

// MapChunkResponse is the JSON payload returned by GET /api/maps/chunk.
type MapChunkResponse struct {
	Name       string     `json:"name"`
	Width      int        `json:"width"`
	Height     int        `json:"height"`
	TileWidth  int        `json:"tilewidth"`
	TileHeight int        `json:"tileheight"`
	Layers     []MapLayer `json:"layers"`
	NPCs       []MapNPC   `json:"npcs"`
}

// MapGMapResponse is the JSON payload returned by GET /api/maps/gmap.
type MapGMapResponse struct {
	Name   string     `json:"name"`
	Width  int        `json:"width"`
	Height int        `json:"height"`
	Levels [][]string `json:"levels"` // [row][col] → chunk filename (.tmx or .nw)
}

// ── Handlers ────────────────────────────────────────────────────────────────

// handleMapChunk serves a single map chunk as JSON.
// Accepts both .tmx (preferred) and .nw files.
// GET /api/maps/chunk?name=aeria_cavern_01.tmx
// GET /api/maps/chunk?name=balamb_a1.nw  (legacy)
func handleMapChunk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "missing ?name=", http.StatusBadRequest)
		return
	}
	// Safety: only allow simple filenames, no path traversal.
	name = filepath.Base(name)
	lower := strings.ToLower(name)

	// Ignore __ prefixed files (internal / developer-only).
	if strings.HasPrefix(name, "__") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	switch {
	case strings.HasSuffix(lower, ".tmx"):
		serveMapChunkTMX(w, name)
	case strings.HasSuffix(lower, ".nw"):
		serveMapChunkNW(w, name)
	default:
		http.Error(w, "only .tmx or .nw files are served here", http.StatusBadRequest)
	}
}

// serveMapChunkTMX parses a .tmx file from maps/tmx/ and writes JSON.
// If the TMX has no terrain layers (old converter output), the corresponding
// .nw file is parsed to supply collision and terrain data automatically.
func serveMapChunkTMX(w http.ResponseWriter, name string) {
	path := filepath.Join(mapsTMXDir, name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	resp, err := ParseTMXFile(path)
	if err != nil {
		http.Error(w, "parse error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Augment with NW terrain/collision when the TMX was generated without them
	// (e.g. by an older version of the converter).
	if !hasTerrainOrCollision(resp) {
		base := strings.TrimSuffix(name, filepath.Ext(name))
		nwPath := filepath.Join(mapsNWDir, base+".nw")
		if lv, err := ParseNWFile(nwPath, 0); err == nil {
			// Collision.
			resp.Layers = append(resp.Layers, MapLayer{
				Name:      "collision",
				Collision: true,
				Data:      lv.CollisionGIDs(),
			})
			// Water terrain (only when non-empty).
			if wgids := lv.TerrainGIDs(false); wgids != nil {
				resp.Layers = append(resp.Layers, MapLayer{
					Name:    "water",
					Terrain: "water",
					Data:    wgids,
				})
			}
			// Lava terrain (only when non-empty).
			if lgids := lv.TerrainGIDs(true); lgids != nil {
				resp.Layers = append(resp.Layers, MapLayer{
					Name:    "lava",
					Terrain: "lava",
					Data:    lgids,
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_ = json.NewEncoder(w).Encode(resp)
}

// hasTerrainOrCollision reports whether a chunk response already contains
// at least one collision or terrain layer (produced by the current converter).
func hasTerrainOrCollision(resp *MapChunkResponse) bool {
	for _, l := range resp.Layers {
		if l.Collision || l.Terrain != "" {
			return true
		}
	}
	return false
}

// serveMapChunkNW parses a legacy .nw file from maps/nw/ and writes JSON.
// If a converted .tmx version exists in maps/tmx/ it is preferred automatically,
// so existing .gmap files keep working without editing.
func serveMapChunkNW(w http.ResponseWriter, name string) {
	// Auto-upgrade: prefer .tmx if it exists.
	base := strings.TrimSuffix(name, filepath.Ext(name))
	tmxPath := filepath.Join(mapsTMXDir, base+".tmx")
	if _, err := os.Stat(tmxPath); err == nil {
		serveMapChunkTMX(w, base+".tmx")
		return
	}

	path := filepath.Join(mapsNWDir, name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	lv, err := ParseNWFile(path, 0) // page 0 = interior tileset
	if err != nil {
		http.Error(w, "parse error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	resp := buildChunkResponse(lv)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleMapGMap serves a GMAP layout as JSON.
// GET /api/maps/gmap?name=balambisland.gmap
func handleMapGMap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "missing ?name=", http.StatusBadRequest)
		return
	}
	name = filepath.Base(name)
	if !strings.HasSuffix(strings.ToLower(name), ".gmap") {
		http.Error(w, "only .gmap files are served here", http.StatusBadRequest)
		return
	}

	path := filepath.Join("maps/gmap", name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	gm, err := ParseGMapFile(path)
	if err != nil {
		http.Error(w, "parse error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := MapGMapResponse{
		Name:   gm.Name,
		Width:  gm.Width,
		Height: gm.Height,
		Levels: gm.LevelNames,
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_ = json.NewEncoder(w).Encode(resp)
}

// ── Helper ───────────────────────────────────────────────────────────────────

func buildChunkResponse(lv *NWLevel) *MapChunkResponse {
	resp := &MapChunkResponse{
		Name:       lv.Name,
		Width:      NWCols,
		Height:     NWRows,
		TileWidth:  16,
		TileHeight: 16,
	}

	// Tile layers (base + any overlays).
	for i, layer := range lv.Layers {
		flat := make([]int, NWRows*NWCols)
		for r := 0; r < NWRows; r++ {
			for c := 0; c < NWCols; c++ {
				flat[r*NWCols+c] = layer[r][c]
			}
		}
		name := "base"
		if i == 1 {
			name = "overlay"
		} else if i > 1 {
			name = "overlay" + string(rune('0'+i))
		}
		resp.Layers = append(resp.Layers, MapLayer{Name: name, Data: flat})
	}

	// Collision layer.
	resp.Layers = append(resp.Layers, MapLayer{
		Name:      "collision",
		Collision: true,
		Data:      lv.CollisionGIDs(),
	})

	// Water terrain layer.
	if wgids := lv.TerrainGIDs(false); wgids != nil {
		resp.Layers = append(resp.Layers, MapLayer{
			Name:    "water",
			Terrain: "water",
			Data:    wgids,
		})
	}
	// Lava terrain layer.
	if lgids := lv.TerrainGIDs(true); lgids != nil {
		resp.Layers = append(resp.Layers, MapLayer{
			Name:    "lava",
			Terrain: "lava",
			Data:    lgids,
		})
	}

	// NPCs — convert GraalScript to Lua.
	for _, npc := range lv.NPCs {
		resp.NPCs = append(resp.NPCs, MapNPC{
			Image:  npc.Image,
			X:      npc.X,
			Y:      npc.Y,
			Script: GraalScriptToLua(npc.Script),
		})
	}

	return resp
}

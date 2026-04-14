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

// ── JSON response types ──────────────────────────────────────────────────────

// MapLayer is one tile layer inside a chunk response.
type MapLayer struct {
	Name      string `json:"name"`
	Collision bool   `json:"collision,omitempty"`
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
	Levels [][]string `json:"levels"` // [row][col] → .nw filename
}

// ── Handlers ────────────────────────────────────────────────────────────────

// handleMapChunk serves a single NW chunk as JSON.
// GET /api/maps/chunk?name=gc_island_01.nw
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
	if !strings.HasSuffix(strings.ToLower(name), ".nw") {
		http.Error(w, "only .nw files are served here", http.StatusBadRequest)
		return
	}
	// Ignore __ prefixed files (internal / developer-only).
	if strings.HasPrefix(name, "__") {
		http.Error(w, "not found", http.StatusNotFound)
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

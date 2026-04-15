package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// GameConfig holds all values that can be tuned without recompiling.
// Edit config.json in the repo root to change them.
type GameConfig struct {
	// Server address (host:port)
	ServerURL string `json:"serverURL"`
	// Minimum number of chunks to keep loaded in each direction around the player.
	// The actual load radius may be larger when zoomed out.
	ChunkRadius int `json:"chunkRadius"`
	// Player movement speed in pixels/second (normal and mounted).
	PlayerSpeed  float64 `json:"playerSpeed"`
	MountedSpeed float64 `json:"mountedSpeed"`
	// Camera zoom limits.
	ZoomMin float64 `json:"zoomMin"`
	ZoomMax float64 `json:"zoomMax"`
}

// Cfg is the active configuration. Populated by LoadConfig; defaults match
// the previous hard-coded values so existing behaviour is preserved when
// config.json is absent.
var Cfg = GameConfig{
	ServerURL:    "localhost:8080",
	ChunkRadius:  2,
	PlayerSpeed:  700.0,
	MountedSpeed: 320.0,
	ZoomMin:      0.35,
	ZoomMax:      2.5,
}

// LoadConfig reads config.json from path and merges it into Cfg.
// Missing keys keep their default values.
func LoadConfig(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Println("[CFG] config.json not found, using defaults")
		return
	}
	if err := json.Unmarshal(data, &Cfg); err != nil {
		fmt.Println("[CFG] parse error:", err, "— using defaults")
		return
	}
	// Sanity clamps.
	if Cfg.ChunkRadius < 1 {
		Cfg.ChunkRadius = 1
	}
	if Cfg.PlayerSpeed <= 0 {
		Cfg.PlayerSpeed = 700.0
	}
	if Cfg.MountedSpeed <= 0 {
		Cfg.MountedSpeed = 320.0
	}
	if Cfg.ZoomMin <= 0 {
		Cfg.ZoomMin = 0.1
	}
	if Cfg.ZoomMax < Cfg.ZoomMin {
		Cfg.ZoomMax = Cfg.ZoomMin
	}
	fmt.Printf("[CFG] loaded %s (speed=%.0f chunk_r=%d zoom=%.2f–%.2f)\n",
		path, Cfg.PlayerSpeed, Cfg.ChunkRadius, Cfg.ZoomMin, Cfg.ZoomMax)
}

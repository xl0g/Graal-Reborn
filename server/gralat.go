package main

import "time"

// respawnDelay is how long after collection before a gralat reappears.
const respawnDelay = 45 * time.Second

// gralatSpawnDefs defines the fixed world positions and values of gralat pickups.
// Positions are in pixels; the world is 960×640 (30×20 tiles at 32 px each).
var gralatSpawnDefs = []struct {
	id    string
	x, y  float64
	value int
}{
	{"g0", 160, 100, 1},
	{"g1", 480, 160, 5},
	{"g2", 820, 200, 1},
	{"g3", 200, 400, 30},
	{"g4", 650, 440, 5},
	{"g5", 880, 120, 1},
	{"g6", 340, 550, 100},
	{"g7", 560, 500, 5},
}

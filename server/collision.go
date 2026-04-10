package main

import (
	"encoding/xml"
	"os"
	"strconv"
	"strings"
)

const tileGIDMaskServer = 0x1FFFFFFF // strip Tiled flip flags

// CollisionMap holds a 2-D grid of solid tiles parsed from a TMX file.
// It is used server-side to prevent NPCs from walking through walls.
type CollisionMap struct {
	tileW, tileH int
	cols, rows   int
	solid        [][]bool
}

// collTMX is the minimal XML structure needed for collision parsing.
type collTMX struct {
	TileW  int         `xml:"tilewidth,attr"`
	TileH  int         `xml:"tileheight,attr"`
	Cols   int         `xml:"width,attr"`
	Rows   int         `xml:"height,attr"`
	Layers []collLayer `xml:"layer"`
}

type collLayer struct {
	Props []collProp `xml:"properties>property"`
	Cols  int        `xml:"width,attr"`
	Rows  int        `xml:"height,attr"`
	Data  struct {
		Raw string `xml:",chardata"`
	} `xml:"data"`
}

type collProp struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// LoadCollisionMap parses a TMX file and returns a CollisionMap.
// Only layers with property collision=true are treated as solid.
func LoadCollisionMap(path string) (*CollisionMap, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var mx collTMX
	if err := xml.Unmarshal(raw, &mx); err != nil {
		return nil, err
	}

	cm := &CollisionMap{
		tileW: mx.TileW,
		tileH: mx.TileH,
		cols:  mx.Cols,
		rows:  mx.Rows,
		solid: make([][]bool, mx.Rows),
	}
	for i := range cm.solid {
		cm.solid[i] = make([]bool, mx.Cols)
	}

	for _, layer := range mx.Layers {
		isCollision := false
		for _, p := range layer.Props {
			if p.Name == "collision" && p.Value == "true" {
				isCollision = true
				break
			}
		}
		if !isCollision {
			continue
		}
		tiles := parseCollCSV(layer.Data.Raw, layer.Cols, layer.Rows)
		for r := 0; r < layer.Rows; r++ {
			for c := 0; c < layer.Cols; c++ {
				if tiles[r][c] != 0 {
					cm.solid[r][c] = true
				}
			}
		}
	}
	return cm, nil
}

func parseCollCSV(raw string, cols, rows int) [][]int {
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
			grid[r][c] = int(v) & tileGIDMaskServer
			i++
		}
	}
	return grid
}

// IsFreePoint returns true if the single world-space point (x,y) is not blocked.
func (cm *CollisionMap) IsFreePoint(x, y float64) bool {
	if cm == nil {
		return true
	}
	col := int(x) / cm.tileW
	row := int(y) / cm.tileH
	if col < 0 || col >= cm.cols || row < 0 || row >= cm.rows {
		return false
	}
	return !cm.solid[row][col]
}

// IsBlocked returns true if the axis-aligned box (x,y,w,h) in world-space
// overlaps any solid tile. A 2-pixel margin is applied for smoother movement.
func (cm *CollisionMap) IsBlocked(x, y, w, h float64) bool {
	if cm == nil {
		return false
	}
	const margin = 2.0
	x1 := int(x+margin) / cm.tileW
	y1 := int(y+margin) / cm.tileH
	x2 := int(x+w-margin-1) / cm.tileW
	y2 := int(y+h-margin-1) / cm.tileH
	for row := y1; row <= y2; row++ {
		for col := x1; col <= x2; col++ {
			if row < 0 || row >= cm.rows || col < 0 || col >= cm.cols {
				return true
			}
			if cm.solid[row][col] {
				return true
			}
		}
	}
	return false
}

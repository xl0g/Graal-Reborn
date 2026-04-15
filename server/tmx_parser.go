package main

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ── TMX XML structures (server-side, no Ebiten dependency) ────────────────────

type tmxMapXML struct {
	TileW  int            `xml:"tilewidth,attr"`
	TileH  int            `xml:"tileheight,attr"`
	Cols   int            `xml:"width,attr"`
	Rows   int            `xml:"height,attr"`
	Layers []tmxLayerXML  `xml:"layer"`
}

type tmxLayerXML struct {
	Name  string           `xml:"name,attr"`
	Cols  int              `xml:"width,attr"`
	Rows  int              `xml:"height,attr"`
	Props []tmxPropertyXML `xml:"properties>property"`
	Data  struct {
		Encoding string `xml:"encoding,attr"`
		Raw      string `xml:",chardata"`
	} `xml:"data"`
}

type tmxPropertyXML struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// ParseTMXFile reads a .tmx file and returns a MapChunkResponse in the same
// format used by the NW chunk handler.  Only CSV-encoded data is supported
// (all files produced by the .nw → .tmx converter use CSV).
func ParseTMXFile(path string) (*MapChunkResponse, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var m tmxMapXML
	if err := xml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("xml parse: %w", err)
	}

	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	resp := &MapChunkResponse{
		Name:       name,
		Width:      m.Cols,
		Height:     m.Rows,
		TileWidth:  m.TileW,
		TileHeight: m.TileH,
	}

	for _, l := range m.Layers {
		isCollision := false
		terrainType := ""
		for _, p := range l.Props {
			switch {
			case strings.EqualFold(p.Name, "collision") && (p.Value == "true" || p.Value == "1"):
				isCollision = true
			case strings.EqualFold(p.Name, "terrain"):
				terrainType = strings.ToLower(p.Value) // "water" or "lava"
			}
		}

		data, err := parseTMXCSV(l.Data.Raw, l.Cols*l.Rows)
		if err != nil {
			return nil, fmt.Errorf("layer %q: %w", l.Name, err)
		}

		resp.Layers = append(resp.Layers, MapLayer{
			Name:      l.Name,
			Collision: isCollision,
			Terrain:   terrainType,
			Data:      data,
		})
	}

	return resp, nil
}

// parseTMXCSV splits the raw CSV tile data into a flat int slice.
// Tiled stores one GID per cell; 0 means empty.
// Flip flags (top bits) are stripped — the client handles flipping separately,
// and the server only needs solid/empty for collision.
func parseTMXCSV(raw string, expectedLen int) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return make([]int, expectedLen), nil
	}

	parts := strings.Split(raw, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("bad tile value %q: %w", p, err)
		}
		// Strip Tiled flip flags (top 3 bits of uint32).
		gid := int(uint32(v) & 0x1FFFFFFF)
		out = append(out, gid)
	}
	return out, nil
}

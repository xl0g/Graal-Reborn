package main

import "math"

const (
	spatialCellSize = 512.0  // px per grid cell
	viewRadius      = 2048.0 // interest-management radius in world-px (~2 GMAP chunks)
)

type cellKey struct{ col, row int32 }

// tempGrid is rebuilt once per game tick for O(1) per-cell neighbour lookups.
// It does not persist between ticks; allocate with buildSpatialGrid.
type tempGrid struct {
	lookup map[cellKey][]int
	xs, ys []float64
}

// buildSpatialGrid buckets player indices by spatial cell.
// xs and ys are parallel position slices; indices match the caller's snapshot slice.
func buildSpatialGrid(xs, ys []float64) *tempGrid {
	g := &tempGrid{
		lookup: make(map[cellKey][]int, len(xs)/4+1),
		xs:     xs,
		ys:     ys,
	}
	for i, x := range xs {
		k := posToCell(x, ys[i])
		g.lookup[k] = append(g.lookup[k], i)
	}
	return g
}

func posToCell(x, y float64) cellKey {
	return cellKey{
		col: int32(math.Floor(x / spatialCellSize)),
		row: int32(math.Floor(y / spatialCellSize)),
	}
}

// nearby returns indices of all entries within viewRadius of (cx, cy).
// The radius check is exact (circle, not square).
func (g *tempGrid) nearby(cx, cy float64) []int {
	rCells := int32(math.Ceil(viewRadius / spatialCellSize))
	ck := posToCell(cx, cy)
	radiusSq := viewRadius * viewRadius
	var result []int
	for dc := -rCells; dc <= rCells; dc++ {
		for dr := -rCells; dr <= rCells; dr++ {
			for _, idx := range g.lookup[cellKey{ck.col + dc, ck.row + dr}] {
				dx := g.xs[idx] - cx
				dy := g.ys[idx] - cy
				if dx*dx+dy*dy <= radiusSq {
					result = append(result, idx)
				}
			}
		}
	}
	return result
}

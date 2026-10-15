package photon

import (
	"encoding/json"
	"log"
	"math"
	"os"
	"strings"
	"sync"
)


type navmeshLink struct {
	To  int        `json:"to"`
	Mid [3]float32 `json:"mid"`
}

type navmeshPoly struct {
	Verts     []int         `json:"verts"`
	Area      int           `json:"area"`
	IsOffMesh bool          `json:"isOffMesh"`
	Links     []navmeshLink `json:"links"`
}

type navmeshOffMeshLink struct {
	Start  [3]float32 `json:"start"`
	End    [3]float32 `json:"end"`
	Radius float32    `json:"radius"`
}

type navmeshTile struct {
	Bmin  [3]float32    `json:"bmin"`
	Bmax  [3]float32    `json:"bmax"`
	Verts [][3]float32  `json:"verts"`
	Polys []navmeshPoly `json:"polys"`
}

type navmeshData struct {
	Tiles []navmeshTile `json:"tiles"`
}

var (
	navmeshMu    sync.Mutex
	navmeshCache = map[string]*navmeshData{} // nil entry = confirmed absent, don't retry the file read
)

func loadNavmesh(level string) *navmeshData {
	key := strings.ToLower(level)
	navmeshMu.Lock()
	defer navmeshMu.Unlock()
	if nm, ok := navmeshCache[key]; ok {
		return nm
	}
	raw, err := os.ReadFile("data/navmeshes/" + key + ".json")
	if err != nil {
		navmeshCache[key] = nil
		return nil
	}
	var nm navmeshData
	if err := json.Unmarshal(raw, &nm); err != nil {
		log.Printf("[HOST] data/navmeshes/%s.json parse error: %v", key, err)
		navmeshCache[key] = nil
		return nil
	}
	navmeshCache[key] = &nm
	return &nm
}

func navmeshGroundHeight(level string, x, z, refY float32) (float32, bool) {
	return navmeshHeightTol(level, x, z, refY, navmeshEdgeTol)
}

func navmeshOnMesh(level string, x, z, refY float32) bool {
	_, ok := navmeshHeightTol(level, x, z, refY, navmeshAgentRadius)
	return ok
}

const navmeshAgentRadius = 0.35

func navmeshHeightTol(level string, x, z, refY, edgeTol float32) (float32, bool) {
	nm := loadNavmesh(level)
	if nm == nil {
		return 0, false
	}
	margin := edgeTol
	bestY := float32(0)
	bestDiff := float32(1 << 30)
	found := false
	edgeY := float32(0)
	edgeBest := edgeTol
	edgeFound := false
	for _, t := range nm.Tiles {
		if x < t.Bmin[0]-margin || x > t.Bmax[0]+margin || z < t.Bmin[2]-margin || z > t.Bmax[2]+margin {
			continue
		}
		for _, p := range t.Polys {
			if p.IsOffMesh || len(p.Verts) < 3 {
				continue
			}
			y, ok := polyHeightAt(t.Verts, p.Verts, x, z)
			if ok {
				if diff := absf32(y - refY); !found || diff < bestDiff {
					bestY, bestDiff, found = y, diff, true
				}
				continue
			}
			if found || edgeTol <= 0 {
				continue
			}
			for i := range p.Verts {
				a := t.Verts[p.Verts[i]]
				b := t.Verts[p.Verts[(i+1)%len(p.Verts)]]
				if d, yy := segPointDistXZ(a, b, x, z); d <= edgeBest {
					edgeBest, edgeY, edgeFound = d, yy, true
				}
			}
		}
	}
	if found {
		return bestY, true
	}
	if edgeFound {
		return edgeY, true
	}
	return 0, false
}

const navmeshEdgeTol = 1.0

func segPointDistXZ(a, b [3]float32, x, z float32) (float32, float32) {
	dx, dz := b[0]-a[0], b[2]-a[2]
	l2 := dx*dx + dz*dz
	t := float32(0)
	if l2 > 0 {
		t = ((x-a[0])*dx + (z-a[2])*dz) / l2
		if t < 0 {
			t = 0
		} else if t > 1 {
			t = 1
		}
	}
	px, pz := a[0]+t*dx, a[2]+t*dz
	ddx, ddz := x-px, z-pz
	return float32(math.Sqrt(float64(ddx*ddx + ddz*ddz))), a[1] + t*(b[1]-a[1])
}

func levelHasNavmesh(level string) bool {
	return loadNavmesh(level) != nil
}

func absf32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func polyHeightAt(tileVerts [][3]float32, idx []int, x, z float32) (float32, bool) {
	v0 := tileVerts[idx[0]]
	for i := 1; i < len(idx)-1; i++ {
		v1 := tileVerts[idx[i]]
		v2 := tileVerts[idx[i+1]]
		if y, ok := triHeightAt(v0, v1, v2, x, z); ok {
			return y, true
		}
	}
	return 0, false
}

func triHeightAt(a, b, c [3]float32, x, z float32) (float32, bool) {
	d00 := (c[0]-a[0])*(b[2]-a[2]) - (c[2]-a[2])*(b[0]-a[0])
	if d00 == 0 {
		return 0, false
	}
	w1 := ((b[2]-c[2])*(x-c[0]) + (c[0]-b[0])*(z-c[2])) / ((b[2]-c[2])*(a[0]-c[0]) + (c[0]-b[0])*(a[2]-c[2]))
	w2 := ((c[2]-a[2])*(x-c[0]) + (a[0]-c[0])*(z-c[2])) / ((b[2]-c[2])*(a[0]-c[0]) + (c[0]-b[0])*(a[2]-c[2]))
	w3 := 1 - w1 - w2
	const eps = -0.001
	if w1 < eps || w2 < eps || w3 < eps {
		return 0, false
	}
	return w1*a[1] + w2*b[1] + w3*c[1], true
}

const (
	navmeshLOSStep     = 0.5
	navmeshLOSBlockRun = 3
)

func navmeshLineClear(level string, from, to [3]float32) bool {
	if !levelHasNavmesh(level) {
		return true
	}
	dx, dy, dz := to[0]-from[0], to[1]-from[1], to[2]-from[2]
	dist := float32(math.Sqrt(float64(dx*dx + dz*dz)))
	steps := int(dist / navmeshLOSStep)
	run := 0
	for i := 1; i < steps; i++ {
		t := float32(i) / float32(steps)
		x, y, z := from[0]+dx*t, from[1]+dy*t, from[2]+dz*t
		if navmeshOnMesh(level, x, z, y) {
			run = 0
			continue
		}
		run++
		if run >= navmeshLOSBlockRun {
			return false
		}
	}
	return true
}

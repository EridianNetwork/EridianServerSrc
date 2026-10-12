package photon

import (
	"container/heap"
	"math"
	"strings"
	"sync"
)

type navGraphEdge struct {
	to  int
	mid [3]float32 
}

type navGraphPoly struct {
	centroid  [3]float32
	verts     [][3]float32
	neighbors []navGraphEdge
}

type navGraph struct {
	polys []navGraphPoly
}

var (
	navGraphsMu sync.Mutex
	navGraphs   map[string]*navGraph
)

func getNavGraph(level string) *navGraph {
	nm := loadNavmesh(level)
	level = strings.ToLower(level)

	navGraphsMu.Lock()
	defer navGraphsMu.Unlock()
	if navGraphs == nil {
		navGraphs = map[string]*navGraph{}
	}
	if g, cached := navGraphs[level]; cached {
		return g
	}
	var g *navGraph
	if nm != nil {
		g = buildNavGraph(*nm)
	}
	navGraphs[level] = g
	return g
}

func buildNavGraph(nm navmeshData) *navGraph {
	g := &navGraph{}
	for _, t := range nm.Tiles {
		for _, p := range t.Polys {
			if p.IsOffMesh || len(p.Verts) < 3 {
				g.polys = append(g.polys, navGraphPoly{})
				continue
			}
			verts := make([][3]float32, len(p.Verts))
			var c [3]float32
			for i, vi := range p.Verts {
				v := t.Verts[vi]
				verts[i] = v
				c[0] += v[0]
				c[1] += v[1]
				c[2] += v[2]
			}
			n := float32(len(verts))
			edges := make([]navGraphEdge, 0, len(p.Links))
			for _, l := range p.Links {
				edges = append(edges, navGraphEdge{to: l.To, mid: l.Mid})
			}
			g.polys = append(g.polys, navGraphPoly{
				centroid:  [3]float32{c[0] / n, c[1] / n, c[2] / n},
				verts:     verts,
				neighbors: edges,
			})
		}
	}

	for i := range g.polys {
		kept := g.polys[i].neighbors[:0]
		for _, e := range g.polys[i].neighbors {
			if e.to >= 0 && e.to < len(g.polys) {
				kept = append(kept, e)
			}
		}
		g.polys[i].neighbors = kept
	}
	return g
}

func (g *navGraph) findPoly(x, z, refY float32) (int, float32, bool) {
	best := -1
	bestY := float32(0)
	bestDiff := float32(1 << 30)
	edgeBest := -1
	edgeY := float32(0)
	edgeDist := float32(navmeshEdgeTol)
	for i := range g.polys {
		if len(g.polys[i].verts) < 3 {
			continue
		}
		y, ok := polyHeightAt(g.polys[i].verts, identityIdx(len(g.polys[i].verts)), x, z)
		if ok {
			if diff := absf32(y - refY); best < 0 || diff < bestDiff {
				best, bestY, bestDiff = i, y, diff
			}
			continue
		}
		if best >= 0 {
			continue
		}
		verts := g.polys[i].verts
		for j := range verts {
			a, b := verts[j], verts[(j+1)%len(verts)]
			if d, yy := segPointDistXZ(a, b, x, z); d <= edgeDist {
				edgeDist, edgeY, edgeBest = d, yy, i
			}
		}
	}
	if best >= 0 {
		return best, bestY, true
	}
	if edgeBest >= 0 {
		return edgeBest, edgeY, true
	}
	return -1, 0, false
}

func identityIdx(n int) []int {
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	return idx
}

type navPQItem struct {
	poly int
	dist float32
}
type navPQ []navPQItem

func (q navPQ) Len() int           { return len(q) }
func (q navPQ) Less(i, j int) bool { return q[i].dist < q[j].dist }
func (q navPQ) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *navPQ) Push(x any)        { *q = append(*q, x.(navPQItem)) }
func (q *navPQ) Pop() any {
	old := *q
	n := len(old)
	item := old[n-1]
	*q = old[:n-1]
	return item
}

func navmeshFindPath(level string, sx, sz, gx, gz, refY float32) ([][3]float32, bool) {
	g := getNavGraph(level)
	if g == nil || len(g.polys) == 0 {
		return nil, false
	}
	startPoly, startY, ok := g.findPoly(sx, sz, refY)
	if !ok {
		return nil, false
	}
	goalPoly, goalY, ok := g.findPoly(gx, gz, startY)
	if !ok {
		return nil, false
	}
	goal := [3]float32{gx, goalY, gz}
	if startPoly == goalPoly {
		return [][3]float32{goal}, true
	}

	const inf = float32(1 << 30)
	dist := make([]float32, len(g.polys))
	prev := make([]int, len(g.polys))
	for i := range dist {
		dist[i] = inf
		prev[i] = -1
	}
	dist[startPoly] = 0
	pq := &navPQ{{startPoly, 0}}
	visited := make([]bool, len(g.polys))
	for pq.Len() > 0 {
		cur := heap.Pop(pq).(navPQItem)
		if visited[cur.poly] {
			continue
		}
		visited[cur.poly] = true
		if cur.poly == goalPoly {
			break
		}
		for _, e := range g.polys[cur.poly].neighbors {
			if visited[e.to] {
				continue
			}
			c := g.polys[cur.poly].centroid
			d := dist[cur.poly] + dist3(c, e.mid)
			if d < dist[e.to] {
				dist[e.to] = d
				prev[e.to] = cur.poly
				heap.Push(pq, navPQItem{e.to, d})
			}
		}
	}
	if dist[goalPoly] >= inf {
		return nil, false
	}

	var polyPath []int
	for p := goalPoly; p != -1; p = prev[p] {
		polyPath = append(polyPath, p)
	}
	waypoints := make([][3]float32, 0, len(polyPath))
	for i := len(polyPath) - 1; i > 0; i-- {
		from, to := polyPath[i], polyPath[i-1]
		for _, e := range g.polys[from].neighbors {
			if e.to == to {
				waypoints = append(waypoints, e.mid)
				break
			}
		}
	}
	waypoints = append(waypoints, goal)
	return waypoints, true
}

func dist3(a, b [3]float32) float32 {
	dx, dy, dz := a[0]-b[0], a[1]-b[1], a[2]-b[2]
	return float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
}

type navBoundarySeg struct {
	ax, az, bx, bz float32
}

var (
	navBoundariesMu sync.Mutex
	navBoundaries   map[string][]navBoundarySeg
)

const navBoundaryMidEpsilon = 0.05

func getNavBoundaries(level string) []navBoundarySeg {
	g := getNavGraph(level)
	key := strings.ToLower(level)

	navBoundariesMu.Lock()
	defer navBoundariesMu.Unlock()
	if navBoundaries == nil {
		navBoundaries = map[string][]navBoundarySeg{}
	}
	if segs, cached := navBoundaries[key]; cached {
		return segs
	}
	var segs []navBoundarySeg
	if g != nil {
		for _, p := range g.polys {
			n := len(p.verts)
			if n < 3 {
				continue
			}
			for i := 0; i < n; i++ {
				a, b := p.verts[i], p.verts[(i+1)%n]
				midX, midZ := (a[0]+b[0])/2, (a[2]+b[2])/2
				shared := false
				for _, e := range p.neighbors {
					if absf32(e.mid[0]-midX) < navBoundaryMidEpsilon && absf32(e.mid[2]-midZ) < navBoundaryMidEpsilon {
						shared = true
						break
					}
				}
				if !shared {
					segs = append(segs, navBoundarySeg{a[0], a[2], b[0], b[2]})
				}
			}
		}
	}
	navBoundaries[key] = segs
	return segs
}

func navmeshWallBlocked(level string, from, to [3]float32) bool {
	segs := getNavBoundaries(level)
	if len(segs) == 0 {
		return false
	}
	fx, fz, tx, tz := from[0], from[2], to[0], to[2]
	minX, maxX := fx, tx
	if minX > maxX {
		minX, maxX = maxX, minX
	}
	minZ, maxZ := fz, tz
	if minZ > maxZ {
		minZ, maxZ = maxZ, minZ
	}
	for _, s := range segs {
		segMinX, segMaxX := s.ax, s.bx
		if segMinX > segMaxX {
			segMinX, segMaxX = segMaxX, segMinX
		}
		if segMaxX < minX || segMinX > maxX {
			continue
		}
		segMinZ, segMaxZ := s.az, s.bz
		if segMinZ > segMaxZ {
			segMinZ, segMaxZ = segMaxZ, segMinZ
		}
		if segMaxZ < minZ || segMinZ > maxZ {
			continue
		}
		if segmentsIntersectXZ(fx, fz, tx, tz, s.ax, s.az, s.bx, s.bz) {
			return true
		}
	}
	return false
}

func segmentsIntersectXZ(p1x, p1z, p2x, p2z, p3x, p3z, p4x, p4z float32) bool {
	d1 := cross2D(p4x-p3x, p4z-p3z, p1x-p3x, p1z-p3z)
	d2 := cross2D(p4x-p3x, p4z-p3z, p2x-p3x, p2z-p3z)
	d3 := cross2D(p2x-p1x, p2z-p1z, p3x-p1x, p3z-p1z)
	d4 := cross2D(p2x-p1x, p2z-p1z, p4x-p1x, p4z-p1z)
	return ((d1 > 0 && d2 < 0) || (d1 < 0 && d2 > 0)) && ((d3 > 0 && d4 < 0) || (d3 < 0 && d4 > 0))
}

func cross2D(ax, az, bx, bz float32) float32 { return ax*bz - az*bx }

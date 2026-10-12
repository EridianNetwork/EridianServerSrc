package photon


import (
	"log"
	"strings"
	"sync"
)

type coverPoint struct {
	Pos       [3]float32 `json:"pos"`
	Facing    [3]float32 `json:"facing"`
	Height    int        `json:"height"`
	Depth     int        `json:"depth"`
	Direction int        `json:"directions"`
	Radius    float32    `json:"radius"`
}

func (e *coverPoint) SetPos(x, y, z float32) { e.Pos = [3]float32{x, y, z} }

func (e *coverPoint) SetRot(x, y, z, w float32) {
	e.Facing = quatRotateVector([3]float32{x, y, z}, w, [3]float32{0, 0, 1})
}

func quatRotateVector(qxyz [3]float32, qw float32, v [3]float32) [3]float32 {
	cross := func(a, b [3]float32) [3]float32 {
		return [3]float32{
			a[1]*b[2] - a[2]*b[1],
			a[2]*b[0] - a[0]*b[2],
			a[0]*b[1] - a[1]*b[0],
		}
	}
	t := cross(qxyz, v)
	t = [3]float32{t[0] * 2, t[1] * 2, t[2] * 2}
	u := cross(qxyz, t)
	return [3]float32{
		v[0] + qw*t[0] + u[0],
		v[1] + qw*t[1] + u[1],
		v[2] + qw*t[2] + u[2],
	}
}

var (
	coversOnce  sync.Once
	levelCovers map[string][]coverPoint
)

func loadLevelCovers() {
	coversOnce.Do(func() {
		loadLowercaseJSON("data/level_covers.json", &levelCovers)
		total := 0
		for _, v := range levelCovers {
			total += len(v)
		}
		if total > 0 {
			log.Printf("[COVER] loaded %d cover points across %d levels", total, len(levelCovers))
		}
	})
}


func reloadLevelCovers() {
	coversOnce = sync.Once{}
	levelCovers = nil
}

func levelCoverPoints(level string) []coverPoint {
	loadLevelCovers()
	return levelCovers[strings.ToLower(level)]
}

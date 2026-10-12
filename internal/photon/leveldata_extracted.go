package photon

import (
	"strings"
	"sync"
)

type destroyableEntry struct {
	Pid          int64     `json:"pid"`
	GO           string    `json:"go"`
	Pos          []float64 `json:"pos"`
	Name         string    `json:"name"`
	HideUI       bool      `json:"hideUI"`
	ReceiveMelee bool      `json:"receiveMelee"`
	ReceiveArea  bool      `json:"receiveArea"`
	ReceiveShoot bool      `json:"receiveShoot"`
	ReceiveNpc   bool      `json:"receiveNpc"`
	InitHP       int32     `json:"initHP"`
}

func (e *destroyableEntry) SetPos(x, y, z float32) {
	e.Pos = []float64{float64(x), float64(y), float64(z)}
}

type difficultyEntry struct {
	Pid                        int64     `json:"pid"`
	GO                         string    `json:"go"`
	Pos                        []float64 `json:"pos"`
	HPModifier                 float32   `json:"hpModifier"`
	DamageModifier             float32   `json:"damageModifier"`
	ThrowGrenadeChanceModifier float32   `json:"throwGrenadeChanceModifier"`
}

func (e *difficultyEntry) SetPos(x, y, z float32) {
	e.Pos = []float64{float64(x), float64(y), float64(z)}
}

var (
	levelDestroyablesOnce sync.Once
	levelDestroyables     map[string][]destroyableEntry

	levelDifficultyOnce sync.Once
	levelDifficulty     map[string][]difficultyEntry
)

func levelDestroyableEntries(level string) []destroyableEntry {
	levelDestroyablesOnce.Do(func() { loadLowercaseJSON("data/level_destroyables.json", &levelDestroyables) })
	return levelDestroyables[strings.ToLower(level)]
}

func destroyableSettingsFor(level, goName string) *destroyableEntry {
	for _, e := range levelDestroyableEntries(level) {
		if e.GO == goName {
			return &e
		}
	}
	return nil
}

func levelDifficultyEntries(level string) []difficultyEntry {
	levelDifficultyOnce.Do(func() { loadLowercaseJSON("data/level_difficulty.json", &levelDifficulty) })
	return levelDifficulty[strings.ToLower(level)]
}

func levelHPModifier(level string) float32 {
	if es := levelDifficultyEntries(level); len(es) > 0 {
		return es[0].HPModifier
	}
	return 1
}

func levelDamageModifier(level string) float32 {
	if es := levelDifficultyEntries(level); len(es) > 0 {
		return es[0].DamageModifier
	}
	return 1
}

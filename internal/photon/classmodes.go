package photon


import (
	"encoding/json"
	"log"
	"os"
	"slices"
	"sync"
)


type classModePreset struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Rarity   string `json:"rarity"`
	Level    int    `json:"level"`
	CMType   string `json:"cmType"`
	Adapter  int    `json:"adapter"`
	Body     int    `json:"body"`
	Chassis  int    `json:"chassis"`
	Drum     int    `json:"drum"`
	Material int    `json:"material"`
}

var (
	classModesOnce   sync.Once
	classModePresets []classModePreset
)

func loadClassModePresets() {
	classModesOnce.Do(func() {
		raw, err := os.ReadFile("data/classmodes.json")
		if err != nil {
			log.Printf("[SHOP] data/classmodes.json not loaded: %v", err)
			return
		}
		if err := json.Unmarshal(raw, &classModePresets); err != nil {
			log.Printf("[SHOP] data/classmodes.json parse error: %v", err)
			classModePresets = nil
			return
		}
		log.Printf("[SHOP] loaded %d real player class mod presets", len(classModePresets))
	})
}

var classModeTypeValues = map[string]int{
	"SoldierDemoMan": 0, "SoldierMachinist": 1,
	"SirenEnchanter": 2, "SirenSorcerer": 3,
	"BerserkerGuardian": 4, "BerserkerButcher": 5,
	"HunterAgent": 6, "HunterMarksman": 7,
}


var classModeTypesForAvatar = map[int][]string{
	0: {"SirenEnchanter", "SirenSorcerer"},
	1: {"SoldierDemoMan", "SoldierMachinist"},
	2: {"BerserkerGuardian", "BerserkerButcher"},
	3: {"HunterAgent", "HunterMarksman"},
}


func randomClassModePresetAnyRarity(avatarClass, maxLevel int) *classModePreset {
	loadClassModePresets()
	cmTypes := classModeTypesForAvatar[avatarClass]
	if len(classModePresets) == 0 || len(cmTypes) == 0 {
		return nil
	}
	var pool []*classModePreset
	for i := range classModePresets {
		if slices.Contains(cmTypes, classModePresets[i].CMType) {
			pool = append(pool, &classModePresets[i])
		}
	}
	picked, ok := levelGatedPick(pool, func(cp *classModePreset) int { return cp.Level }, maxLevel)
	if !ok {
		return nil
	}
	return picked
}

package photon
import (
	"encoding/json"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"
)
type weaponSubparts struct {
	ACC      int `json:"ACC"`
	ACTION   int `json:"ACTION"`
	BARREL   int `json:"BARREL"`
	BODY     int `json:"BODY"`
	GRIP     int `json:"GRIP"`
	MAG      int `json:"MAG"`
	SIGHT    int `json:"SIGHT"`
	STOCK    int `json:"STOCK"`
	MATERIAL int `json:"MATERIAL"`
}
func (w weaponSubparts) AsSlice() [9]int {
	return [9]int{w.ACC, w.ACTION, w.BARREL, w.BODY, w.GRIP, w.MAG, w.SIGHT, w.STOCK, w.MATERIAL}
}
type weaponPreset struct {
	ID        int            `json:"id"`
	Name      string         `json:"name"`
	Type      string         `json:"type"`
	Rarity    string         `json:"rarity"`
	Level     int            `json:"level"`
	Subparts  weaponSubparts `json:"subparts"`
	ExtraSlot int            `json:"extraSlot"`
}
var (
	weaponsOnce   sync.Once
	weaponPresets []weaponPreset
)
func loadWeaponPresets() {
	weaponsOnce.Do(func() {
		raw, err := os.ReadFile("data/weapons.json")
		if err != nil {
			log.Printf("[SHOP] data/weapons.json not loaded: %v", err)
			return
		}
		if err := json.Unmarshal(raw, &weaponPresets); err != nil {
			log.Printf("[SHOP] data/weapons.json parse error: %v", err)
			weaponPresets = nil
			return
		}
		log.Printf("[SHOP] loaded %d real player weapon presets", len(weaponPresets))
	})
}
func weaponTypeName(wantType int) string {
	for name, v := range weaponTypeValues {
		if v == wantType {
			return name
		}
	}
	return ""
}
func randomWeaponPresetAnyRarity(wantType, maxLevel int) *weaponPreset {
	loadWeaponPresets()
	typeName := weaponTypeName(wantType)
	var pool []*weaponPreset
	for i := range weaponPresets {
		if weaponPresets[i].Type == typeName {
			pool = append(pool, &weaponPresets[i])
		}
	}
	picked, ok := levelGatedPick(pool, func(wp *weaponPreset) int { return wp.Level }, maxLevel)
	if !ok {
		return nil
	}
	return picked
}
func weaponPresetByID(id int) *weaponPreset {
	loadWeaponPresets()
	for i := range weaponPresets {
		if weaponPresets[i].ID == id {
			return &weaponPresets[i]
		}
	}
	return nil
}
const (
	elementNone      = int32(0)
	elementFire      = int32(1)
	elementShock     = int32(2)
	elementCorrosive = int32(3)
	elementExplosive = int32(4)
)
var elementChanceByRarity = map[int32]float64{
	0: 0.00, 
	1: 0.10, 
	2: 0.20, 
	3: 0.35, 
	4: 0.50, 
	5: 0.65, 
}
func rollWeaponElement(rarity int32) int32 {
	chance, ok := elementChanceByRarity[rarity]
	if !ok || rand.Float64() >= chance {
		return elementNone
	}
	switch rand.Intn(4) {
	case 0:
		return elementFire
	case 1:
		return elementShock
	case 2:
		return elementCorrosive
	default:
		return elementExplosive
	}
}
type weaponSubpartElement struct {
	Type     string  `json:"type"`
	PartType string  `json:"partType"`
	Index    int     `json:"index"`
	Element  int32   `json:"element"`
	ProcCoef float64 `json:"procCoef"`
	Dmg      float64 `json:"dmg"`
}
var (
	weaponElementalPartsOnce sync.Once
	weaponElementalParts     []weaponSubpartElement
)
func loadWeaponElementalParts() {
	weaponElementalPartsOnce.Do(func() {
		raw, err := os.ReadFile("data/weapon_elemental_parts.json")
		if err != nil {
			log.Printf("[SHOP] data/weapon_elemental_parts.json not loaded: %v", err)
			return
		}
		if err := json.Unmarshal(raw, &weaponElementalParts); err != nil {
			log.Printf("[SHOP] data/weapon_elemental_parts.json parse error: %v", err)
			weaponElementalParts = nil
			return
		}
		log.Printf("[SHOP] loaded %d real weapon elemental subpart entries", len(weaponElementalParts))
	})
}
func presetRealElement(wp *weaponPreset) (int32, bool) {
	if wp == nil {
		return elementNone, false
	}
	loadWeaponElementalParts()
	for _, e := range weaponElementalParts {
		if e.Type == wp.Type && e.PartType == "ACC" && e.Index == wp.Subparts.ACC {
			return e.Element, true
		}
	}
	return elementNone, false
}
var weaponFireRateRPS = map[int32]float64{
	weaponTypeSMG:            9.0,
	weaponTypeSniperRifle:    1.2,
	weaponTypeShotGun:        1.8,
	weaponTypeRepeaterPistol: 4.5,
	weaponTypeCombatRifle:    7.0,
}
func weaponFireInterval(weaponType int32) time.Duration {
	rps, ok := weaponFireRateRPS[weaponType]
	if !ok || rps <= 0 {
		rps = weaponFireRateRPS[weaponTypeSMG]
	}
	return time.Duration(float64(time.Second) / rps)
}
var weakpointStrengthModifiers = []float64{2.0, 3.0, 1.0, 0.5, 0.25, 0.0}
func weakpointMultiplier(strength int32) float64 {
	if strength < 0 || int(strength) >= len(weakpointStrengthModifiers) {
		return 1.0
	}
	return weakpointStrengthModifiers[strength]
}

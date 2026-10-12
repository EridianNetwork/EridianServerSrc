package photon

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

type shieldPreset struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Rarity       string `json:"rarity"`
	Level        int    `json:"level"`
	Manufacturer string `json:"manufacturer"`
	Body         int    `json:"body"`
	Battery      int    `json:"battery"`
	Capacitor    int    `json:"capacitor"`
	Accessory    int    `json:"accessory"`
	Material     int    `json:"material"`
}

var (
	shieldsOnce   sync.Once
	shieldPresets []shieldPreset
)

func loadShieldPresets() {
	shieldsOnce.Do(func() {
		raw, err := os.ReadFile("data/shields.json")
		if err != nil {
			log.Printf("[SHOP] data/shields.json not loaded: %v", err)
			return
		}
		if err := json.Unmarshal(raw, &shieldPresets); err != nil {
			log.Printf("[SHOP] data/shields.json parse error: %v", err)
			shieldPresets = nil
			return
		}
		log.Printf("[SHOP] loaded %d real player shield presets", len(shieldPresets))
	})
}


var shieldManufacturerValues = map[string]int{
	"Vladof": 0, "Dahl": 1, "Maliwan": 2, "Atlas": 3,
}

func shieldManufacturerName(ordinal int) string {
	for name, v := range shieldManufacturerValues {
		if v == ordinal {
			return name
		}
	}
	return ""
}

func shieldPrefabForManufacturer(ordinal int) string {
	name := shieldManufacturerName(ordinal)
	if name == "" {
		return "Entities/Object/Shield"
	}
	return "Entities/Object/Item/PRO_Shield_" + name + "_P"
}


const shieldRegenDelay = 15 * time.Second

var (
	shieldSubpartsOnce sync.Once
	shieldBodyCapacityScale []float64
)

func loadShieldSubparts() {
	shieldSubpartsOnce.Do(func() {
		raw, err := os.ReadFile("data/shield_subparts.json")
		if err != nil {
			log.Printf("[SHOP] data/shield_subparts.json not loaded: %v", err)
			return
		}
		if err := json.Unmarshal(raw, &shieldBodyCapacityScale); err != nil {
			log.Printf("[SHOP] data/shield_subparts.json parse error: %v", err)
			shieldBodyCapacityScale = nil
			return
		}
		log.Printf("[SHOP] loaded %d real shield Body->Capacity scale rows", len(shieldBodyCapacityScale))
	})
}

func realMaxShield(bodyIdx, itemLevel int) int32 {
	loadShieldSubparts()
	scale := 1.0
	if len(shieldBodyCapacityScale) > 0 {
		if bodyIdx < 0 {
			bodyIdx = 0
		}
		if bodyIdx >= len(shieldBodyCapacityScale) {
			bodyIdx = len(shieldBodyCapacityScale) - 1
		}
		scale = shieldBodyCapacityScale[bodyIdx]
	}
	return int32(scale * float64(maxLifeForLevel(itemLevel)))
}

const shieldFullRechargeSeconds = 4.0

func shieldAfterRegen(baseline, maxShield int32, elapsed time.Duration) int32 {
	if elapsed <= 0 {
		return baseline
	}
	frac := elapsed.Seconds() / shieldFullRechargeSeconds
	if frac >= 1 {
		return maxShield
	}
	shield := baseline + int32(float64(maxShield)*frac)
	if shield > maxShield {
		shield = maxShield
	}
	return shield
}

func shieldPresetByID(id int) *shieldPreset {
	loadShieldPresets()
	for i := range shieldPresets {
		if shieldPresets[i].ID == id {
			return &shieldPresets[i]
		}
	}
	return nil
}


func randomShieldPresetAnyRarity(maxLevel int) *shieldPreset {
	loadShieldPresets()
	pool := make([]*shieldPreset, len(shieldPresets))
	for i := range shieldPresets {
		pool[i] = &shieldPresets[i]
	}
	picked, ok := levelGatedPick(pool, func(sp *shieldPreset) int { return sp.Level }, maxLevel)
	if !ok {
		return nil
	}
	return picked
}

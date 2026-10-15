package photon

import (
	"encoding/json"
	"log"
	"os"
	"sync"

	"eridian/internal/domain"
)

type weaponAttrDelta struct {
	Value      float64 `json:"value"`
	AffectType string  `json:"affectType"`
}

type weaponSubpartEntry struct {
	Index      int                        `json:"index"`
	Slot       string                     `json:"slot"`
	Name       string                     `json:"name"`
	Attributes map[string]weaponAttrDelta `json:"attributes"`
}

var (
	weaponStatsOnce sync.Once
	weaponBaseStats map[string]map[string]weaponAttrDelta
	weaponSubpartIndex map[string]map[string]map[int]weaponSubpartEntry
)

func loadWeaponDamageStats() {
	weaponStatsOnce.Do(func() {
		weaponBaseStats = map[string]map[string]weaponAttrDelta{}
		weaponSubpartIndex = map[string]map[string]map[int]weaponSubpartEntry{}

		raw, err := os.ReadFile("data/weapon_base_stats.json")
		if err != nil {
			log.Printf("[PHOTON] weapon_base_stats.json not loaded: %v", err)
			return
		}
		if err := json.Unmarshal(raw, &weaponBaseStats); err != nil {
			log.Printf("[PHOTON] weapon_base_stats.json parse error: %v", err)
		}

		raw2, err := os.ReadFile("data/weapon_subpart_stats.json")
		if err != nil {
			log.Printf("[PHOTON] weapon_subpart_stats.json not loaded: %v", err)
			return
		}
		var bySubtype map[string][]weaponSubpartEntry
		if err := json.Unmarshal(raw2, &bySubtype); err != nil {
			log.Printf("[PHOTON] weapon_subpart_stats.json parse error: %v", err)
			return
		}
		for wt, entries := range bySubtype {
			bySlot := map[string]map[int]weaponSubpartEntry{}
			for _, e := range entries {
				if bySlot[e.Slot] == nil {
					bySlot[e.Slot] = map[int]weaponSubpartEntry{}
				}
				bySlot[e.Slot][e.Index] = e
			}
			weaponSubpartIndex[wt] = bySlot
		}
		log.Printf("[PHOTON] loaded weapon damage stats for %d weapon types", len(weaponBaseStats))
	})
}

func combineFloatAttribute(base float64, deltas []weaponAttrDelta) float64 {
	var prevAdd, posScale, negScale, postAdd float64
	for _, d := range deltas {
		switch d.AffectType {
		case "Scale":
			if d.Value > 0 {
				posScale += d.Value
			} else {
				negScale += d.Value
			}
		case "PostAdd":
			postAdd += d.Value
		case "PrevAdd":
			prevAdd += d.Value
		}
	}
	return (base+prevAdd)*((1+posScale)/(1-negScale)) + postAdd
}

func computeWeaponStats(typeName string, sp [9]int) (dps, fireRate float64, ok bool) {
	loadWeaponDamageStats()
	base := weaponBaseStats[typeName]
	if base == nil {
		return 0, 0, false
	}
	slots := weaponSubpartIndex[typeName]
	var dpsDeltas, fireRateDeltas []weaponAttrDelta
	for i, slot := range npcWeaponSubpartOrder {
		entry, found := slots[slot][sp[i]]
		if !found {
			continue
		}
		if d, found := entry.Attributes["WeaponDPS"]; found {
			dpsDeltas = append(dpsDeltas, d)
		}
		if d, found := entry.Attributes["FireRate"]; found {
			fireRateDeltas = append(fireRateDeltas, d)
		}
	}
	dps = combineFloatAttribute(base["WeaponDPS"].Value, dpsDeltas)
	fireRate = combineFloatAttribute(base["FireRate"].Value, fireRateDeltas)
	return dps, fireRate, true
}

func weaponPelletCount(it *domain.InventoryItem) float64 {
	loadWeaponDamageStats()
	typeName := weaponTypeName(it.PartA)
	base := weaponBaseStats[typeName]
	if base == nil {
		return 1
	}
	sp := weaponItemSubparts(it)
	var deltas []weaponAttrDelta
	for i, slot := range npcWeaponSubpartOrder {
		if d, found := weaponSubpartIndex[typeName][slot][sp[i]].Attributes["WeaponProjectilesPerShot"]; found {
			deltas = append(deltas, d)
		}
	}
	if n := combineFloatAttribute(base["WeaponProjectilesPerShot"].Value, deltas); n >= 1 {
		return n
	}
	return 1
}

func computeWeaponDamage(typeName string, sp [9]int, level int) float64 {
	dps, fireRate, ok := computeWeaponStats(typeName, sp)
	if !ok || fireRate <= 0 {
		return 0
	}
	return (dps / fireRate) * powerRatingForLevel(level)
}

// resolveWeaponFireRate is the weapon's real shots per second (0 if unknown).
func resolveWeaponFireRate(it *domain.InventoryItem) float64 {
	_, fireRate, ok := computeWeaponStats(weaponTypeName(it.PartA), weaponItemSubparts(it))
	if !ok || fireRate <= 0 {
		return 0
	}
	return fireRate
}

const playerWeaponDamageFallback = 20.0

func (s *EnetServer) playerWeaponDamageAtLevel(sess *ClientSession) float64 {
	if sess == nil || sess.hostChar == nil {
		return playerWeaponDamageFallback
	}
	weapons := equippedWeaponsOrdered(sess.hostChar.CharID)
	slot := int(sess.activeWeaponSlot)
	if slot < 0 || slot >= len(weapons) || weapons[slot] == nil {
		return playerWeaponDamageFallback
	}
	if dmg := resolveWeaponDamage(weapons[slot]); dmg > 0 {
		return dmg
	}
	return playerWeaponDamageFallback
}

var weaponRarityDamageMult = [...]float64{0.75, 1.0, 1.3, 1.7, 2.2, 3.0}

func rarityDamageMult(rarity int) float64 {
	if rarity < 0 || rarity >= len(weaponRarityDamageMult) {
		return 1
	}
	return weaponRarityDamageMult[rarity]
}

func resolveWeaponDamage(it *domain.InventoryItem) float64 {
	dmg := it.Damage
	if dmg <= 0 {
		dmg = computeWeaponDamage(weaponTypeName(it.PartA), weaponItemSubparts(it), it.Level)
	}
	return dmg * rarityDamageMult(it.Rarity) * (1 + weaponExtraBonus(it, "BulletDamageScale"))
}

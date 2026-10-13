package photon

import (
	"encoding/xml"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"

	"eridian/internal/db"
	"eridian/internal/domain"
)

// Port of the client's WeaponGeneratorService.method_7 tail: after the subparts are rolled, a weapon
// gets extra effects. Count comes from ExtraAttrSlot (per rarity), the effect types from
// ExtraAttrLevelEffect (distinct, weighted, filtered by rarity+level) and each effect's tier index from
// ExtraAttrEffect (weighted, filtered by rarity+effect+level). The DNA stores (effect, index) pairs.

const maxWeaponExtraEffects = 8 // ItemDNA.m_extraEffectNums

// weaponEffectTypeValues is XsdSettings.EffectType (declaration order) for the effects weapons can roll.
var weaponEffectTypeValues = map[string]int32{
	"ClipSize": 9, "FixedTimeRegenHealthSpeed": 18, "WeaponSwitchSpeed": 23, "WeaponReloadSpeed": 24,
	"CriticalHitDamage": 44, "WeaponRecoil": 49, "WeaponSway": 50, "GrenadeDmg": 69,
	"BulletDamageScale": 114, "CriticalChance": 119, "MeleeDamageBonus": 120,
	"ShieldRechargeOnKill": 122, "BadassDamageBonus": 123, "ZoomTransitionSpeed": 126,
}

type xmlExtraSlot struct {
	Rarity string `xml:"m_rarity"`
	Num    int    `xml:"m_slotNum"`
	Prob   int    `xml:"m_probability"`
}

type xmlExtraLevelEffect struct {
	Rarity string `xml:"m_rarity"`
	Effect string `xml:"m_effectType"`
	Prob   int    `xml:"m_probability"`
	MinLvl int    `xml:"m_minLevel"`
	MaxLvl int    `xml:"m_maxLevel"`
}

type xmlExtraEffect struct {
	Rarity string  `xml:"m_rarity"`
	Effect string  `xml:"m_effect"`
	Index  int     `xml:"m_index"`
	Value  float64 `xml:"m_attributeValue"`
	Prob   int     `xml:"m_probability"`
	MinLvl int     `xml:"m_minLevel"`
	MaxLvl int     `xml:"m_maxLevel"`
}

var (
	weaponExtraOnce   sync.Once
	weaponExtraSlots  []xmlExtraSlot
	weaponExtraLevels []xmlExtraLevelEffect
	weaponExtraTiers  []xmlExtraEffect
)

func loadWeaponExtras() {
	weaponExtraOnce.Do(func() {
		read := func(path string, root any) bool {
			raw, err := os.ReadFile(path)
			if err != nil {
				log.Printf("[WEAPON] %s not loaded: %v", path, err)
				return false
			}
			if err := xml.Unmarshal(raw, root); err != nil {
				log.Printf("[WEAPON] %s parse error: %v", path, err)
				return false
			}
			return true
		}
		var slots struct {
			List []xmlExtraSlot `xml:"m_extraAttrSlotList>m_extraAttrSlot"`
		}
		var levels struct {
			List []xmlExtraLevelEffect `xml:"m_extraAttrLevelEffectList>m_extraAttrLevelEffect"`
		}
		var tiers struct {
			List []xmlExtraEffect `xml:"m_extraAttrEffectList>m_extraAttrEffect"`
		}
		if read("data/weapons/ExtraAttrSlot.xml", &slots) {
			weaponExtraSlots = slots.List
		}
		if read("data/weapons/ExtraAttrLevelEffect.xml", &levels) {
			weaponExtraLevels = levels.List
		}
		if read("data/weapons/ExtraAttrEffect.xml", &tiers) {
			weaponExtraTiers = tiers.List
		}
		log.Printf("[WEAPON] loaded weapon extra effects: %d slot rows, %d effect types, %d tiers",
			len(weaponExtraSlots), len(weaponExtraLevels), len(weaponExtraTiers))
	})
}

func rarityName(rarity int) string {
	for name, v := range itemRarityValues {
		if v == rarity {
			return name
		}
	}
	return ""
}

// weightedPick returns the index chosen from weights, or -1 when the total weight is 0.
func weightedPick(weights []int) int {
	total := 0
	for _, w := range weights {
		total += w
	}
	if total <= 0 {
		return -1
	}
	roll := rand.Intn(total)
	for i, w := range weights {
		if roll < w {
			return i
		}
		roll -= w
	}
	return -1
}

type weaponExtra struct {
	Effect int32
	Index  int32
}

func rollWeaponExtras(rarity, level int) []weaponExtra {
	loadWeaponExtras()
	rn := rarityName(rarity)

	var slotWeights, slotNums []int
	for _, s := range weaponExtraSlots {
		if s.Rarity == rn {
			slotWeights = append(slotWeights, s.Prob)
			slotNums = append(slotNums, s.Num)
		}
	}
	pick := weightedPick(slotWeights)
	if pick < 0 {
		return nil
	}
	count := slotNums[pick]
	if count > maxWeaponExtraEffects {
		count = maxWeaponExtraEffects
	}

	var pool []xmlExtraLevelEffect
	for _, e := range weaponExtraLevels {
		if e.Rarity == rn && level >= e.MinLvl && level <= e.MaxLvl {
			pool = append(pool, e)
		}
	}

	var out []weaponExtra
	for ; count > 0 && len(pool) > 0; count-- {
		weights := make([]int, len(pool))
		for i, e := range pool {
			weights[i] = e.Prob
		}
		i := weightedPick(weights)
		if i < 0 {
			break
		}
		name := pool[i].Effect
		pool = append(pool[:i], pool[i+1:]...)
		effect, ok := weaponEffectTypeValues[name]
		if !ok {
			continue
		}
		var tierWeights, tierIdx []int
		for _, t := range weaponExtraTiers {
			if t.Rarity == rn && t.Effect == name && level >= t.MinLvl && level <= t.MaxLvl {
				tierWeights = append(tierWeights, t.Prob)
				tierIdx = append(tierIdx, t.Index)
			}
		}
		j := weightedPick(tierWeights)
		if j < 0 {
			continue
		}
		out = append(out, weaponExtra{Effect: effect, Index: int32(tierIdx[j])})
	}
	return out
}

func encodeWeaponExtras(extras []weaponExtra) string {
	parts := make([]string, len(extras))
	for i, e := range extras {
		parts[i] = strconv.Itoa(int(e.Effect)) + ":" + strconv.Itoa(int(e.Index))
	}
	return strings.Join(parts, ",")
}

func decodeWeaponExtras(s string) []weaponExtra {
	if s == "" {
		return nil
	}
	var out []weaponExtra
	for _, f := range strings.Split(s, ",") {
		pair := strings.SplitN(f, ":", 2)
		if len(pair) != 2 {
			continue
		}
		eff, err1 := strconv.Atoi(pair[0])
		idx, err2 := strconv.Atoi(pair[1])
		if err1 != nil || err2 != nil || len(out) >= maxWeaponExtraEffects {
			continue
		}
		out = append(out, weaponExtra{Effect: int32(eff), Index: int32(idx)})
	}
	return out
}

// weaponExtraBonus sums an effect's attribute value over the weapon's rolled extras (0 if none).
func weaponExtraBonus(it *domain.InventoryItem, effectName string) float64 {
	extras := decodeWeaponExtras(it.ExtraEffects)
	if len(extras) == 0 {
		return 0
	}
	loadWeaponExtras()
	want, ok := weaponEffectTypeValues[effectName]
	if !ok {
		return 0
	}
	rn := rarityName(it.Rarity)
	total := 0.0
	for _, e := range extras {
		if e.Effect != want {
			continue
		}
		for _, t := range weaponExtraTiers {
			if t.Rarity == rn && t.Effect == effectName && int32(t.Index) == e.Index {
				total += t.Value
				break
			}
		}
	}
	return total
}

// BackfillWeaponExtras rolls extra effects for stored weapons that predate them (rarities that carry
// extras but have an empty extra_effects). Runs at startup; already-rolled weapons are left untouched.
func BackfillWeaponExtras() {
	var rarities []int
	for _, name := range []string{"Uncommon", "Rare", "VeryRare", "MetaEpic"} {
		rarities = append(rarities, itemRarityValues[name])
	}
	total := 0
	for _, table := range []string{"inventory", "warehouse"} {
		rows, err := db.ListWeaponsMissingExtras(table, rarities)
		if err != nil {
			log.Printf("[WEAPON] backfill extra effects (%s): %v", table, err)
			continue
		}
		for _, r := range rows {
			level := r.Level
			if level < 1 {
				level = 1
			}
			extras := encodeWeaponExtras(rollWeaponExtras(r.Rarity, level))
			if extras == "" {
				continue
			}
			if err := db.SetItemExtraEffects(table, r.ID, extras); err != nil {
				log.Printf("[WEAPON] backfill extra effects (%s item %d): %v", table, r.ID, err)
				continue
			}
			total++
		}
	}
	if total > 0 {
		log.Printf("[WEAPON] backfilled extra effects for %d existing weapons", total)
	}
}

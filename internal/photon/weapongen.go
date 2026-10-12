package photon

import (
	"encoding/xml"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"

	"eridian/internal/domain"
)

// Port of the client's WeaponGeneratorService.method_7 + RarityStore.method_1:
// every subpart slot is rolled independently from data/weapons/Rarity.xml,
// weighted by (weapon type, rarity, slot).

type xmlSubpartChoice struct {
	Index       int     `xml:"m_subpartIndex"`
	Probability float64 `xml:"m_probability"`
}

type xmlSubpartsInRarity struct {
	Slot    string             `xml:"m_weaponSubPart"`
	Choices []xmlSubpartChoice `xml:"m_subParts>SubpartChoice"`
}

type xmlWeaponRarityType struct {
	Rarity string                `xml:"m_weaponRarity"`
	Slots  []xmlSubpartsInRarity `xml:"m_subPartTypes>SubPartsInRarity"`
}

type xmlWeaponRarityAttr struct {
	WeaponType string                `xml:"m_weaponType"`
	Rarities   []xmlWeaponRarityType `xml:"m_weaponRarityType>WeaponRarityType"`
}

type xmlRarityStore struct {
	Attrs []xmlWeaponRarityAttr `xml:"m_weaponRarityAttr>WeaponRarityAttr"`
}

var (
	weaponRarityOnce  sync.Once
	weaponRarityTable map[string]map[int]map[string][]xmlSubpartChoice
)

func loadWeaponRarityTable() {
	weaponRarityOnce.Do(func() {
		weaponRarityTable = map[string]map[int]map[string][]xmlSubpartChoice{}
		raw, err := os.ReadFile("data/weapons/Rarity.xml")
		if err != nil {
			log.Printf("[WEAPON] data/weapons/Rarity.xml not loaded: %v", err)
			return
		}
		var store xmlRarityStore
		if err := xml.Unmarshal(raw, &store); err != nil {
			log.Printf("[WEAPON] data/weapons/Rarity.xml parse error: %v", err)
			return
		}
		for _, a := range store.Attrs {
			byRarity := map[int]map[string][]xmlSubpartChoice{}
			for _, r := range a.Rarities {
				bySlot := map[string][]xmlSubpartChoice{}
				for _, s := range r.Slots {
					bySlot[s.Slot] = s.Choices
				}
				byRarity[itemRarityValues[r.Rarity]] = bySlot
			}
			weaponRarityTable[a.WeaponType] = byRarity
		}
		log.Printf("[WEAPON] loaded subpart rarity tables for %d weapon types", len(weaponRarityTable))
	})
}

func weaponSlotUnused(weaponType int, slot string) bool {
	isPistol := int32(weaponType) == weaponTypeRepeaterPistol
	return (slot == "STOCK" && isPistol) || (slot == "ACTION" && !isPistol)
}

func pickSubpartChoice(choices []xmlSubpartChoice) int {
	total := 0.0
	for _, c := range choices {
		total += c.Probability
	}
	if total <= 0 {
		return 0
	}
	roll := rand.Float64() * total
	acc := 0.0
	for _, c := range choices {
		if roll >= acc && roll < acc+c.Probability {
			return c.Index
		}
		acc += c.Probability
	}
	return 0
}

func rollWeaponSubparts(weaponType, rarity int) [9]int {
	loadWeaponRarityTable()
	slots := weaponRarityTable[weaponTypeName(weaponType)][rarity]
	var out [9]int
	for i, slot := range npcWeaponSubpartOrder {
		if weaponSlotUnused(weaponType, slot) {
			out[i] = -1
			continue
		}
		out[i] = pickSubpartChoice(slots[slot])
	}
	return out
}

func encodeWeaponSubparts(sp [9]int) string {
	parts := make([]string, len(sp))
	for i, v := range sp {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ",")
}

func decodeWeaponSubparts(s string) ([9]int, bool) {
	var out [9]int
	fields := strings.Split(s, ",")
	if len(fields) != len(out) {
		return out, false
	}
	for i, f := range fields {
		v, err := strconv.Atoi(f)
		if err != nil {
			return out, false
		}
		out[i] = v
	}
	return out, true
}

// weaponItemSubparts returns the item's own rolled subparts, falling back to
// its preset (fixed weapons and items created before subparts were stored).
func weaponItemSubparts(it *domain.InventoryItem) [9]int {
	if sp, ok := decodeWeaponSubparts(it.Subparts); ok {
		return sp
	}
	if wp := weaponPresetByID(it.ResourceID); wp != nil {
		return wp.Subparts.AsSlice()
	}
	return [9]int{}
}

func newGeneratedWeapon(weaponType, rarity, level int, source int) *domain.InventoryItem {
	sp := rollWeaponSubparts(weaponType, rarity)
	typeName := weaponTypeName(weaponType)
	element, _ := subpartElement(typeName, sp[0])
	return &domain.InventoryItem{
		ItemKind:         domain.ItemTypeWeapon,
		IsWeapon:         true,
		ResourceID:       0,
		Level:            level,
		Rarity:           rarity,
		PartA:            weaponType,
		Subparts:         encodeWeaponSubparts(sp),
		Damage:           computeWeaponDamage(typeName, sp, level),
		ElementType:      int(element),
		ItemName:         weaponTypeNames[weaponType],
		GenerationSource: source,
	}
}

func newPresetWeapon(wp *weaponPreset, level int, source int) *domain.InventoryItem {
	sp := wp.Subparts.AsSlice()
	element, _ := subpartElement(wp.Type, sp[0])
	return &domain.InventoryItem{
		ItemKind:         domain.ItemTypeWeapon,
		IsWeapon:         true,
		ResourceID:       wp.ID,
		Level:            level,
		Rarity:           itemRarityValues[wp.Rarity],
		PartA:            weaponTypeValues[wp.Type],
		Subparts:         encodeWeaponSubparts(sp),
		Damage:           computeWeaponDamage(wp.Type, sp, level),
		ElementType:      int(element),
		ItemName:         wp.Name,
		GenerationSource: source,
	}
}

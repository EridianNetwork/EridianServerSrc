package photon

import (
	"encoding/binary"
	"encoding/xml"
	"log"
	"math/rand"
	"os"
	"sync"

	"eridian/internal/domain"
)

var materialTypeNames = []string{
	"MT_Titanium", "MT_Ceramic", "MT_Chitin", "MT_Tongue", "MT_Gland",
	"MT_Sheet", "MT_Spider_Fang", "MT_Diamond_Shell", "MT_Purple_Crystal",
	"MT_PolyesterPlastic", "MT_TitaniumAxiscore", "MT_PulseBrass", "MT_MetalCoil",
}

type xmlLoot struct {
	LootType     string `xml:"m_lootType"`
	Probability  int    `xml:"m_lootProbability"`
	Quantity     int    `xml:"m_lootQuantity"`
	WeaponID     int    `xml:"m_weaponID"`
	WeaponType   string `xml:"m_weaponType"`  
	MaterialType string `xml:"m_materialType"`
	AmmoType     string `xml:"m_ammoType"`    
	ItemRarity   string `xml:"m_itemRarity"`  
}

type xmlLootGroup struct {
	Loots []xmlLoot `xml:"m_lootList>m_loot"`
}

type xmlLootGroups struct {
	Groups []xmlLootGroup `xml:"m_lootGroupList>m_lootGroup"`
}

type xmlRandomLoots struct {
	LootMax int       `xml:"m_lootMax"`
	Loots   []xmlLoot `xml:"m_lootList>m_loot"`
}

type xmlLootingLevelRange struct {
	LevelMin    int            `xml:"m_levelMin"`
	LevelMax    int            `xml:"m_levelMax"`
	RandomLoots xmlRandomLoots `xml:"m_randomLoots"`
	LootGroups  xmlLootGroups  `xml:"m_lootGroups"`
}

type xmlLootingSubType struct {
	SubType     string                 `xml:"m_lootingSubType"`
	LevelRanges []xmlLootingLevelRange `xml:"m_lootingLevelRangeList>m_lootingLevelRange"`
}

type xmlLooting struct {
	LootingType string              `xml:"m_lootingType"`
	SubTypes    []xmlLootingSubType `xml:"m_lootingSubTypeList>m_lootingSubType"`
}

type xmlDefaultQuantity struct {
	LootType     string `xml:"m_lootType"`
	Quantity     int    `xml:"m_lootDefaultQuantity"`
	MaterialType string `xml:"m_materialType"`
	AmmoType     string `xml:"m_ammoType"`
}

type xmlLootSetup struct {
	LootingList       []xmlLooting         `xml:"m_lootingList>m_looting"`
	DefaultQuantities []xmlDefaultQuantity `xml:"m_lootDefaultQuantityList>m_lootDefaultQuantity"`
}

var (
	lootConfigOnce sync.Once
	lootSetup      xmlLootSetup
)

func loadLootConfig() {
	lootConfigOnce.Do(func() {
		if raw, err := os.ReadFile("data/loot/LootSetup.xml"); err == nil {
			if err := xml.Unmarshal(raw, &lootSetup); err != nil {
				log.Printf("[LOOT] bad LootSetup.xml: %v", err)
			}
		} else {
			log.Printf("[LOOT] data/loot/LootSetup.xml not found: %v", err)
		}
		log.Printf("[LOOT] loaded %d looting-type configs", len(lootSetup.LootingList))
	})
}

var lootingTypeNames = []string{
	"BadassBandit", "BadassBanditKiller", "BadassBanditPsycho", "BadassPsycho", "BadassVKag",
	"BanditBruiser", "BanditKiller", "BanditNineToes", "BanditPsycho", "BanditRaider",
	"BanditSuicidePsycho", "BloodWing", "CrimsonLanceDefender", "CrimsonMechaKnoxx", "GuardianWraith",
	"SkagAdult", "SkagAlpha", "SkagPup", "SkagSpitter", "SkagZilla",
	"SpiderantTalos", "SpiderantWidowmaker", "SpiderantWorker", "CashBox", "ChestTreasure",
	"CrateMetal", "CrateMilitary", "SkagPile", "StorageLocker", "TrashcanLootable",
	"SpiderantTalosHorn", "BadassCrimsonLanceDefender", "BadassGuardianWraith", "BadassSpiderantWorker", "CrystalMine",
	"CrimsonLanceMedic", "Rakk", "MidgetShotgunner", "BanditScout", "BanditApollyon",
	"CrateMetalTut", "ArchAngel", "TheInsane", "Tenacle", "TentacleLarge",
	"Crabworm", "CrimsonLanceInfantry", "CrimsonLanceSniper", "CrimsonLanceAssassin",
}

var lootingSubTypeNames = []string{"Normal", "Badass", "Badmusa", "Boss"}

func findLootingLevelRange(lootingType, lootingSubType, level int) *xmlLootingLevelRange {
	loadLootConfig()
	if lootingType < 0 || lootingType >= len(lootingTypeNames) {
		return nil
	}
	typeName := lootingTypeNames[lootingType]
	subName := "Normal"
	if lootingSubType >= 0 && lootingSubType < len(lootingSubTypeNames) {
		subName = lootingSubTypeNames[lootingSubType]
	}
	for i := range lootSetup.LootingList {
		lt := &lootSetup.LootingList[i]
		if lt.LootingType != typeName {
			continue
		}
		for j := range lt.SubTypes {
			st := &lt.SubTypes[j]
			if st.SubType != subName {
				continue
			}
			for k := range st.LevelRanges {
				lr := &st.LevelRanges[k]
				if level >= lr.LevelMin && level <= lr.LevelMax {
					return lr
				}
			}
			if len(st.LevelRanges) > 0 {
				best := &st.LevelRanges[0]
				for k := range st.LevelRanges {
					if level > st.LevelRanges[k].LevelMax {
						best = &st.LevelRanges[k]
					}
				}
				return best
			}
		}
	}
	return nil
}

type rolledLoot struct {
	ItemType     string
	Quantity     int
	WeaponID     int
	WeaponType   string
	ItemRarity   string
	AmmoType     string
	MaterialType string
}

func toRolledLoot(l xmlLoot) rolledLoot {
	return rolledLoot{
		ItemType:     l.LootType,
		Quantity:     l.Quantity,
		WeaponID:     l.WeaponID,
		WeaponType:   l.WeaponType,
		ItemRarity:   l.ItemRarity,
		AmmoType:     l.AmmoType,
		MaterialType: l.MaterialType,
	}
}

func defaultLootQuantity(itemType, ammoType string) int {
	loadLootConfig()
	for _, q := range lootSetup.DefaultQuantities {
		if q.LootType == itemType && (itemType != "Ammo" || q.AmmoType == ammoType) {
			return q.Quantity
		}
	}
	return 1
}

func lootQuantity(d rolledLoot) int {
	if d.Quantity > 0 {
		return d.Quantity
	}
	return defaultLootQuantity(d.ItemType, d.AmmoType)
}

func rollWeightedLoot(loots []xmlLoot) *xmlLoot {
	total := 0
	for _, l := range loots {
		total += l.Probability
	}
	if total <= 0 {
		return nil
	}
	roll := rand.Intn(total)
	acc := 0
	for i := range loots {
		acc += loots[i].Probability
		if roll < acc {
			return &loots[i]
		}
	}
	return &loots[len(loots)-1]
}

func rollLoot(lootingType, lootingSubType, level int) []rolledLoot {
	lr := findLootingLevelRange(lootingType, lootingSubType, level)
	if lr == nil {
		return nil
	}
	return rollFromRange(lr)
}

const chestBonusWeaponChance = 0.25

type rarityBand struct{ Min, Max int }

func (b rarityBand) clamp(rarity int) int {
	if rarity < b.Min {
		return b.Min
	}
	if rarity > b.Max {
		return b.Max
	}
	return rarity
}

func lootRarityBand(lootingType int) (band rarityBand, hardOverride bool) {
	name := ""
	if lootingType >= 0 && lootingType < len(lootingTypeNames) {
		name = lootingTypeNames[lootingType]
	}
	switch name {
	case "SkagPile":
		return rarityBand{0, 0}, true
	case "CrateMilitary":
		return rarityBand{1, 2}, true
	}
	return rarityBand{0, 4}, false
}

func rollBonusWeapon(drops []rolledLoot) []rolledLoot {
	for _, d := range drops {
		if d.ItemType == "Weapon" {
			return drops
		}
	}
	if rand.Float64() < chestBonusWeaponChance {
		drops = append(drops, rolledLoot{ItemType: "Weapon"})
	}
	return drops
}

func rollFromRange(lr *xmlLootingLevelRange) []rolledLoot {
	var out []rolledLoot
	for _, l := range lr.RandomLoots.Loots {
		if lr.RandomLoots.LootMax > 0 && len(out) >= lr.RandomLoots.LootMax {
			break
		}
		if rand.Intn(1000000) < l.Probability {
			out = append(out, toRolledLoot(l))
		}
	}
	for _, g := range lr.LootGroups.Groups {
		if l := rollWeightedLoot(g.Loots); l != nil {
			out = append(out, toRolledLoot(*l))
		}
	}
	return out
}

func rolledWeaponDNA(l rolledLoot, level int, band rarityBand, hardOverride bool) (Custom, *domain.InventoryItem) {
	weaponType, ok := weaponTypeValues[l.WeaponType]
	if !ok {
		weaponType = rand.Intn(5)
	}
	rarity, ok := itemRarityValues[l.ItemRarity]
	if !ok {
		rarity = rand.Intn(6)
		rarity = band.clamp(rarity)
	} else if hardOverride {
		rarity = band.clamp(rarity)
	}
	var item *domain.InventoryItem
	if wp := weaponPresetByID(l.WeaponID); l.WeaponID > 0 && wp != nil {
		item = newPresetWeapon(wp, level, domain.GenerationSourceLoot)
	} else {
		item = newGeneratedWeapon(weaponType, rarity, level, domain.GenerationSourceLoot)
	}
	return weaponItemDNA(item), item
}

var itemTypeValues = map[string]int{
	"HealthPack": 1, "Weapon": 2, "Money": 3, "Ammo": 4, "Shield": 5,
	"Material": 6, "AmmoPack": 7, "ClassMode": 8, "Claptrap": 10, "Outfit": 11,
}

var ammoTypeValues = map[string]int{
	"SMG": 0, "SniperRifle": 1, "Grenade": 2, "ShotGun": 3, "Pistol": 4, "AssaultRifle": 5,
}

var classModeTypeNames = []string{
	"SoldierDemoMan", "SoldierMachinist", "SirenEnchanter", "SirenSorcerer",
	"BerserkerGuardian", "BerserkerButcher", "HunterAgent", "HunterMarksman",
}

func itemDNASimple(itemType, value int) Custom {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf[0:], uint32(itemType))
	binary.LittleEndian.PutUint32(buf[4:], uint32(value))
	return Custom{Code: 66, Data: buf}
}

func itemDNAAmmo(quantity, ammoType int) Custom {
	buf := make([]byte, 12)
	binary.LittleEndian.PutUint32(buf[0:], uint32(itemTypeValues["Ammo"]))
	binary.LittleEndian.PutUint32(buf[4:], uint32(ammoType))
	binary.LittleEndian.PutUint32(buf[8:], uint32(quantity))
	return Custom{Code: 66, Data: buf}
}

func itemDNAMaterial(quantity, materialType int) Custom {
	buf := make([]byte, 12)
	binary.LittleEndian.PutUint32(buf[0:], uint32(itemTypeValues["Material"]))
	binary.LittleEndian.PutUint32(buf[4:], uint32(materialType))
	binary.LittleEndian.PutUint32(buf[8:], uint32(quantity))
	return Custom{Code: 66, Data: buf}
}

func materialLootItem(quantity, materialType int) *domain.InventoryItem {
	return &domain.InventoryItem{
		ItemKind:   domain.ItemTypeMaterial,
		ResourceID: materialType,
		Quantity:   quantity,
		ItemName:   materialTypeNames[materialType],
	}
}

func itemDNAClassMode(cmType, rarity int) Custom {
	name := utf16LEBytes(classModeTypeNames[cmType])
	buf := make([]byte, 0, 44+len(name))
	putI32 := func(v int32) { buf = binary.LittleEndian.AppendUint32(buf, uint32(v)) }
	putI32(int32(itemTypeValues["ClassMode"]))
	putI32(0)
	putI32(int32(cmType))
	putI32(int32(rarity))
	putI32(int32(domain.GenerationSourceLoot))
	putI32(0)
	putI32(0)
	putI32(0)
	putI32(0)
	putI32(0)
	putI32(1)
	putI32(int32(len(name)))
	buf = append(buf, name...)
	return Custom{Code: 66, Data: buf}
}

func outfitItemDNA(it *domain.InventoryItem) Custom {
	name := utf16LEBytes(it.ItemName)
	buf := make([]byte, 0, 40+len(name))
	putI32 := func(v int32) { buf = binary.LittleEndian.AppendUint32(buf, uint32(v)) }
	putI32(int32(itemTypeValues["Outfit"]))
	putI32(int32(it.ResourceID))
	putI32(int32(it.PartA))
	putI32(int32(it.PartB))
	putI32(0)              
	putI32(int32(it.Rarity))
	putI32(int32(it.Level))
	putI32(int32(it.PartC))
	putI32(0)              
	putI32(int32(len(name)))
	buf = append(buf, name...)
	return Custom{Code: 66, Data: buf}
}

func shieldItemDNA(it *domain.InventoryItem) Custom {
	name := utf16LEBytes(it.ItemName)
	buf := make([]byte, 0, 48+len(name))
	putI32 := func(v int32) { buf = binary.LittleEndian.AppendUint32(buf, uint32(v)) }
	putI32(int32(itemTypeValues["Shield"]))
	putI32(int32(it.ResourceID))
	putI32(int32(it.Level))
	putI32(int32(it.PartA))
	putI32(int32(it.PartB))
	putI32(int32(it.PartC))
	putI32(int32(it.PartD))
	putI32(int32(it.PartE))
	putI32(int32(it.PartF))
	putI32(int32(it.GenerationSource))
	putI32(int32(it.Rarity))
	putI32(0)
	putI32(int32(len(name)))
	buf = append(buf, name...)
	return Custom{Code: 66, Data: buf}
}

func genericItemDNA(it *domain.InventoryItem) Custom {
	switch it.ItemKind {
	case domain.ItemTypeWeapon:
		return weaponItemDNA(it)
	case domain.ItemTypeShield:
		return shieldItemDNA(it)
	case domain.ItemTypeMaterial:
		return itemDNAMaterial(it.Quantity, it.ResourceID)
	case domain.ItemTypeClassMode:
		return itemDNAClassMode(int(it.PartA), it.Rarity)
	case domain.ItemTypeOutfit:
		return outfitItemDNA(it)
	default:
		return itemDNASimple(it.ItemKind, it.Quantity)
	}
}

func ammoPrefab(ammoType int) string {
	switch ammoType {
	case 1: // SniperRifle
		return "Entities/Object/Item/PRO_Ammo_Sniper_P"
	case 2: // Grenade
		return "Entities/Object/Item/PRO_Ammo_Grenade_P"
	case 3: // ShotGun
		return "Entities/Object/Item/PRO_Ammo_Shotgun_P"
	case 4: // Pistol
		return "Entities/Object/Item/PRO_Ammo_RepeaterPistol_P"
	default: // SMG(0), AssaultRifle(5)
		return "Entities/Object/Item/PRO_Ammo_SMG_P"
	}
}

type dropReward struct {
	kind     string // "Money", "HealthPack", "AmmoPack", "GrenadeAmmo"
	quantity int32
}

func lootDropPayload(d rolledLoot, level int, band rarityBand, hardOverride bool) (prefab string, dna Custom, item *domain.InventoryItem, reward *dropReward, ok bool) {
	switch d.ItemType {
	case "Weapon":
		dna, item := rolledWeaponDNA(d, level, band, hardOverride)
		return "Entities/Object/Weapon/Weapon", dna, item, nil, true
	case "HealthPack":
		qty := lootQuantity(d)
		return "Entities/Object/Item/PRO_Pack_HP_P", itemDNASimple(itemTypeValues["HealthPack"], qty), nil, &dropReward{kind: "HealthPack", quantity: int32(qty)}, true
	case "Money":
		qty := lootQuantity(d)
		return "Entities/Object/Item/PRO_Cash_P", itemDNASimple(itemTypeValues["Money"], qty), nil, &dropReward{kind: "Money", quantity: int32(qty)}, true
	case "AmmoPack":
		qty := lootQuantity(d)
		return "Entities/Object/Item/PRO_Ammo_Pack_P", itemDNASimple(itemTypeValues["AmmoPack"], qty), nil, &dropReward{kind: "AmmoPack", quantity: int32(qty)}, true
	case "Ammo":
		ammoType, found := ammoTypeValues[d.AmmoType]
		if !found {
			ammoType = rand.Intn(5)
		}
		qty := lootQuantity(d)
		if d.AmmoType == "Grenade" {
			return ammoPrefab(ammoType), itemDNAAmmo(qty, ammoType), nil, &dropReward{kind: "GrenadeAmmo", quantity: int32(qty)}, true
		}
		return ammoPrefab(ammoType), itemDNAAmmo(qty, ammoType), nil, &dropReward{kind: "AmmoPack", quantity: int32(qty)}, true
	case "Material":
		materialName := d.MaterialType
		materialType, found := materialTypeValues[materialName]
		if !found {
			materialType = rand.Intn(len(materialTypeNames)) // TOTAL = random, matches ItemGeneratorService.method_23
			materialName = materialTypeNames[materialType]
		}
		qty := lootQuantity(d)
		return "Entities/Object/Item/PRO_" + materialName + "_P", itemDNAMaterial(qty, materialType), materialLootItem(qty, materialType), nil, true
	case "ClassMode":
		cp := randomClassModePresetAnyRarity(rand.Intn(4), level)
		if cp == nil {
			return "", Custom{}, nil, nil, false
		}
		item := &domain.InventoryItem{
			ItemKind:         domain.ItemTypeClassMode,
			ResourceID:       cp.ID,
			ItemName:         cp.Name,
			Rarity:           itemRarityValues[cp.Rarity],
			PartA:            classModeTypeValues[cp.CMType],
			PartB:            cp.Adapter,
			PartC:            cp.Body,
			PartD:            cp.Chassis,
			PartE:            cp.Drum,
			PartF:            cp.Material,
			Level:            1,
			GenerationSource: domain.GenerationSourceLoot,
		}
		return "Entities/Object/Item/ClassMode/PRO_CM_" + cp.CMType + "_P", itemDNAClassMode(classModeTypeValues[cp.CMType], item.Rarity), item, nil, true
	case "Shield":
		return "", Custom{}, nil, nil, false
	default:
		return "", Custom{}, nil, nil, false
	}
}

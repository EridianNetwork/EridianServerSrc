package photon


import (
	"encoding/xml"
	"log"
	"math/rand"
	"os"
	"sync"
)


var materialTypeValues = map[string]int{
	"MT_Titanium": 0, "MT_Ceramic": 1, "MT_Chitin": 2, "MT_Tongue": 3,
	"MT_Gland": 4, "MT_Sheet": 5, "MT_Spider_Fang": 6, "MT_Diamond_Shell": 7,
	"MT_Purple_Crystal": 8, "MT_PolyesterPlastic": 9, "MT_TitaniumAxiscore": 10,
	"MT_PulseBrass": 11, "MT_MetalCoil": 12,
}

type xmlAcquiredMaterial struct {
	MaterialType string `xml:"m_MaterialType"`
	Quantity     int    `xml:"m_MaterialNums"`
	Percentage   int    `xml:"m_Percentage"`
}

type xmlWeaponMeltConfig struct {
	MinWeaponLevel int                   `xml:"m_MinWeaponLevel"`
	MaxWeaponLevel int                   `xml:"m_MaxWeaponLevel"`
	WeaponRarity   string                `xml:"m_WeaponRarity"`
	WeaponType     string                `xml:"m_WeaponType"`
	Cost           int                   `xml:"m_cost"`
	Materials      []xmlAcquiredMaterial `xml:"m_AcquiredMaterials>m_AcquiredMaterial"`
}

type xmlWeaponMeltManager struct {
	Configs []xmlWeaponMeltConfig `xml:"m_WeaponMeltConfigs>m_WeaponMeltConfig"`
}

type xmlCraftCondition struct {
	MaterialType string `xml:"m_MatCondition"`
	Quantity     int    `xml:"m_MatConditionQuantity"`
}

type xmlCraftRarity struct {
	Rarity      string `xml:"m_itemRarity"`
	Probability int    `xml:"m_probability"`
}

type xmlWeaponCraftConfig struct {
	MinLevel   int                 `xml:"m_MinLevel"`
	MaxLevel   int                 `xml:"m_MaxLevel"`
	Currency   int                 `xml:"m_Currency"`
	WeaponType string              `xml:"m_WeaponType"`
	Conditions []xmlCraftCondition `xml:"m_CraftConditions>m_CraftCondition"`
	Rarities   []xmlCraftRarity    `xml:"m_rarityList>m_rarity"`
}

type xmlWeaponCraftManager struct {
	Configs []xmlWeaponCraftConfig `xml:"m_WeaponCraftConfigs>m_WeaponCraftConfig"`
}

type xmlItemConsume struct {
	ItemType     string `xml:"m_itemType"`
	ItemNums     int    `xml:"m_itemNums"`
	MaterialType string `xml:"m_materialType"`
	MaterialNums int    `xml:"m_materialNums"`
}

type xmlStarLevelConfig struct {
	StarLevel    int            `xml:"m_starLevel"`
	BasicStarExp int            `xml:"m_basicStarExp"`
	SuccessRate  float64        `xml:"m_successRate"`
	ItemConsume  xmlItemConsume `xml:"m_itemConsume"`
}

type xmlStarLevelUpgrade struct {
	WeaponType string               `xml:"m_weaponType"`
	Configs    []xmlStarLevelConfig `xml:"m_starLevelConfigList>m_starLevelConfig"`
}

type xmlStarLevelUpgradeSetup struct {
	Upgrades []xmlStarLevelUpgrade `xml:"m_starLevelUpgrade>m_starLevelUpgrade"`
}


type xmlConsumedMaterial struct {
	MaterialType string `xml:"m_consumedMaterialType"`
	MaterialNums int    `xml:"m_consumedMaterialNums"`
}

type xmlMaterialConsume struct {
	WeaponType string                `xml:"m_weaponType"`
	Materials  []xmlConsumedMaterial `xml:"m_consumedMaterialList>m_consumedMaterial"`
}

type xmlStarExpUpgradeSetup struct {
	RewardValue int                  `xml:"m_rewardValue"`
	Consumes    []xmlMaterialConsume `xml:"m_materialConsumeList>m_materialConsume"`
}

var (
	craftConfigOnce sync.Once
	meltManager     xmlWeaponMeltManager
	craftManager    xmlWeaponCraftManager
	starLevelSetup  xmlStarLevelUpgradeSetup
	starExpSetup    xmlStarExpUpgradeSetup
)

func loadCraftConfig() {
	craftConfigOnce.Do(func() {
		if raw, err := os.ReadFile("data/crafting/WeaponMelting.xml"); err == nil {
			if err := xml.Unmarshal(raw, &meltManager); err != nil {
				log.Printf("[CRAFT] bad WeaponMelting.xml: %v", err)
			}
		} else {
			log.Printf("[CRAFT] data/crafting/WeaponMelting.xml not found: %v", err)
		}
		if raw, err := os.ReadFile("data/crafting/WeaponCrafting.xml"); err == nil {
			if err := xml.Unmarshal(raw, &craftManager); err != nil {
				log.Printf("[CRAFT] bad WeaponCrafting.xml: %v", err)
			}
		} else {
			log.Printf("[CRAFT] data/crafting/WeaponCrafting.xml not found: %v", err)
		}
		if raw, err := os.ReadFile("data/crafting/StarLevelUpgrade.xml"); err == nil {
			if err := xml.Unmarshal(raw, &starLevelSetup); err != nil {
				log.Printf("[CRAFT] bad StarLevelUpgrade.xml: %v", err)
			}
		} else {
			log.Printf("[CRAFT] data/crafting/StarLevelUpgrade.xml not found: %v", err)
		}
		if raw, err := os.ReadFile("data/crafting/StarExpUpgrade.xml"); err == nil {
			if err := xml.Unmarshal(raw, &starExpSetup); err != nil {
				log.Printf("[CRAFT] bad StarExpUpgrade.xml: %v", err)
			}
		} else {
			log.Printf("[CRAFT] data/crafting/StarExpUpgrade.xml not found: %v", err)
		}
		log.Printf("[CRAFT] loaded %d melt configs, %d craft configs, %d star-level weapon types, %d star-exp weapon types",
			len(meltManager.Configs), len(craftManager.Configs), len(starLevelSetup.Upgrades), len(starExpSetup.Consumes))
	})
}

func findStarLevelConfig(weaponType, starLevel int) *xmlStarLevelConfig {
	loadCraftConfig()
	for i := range starLevelSetup.Upgrades {
		u := &starLevelSetup.Upgrades[i]
		if weaponTypeValues[u.WeaponType] != weaponType {
			continue
		}
		for j := range u.Configs {
			if u.Configs[j].StarLevel == starLevel {
				return &u.Configs[j]
			}
		}
	}
	return nil
}

func starExpMaterials(weaponType int) *xmlMaterialConsume {
	loadCraftConfig()
	for i := range starExpSetup.Consumes {
		if weaponTypeValues[starExpSetup.Consumes[i].WeaponType] == weaponType {
			return &starExpSetup.Consumes[i]
		}
	}
	return nil
}

func findMeltConfig(weaponLevel, rarity, weaponType int) *xmlWeaponMeltConfig {
	loadCraftConfig()
	for i := range meltManager.Configs {
		c := &meltManager.Configs[i]
		if weaponLevel >= c.MinWeaponLevel && weaponLevel <= c.MaxWeaponLevel &&
			itemRarityValues[c.WeaponRarity] == rarity && weaponTypeValues[c.WeaponType] == weaponType {
			return c
		}
	}
	return nil
}


func findCraftConfig(charLevel, weaponType int) *xmlWeaponCraftConfig {
	loadCraftConfig()
	for i := range craftManager.Configs {
		c := &craftManager.Configs[i]
		if charLevel >= c.MinLevel && charLevel <= c.MaxLevel && weaponTypeValues[c.WeaponType] == weaponType {
			return c
		}
	}
	return nil
}


func rollCraftRarity(rarities []xmlCraftRarity) int {
	total := 0
	for _, r := range rarities {
		total += r.Probability
	}
	if total <= 0 {
		return itemRarityValues["Common"]
	}
	roll := rand.Intn(total)
	acc := 0
	for _, r := range rarities {
		acc += r.Probability
		if roll < acc {
			return itemRarityValues[r.Rarity]
		}
	}
	return itemRarityValues[rarities[len(rarities)-1].Rarity]
}

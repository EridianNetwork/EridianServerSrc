package photon

import (
	"encoding/xml"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"

	"eridian/internal/domain"
)

type xmlWeightedRarity struct {
	Rarity string  `xml:"m_rarity"`
	Weight float64 `xml:"m_weight"`
}

type xmlWeightedWeaponType struct {
	WeaponType string  `xml:"m_weaponType"`
	Weight     float64 `xml:"m_weight"`
}

type xmlWeightedModType struct {
	ModType string  `xml:"m_modType"`
	Weight  float64 `xml:"m_weight"`
}

type xmlItemAvailability struct {
	ItemType   string                  `xml:"m_itemType"`
	RarityDist []xmlWeightedRarity     `xml:"m_rarityDistribution>m_weightedRarity"`
	WeaponDist []xmlWeightedWeaponType `xml:"m_weaponDistribution>m_weightedWeaponType"`
	ModDist    []xmlWeightedModType    `xml:"m_modDistribution>m_weightedModType"`
}

type xmlShop struct {
	ID           int                   `xml:"m_id"`
	RefreshTime  int                   `xml:"m_refreshTime"`
	Availability []xmlItemAvailability `xml:"m_itemAvailabilitySettingsList>m_itemAvailabilitySettings"`
}

type xmlShopSetup struct {
	SoldRatio float64   `xml:"m_soldRatio"`
	Shops     []xmlShop `xml:"m_shops>m_shop"`
}

type xmlRarityAdjustValue struct {
	Rarity string  `xml:"m_rarety"`
	Value  float64 `xml:"m_adjustValue"`
}

type xmlItemPriceSetting struct {
	ItemType    string                 `xml:"m_itemType"`
	BasicValue  int                    `xml:"m_basicValue"`
	Adjustments []xmlRarityAdjustValue `xml:"m_adjustements>m_raretyAdjustValue"`
}

type xmlEconomicSettings struct {
	PriceSettings []xmlItemPriceSetting `xml:"m_priceSettings>m_itemPriceSetting"`
}

var (
	shopConfigOnce sync.Once
	shopSetup      xmlShopSetup
	economicSetup  xmlEconomicSettings
)

func loadShopConfig() {
	shopConfigOnce.Do(func() {
		if raw, err := os.ReadFile("data/shop/Shops.xml"); err == nil {
			if err := xml.Unmarshal(raw, &shopSetup); err != nil {
				log.Printf("[SHOP] bad Shops.xml: %v", err)
			}
		} else {
			log.Printf("[SHOP] data/shop/Shops.xml not found: %v", err)
		}
		if raw, err := os.ReadFile("data/shop/EconomicSettings.xml"); err == nil {
			if err := xml.Unmarshal(raw, &economicSetup); err != nil {
				log.Printf("[SHOP] bad EconomicSettings.xml: %v", err)
			}
		} else {
			log.Printf("[SHOP] data/shop/EconomicSettings.xml not found: %v", err)
		}
		log.Printf("[SHOP] loaded %d shops, %d price settings", len(shopSetup.Shops), len(economicSetup.PriceSettings))
	})
}

// itemRarityValues maps the XML's string rarity names to ItemRarity ints
// (Common=0..MetaEpic=5. See XsdSettings.ItemRarity/WeaponRarity).
var itemRarityValues = map[string]int{
	"Common": 0, "Uncommon": 1, "Rare": 2, "VeryRare": 3, "Epic": 4, "MetaEpic": 5,
}

var weaponTypeValues = map[string]int{
	"SMG": 0, "SniperRifle": 1, "ShotGun": 2, "RepeaterPistol": 3, "CombatRifle": 4,
}

// Named ordinals for weaponTypeValues, used where a raw 0-4 would be
// unreadable (e.g. weaponFireRateRPS).
const (
	weaponTypeSMG            = int32(0)
	weaponTypeSniperRifle    = int32(1)
	weaponTypeShotGun        = int32(2)
	weaponTypeRepeaterPistol = int32(3)
	weaponTypeCombatRifle    = int32(4)
)

var weaponTypeNames = map[int]string{
	0: "SMG", 1: "Sniper Rifle", 2: "Shotgun", 3: "Repeater Pistol", 4: "Combat Rifle",
}

// avatarType values reuse the same SIREN=0/SOLDIER=1/BERSERKER=2/HUNTER=3
// convention as domain.Character.ClassType.
var avatarTypeValues = map[string]int{
	"SIREN": 0, "SOLDIER": 1, "BERSERKER": 2, "HUNTER": 3,
}

var classModNames = map[int]string{
	0: "Siren Mod", 1: "Soldier Mod", 2: "Berserker Mod", 3: "Hunter Mod",
}

// classModeTypeForAvatar converts an AvatarType (SIREN=0/SOLDIER=1/ BERSERKER=2/HUNTER=3) to one of
// its two XsdSettings.ClassModeType variants. ClassModeDNA.smethod_0 parses the wire's cmType field
// (itemDNAHashtable key 10) as ClassModeType, NOT AvatarType, and ClassModeDNA.method_3 maps it back
// to a class (SoldierDemoMan/SoldierMachinist=SOLDIER, SirenEnchanter/SirenSorcerer=SIREN,
var classModeTypeForAvatar = map[int]int{
	0: 2, // SIREN -> SirenEnchanter
	1: 0, // SOLDIER -> SoldierDemoMan
	2: 4, // BERSERKER -> BerserkerGuardian
	3: 6, // HUNTER -> HunterAgent
}

func weightedRarity(dist []xmlWeightedRarity) int {
	total := 0.0
	for _, r := range dist {
		total += r.Weight
	}
	if total <= 0 {
		return itemRarityValues["Common"]
	}
	roll := rand.Float64() * total
	acc := 0.0
	for _, r := range dist {
		acc += r.Weight
		if roll < acc {
			return itemRarityValues[r.Rarity]
		}
	}
	return itemRarityValues[dist[len(dist)-1].Rarity]
}

func weightedWeaponType(dist []xmlWeightedWeaponType) int {
	total := 0.0
	for _, w := range dist {
		total += w.Weight
	}
	if total <= 0 {
		return 0
	}
	roll := rand.Float64() * total
	acc := 0.0
	for _, w := range dist {
		acc += w.Weight
		if roll < acc {
			return weaponTypeValues[w.WeaponType]
		}
	}
	return weaponTypeValues[dist[len(dist)-1].WeaponType]
}

func weightedModType(dist []xmlWeightedModType) int {
	total := 0.0
	for _, m := range dist {
		total += m.Weight
	}
	if total <= 0 {
		return 0
	}
	roll := rand.Float64() * total
	acc := 0.0
	for _, m := range dist {
		acc += m.Weight
		if roll < acc {
			return avatarTypeValues[m.ModType]
		}
	}
	return avatarTypeValues[dist[len(dist)-1].ModType]
}

// levelGatedPick returns a uniformly random element of pool whose level() is <= maxLevel, so a shop
// roll.
func levelGatedPick[T any](pool []T, level func(T) int, maxLevel int) (T, bool) {
	var zero T
	if len(pool) == 0 {
		return zero, false
	}
	minLevel := level(pool[0])
	for _, p := range pool[1:] {
		if l := level(p); l < minLevel {
			minLevel = l
		}
	}
	var eligible []T
	for _, p := range pool {
		if level(p) <= maxLevel {
			eligible = append(eligible, p)
		}
	}
	if len(eligible) == 0 {
		for _, p := range pool {
			if level(p) == minLevel {
				eligible = append(eligible, p)
			}
		}
	}
	return eligible[rand.Intn(len(eligible))], true
}

func newShopItemUID() string { return newRandomUID("shop") }

// newRandomUID generates a unique opaque item UID with a prefix identifying
// where it came from (log/debugging readability only, nothing parses the
// prefix back out).
func newRandomUID(prefix string) string {
	const hex = "0123456789abcdef"
	b := make([]byte, 16)
	for i := range b {
		b[i] = hex[rand.Intn(len(hex))]
	}
	return prefix + "-" + string(b)
}

// shopItemPrice mirrors ShopServiceWrapper.method_4: basicValue * rarity
// adjustment * item level, looked up by item kind (domain.ItemType*) and
// XsdSettings.ItemRarity int.
func shopItemPrice(itemKind, rarity, level int) int {
	var kindName string
	switch itemKind {
	case domain.ItemTypeWeapon:
		kindName = "Weapon"
	case domain.ItemTypeShield:
		kindName = "Shield"
	case domain.ItemTypeClassMode:
		kindName = "ClassMode"
	case domain.ItemTypeMaterial:
		kindName = "Material"
	default:
		return 0
	}
	for _, ps := range economicSetup.PriceSettings {
		if ps.ItemType != kindName {
			continue
		}
		for _, adj := range ps.Adjustments {
			if itemRarityValues[adj.Rarity] == rarity {
				return int(float64(ps.BasicValue) * adj.Value * float64(level))
			}
		}
		return ps.BasicValue
	}
	return 0
}

// outfitShopID is Lil Sis Jingle's shop (data/shop/Shops.xml id=3, m_itemType=Outfit), the 4th shop,
// server-only addition — see internal/db/pvpstats.go's AddCharacterHonor for why it's priced in Honor
// Points instead of money.
const outfitShopID = 3

// outfitHonorPrice is the Honor Points cost for one shop-rolled outfit, keyed by its real rarity
// (itemRarityValues ordinal).
var outfitHonorPriceTable = map[int]int32{
	0: 1000, // Common
	1: 1000, // Uncommon
	2: 2000, // Rare
	3: 3500, // VeryRare
	4: 5000, // Epic
	5: 7500, // MetaEpic
}

func outfitHonorPrice(rarity int) int32 {
	if p, ok := outfitHonorPriceTable[rarity]; ok {
		return p
	}
	return outfitHonorPriceTable[0]
}

// rollShopItem generates one item DNA for a shop's ItemAvailabilitySettings
// entry at the given character level, class picks the outfit shop's real per-class outfit pool
// (SIREN=0/SOLDIER=1/BERSERKER=2/HUNTER=3, domain.Character.ClassType's own convention).
func rollShopItem(avail xmlItemAvailability, level, class int) *domain.InventoryItem {
	if level < 1 {
		level = 1
	}
	rarity := weightedRarity(avail.RarityDist)
	it := &domain.InventoryItem{
		Level:  level,
		Rarity: rarity,
		UID:    newShopItemUID(),
	}
	switch avail.ItemType {
	case "Weapon":
		gen := newGeneratedWeapon(weightedWeaponType(avail.WeaponDist), it.Rarity, it.Level, it.GenerationSource)
		gen.UID = it.UID
		it = gen
	case "Shield":
		it.ItemKind = domain.ItemTypeShield
		if sp := randomShieldPresetAnyRarity(level); sp != nil {
			it.ResourceID = sp.ID
			it.ItemName = sp.Name
			it.Rarity = itemRarityValues[sp.Rarity]
			it.Level = sp.Level
			it.PartA = shieldManufacturerValues[sp.Manufacturer]
			it.PartB = sp.Body
			it.PartC = sp.Battery
			it.PartD = sp.Capacitor
			it.PartE = sp.Accessory
			it.PartF = sp.Material
		} else {
			it.ResourceID = 1
			it.ItemName = "Shield"
		}
	case "ClassMode":
		it.ItemKind = domain.ItemTypeClassMode
		mt := weightedModType(avail.ModDist)
		if cp := randomClassModePresetAnyRarity(mt, level); cp != nil {
			it.ResourceID = cp.ID
			it.ItemName = cp.Name
			it.Rarity = itemRarityValues[cp.Rarity]
			it.Level = cp.Level
			it.PartA = classModeTypeValues[cp.CMType]
			it.PartB = cp.Adapter
			it.PartC = cp.Body
			it.PartD = cp.Chassis
			it.PartE = cp.Drum
			it.PartF = cp.Material
		} else {
			it.PartA = classModeTypeForAvatar[mt]
			it.ResourceID = mt + 1
			it.ItemName = classModNames[mt]
		}
	case "Outfit":
		it.ItemKind = domain.ItemTypeOutfit
		matches := findOutfitPresets(class, "")
		if len(matches) == 0 {
			return nil
		}
		preset := matches[rand.Intn(len(matches))]
		it.ResourceID = preset.ID
		it.ItemName = preset.Name
		it.Rarity = itemRarityValues[preset.Rarity]
		it.PartA = avatarTypeValues[preset.AvatarType]
		it.PartB = avatarSubPartValues[preset.AvatarSubpart]
		it.PartC = preset.Index
		it.Level = 1 // outfits aren't leveled, same as hostGrantOutfit's own "/outfit" grant (host.go)
		it.Price = int(outfitHonorPrice(it.Rarity))
		return it
	default:
		return nil
	}
	it.Price = shopItemPrice(it.ItemKind, it.Rarity, it.Level)
	return it
}

// shopStockSlots is how many items each shop rolls per refresh.
const shopStockSlots = 12

type shopStock struct {
	LastGenerated int64
	Items         []*domain.InventoryItem // index = shop slot
}

type shopStockKey struct {
	shopID int
	charID int64
}

var (
	shopStockMu sync.Mutex
	shopStocks  = map[shopStockKey]*shopStock{}
)

func getShopStock(shopID int, charID int64, level, class int) (*shopStock, *xmlShop) {
	loadShopConfig()
	var cfg *xmlShop
	for i := range shopSetup.Shops {
		if shopSetup.Shops[i].ID == shopID {
			cfg = &shopSetup.Shops[i]
			break
		}
	}
	if cfg == nil {
		return &shopStock{LastGenerated: time.Now().Unix()}, nil
	}

	shopStockMu.Lock()
	defer shopStockMu.Unlock()

	key := shopStockKey{shopID, charID}
	stock := shopStocks[key]
	now := time.Now().Unix()
	if stock != nil && now < stock.LastGenerated+int64(cfg.RefreshTime) {
		return stock, cfg
	}

	items := make([]*domain.InventoryItem, 0, shopStockSlots)
	for i := 0; i < shopStockSlots && len(cfg.Availability) > 0; i++ {
		avail := cfg.Availability[i%len(cfg.Availability)]
		if it := rollShopItem(avail, level, class); it != nil {
			items = append(items, it)
		}
	}
	stock = &shopStock{LastGenerated: now, Items: items}
	shopStocks[key] = stock
	return stock, cfg
}

// shopSellRatio is XsdSettings.ShopSetup.m_soldRatio (0.2 in Shops.xml
// selling an item refunds 20% of its shop price).
func shopSellRatio() float64 {
	loadShopConfig()
	if shopSetup.SoldRatio <= 0 {
		return 0.2
	}
	return shopSetup.SoldRatio
}

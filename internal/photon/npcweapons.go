package photon

import (
	"encoding/binary"
	"encoding/json"
	"log"
	"os"
	"sync"

	"eridian/internal/domain"
)

type npcWeaponPreset struct {
	Name      string         `json:"name"`
	ID        int            `json:"id"`
	Type      string         `json:"type"` // XsdSettings.WeaponType name (SMG, SniperRifle, ShotGun, RepeaterPistol, CombatRifle)
	Rarity    string         `json:"rarity"`
	Level     int            `json:"level"`
	Subparts  map[string]int `json:"subparts"` // ACC, ACTION, BARREL, BODY, GRIP, MAG, SIGHT, STOCK, MATERIAL, real authored variant indices
	ExtraSlot int            `json:"extraSlot"`
}

var npcWeaponSubpartOrder = [9]string{"ACC", "ACTION", "BARREL", "BODY", "GRIP", "MAG", "SIGHT", "STOCK", "MATERIAL"}

var npcWeaponTypeOrdinal = map[string]int32{
	"SMG": 0, "SniperRifle": 1, "ShotGun": 2, "RepeaterPistol": 3, "CombatRifle": 4,
}

var npcWeaponRarityOrdinal = map[string]int32{
	"Common": 0, "Uncommon": 1, "Rare": 2, "VeryRare": 3, "Epic": 4, "MetaEpic": 5,
}

var npcKitWeapon = map[string]string{
	"BanditKiller":         "Killer SMG",
	"BadassBanditKiller":   "Killer SMG",
	"BanditRaider":         "Raider SMG",
	"BanditBruiser":        "Bruiser Shotgun",
	"BanditApollyon":       "Apollyon Shotgun",
	"MidgetShotgunner":     "Midget Shotgun",
	"CrimsonLanceInfantry": "Infantry CR",
	"CrimsonLanceMedic":    "Medic SMG",
	"CrimsonLanceSniper":   "Lance Sniper",
	"SoldierTurretBasic":   "Soldier Turret Gun",
	"BanditScout": "Bandit Sniper",
	"CrimsonLanceDefender":       "Badass Fire CR",
	"BadassCrimsonLanceDefender": "Badass Fire CR",
	"CrimsonMechaKnoxx": "Badass SMG",
	"DrBlofeld":         "Badass Pistol",
	"MajorVanCleef":     "Badass Electric Pistol",
}

var (
	npcWeaponsOnce  sync.Once
	npcWeaponsByID  map[string]*npcWeaponPreset
	npcWeaponsFound bool
)

func loadNpcWeapons() {
	npcWeaponsOnce.Do(func() {
		npcWeaponsByID = map[string]*npcWeaponPreset{}
		raw, err := os.ReadFile("data/npc_weapons.json")
		if err != nil {
			log.Printf("[NPC] data/npc_weapons.json not loaded: %v", err)
			return
		}
		var list []npcWeaponPreset
		if err := json.Unmarshal(raw, &list); err != nil {
			log.Printf("[NPC] data/npc_weapons.json parse error: %v", err)
			return
		}
		for i := range list {
			npcWeaponsByID[list[i].Name] = &list[i]
		}
		npcWeaponsFound = len(list) > 0
		if npcWeaponsFound {
			log.Printf("[NPC] loaded %d NPC weapon presets from data/npc_weapons.json", len(list))
		}
	})
}

func npcWeaponFor(kit string) *npcWeaponPreset {
	loadNpcWeapons()
	name, ok := npcKitWeapon[kit]
	if !ok {
		return nil
	}
	return npcWeaponsByID[name]
}

func npcWeaponItemDNA(w *npcWeaponPreset) Custom {
	wtype := npcWeaponTypeOrdinal[w.Type]
	name := utf16LEBytes(w.Name)
	buf := make([]byte, 0, 36+9*4+len(name))
	putI32 := func(v int32) { buf = binary.LittleEndian.AppendUint32(buf, uint32(v)) }
	putI32(2) // ItemType.Weapon
	putI32(int32(w.ID))
	putI32(int32(w.Level))
	putI32(wtype)
	putI32(npcWeaponRarityOrdinal[w.Rarity])
	putI32(domain.GenerationSourceLoot)
	putI32(0) // starLevel
	putI32(0) // starExperience
	putI32(9) // subPartsCode.Length
	for i, part := range npcWeaponSubpartOrder {
		idx := int32(w.Subparts[part])
		putI32(wtype<<24 | int32(i)<<16 | (idx & 0xFFFF))
	}
	putI32(0) // extraEffectCount
	putI32(int32(len(name)))
	buf = append(buf, name...)
	return Custom{Code: 66, Data: buf}
}

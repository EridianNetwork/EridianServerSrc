package photon

import (
	"log"
	"math/rand"

	"eridian/internal/db"
	"eridian/internal/domain"
)

func (s *EnetServer) opCraftWeapon(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	weaponType := int(asInt32(arr[0]))

	ch, err := db.GetCharacterByID(sess.selectedCharID)
	if err != nil {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}

	cfg := findCraftConfig(ch.Level, weaponType)
	if cfg == nil {
		log.Printf("[PHOTON] CraftWeapon charID=%d: no recipe for level=%d weaponType=%d", sess.selectedCharID, ch.Level, weaponType)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	if ch.Money < cfg.Currency {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	for _, cond := range cfg.Conditions {
		have, err := db.MaterialQuantity(sess.selectedCharID, materialTypeValues[cond.MaterialType])
		if err != nil || have < cond.Quantity {
			s.sendOpResponse(sess, opCode, -1, map[byte]any{})
			return
		}
	}

	if err := db.AddCharacterMoney(sess.selectedCharID, -cfg.Currency); err != nil {
		log.Printf("[PHOTON] CraftWeapon error charging currency: %v", err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	for _, cond := range cfg.Conditions {
		if err := db.ConsumeMaterial(sess.selectedCharID, materialTypeValues[cond.MaterialType], cond.Quantity); err != nil {
			log.Printf("[PHOTON] CraftWeapon error consuming material: %v", err)
		}
	}

	rarity := rollCraftRarity(cfg.Rarities)

	newWeapon := newGeneratedWeapon(weaponType, rarity, ch.Level, domain.GenerationSourceCraft)
	newWeapon.CharID = sess.selectedCharID
	newWeapon.UID = newRandomUID("craft")
	_, slot, err := db.AddItemToBackpack(newWeapon, clampBackpackSize(ch.BackpackSize))
	if err != nil {
		log.Printf("[PHOTON] CraftWeapon error adding weapon to backpack: %v", err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}

	log.Printf("[PHOTON] CraftWeapon charID=%d weaponType=%d rarity=%d cost=%d slot=%d", sess.selectedCharID, weaponType, rarity, cfg.Currency, slot)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: itemDNAHashtable(newWeapon), 1: int32(slot)})

	bumpAchievement(sess.selectedCharID, achIDCraftItems, 1)
	s.sendCurrencyUpdate(sess, sess.selectedCharID)
	s.sendInventorySync(sess)
	s.hostAdvanceCraftQuestTasks(sess, sess.selectedCharID)
}


func (s *EnetServer) opSmeltWeapon(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		s.sendOpResponse(sess, opCode, SmeltNoItem, map[byte]any{})
		return
	}
	slot := int(asInt32(arr[0]))

	item, err := db.GetInventoryItemAt(sess.selectedCharID, slot, false)
	if err != nil || item.ItemKind != domain.ItemTypeWeapon {
		s.sendOpResponse(sess, opCode, SmeltNoItem, map[byte]any{})
		return
	}
	if item.GenerationSource == domain.GenerationSourceShop {
		s.sendOpResponse(sess, opCode, SmeltShopBought, map[byte]any{})
		return
	}
	cfg := findMeltConfig(item.Level, item.Rarity, item.PartA)
	if cfg == nil {
		log.Printf("[PHOTON] SmeltWeapon charID=%d: no melt config for level=%d rarity=%d type=%d", sess.selectedCharID, item.Level, item.Rarity, item.PartA)
		s.sendOpResponse(sess, opCode, SmeltNoItem, map[byte]any{})
		return
	}
	ch, err := db.GetCharacterByID(sess.selectedCharID)
	if err != nil {
		s.sendOpResponse(sess, opCode, SmeltNoItem, map[byte]any{})
		return
	}
	if ch.Money < cfg.Cost {
		s.sendOpResponse(sess, opCode, SmeltNotEnoughCurrency, map[byte]any{})
		return
	}

	if err := db.RemoveInventoryItem(item.ItemID); err != nil {
		log.Printf("[PHOTON] SmeltWeapon error removing item: %v", err)
		s.sendOpResponse(sess, opCode, SmeltNoItem, map[byte]any{})
		return
	}
	if err := db.AddCharacterMoney(sess.selectedCharID, -cfg.Cost); err != nil {
		log.Printf("[PHOTON] SmeltWeapon error charging cost: %v", err)
	}

	granted := map[int]any{}
	backpackSize := clampBackpackSize(ch.BackpackSize)
	for _, mat := range cfg.Materials {
		if rand.Intn(100) >= mat.Percentage {
			continue
		}
		mt := materialTypeValues[mat.MaterialType]
		if err := db.GrantMaterial(sess.selectedCharID, mt, mat.Quantity, backpackSize); err != nil {
			log.Printf("[PHOTON] SmeltWeapon error granting material: %v", err)
			continue
		}
		granted[mt] = int32(mat.Quantity)
	}

	log.Printf("[PHOTON] SmeltWeapon charID=%d slot=%d cost=%d materials=%v", sess.selectedCharID, slot, cfg.Cost, granted)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: granted})

	bumpAchievement(sess.selectedCharID, achIDMeltItems, 1)
	s.sendCurrencyUpdate(sess, sess.selectedCharID)
	s.sendInventorySync(sess)
}

func (s *EnetServer) opUpgradeStarExperience(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	slot := int(asInt32(arr[0]))

	item, err := db.GetInventoryItemAt(sess.selectedCharID, slot, false)
	if err != nil || item.ItemKind != domain.ItemTypeWeapon {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}

	consume := starExpMaterials(item.PartA)
	if consume == nil {
		log.Printf("[PHOTON] UpgradeStarExperience charID=%d: no star-exp config for weaponType=%d", sess.selectedCharID, item.PartA)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	for _, mat := range consume.Materials {
		have, err := db.MaterialQuantity(sess.selectedCharID, materialTypeValues[mat.MaterialType])
		if err != nil || have < mat.MaterialNums {
			s.sendOpResponse(sess, opCode, -1, map[byte]any{})
			return
		}
	}
	for _, mat := range consume.Materials {
		if err := db.ConsumeMaterial(sess.selectedCharID, materialTypeValues[mat.MaterialType], mat.MaterialNums); err != nil {
			log.Printf("[PHOTON] UpgradeStarExperience error consuming material: %v", err)
		}
	}

	item.StarExperience += starExpSetup.RewardValue
	if err := db.UpdateWeaponStar(item.ItemID, item.StarLevel, item.StarExperience); err != nil {
		log.Printf("[PHOTON] UpgradeStarExperience error persisting: %v", err)
	}

	log.Printf("[PHOTON] UpgradeStarExperience charID=%d slot=%d weaponType=%d newExp=%d", sess.selectedCharID, slot, item.PartA, item.StarExperience)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: itemDNAHashtable(item)})
	s.sendInventorySync(sess)
}


func (s *EnetServer) opUpgradeStarLevel(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	slot := int(asInt32(arr[0]))

	item, err := db.GetInventoryItemAt(sess.selectedCharID, slot, false)
	if err != nil || item.ItemKind != domain.ItemTypeWeapon {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}

	cfg := findStarLevelConfig(item.PartA, item.StarLevel)
	if cfg == nil {
		log.Printf("[PHOTON] UpgradeStarLevel charID=%d: no config for weaponType=%d starLevel=%d (maxed?)", sess.selectedCharID, item.PartA, item.StarLevel)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	if item.StarExperience < cfg.BasicStarExp {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	ch, err := db.GetCharacterByID(sess.selectedCharID)
	if err != nil || ch.Money < cfg.ItemConsume.ItemNums {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	have, err := db.MaterialQuantity(sess.selectedCharID, materialTypeValues[cfg.ItemConsume.MaterialType])
	if err != nil || have < cfg.ItemConsume.MaterialNums {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}

	if err := db.AddCharacterMoney(sess.selectedCharID, -cfg.ItemConsume.ItemNums); err != nil {
		log.Printf("[PHOTON] UpgradeStarLevel error charging currency: %v", err)
	}
	if err := db.ConsumeMaterial(sess.selectedCharID, materialTypeValues[cfg.ItemConsume.MaterialType], cfg.ItemConsume.MaterialNums); err != nil {
		log.Printf("[PHOTON] UpgradeStarLevel error consuming material: %v", err)
	}

	success := rand.Float64() < cfg.SuccessRate
	if success {
		item.StarLevel++
		item.StarExperience = 0
	}
	if err := db.UpdateWeaponStar(item.ItemID, item.StarLevel, item.StarExperience); err != nil {
		log.Printf("[PHOTON] UpgradeStarLevel error persisting: %v", err)
	}

	log.Printf("[PHOTON] UpgradeStarLevel charID=%d slot=%d weaponType=%d success=%v newStarLevel=%d", sess.selectedCharID, slot, item.PartA, success, item.StarLevel)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: success, 1: itemDNAHashtable(item)})
	s.sendCurrencyUpdate(sess, sess.selectedCharID)
	s.sendInventorySync(sess)
}

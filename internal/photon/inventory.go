package photon

import (
	"encoding/json"
	"log"
	"time"

	"eridian/internal/db"
	"eridian/internal/domain"
)

const maxClientBackpackSlots = 60

func clampBackpackSize(n int) int {
	if n > maxClientBackpackSlots {
		return maxClientBackpackSlots
	}
	return n
}

func (s *EnetServer) opGetInventory(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	charID := int64(0)
	if len(arr) > 0 {
		charID = int64(asInt32(arr[0]))
	}
	if charID == 0 {
		charID = sess.selectedCharID
	}
	invHT := realInventoryHashtable(charID)
	log.Printf("[PHOTON] GetInventory user=%s charID=%d", sess.userID, charID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: invHT})
}

func (s *EnetServer) opTidy(sess *ClientSession, opCode byte, params map[byte]any) {
	if err := db.TidyInventory(sess.selectedCharID); err != nil {
		log.Printf("[PHOTON] Tidy charID=%d error: %v", sess.selectedCharID, err)
	}
	log.Printf("[PHOTON] Tidy charID=%d", sess.selectedCharID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: realInventoryHashtable(sess.selectedCharID)})
}

func (s *EnetServer) opSplitStack(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 3 {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{})
		return
	}
	fromSlot, toSlot, amount := int(asInt32(arr[0])), int(asInt32(arr[1])), int(asInt32(arr[2]))
	if err := db.SplitStack(sess.selectedCharID, fromSlot, toSlot, amount); err != nil {
		log.Printf("[PHOTON] SplitStack charID=%d from=%d to=%d amount=%d error: %v", sess.selectedCharID, fromSlot, toSlot, amount, err)
	}
	log.Printf("[PHOTON] SplitStack charID=%d from=%d to=%d amount=%d", sess.selectedCharID, fromSlot, toSlot, amount)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
	s.sendInventorySync(sess)
}

func (s *EnetServer) opMergeStack(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) >= 2 {
		fromSlot, toSlot := int(asInt32(arr[0])), int(asInt32(arr[1]))
		if err := db.MergeStack(sess.selectedCharID, fromSlot, toSlot); err != nil {
			log.Printf("[PHOTON] MergeStack charID=%d from=%d to=%d error: %v", sess.selectedCharID, fromSlot, toSlot, err)
		} else {
			log.Printf("[PHOTON] MergeStack charID=%d from=%d to=%d", sess.selectedCharID, fromSlot, toSlot)
		}
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: realInventoryHashtable(sess.selectedCharID)})
}

const maxWarehouseSlots = 60

func warehouseHashtable(sess *ClientSession) map[int]any {
	items, err := db.GetWarehouseItems(sess.dbAccountID)
	slots := map[int]any{}
	if err != nil {
		log.Printf("[PHOTON] GetWarehouse charID=%d error: %v", sess.selectedCharID, err)
	} else {
		for _, it := range items {
			slots[it.Slot] = itemDNAHashtable(it)
		}
	}
	return map[int]any{0: int32(sess.accountID), 1: int32(maxWarehouseSlots), 2: slots}
}

func (s *EnetServer) sendWarehouseSync(sess *ClientSession) {
	s.sendEventTo(sess, EvWarehouseUpdated, map[byte]any{0: warehouseHashtable(sess)})
}

func (s *EnetServer) opGetWarehouse(sess *ClientSession, opCode byte, params map[byte]any) {
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: warehouseHashtable(sess)})
}

func (s *EnetServer) opSwapWarehouseSlot(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 2 {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{})
		return
	}
	slotA, slotB := byte(asInt32(arr[0])), byte(asInt32(arr[1]))
	if err := db.SwapWarehouseSlots(sess.dbAccountID, slotA, slotB); err != nil {
		log.Printf("[PHOTON] SwapWarehouseSlot charID=%d a=%d b=%d error: %v", sess.selectedCharID, slotA, slotB, err)
	}
	log.Printf("[PHOTON] SwapWarehouseSlot charID=%d a=%d b=%d", sess.selectedCharID, slotA, slotB)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
	s.sendWarehouseSync(sess)
}

func (s *EnetServer) opSwapInventoryWarehouseSlot(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 2 {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{})
		return
	}
	var fromWarehousePanel bool
	if len(arr) > 2 {
		fromWarehousePanel, _ = arr[2].(bool)
	}
	whSlot, invSlot := int(asInt32(arr[0])), int(asInt32(arr[1]))
	if fromWarehousePanel {
		whSlot, invSlot = invSlot, whSlot
	}
	if err := db.SwapInventoryWarehouseSlot(sess.selectedCharID, sess.dbAccountID, invSlot, whSlot); err != nil {
		log.Printf("[PHOTON] SwapInventoryWarehouseSlot charID=%d wh=%d inv=%d error: %v", sess.selectedCharID, whSlot, invSlot, err)
	}
	log.Printf("[PHOTON] SwapInventoryWarehouseSlot charID=%d wh=%d inv=%d fromWarehousePanel=%v", sess.selectedCharID, whSlot, invSlot, fromWarehousePanel)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
	s.sendInventorySync(sess)
	s.sendWarehouseSync(sess)
}

func (s *EnetServer) opEquipWeapon(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 2 {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{})
		return
	}
	backpackSlot, weaponSlot := byte(asInt32(arr[0])), byte(asInt32(arr[1]))
	log.Printf("[PHOTON] EquipWeapon charID=%d backpack=%d weapon=%d", sess.selectedCharID, backpackSlot, weaponSlot)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})

	applied := false
	for _, roomSess := range s.sessionsByAccount(sess.dbAccountID) {
		if roomSess.hostChar == nil || roomSess.hostChar.CharID != sess.selectedCharID || roomSess.avatarBase == 0 {
			continue
		}
		room := s.rooms[roomSess.roomName]
		if room == nil {
			continue
		}
		applied = true
		s.hostEquipWeaponFromBackpack(room, roomSess, backpackSlot, weaponSlot)
	}
	if !applied {
		if err := db.EquipWeaponFromBackpack(sess.selectedCharID, backpackSlot, weaponSlot); err != nil {
			log.Printf("[PHOTON] EquipWeapon charID=%d backpack=%d weapon=%d error: %v", sess.selectedCharID, backpackSlot, weaponSlot, err)
		}
	}
	s.sendInventorySync(sess)
}

func (s *EnetServer) opEquipNewWeapon(sess *ClientSession, opCode byte, params map[byte]any) {
	log.Printf("[PHOTON] EquipNewWeapon charID=%d", sess.selectedCharID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}

func (s *EnetServer) opSetActiveWeapon(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 1 {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{})
		return
	}
	weaponSlot := byte(asInt32(arr[0]))
	if err := db.SetActiveWeaponSlot(sess.selectedCharID, weaponSlot); err != nil {
		log.Printf("[PHOTON] SetActiveWeapon charID=%d slot=%d error: %v", sess.selectedCharID, weaponSlot, err)
	}
	log.Printf("[PHOTON] SetActiveWeapon charID=%d slot=%d", sess.selectedCharID, weaponSlot)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
	s.sendInventorySync(sess)
	s.hostLiveApplyActiveWeapon(sess.dbAccountID, sess.selectedCharID, weaponSlot)
}

func (s *EnetServer) hostLiveApplyActiveWeapon(accountID int64, charID int64, weaponSlot byte) {
	for _, roomSess := range s.sessionsByAccount(accountID) {
		if roomSess.hostChar == nil || roomSess.avatarBase == 0 || roomSess.hostChar.CharID != charID {
			continue
		}
		room := s.rooms[roomSess.roomName]
		if room == nil {
			continue
		}
		if !isEquipCapableGamemode(gamemodeName(room)) {
			continue
		}
		weapons := equippedWeaponsOrdered(charID)
		active := -1
		for i, it := range weapons {
			if it.Slot == int(weaponSlot) {
				active = i
				break
			}
		}
		if active < 0 {
			log.Printf("[HOST] live-apply active weapon: slot=%d not found among charID=%d's equipped weapons", weaponSlot, charID)
			continue
		}

		roomSess.equipAt = time.Time{}
		previousActive := roomSess.activeWeaponSlot
		roomSess.activeWeaponSlot = byte(active)

		if byte(active) == previousActive {
			roomSess.activeWeaponSlot = (byte(active) + 1) % 4
			s.hostSendSerialize(room)
			roomSess.equipSlot = byte(active)
			roomSess.equipAt = time.Now().Add(600 * time.Millisecond)
		} else {
			s.hostSendSerialize(room)
		}
		log.Printf("[HOST] live-applied active weapon charID=%d room=%s avatarBase=%d slot=%d (compacted=%d)",
			charID, room.Name, roomSess.avatarBase, weaponSlot, active)
	}
}

func (s *EnetServer) opUnequipWeapon(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 2 {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{})
		return
	}
	weaponSlot, backpackSlot := byte(asInt32(arr[0])), byte(asInt32(arr[1]))
	if err := db.UnequipWeaponToBackpack(sess.selectedCharID, weaponSlot, backpackSlot); err != nil {
		log.Printf("[PHOTON] UnequipWeapon charID=%d weapon=%d backpack=%d error: %v", sess.selectedCharID, weaponSlot, backpackSlot, err)
	}
	log.Printf("[PHOTON] UnequipWeapon charID=%d weapon=%d backpack=%d", sess.selectedCharID, weaponSlot, backpackSlot)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
	s.sendInventorySync(sess)
}

func (s *EnetServer) opSwapWeapons(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 2 {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{})
		return
	}
	slotA, slotB := byte(asInt32(arr[0])), byte(asInt32(arr[1]))
	if err := db.SwapWeaponSlots(sess.selectedCharID, slotA, slotB); err != nil {
		log.Printf("[PHOTON] SwapWeapons charID=%d a=%d b=%d error: %v", sess.selectedCharID, slotA, slotB, err)
	}
	log.Printf("[PHOTON] SwapWeapons charID=%d a=%d b=%d", sess.selectedCharID, slotA, slotB)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
	s.sendInventorySync(sess)
}

func (s *EnetServer) opEquipClassMod(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 1 {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{})
		return
	}
	slot := byte(asInt32(arr[0]))
	if err := db.EquipClassModFromBackpack(sess.selectedCharID, slot); err != nil {
		log.Printf("[PHOTON] EquipClassMod charID=%d slot=%d error: %v", sess.selectedCharID, slot, err)
	}
	log.Printf("[PHOTON] EquipClassMod charID=%d slot=%d", sess.selectedCharID, slot)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
	s.sendInventorySync(sess)
}

func (s *EnetServer) opEquipShield(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 1 {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{})
		return
	}
	slot := byte(asInt32(arr[0]))
	if err := db.EquipShieldFromBackpack(sess.selectedCharID, slot); err != nil {
		log.Printf("[PHOTON] EquipShield charID=%d slot=%d error: %v", sess.selectedCharID, slot, err)
	}
	log.Printf("[PHOTON] EquipShield charID=%d slot=%d", sess.selectedCharID, slot)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
	s.sendInventorySync(sess)
	s.hostLiveApplyEquippedShield(sess.dbAccountID, sess.selectedCharID)
}

func (s *EnetServer) hostLiveApplyEquippedShield(accountID int64, charID int64) {
	found := false
	for _, roomSess := range s.sessionsByAccount(accountID) {
		if roomSess.hostChar == nil || roomSess.avatarBase == 0 || roomSess.hostChar.CharID != charID {
			continue
		}
		room := s.rooms[roomSess.roomName]
		if room == nil {
			continue
		}
		found = true
		s.hostApplyEquippedShield(room, roomSess, roomSess.avatarBase, roomSess.broadcastPos)
		rpcView := vitalsRPCView(room, roomSess.avatarBase)
		s.hostSendRPC(room, rpcView, "RPC_RequestAllLifeAndShield", []any{
			map[int]any{0: int32(1), 1: rpcView, 2: playerLifeTable(roomSess.hp, roomSess.maxHP, roomSess.shield, roomSess.maxShield)},
		})

		for _, d := range []time.Duration{300 * time.Millisecond, 1 * time.Second, 3 * time.Second} {
			room.pendingShieldResyncs = append(room.pendingShieldResyncs, pendingShieldResync{
				sess: roomSess, base: roomSess.avatarBase, at: time.Now().Add(d),
			})
		}
		log.Printf("[HOST] live-applied equipped shield charID=%d room=%s avatarBase=%d shield=%d/%d shieldView=%d",
			charID, room.Name, roomSess.avatarBase, roomSess.shield, roomSess.maxShield, roomSess.shieldView)
	}
	if !found {
		log.Printf("[HOST] live-apply shield: no live room session found for accountID=%d charID=%d (player not in a room, or session lookup missed)", accountID, charID)
	}
}

func (s *EnetServer) opEquipOutfit(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 1 {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{})
		return
	}
	slot := byte(asInt32(arr[0]))
	if err := db.EquipOutfitFromBackpack(sess.selectedCharID, slot); err != nil {
		log.Printf("[PHOTON] EquipOutfit charID=%d slot=%d error: %v", sess.selectedCharID, slot, err)
	}
	log.Printf("[PHOTON] EquipOutfit charID=%d slot=%d", sess.selectedCharID, slot)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
	s.sendInventorySync(sess)
	s.hostLiveApplyEquippedOutfit(sess.dbAccountID, sess.selectedCharID)
}

func (s *EnetServer) opUnequipOutfit(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 1 {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{})
		return
	}
	subpart := byte(asInt32(arr[0]))
	backpackSize := 42
	if ch, err := db.GetCharacterByID(sess.selectedCharID); err == nil {
		backpackSize = clampBackpackSize(ch.BackpackSize)
	}
	if err := db.UnequipOutfitPiece(sess.selectedCharID, subpart, backpackSize); err != nil {
		log.Printf("[PHOTON] UnequipOutfit charID=%d subpart=%d error: %v", sess.selectedCharID, subpart, err)
	}
	log.Printf("[PHOTON] UnequipOutfit charID=%d subpart=%d", sess.selectedCharID, subpart)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
	s.sendInventorySync(sess)
	s.hostLiveApplyEquippedOutfit(sess.dbAccountID, sess.selectedCharID)
}

func (s *EnetServer) hostLiveApplyEquippedOutfit(accountID int64, charID int64) {
	for _, roomSess := range s.sessionsByAccount(accountID) {
		if roomSess.hostChar == nil || roomSess.avatarBase == 0 || roomSess.hostChar.CharID != charID {
			continue
		}
		room := s.rooms[roomSess.roomName]
		if room == nil {
			continue
		}
		gamemode := gamemodeName(room)
		if !isEquipCapableGamemode(gamemode) {
			continue
		}
		rpcName := outfitRPCName(gamemode)
		outfitArgs := outfitRPCArgs(charID)
		s.hostSendRPC(room, roomSess.avatarBase+3, rpcName, outfitArgs)
		for _, d := range []time.Duration{300 * time.Millisecond, 1 * time.Second, 3 * time.Second} {
			room.pendingOutfitResyncs = append(room.pendingOutfitResyncs, pendingOutfitResync{
				sess: roomSess, base: roomSess.avatarBase, outfitArgs: outfitArgs, rpcName: rpcName, at: time.Now().Add(d),
			})
		}
		log.Printf("[HOST] live-applied equipped outfit charID=%d room=%s avatarBase=%d", charID, room.Name, roomSess.avatarBase)
	}
}

func (s *EnetServer) opDropItemFromInventory(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 1 {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{})
		return
	}
	slot := byte(asInt32(arr[0]))
	item, _ := db.GetInventoryItemAt(sess.selectedCharID, int(slot), false)
	if err := db.DropBackpackItem(sess.selectedCharID, slot); err != nil {
		log.Printf("[PHOTON] DropItem charID=%d slot=%d error: %v", sess.selectedCharID, slot, err)
	}
	log.Printf("[PHOTON] DropItem charID=%d slot=%d", sess.selectedCharID, slot)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
	s.sendInventorySync(sess)

	if item != nil && item.ItemKind == domain.ItemTypeWeapon {
		s.hostDropItemInWorld(sess, item)
	}
}

func (s *EnetServer) opSwapInventorySlots(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 2 {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{})
		return
	}
	slotA, slotB := byte(asInt32(arr[0])), byte(asInt32(arr[1]))
	if err := db.SwapBackpackSlots(sess.selectedCharID, slotA, slotB); err != nil {
		log.Printf("[PHOTON] SwapInventorySlots error: %v", err)
	}
	log.Printf("[PHOTON] SwapInventorySlots charID=%d a=%d b=%d", sess.selectedCharID, slotA, slotB)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
	s.sendInventorySync(sess)
}

func (s *EnetServer) opGetMyCraftedWeapons(sess *ClientSession, opCode byte, params map[byte]any) {
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: map[int]any{}})
}

func emptyInvHT(charID int64) map[int]any {
	return map[int]any{
		0:  int32(charID),
		1:  Byte(4),
		2:  Byte(0),
		3:  map[int]any{0: starterWeaponHT()},
		4:  map[int]any{0: int32(0), 1: int32(0)},
		5:  map[int]any{0: int32(0), 1: int32(0)},
		6:  map[int]any{0: int32(0), 1: int32(0)},
		7:  map[int]any{0: int32(0), 1: int32(0)},
		8:  map[int]any{0: int32(0), 1: int32(0)},
		9:  int32(0),
		10: map[int]any{},
		12: map[int]any{0: int32(0), 1: int32(0)},
	}
}

func starterWeaponHT() map[int]any {
	ht := make(map[int]any, len(StarterWeaponHT))
	for k, v := range StarterWeaponHT {
		ht[k] = v
	}
	return ht
}

func itemDNAHashtable(it *domain.InventoryItem) map[int]any {
	switch it.ItemKind {
	case domain.ItemTypeWeapon:
		sub := weaponItemSubparts(it)
		return map[int]any{
			0: int32(domain.ItemTypeWeapon), 1: int32(it.Level), 2: int32(it.Rarity), 3: int32(it.GenerationSource), 4: int32(it.ResourceID),
			5: int32(it.Price), 6: int32(it.Expiration), 7: int32(it.OwnerID), 8: it.UID,
			9: it.ItemName, 10: int32(it.PartA),
			11: int32(sub[0]), 12: int32(sub[1]), 13: int32(sub[2]), 14: int32(sub[3]), 15: int32(sub[4]),
			16: int32(sub[5]), 17: int32(sub[6]), 18: int32(sub[7]), 19: int32(sub[8]),
			20: int32(it.StarLevel), 21: int32(it.StarExperience), 22: int32(0),
		}
	case domain.ItemTypeShield:
		return map[int]any{
			0: int32(domain.ItemTypeShield), 1: int32(it.Level), 2: int32(it.Rarity), 3: int32(it.GenerationSource), 4: int32(it.ResourceID),
			5: int32(it.Price), 6: int32(it.Expiration), 7: int32(it.OwnerID), 8: it.UID,
			9: it.ItemName, 10: int32(0),
			11: int32(it.PartA), 12: int32(it.PartB), 13: int32(it.PartC), 14: int32(it.PartD), 15: int32(it.PartE), 16: int32(it.PartF),
			17: int32(0),
		}
	case domain.ItemTypeMaterial:
		return map[int]any{
			0: int32(domain.ItemTypeMaterial), 1: int32(1), 2: int32(0), 3: int32(0), 4: int32(0),
			5: int32(it.Price), 6: int32(it.Expiration), 7: int32(it.OwnerID), 8: it.UID,
			9: int32(it.ResourceID), 10: int32(it.Quantity),
		}
	case domain.ItemTypeLuckyKey:
		return map[int]any{
			0: int32(domain.ItemTypeLuckyKey), 1: int32(1), 2: int32(0), 3: int32(0), 4: int32(0),
			5: int32(it.Price), 6: int32(it.Expiration), 7: int32(it.OwnerID), 8: it.UID,
			9: int32(it.ResourceID), 10: int32(it.Quantity),
		}
	case domain.ItemTypeClassMode:
		return map[int]any{
			0: int32(domain.ItemTypeClassMode), 1: int32(it.Level), 2: int32(it.Rarity), 3: int32(it.GenerationSource), 4: int32(it.ResourceID),
			5: int32(it.Price), 6: int32(it.Expiration), 7: int32(it.OwnerID), 8: it.UID,
			9: it.ItemName, 10: int32(it.PartA), 11: int32(it.PartB), 12: int32(it.PartC), 13: int32(it.PartD), 14: int32(it.PartE), 15: int32(it.PartF),
		}
	case domain.ItemTypeOutfit:
		return map[int]any{
			0: int32(domain.ItemTypeOutfit), 1: int32(it.Level), 2: int32(it.Rarity), 3: int32(0), 4: int32(it.ResourceID),
			5: int32(0), 6: int32(it.Expiration), 7: int32(it.OwnerID), 8: it.UID,
			9: it.ItemName, 10: it.Description, 11: int32(it.PartA), 12: int32(it.PartB), 13: int32(it.PartC),
		}
	default:
		log.Printf("[INVENTORY] itemDNAHashtable: unhandled item_kind=%d item_id=%d char/owner=%d, sending null DNA (client will likely drop this item on next Tidy)", it.ItemKind, it.ItemID, it.OwnerID)
		return nil
	}
}

type ammoCount struct {
	Cur int `json:"cur"`
	Max int `json:"max"`
}

func decodeAmmo(raw string) map[int]ammoCount {
	m := map[int]ammoCount{}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &m)
	}
	return m
}

func ammoPairHT(ammo map[int]ammoCount, ammoType int) map[int]any {
	c := ammo[ammoType]
	return map[int]any{0: int32(c.Cur), 1: int32(c.Max)}
}

func buildInventoryHashtable(ch *domain.Character, items []*domain.InventoryItem) map[int]any {
	weapons := map[int]any{}
	backpack := map[int]any{}
	outfit := map[int]any{}
	var shield, classMod *domain.InventoryItem

	for _, it := range items {
		switch {
		case it.IsEquipped && it.ItemKind == domain.ItemTypeWeapon:
			weapons[it.Slot] = itemDNAHashtable(it)
		case it.IsEquipped && it.ItemKind == domain.ItemTypeShield:
			shield = it
		case it.IsEquipped && it.ItemKind == domain.ItemTypeClassMode:
			classMod = it
		case it.IsEquipped && it.ItemKind == domain.ItemTypeOutfit:
			outfit[it.Slot] = itemDNAHashtable(it)
		case !it.IsEquipped:
			backpack[it.Slot] = itemDNAHashtable(it)
		}
	}

	ammo := decodeAmmo(ch.Ammo)
	reportedBackpackSize := clampBackpackSize(ch.BackpackSize)
	ht := map[int]any{
		0:  int32(ch.CharID),
		1:  Byte(4),
		2:  Byte(ch.ActiveWeaponSlot),
		3:  weapons,
		4:  ammoPairHT(ammo, domain.AmmoTypePistol),
		5:  ammoPairHT(ammo, domain.AmmoTypeSMG),
		6:  ammoPairHT(ammo, domain.AmmoTypeShotGun),
		7:  ammoPairHT(ammo, domain.AmmoTypeSniperRifle),
		8:  ammoPairHT(ammo, domain.AmmoTypeGrenade),
		12: ammoPairHT(ammo, domain.AmmoTypeAssaultRifle),
		9:  int32(reportedBackpackSize),
		10: backpack,
	}
	if shield != nil {
		ht[11] = itemDNAHashtable(shield)
	}
	if classMod != nil {
		ht[13] = itemDNAHashtable(classMod)
	}
	for sp := 0; sp < domain.AvatarSubPartTotal; sp++ {
		if v, ok := outfit[sp]; ok {
			ht[15+sp] = v
		}
	}
	return ht
}

func realInventoryHashtable(charID int64) map[int]any {
	ch, err := db.GetCharacterByID(charID)
	if err != nil {
		return emptyInvHT(charID)
	}
	items, err := db.GetInventory(charID)
	if err != nil {
		log.Printf("[PHOTON] realInventoryHashtable: GetInventory char=%d error: %v", charID, err)
		return emptyInvHT(charID)
	}

	if !hasEquippedWeapon(items) {
		if err := db.SeedStarterWeapon(charID); err != nil {
			log.Printf("[PHOTON] realInventoryHashtable: seed starter weapon char=%d error: %v", charID, err)
		} else if items, err = db.GetInventory(charID); err != nil {
			log.Printf("[PHOTON] realInventoryHashtable: reload after seed char=%d error: %v", charID, err)
		}
	}

	if !weaponEquippedAtSlot(items, ch.ActiveWeaponSlot) {
		if fixed := firstEquippedWeaponSlot(items); fixed >= 0 && fixed != ch.ActiveWeaponSlot {
			if err := db.SetActiveWeaponSlot(charID, byte(fixed)); err != nil {
				log.Printf("[PHOTON] realInventoryHashtable: fix active_weapon_slot char=%d error: %v", charID, err)
			} else {
				ch.ActiveWeaponSlot = fixed
			}
		}
	}
	return buildInventoryHashtable(ch, items)
}

func hasEquippedWeapon(items []*domain.InventoryItem) bool {
	return firstEquippedWeaponSlot(items) >= 0
}

func weaponEquippedAtSlot(items []*domain.InventoryItem, slot int) bool {
	for _, it := range items {
		if it.IsEquipped && it.ItemKind == domain.ItemTypeWeapon && it.Slot == slot {
			return true
		}
	}
	return false
}

func firstEquippedWeaponSlot(items []*domain.InventoryItem) int {
	for _, it := range items {
		if it.IsEquipped && it.ItemKind == domain.ItemTypeWeapon {
			return it.Slot
		}
	}
	return -1
}

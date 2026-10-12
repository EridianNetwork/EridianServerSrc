package photon

import (
	"log"

	"eridian/internal/db"
	"eridian/internal/domain"
)


func (s *EnetServer) opGetShop(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	shopID := 0
	if len(arr) > 0 {
		shopID = int(asInt32(arr[0]))
	}
	level, class := 1, 0
	if ch, err := db.GetCharacterByID(sess.selectedCharID); err == nil {
		level, class = ch.Level, ch.ClassType
	}
	stock, cfg := getShopStock(shopID, sess.selectedCharID, level, class)
	refreshTime := int32(3600)
	if cfg != nil {
		refreshTime = int32(cfg.RefreshTime)
	}

	items := map[int]any{}
	for i, it := range stock.Items {
		items[i] = shopItemHashtable(it)
	}
	shop := map[int]any{
		0: int32(shopID),
		1: items,
		2: int32(stock.LastGenerated),
		3: refreshTime,
		4: Double(shopSellRatio()),
	}
	log.Printf("[PHOTON] GetShop charID=%d shop=%d → %d items", sess.selectedCharID, shopID, len(stock.Items))
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: shop})
}

func shopItemHashtable(it *domain.InventoryItem) map[int]any {
	ht := itemDNAHashtable(it)
	n := len(ht)
	ht[n] = int32(it.Price)
	ht[n+1] = int32(1)
	return ht
}

func (s *EnetServer) opBuyItem(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 2 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	shopID := int(asInt32(arr[0]))
	itemIndex := int(asInt32(arr[1]))

	ch, err := db.GetCharacterByID(sess.selectedCharID)
	if err != nil {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	stock, _ := getShopStock(shopID, sess.selectedCharID, ch.Level, ch.ClassType)
	if itemIndex < 0 || itemIndex >= len(stock.Items) {
		log.Printf("[PHOTON] BuyItem charID=%d shop=%d index=%d: no such item", sess.selectedCharID, shopID, itemIndex)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	rolled := stock.Items[itemIndex]

	if shopID == outfitShopID {
		err = db.AddCharacterMoney(sess.selectedCharID, -int(rolled.Price))
	} else {
		err = db.AddCharacterMoney(sess.selectedCharID, -rolled.Price)
	}
	if err != nil {
		log.Printf("[PHOTON] BuyItem charID=%d shop=%d error: %v", sess.selectedCharID, shopID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	bought := *rolled
	bought.CharID = sess.selectedCharID
	bought.UID = newShopItemUID()
	if _, _, err := db.AddItemToBackpack(&bought, clampBackpackSize(ch.BackpackSize)); err != nil {
		log.Printf("[PHOTON] BuyItem charID=%d shop=%d error: %v, refunding", sess.selectedCharID, shopID, err)
		if shopID == outfitShopID {
			db.AddCharacterMoney(sess.selectedCharID, -int(rolled.Price))
		} else {
			db.AddCharacterMoney(sess.selectedCharID, rolled.Price)
		}
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	log.Printf("[PHOTON] BuyItem charID=%d shop=%d index=%d price=%d", sess.selectedCharID, shopID, itemIndex, rolled.Price)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: itemDNAHashtable(&bought)})

	s.sendCurrencyUpdate(sess, sess.selectedCharID)
	s.sendInventorySync(sess)
}


const soldItemsCap = 10

func (s *EnetServer) redeemableItemsHashtable(charID int64) map[int]any {
	items := s.soldItems[charID]
	ht := map[int]any{}
	for i, it := range items {
		ht[i] = shopItemHashtable(it)
	}
	return ht
}

func (s *EnetServer) opSellItem(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 2 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	backpackSlot := int(asInt32(arr[1]))

	it, err := db.GetInventoryItemAt(sess.selectedCharID, backpackSlot, false)
	if err != nil {
		log.Printf("[PHOTON] SellItem charID=%d slot=%d error: %v", sess.selectedCharID, backpackSlot, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	sellPrice := int(float64(shopItemPrice(it.ItemKind, it.Rarity, max(it.Level, 1))) * shopSellRatio())
	if err := db.RemoveInventoryItem(it.ItemID); err != nil {
		log.Printf("[PHOTON] SellItem charID=%d itemID=%d error: %v", sess.selectedCharID, it.ItemID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	db.AddCharacterMoney(sess.selectedCharID, sellPrice)

	sold := *it
	sold.Price = sellPrice
	buffer := append(s.soldItems[sess.selectedCharID], &sold)
	if len(buffer) > soldItemsCap {
		buffer = buffer[len(buffer)-soldItemsCap:]
	}
	s.soldItems[sess.selectedCharID] = buffer

	log.Printf("[PHOTON] SellItem charID=%d slot=%d price=%d", sess.selectedCharID, backpackSlot, sellPrice)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{
		0: int32(sellPrice),
		1: itemDNAHashtable(it),
		2: s.redeemableItemsHashtable(sess.selectedCharID),
	})
	s.sendCurrencyUpdate(sess, sess.selectedCharID)
	s.sendInventorySync(sess)
}

func (s *EnetServer) opRedeemItem(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 2 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	slot := int(asInt32(arr[1])) - 14
	buffer := s.soldItems[sess.selectedCharID]
	if slot < 0 || slot >= len(buffer) {
		log.Printf("[PHOTON] RedeemItem charID=%d slot=%d: no such sold item", sess.selectedCharID, slot)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	it := buffer[slot]

	ch, err := db.GetCharacterByID(sess.selectedCharID)
	if err != nil {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	if err := db.AddCharacterMoney(sess.selectedCharID, -it.Price); err != nil {
		log.Printf("[PHOTON] RedeemItem charID=%d slot=%d error: %v", sess.selectedCharID, slot, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	redeemed := *it
	redeemed.CharID = sess.selectedCharID
	redeemed.UID = newShopItemUID()
	if _, _, err := db.AddItemToBackpack(&redeemed, clampBackpackSize(ch.BackpackSize)); err != nil {
		log.Printf("[PHOTON] RedeemItem charID=%d slot=%d error: %v, refunding", sess.selectedCharID, slot, err)
		db.AddCharacterMoney(sess.selectedCharID, it.Price)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	s.soldItems[sess.selectedCharID] = append(buffer[:slot], buffer[slot+1:]...)

	log.Printf("[PHOTON] RedeemItem charID=%d slot=%d price=%d", sess.selectedCharID, slot, it.Price)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{
		0: itemDNAHashtable(&redeemed),
		1: s.redeemableItemsHashtable(sess.selectedCharID),
	})
	s.sendCurrencyUpdate(sess, sess.selectedCharID)
	s.sendInventorySync(sess)
}

func (s *EnetServer) sendCurrencyUpdate(sess *ClientSession, charID int64) {
	ch, err := db.GetCharacterByID(charID)
	if err != nil {
		return
	}
	sess.charCurrency = int32(ch.Money)
	s.sendEventTo(sess, EvCurrenciesUpdated, map[byte]any{
		0: int32(ch.Money),
		1: int32(ch.SeraphCrystals),
		2: int32(ch.Eridium),
	})
}

func (s *EnetServer) sendInventorySync(sess *ClientSession) {
	s.sendEventTo(sess, EvInventoryUpdated, map[byte]any{0: realInventoryHashtable(sess.selectedCharID)})
}

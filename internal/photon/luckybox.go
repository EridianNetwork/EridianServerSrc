package photon

import (
	"log"
	"math/rand"

	"eridian/internal/db"
	"eridian/internal/domain"
)

func (s *EnetServer) opOpenLuckyBox(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	index := int32(0)
	if len(arr) > 0 {
		index = asInt32(arr[0])
	}
	locked := false
	if len(arr) > 1 {
		locked, _ = arr[1].(bool)
	}

	roomSess := s.activeGameSession[sess.userID]
	if roomSess == nil || roomSess.roomName == "" {
		log.Printf("[LUCKYBOX] OpenLuckyBox user=%s: no active room session", sess.userID)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	room := s.rooms[roomSess.roomName]
	if room == nil {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	ch, err := db.GetCharacterByID(sess.selectedCharID)
	if err != nil || ch == nil {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}

	playlistID := asInt32(room.Properties["PlaylistId"])
	difficulty := asInt32(room.Properties["Difficulty"])
	grade := playlistGrade(playlistID, difficulty, matchScore(room, roomSess, s.roomTopDamage(room)))
	if !room.luckyKeyGranted {
		room.luckyKeyGranted = true
		if err := db.GrantLuckyKey(ch.CharID, luckyBoxRankValues["Diamond"], 1, clampBackpackSize(ch.BackpackSize)); err != nil {
			log.Printf("[LUCKYBOX] GrantLuckyKey char=%d: %v", ch.CharID, err)
		} else {
			log.Printf("[LUCKYBOX] GrantLuckyKey char=%d: +1 Diamond key (level clear, grade=%s)", ch.CharID, grade)
		}
	}
	reward := findLuckyBoxReward(playlistID, difficulty, grade)
	if reward == nil {
		log.Printf("[LUCKYBOX] OpenLuckyBox char=%d: no lucky box configured for playlist=%d", ch.CharID, playlistID)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}

	tableName := reward.NormalReward
	grantedLocked := false
	if locked {
		keyRank, keyCount := luckyBoxRequiredKeyRank(reward.LockedRank)
		rank, known := luckyBoxRankValues[keyRank]
		have := 0
		if known && keyCount > 0 {
			have, err = db.LuckyKeyQuantity(ch.CharID, rank)
			if err != nil {
				log.Printf("[LUCKYBOX] LuckyKeyQuantity char=%d: %v", ch.CharID, err)
			}
		}
		if known && keyCount > 0 && have >= keyCount {
			if err := db.ConsumeLuckyKey(ch.CharID, rank); err != nil {
				log.Printf("[LUCKYBOX] ConsumeLuckyKey char=%d: %v", ch.CharID, err)
			} else {
				tableName = reward.LockedReward
				grantedLocked = true
			}
		}
	}

	backpackSize := clampBackpackSize(ch.BackpackSize)
	resp := map[byte]any{0: grantedLocked, 1: index}
	granted := map[int]any{}

	items := [3]*domain.InventoryItem{}
	for i := range items {
		items[i] = rollLuckyBoxItemForSlot(tableName, ch.Level, grantedLocked)
	}
	for i, item := range items {
		slot := byte(2 + i)
		if item == nil {
			resp[slot] = nil
			continue
		}
		item.CharID = ch.CharID
		item.UID = newRandomUID("luckybox")
		if _, _, err := db.AddItemToBackpack(item, backpackSize); err != nil {
			if err == db.ErrInventoryFull {
				log.Printf("[LUCKYBOX] AddItemToBackpack char=%d: backpack full, showing %s without granting it", ch.CharID, item.ItemName)
				resp[slot] = itemDNAHashtable(item)
				continue
			}
			log.Printf("[LUCKYBOX] AddItemToBackpack char=%d: %v", ch.CharID, err)
			resp[slot] = nil
			continue
		}
		dna := itemDNAHashtable(item)
		resp[slot] = dna
		granted[int(item.Slot)] = dna
	}

	log.Printf("[LUCKYBOX] OpenLuckyBox char=%d table=%s locked=%v index=%d", ch.CharID, tableName, grantedLocked, index)
	s.sendOpResponse(sess, opCode, 0, resp)
	if len(granted) > 0 {
		s.sendEventTo(sess, EvInventoryUpdated, map[byte]any{0: realInventoryHashtable(ch.CharID)})
	}
}

var (
	luckyBoxBandNoKey   = rarityBand{2, 3} // Rare/VeryRare
	luckyBoxBandKeyHigh = rarityBand{3, 4} // VeryRare/Epic
)

const luckyBoxKeyHighChance = 0.60

func rollLuckyBoxItemForSlot(tableName string, level int, locked bool) *domain.InventoryItem {
	band := luckyBoxBandNoKey
	if locked && rand.Float64() < luckyBoxKeyHighChance {
		band = luckyBoxBandKeyHigh
	}
	for i := 0; i < 20; i++ {
		rolled := rollLuckyBoxSlot(tableName, level)
		if rolled == nil {
			continue
		}
		if _, _, item, _, ok := lootDropPayload(*rolled, level, band, true); ok && item != nil {
			return item
		}
	}
	return nil
}

package photon

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"eridian/internal/db"
	"eridian/internal/domain"
)


func flushCharacterPlayTime(sess *ClientSession) {
	if sess.selectedCharID == 0 || sess.charSelectedAt.IsZero() {
		return
	}
	elapsed := int(time.Since(sess.charSelectedAt).Seconds())
	if err := db.AddCharacterPlayTime(sess.selectedCharID, elapsed); err != nil {
		log.Printf("[PHOTON] AddCharacterPlayTime charID=%d: %v", sess.selectedCharID, err)
	}
	sess.charSelectedAt = time.Time{}
}

func (s *EnetServer) opGetCharacterList(sess *ClientSession, opCode byte, params map[byte]any) {
	chars, err := db.GetCharactersByAccount(sess.dbAccountID)
	if err != nil {
		log.Printf("[PHOTON] GetCharacterList error: %v", err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	result := map[int]any{}
	for _, ch := range chars {
		result[int(ch.CharID)] = charToHashtable(ch, charListInventoryHashtable(ch.CharID))
	}
	log.Printf("[PHOTON] GetCharacterList user=%s dbID=%d → %d chars", sess.userID, sess.dbAccountID, len(result))
	s.sendOpResponse(sess, opCode, 0, map[byte]any{ParamAccountID: result})
}

func (s *EnetServer) opCreateCharacter(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	name := "Unknown"
	if len(arr) > 0 {
		name = fmt.Sprintf("%v", arr[0])
	}
	avatarType := 0
	if len(arr) > 1 {
		avatarType = int(asInt32(arr[1]))
	}
	avatarIDRaw := uint32(0)
	if len(arr) > 2 {
		avatarIDRaw = uint32(asInt32(arr[2]))
	}
	avatarParts := AvatarDefaultParts
	if len(arr) > 3 && arr[3] != nil {
		p := fmt.Sprintf("%v", arr[3])
		if strings.Contains(p, ",") {
			avatarParts = p
		}
	}
	avatarMats := AvatarDefaultParts
	if len(arr) > 4 && arr[4] != nil {
		m := fmt.Sprintf("%v", arr[4])
		if strings.Contains(m, ",") {
			avatarMats = m
		}
	}
	if avatarIDRaw == 0 {
		avatarIDRaw = AvatarDefaultBkID[avatarType]
	}
	avatarMats = clampAvatarMaterialsCSV(avatarIDRaw, avatarParts, avatarMats)

	charID, err := db.CreateCharacterPhoton(sess.dbAccountID, name, avatarType, avatarIDRaw, avatarParts, avatarMats, "{}")
	if err != nil {
		log.Printf("[PHOTON] CreateCharacter error: %v", err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	ch, err := db.GetCharacterByID(charID)
	if err != nil {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	if err := db.SeedStarterWeapon(charID); err != nil {
		log.Printf("[PHOTON] CreateCharacter: seed starter weapon char=%d error: %v", charID, err)
	}
	flushCharacterPlayTime(sess)
	sess.selectedCharID = charID

	cacheCharInSession(sess, ch)
	sess.charSelectedAt = time.Now()
	log.Printf("[PHOTON] CreateCharacter user=%s name=%s type=%d → charID=%d", sess.userID, name, avatarType, charID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{ParamAccountID: charToHashtable(ch, realInventoryHashtable(charID))})
}

func (s *EnetServer) opSelectCharacter(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	charID := int64(0)
	if len(arr) > 0 {
		charID = int64(asInt32(arr[0]))
	}
	ch, err := db.GetCharacterByID(charID)
	if err != nil {
		log.Printf("[PHOTON] SelectCharacter: charID=%d not found for user=%s", charID, sess.userID)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	if sess.selectedCharID != charID {
		flushCharacterPlayTime(sess)
		sess.charSelectedAt = time.Now()
	}
	sess.selectedCharID = charID
	cacheCharInSession(sess, ch)
	if err := db.UpdateCharacterLastSeen(charID); err != nil {
		log.Printf("[PHOTON] SelectCharacter: UpdateCharacterLastSeen charID=%d: %v", charID, err)
	}
	loadQuestCatalog()
	if err := db.ResetStaleDailyQuests(charID, dailyQuestIDs); err != nil {
		log.Printf("[PHOTON] SelectCharacter: ResetStaleDailyQuests charID=%d: %v", charID, err)
	}
	log.Printf("[PHOTON] SelectCharacter user=%s charID=%d name=%s", sess.userID, charID, ch.Name)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{ParamAccountID: charToHashtable(ch, realInventoryHashtable(charID))})
	s.broadcastPresenceUpdate(charID)
}

func (s *EnetServer) opGetCharacter(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	charID := int64(0)
	if len(arr) > 0 {
		charID = int64(asInt32(arr[0]))
	}
	if charID == 0 {
		charID = sess.selectedCharID
	}
	ch, err := db.GetCharacterByID(charID)
	if err != nil {
		log.Printf("[PHOTON] GetCharacter: charID=%d not found, returning stub", charID)
		stub := stubChar(charID, sess.dbAccountID)
		s.sendOpResponse(sess, opCode, 0, map[byte]any{ParamAccountID: charToHashtable(stub, emptyInvHT(charID))})
		return
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{ParamAccountID: charToHashtable(ch, realInventoryHashtable(charID))})
}

func (s *EnetServer) opDelCharacter(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	charID := int64(0)
	if len(arr) > 0 {
		charID = int64(asInt32(arr[0]))
	}
	if err := db.DeleteCharacterByAccountID(charID, sess.dbAccountID); err != nil {
		log.Printf("[PHOTON] DeleteCharacter user=%s charID=%d failed: %v", sess.userID, charID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	if sess.selectedCharID == charID {
		sess.selectedCharID = 0
	}
	log.Printf("[PHOTON] DeleteCharacter user=%s charID=%d", sess.userID, charID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}
func (s *EnetServer) opGetSettings(sess *ClientSession, opCode byte, params map[byte]any) {
	log.Printf("[PHOTON] GetSettings user=%s", sess.userID)
	settings := ""
	if v, err := db.GetAccountSettings(sess.dbAccountID); err == nil {
		settings = v
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{ParamAccountID: settings})
}

func (s *EnetServer) opSetSettings(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) > 0 {
		incoming := fmt.Sprintf("%v", arr[0])
		existing, _ := db.GetAccountSettings(sess.dbAccountID)
		if err := db.UpdateAccountSettings(sess.dbAccountID, mergePlayerSettings(existing, incoming)); err != nil {
			log.Printf("[PHOTON] SetSettings accountID=%d error: %v", sess.dbAccountID, err)
		}
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}


func mergePlayerSettings(existing, incoming string) string {
	newEntries, ok := decodePlayerSettingEntries(incoming)
	if !ok {
		return incoming // malformed input, nothing to merge, store as-is
	}
	oldEntries, ok := decodePlayerSettingEntries(existing)
	if !ok || len(oldEntries) == 0 {
		return incoming // nothing to merge with, avoid needless reformatting
	}

	byKey := make(map[float64]map[string]any, len(oldEntries)+len(newEntries))
	var order []float64
	add := func(entries []map[string]any) {
		for _, e := range entries {
			key, ok := e["1"].(float64)
			if !ok {
				continue
			}
			if _, seen := byKey[key]; !seen {
				order = append(order, key)
			}
			byKey[key] = e
		}
	}
	add(oldEntries)
	add(newEntries) // same key as an old entry → overwrites it, new value wins
	sort.Float64s(order)

	merged := make([]map[string]any, 0, len(order))
	for _, key := range order {
		merged = append(merged, byKey[key])
	}
	out, err := json.Marshal(map[string]any{"PlayerSetting": merged})
	if err != nil {
		return incoming
	}
	return string(out)
}

func decodePlayerSettingEntries(blob string) ([]map[string]any, bool) {
	if blob == "" {
		return nil, false
	}
	var parsed struct {
		PlayerSetting []map[string]any `json:"PlayerSetting"`
	}
	if err := json.Unmarshal([]byte(blob), &parsed); err != nil {
		return nil, false
	}
	return parsed.PlayerSetting, true
}


func (s *EnetServer) opGetCharacterSettings(sess *ClientSession, opCode byte, params map[byte]any) {
	settings := "{}"
	if ch, err := db.GetCharacterByID(sess.selectedCharID); err == nil && ch.Settings != "" {
		settings = ch.Settings
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{ParamAccountID: settings})
}

func (s *EnetServer) opSetCharacterSettings(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) > 0 {
		settings := fmt.Sprintf("%v", arr[0])
		if err := db.UpdateCharacterSettings(sess.selectedCharID, settings); err != nil {
			log.Printf("[PHOTON] SetCharacterSettings charID=%d error: %v", sess.selectedCharID, err)
		}
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}

func (s *EnetServer) opGetPresence(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	charID := int64(0)
	if len(arr) > 0 {
		charID = int64(asInt32(arr[0]))
	}
	if charID == 0 {
		charID = sess.selectedCharID
	}
	ch, _ := db.GetCharacterByID(charID)
	if ch == nil {
		ch = stubChar(charID, sess.dbAccountID)
	}

	townID := ch.LastTownPlaylistID
	if townID == 0 {
		townID = 28 // lvl_Tutorial_Part1
	}

	p := map[int]any{
		0:  int32(charID),
		1:  s.isCharOnline(charID),
		2:  charLastSeenUnix(ch),
		3:  int32(townID),
		4:  int32(ch.Level),
		5:  int32(ch.Experience),
		6:  int32(ch.Money),
		7:  int32(ch.ClassType),
		8:  sess.charAvatarParts,
		9:  sess.charAvatarMats,
		10: int32(sess.charAvatarID),
		11: ch.Name,
		13: int32(0),
		14: int32(0),
		15: int32(0),
		16: s.currentPlaylistForChar(charID),
	}
	addPvPPresenceFields(p, charID) // keys 17-22, see its own doc comment (lobby.go)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: p})
}


func (s *EnetServer) opUpdatePresence(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) >= 1 {
		sess.currentTown = asInt32(arr[0])
	}
	if len(arr) >= 2 {
		sess.playlistId = asInt32(arr[1])
	}
	log.Printf("[PHOTON] UpdatePresence user=%s currentTown=%d currentPlaylist=%d", sess.userID, sess.currentTown, sess.playlistId)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}


func (s *EnetServer) opGetAllFriendsPresence(sess *ClientSession, opCode byte, params map[byte]any) {
	ids, err := db.GetFriendCharIDs(sess.selectedCharID)
	if err != nil {
		log.Printf("[PHOTON] GetAllFriendsPresence error: %v", err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	resp := make(map[byte]any, len(ids))
	var idx byte
	for _, id := range ids {
		ch, err := db.GetCharacterByID(id)
		if err != nil {
			log.Printf("[PHOTON] GetCharacterByID error: %v", err)
			continue
		}
		resp[idx] = s.presenceHT(ch.CharID, ch, s.isCharOnline(ch.CharID), charLastSeenUnix(ch))
		idx++
	}
	s.sendOpResponse(sess, opCode, 0, resp)
}


func (s *EnetServer) opSearchCharacter(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	name := fmt.Sprintf("%v", arr[0])
	ch, err := db.GetCharacterByName(name)
	if err != nil {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: s.presenceHT(ch.CharID, ch, s.isCharOnline(ch.CharID), charLastSeenUnix(ch))})
}

func (s *EnetServer) opUpdateCharacterInfo(sess *ClientSession, opCode byte, params map[byte]any) {
	log.Printf("[PHOTON] UpdateCharacterInfo user=%s", sess.userID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}


func (s *EnetServer) opUpdateCharacterExp(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	charID := sess.selectedCharID
	exp := int32(0)
	if len(arr) > 0 {
		charID = int64(asInt32(arr[0]))
	}
	if len(arr) > 2 {
		exp = asInt32(arr[2])
	}
	level := levelForExp(int(exp))
	if err := db.UpdateCharacterExpLevel(charID, level, int(exp)); err != nil {
		log.Printf("[PHOTON] UpdateCharacterExp error: %v", err)
	}
	log.Printf("[PHOTON] UpdateCharacterExp charID=%d level=%d exp=%d", charID, level, exp)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})

}

func (s *EnetServer) opUpdateCharacterModel(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	charID := sess.selectedCharID
	avatarParts, avatarMats := AvatarDefaultParts, AvatarDefaultParts
	var avatarID uint32
	if len(arr) > 0 {
		charID = int64(asInt32(arr[0]))
	}
	if len(arr) > 1 {
		avatarParts = fmt.Sprintf("%v", arr[1])
	}
	if len(arr) > 2 {
		avatarMats = fmt.Sprintf("%v", arr[2])
	}
	if len(arr) > 3 {
		avatarID = uint32(asInt32(arr[3]))
	} else if ch, err := db.GetCharacterByID(charID); err == nil {
		avatarID = ch.AvatarID
	}
	avatarMats = clampAvatarMaterialsCSV(avatarID, avatarParts, avatarMats)
	if err := db.UpdateCharacterAvatar(charID, avatarParts, avatarMats, avatarID); err != nil {
		log.Printf("[PHOTON] UpdateCharacterModel error: %v", err)
	}
	log.Printf("[PHOTON] UpdateCharacterModel charID=%d avatarID=%d", charID, avatarID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}

func (s *EnetServer) opUpdateCharacterName(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	charID := sess.selectedCharID
	name := ""
	if len(arr) > 0 {
		charID = int64(asInt32(arr[0]))
	}
	if len(arr) > 1 {
		name = fmt.Sprintf("%v", arr[1])
	}
	if name != "" {
		if err := db.UpdateCharacterNameDB(charID, name); err != nil {
			log.Printf("[PHOTON] UpdateCharacterName error: %v", err)
			s.sendOpResponse(sess, opCode, -1, map[byte]any{})
			return
		}
	}
	log.Printf("[PHOTON] UpdateCharacterName charID=%d name=%q", charID, name)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}

func (s *EnetServer) opGetMyCurrencies(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	charID := sess.selectedCharID
	if len(arr) > 0 {
		charID = int64(asInt32(arr[0]))
	}
	ch, _ := db.GetCharacterByID(charID)
	money, eridium, seraph := int32(0), int32(0), int32(0)
	if ch != nil {
		money = int32(ch.Money)
		eridium = int32(ch.Eridium)
		seraph = int32(ch.SeraphCrystals)
	}
	log.Printf("[PHOTON] GetMyCurrencies charID=%d money=%d eridium=%d seraph=%d", charID, money, eridium, seraph)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{
		0: int32(charID),
		1: money,
		2: eridium,
		3: seraph,
	})
}

func investedSkillsArray(invested map[int]int32) TypedArray {
	items := make([]any, 0, len(invested))
	for id, grade := range invested {
		items = append(items, map[int]any{0: int32(id), 1: grade})
	}
	return TypedArray{ElementType: THash, Items: items}
}


func (s *EnetServer) opGetInvestedSkillPoints(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	charID := sess.selectedCharID
	if len(arr) > 0 {
		charID = int64(asInt32(arr[0]))
	}
	ch, _ := db.GetCharacterByID(charID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{
		0: int32(charID),
		1: investedSkillsArray(investedSkillPoints(ch)),
	})
}


func (s *EnetServer) opInvestSkillPoint(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	skillID := int32(0)
	if len(arr) > 0 {
		skillID = asInt32(arr[0])
	}
	charID := sess.selectedCharID
	ch, err := db.GetCharacterByID(charID)
	if err != nil || ch == nil {
		s.sendOpResponse(sess, opCode, 1, map[byte]any{})
		return
	}
	loadSkillCatalog()
	maxGrade, known := skillMaxGrade[int(skillID)]
	if !known || skillClassType[int(skillID)] != ch.ClassType {
		log.Printf("[PHOTON] InvestSkillPoint char=%d rejected: skill=%d unknown or wrong class", charID, skillID)
		s.sendOpResponse(sess, opCode, 1, map[byte]any{})
		return
	}
	invested := investedSkillPoints(ch)
	unspent := int32(earnedSkillPoints(int(ch.Level))) - totalInvestedSkillPoints(invested)
	if unspent <= 0 || invested[int(skillID)] >= int32(maxGrade) {
		log.Printf("[PHOTON] InvestSkillPoint char=%d rejected: skill=%d unspent=%d grade=%d/%d", charID, skillID, unspent, invested[int(skillID)], maxGrade)
		s.sendOpResponse(sess, opCode, 1, map[byte]any{})
		return
	}
	invested[int(skillID)]++
	if err := persistInvestedSkills(charID, invested); err != nil {
		log.Printf("[PHOTON] InvestSkillPoint persist error char=%d skill=%d: %v", charID, skillID, err)
		s.sendOpResponse(sess, opCode, 1, map[byte]any{})
		return
	}
	s.refreshSessionSkills(sess, charID, invested)
	log.Printf("[PHOTON] InvestSkillPoint char=%d skill=%d grade=%d", charID, skillID, invested[int(skillID)])
	s.sendOpResponse(sess, opCode, 0, map[byte]any{
		0: int32(charID),
		1: skillID,
	})
}


func (s *EnetServer) opGetSkillPoints(sess *ClientSession, opCode byte, params map[byte]any) {
	charID := sess.selectedCharID
	ch, err := db.GetCharacterByID(charID)
	if err != nil || ch == nil {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{
			0: int32(charID), 1: int32(0), 2: int32(0),
			3: investedSkillsArray(nil),
		})
		return
	}
	invested := investedSkillPoints(ch)
	earned := int32(earnedSkillPoints(int(ch.Level)))
	s.sendOpResponse(sess, opCode, 0, map[byte]any{
		0: int32(charID),
		1: earned,
		2: earned - totalInvestedSkillPoints(invested),
		3: investedSkillsArray(fullClassSkillPoints(ch)),
	})
}

func (s *EnetServer) opResetSkillPoints(sess *ClientSession, opCode byte, params map[byte]any) {
	charID := sess.selectedCharID
	if err := db.UpdateCharacterSkills(charID, "{}"); err != nil {
		log.Printf("[PHOTON] ResetSkillPoints error char=%d: %v", charID, err)
		s.sendOpResponse(sess, opCode, 1, map[byte]any{})
		return
	}
	s.refreshSessionSkills(sess, charID, map[int]int32{})
	log.Printf("[PHOTON] ResetSkillPoints char=%d", charID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(charID)})
}


func (s *EnetServer) refreshSessionSkills(sess *ClientSession, charID int64, invested map[int]int32) {
	if sess.hostChar == nil || sess.hostChar.CharID != charID {
		return
	}
	raw := make(map[string]int32, len(invested))
	for id, grade := range invested {
		raw[strconv.Itoa(id)] = grade
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return
	}
	sess.hostChar.Skills = string(b)

	if sess.roomName == "" || sess.avatarBase == 0 {
		return // Town/menu/not spawned yet: nothing cached to refresh
	}
	room := s.rooms[sess.roomName]
	if room == nil {
		return
	}

	
	newCap := grenadeMaxFor(sess.hostChar)
	if newCap != sess.grenadeCap {
		sess.grenadeCap = newCap
		if sess.grenadeCount > sess.grenadeCap {
			sess.grenadeCount = sess.grenadeCap
		}
		s.hostSendGrenadeCount(room, sess)
	}

	if newMaxHP := maxLifeForCharacter(sess.hostChar); newMaxHP != sess.maxHP && sess.maxHP > 0 {
		frac := float64(sess.hp) / float64(sess.maxHP)
		sess.maxHP = newMaxHP
		sess.hp = int32(float64(sess.maxHP) * frac)
		if sess.hp > sess.maxHP {
			sess.hp = sess.maxHP
		}
	}

	if sh, err := db.GetEquippedSingle(sess.hostChar.CharID, domain.ItemTypeShield); err == nil && sh != nil {
		if newMaxShield := int32(float64(realMaxShield(sh.PartB, sh.Level)) * (1 + maxShieldScale(sess.hostChar))); newMaxShield != sess.maxShield {
			if sess.maxShield > 0 {
				frac := float64(sess.shield) / float64(sess.maxShield)
				sess.shield = int32(float64(newMaxShield) * frac)
			} else {
				sess.shield = newMaxShield
			}
			sess.maxShield = newMaxShield
			if sess.shield > sess.maxShield {
				sess.shield = sess.maxShield
			}
			sess.shieldRegenBaseline = sess.shield
		}
	}
	s.hostSendRPC(room, sess.avatarBase, "RPC_RequestAllLifeAndShield", []any{
		map[int]any{0: int32(1), 1: sess.avatarBase, 2: playerLifeTable(sess.hp, sess.maxHP, sess.shield, sess.maxShield)},
	})

	if len(sess.equippedWeapons) > 0 {
		items := equippedWeaponsOrdered(sess.hostChar.CharID)
		scale := 1 + weaponDamageScale(sess.hostChar)
		for i, slot := range sess.equippedWeapons {
			if i >= len(items) || items[i] == nil || room.weaponDamage == nil {
				continue
			}
			room.weaponDamage[slot.view] = resolveWeaponDamage(items[i]) * scale
			setWeaponFireRate(room, slot.view, items[i])
		}
	}
}

func (s *EnetServer) opGetAntiLevel(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	accountID := int32(sess.accountID)
	if len(arr) > 0 {
		accountID = asInt32(arr[0])
	}
	log.Printf("[PHOTON] GetAntiLevel accountID=%d", accountID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{
		0: accountID,
		1: int32(0),
	})
}

func (s *EnetServer) opGetAchievements(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	charID := sess.selectedCharID
	if len(arr) > 0 {
		charID = int64(asInt32(arr[0]))
	}
	stored, err := db.GetAchievementProgress(charID)
	if err != nil {
		log.Printf("[PHOTON] GetAchievementProgress char=%d: %v", charID, err)
		stored = map[int]domain.AchievementProgress{}
	}
	ch, _ := db.GetCharacterByID(charID)

	defs := allAchievementDefs()
	items := make([]any, 0, len(defs))
	for _, def := range defs {
		stage, progress, date := 0, 0, int32(0)
		if def.ID == achIDCharacterLevel && ch != nil {
			progress = ch.Level
			stage = achievementStageForProgress(def, progress)
		} else if p, ok := stored[def.ID]; ok {
			stage, progress, date = p.Stage, p.Progress, int32(p.UpdatedAt)
		}
		items = append(items, map[int]any{0: int32(def.ID), 1: int32(stage), 2: int32(progress), 3: date})
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{
		0: int32(charID),
		1: TypedArray{ElementType: THash, Items: items},
	})
}

func rankHashtable(c *domain.Character, rank int32) map[int]any {
	guildID, _ := db.GetAccountGuildID(c.AccountID)
	guildName := ""
	if guildID != 0 {
		if g, err := db.GetGuild(guildID); err == nil {
			guildName = g.Name
		}
	}
	st, err := db.GetPvPStats(c.CharID)
	rating, kills := int32(1000), int32(0)
	if err == nil {
		kills = int32(st.Kills)
		if st.Rating != 0 {
			rating = int32(st.Rating)
		}
	}
	return map[int]any{
		0: int32(c.CharID),
		1: rank,
		2: int32(c.Level),
		3: c.Name,
		4: int32(guildID),
		5: guildName,
		6: kills * 10000,
		7: rating * 10000,
	}
}

func (s *EnetServer) opGetRanking(sess *ClientSession, opCode byte, params map[byte]any) {
	top, err := db.GetTopPvPCharacters(20)
	if err != nil {
		log.Printf("[PHOTON] GetRanking error: %v", err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	resp := map[byte]any{}
	if self, err := db.GetCharacterByID(sess.selectedCharID); err == nil {
		rank, err := db.GetCharacterPvPRank(self)
		if err != nil {
			rank = int32(len(top) + 1)
		}
		resp[0] = rankHashtable(self, rank)
	}
	for i, c := range top {
		resp[byte(i+1)] = rankHashtable(c, int32(i+1))
	}
	log.Printf("[PHOTON] GetRanking user=%s → %d entries", sess.userID, len(top))
	s.sendOpResponse(sess, opCode, 0, resp)
}

func charToHashtable(ch *domain.Character, invHT map[int]any) map[int]any {
	avatarType := ch.ClassType
	avatarID := int(AvatarDefaultBkID[avatarType])
	if ch.AvatarID != 0 {
		avatarID = int(ch.AvatarID)
	}
	avatarParts := ch.AvatarParts
	if avatarParts == "" || !strings.Contains(avatarParts, ",") {
		avatarParts = AvatarDefaultParts
	}
	avatarMats := ch.AvatarMaterials
	if avatarMats == "" || !strings.Contains(avatarMats, ",") {
		avatarMats = AvatarDefaultParts
	}

	if invHT == nil {
		invHT = emptyInvHT(ch.CharID)
	}

	instancesHT := map[int]any{}
	for id, difficulty := range unlockedInstancesForChar(ch.CharID, int(ch.Level)) {
		instancesHT[id] = difficulty
	}

	investedSkills := investedSkillPoints(ch)
	skillsHT := map[int]any{}
	for id, grade := range investedSkills {
		skillsHT[id] = grade
	}
	earnedSkills := int32(earnedSkillPoints(int(ch.Level)))

	return map[int]any{
		0: int32(ch.CharID),
		1: int32(ch.AccountID),
		2: ch.Name,
		3: int32(ch.Experience),
		4: int32(ch.Level),
		5: int32(avatarType),
		6: avatarParts,
		7: avatarMats,
		8: avatarID,
		9: invHT,
		10: map[int]any{0: int32(ch.CharID), 1: int32(ch.Money), 2: int32(ch.Eridium), 3: int32(ch.SeraphCrystals), 4: map[int]any{}, 5: luckyKeyExpiringHashtable(ch.CharID)},
		11: map[int]any{},
		12: instancesHT, // Character.Instances zones the client may travel to
		13: map[int]any{0: int32(ch.CharID), 1: earnedSkills, 2: earnedSkills - totalInvestedSkillPoints(investedSkills), 3: skillsHT},
		14: int32(ch.PlayTime),
		15: int32(0),
		16: int32(0),          // Tier
		17: int32(0),          // Grade
		18: currentEnergy(ch), // Energy
	}
}

// isCharOnline reports whether charID currently has an active session with
// that character selected. it's the signal behind Presence.mIsOnline
func (s *EnetServer) isCharOnline(charID int64) bool {
	for _, other := range s.sessions {
		if other.selectedCharID == charID {
			return true
		}
	}
	return false
}

func charLastSeenUnix(ch *domain.Character) int32 {
	t, err := time.Parse("2006-01-02 15:04:05", ch.LastSeen)
	if err != nil {
		return 0
	}
	return int32(t.Unix())
}

func (s *EnetServer) presenceHT(charID int64, ch *domain.Character, online bool, lastSeen int32) map[int]any {
	avatarType := ch.ClassType
	avatarID := int(AvatarDefaultBkID[avatarType])
	if ch.AvatarID != 0 {
		avatarID = int(ch.AvatarID)
	}
	avatarParts := ch.AvatarParts
	if avatarParts == "" || !strings.Contains(avatarParts, ",") {
		avatarParts = AvatarDefaultParts
	}
	avatarMats := ch.AvatarMaterials
	if avatarMats == "" || !strings.Contains(avatarMats, ",") {
		avatarMats = AvatarDefaultParts
	}
	ht := map[int]any{
		0:  int32(ch.CharID),
		1:  online,
		2:  lastSeen,
		3:  int32(0),
		4:  int32(ch.Level),
		5:  int32(ch.Experience),
		6:  int32(ch.Money),
		7:  int32(avatarType),
		8:  avatarParts,
		9:  avatarMats,
		10: avatarID,
		11: ch.Name,
		13: int32(0),
		14: int32(0),
		15: int32(0),
		16: s.currentPlaylistForChar(charID),
	}
	addPvPPresenceFields(ht, charID)
	return ht
}

func (s *EnetServer) broadcastPresenceUpdate(charID int64) {
	ch, err := db.GetCharacterByID(charID)
	if err != nil {
		return
	}
	ht := s.presenceHT(charID, ch, s.isCharOnline(charID), charLastSeenUnix(ch))
	friendIDs, err := db.GetFriendCharIDs(charID)
	if err != nil {
		return
	}
	for _, friendID := range friendIDs {
		for _, other := range s.sessions {
			if other.selectedCharID == friendID || (other.hostChar != nil && other.hostChar.CharID == friendID) {
				s.sendEventTo(other, EvPresenceUpdated, map[byte]any{0: ht})
			}
		}
	}
}

func (s *EnetServer) currentPlaylistForChar(charID int64) int32 {
	for _, other := range s.sessions {
		matches := other.selectedCharID == charID || (other.hostChar != nil && other.hostChar.CharID == charID)
		if !matches || other.roomName == "" {
			continue
		}
		room := s.rooms[other.roomName]
		if room == nil {
			continue
		}
		pid, ok := room.Properties["PlaylistId"]
		if !ok {
			continue
		}
		id := asInt32(pid)
		if playlistInfo(id) == nil {
			return defaultTownPlaylistID
		}
		return id
	}
	return -1
}

// cacheCharInSession stores character fields in the session for use in lobby presence serialization.
func cacheCharInSession(sess *ClientSession, ch *domain.Character) {
	avatarID := AvatarDefaultBkID[ch.ClassType]
	if ch.AvatarID != 0 {
		avatarID = ch.AvatarID
	}
	avatarParts := ch.AvatarParts
	if avatarParts == "" || !strings.Contains(avatarParts, ",") {
		avatarParts = AvatarDefaultParts
	}
	avatarMats := ch.AvatarMaterials
	if avatarMats == "" || !strings.Contains(avatarMats, ",") {
		avatarMats = AvatarDefaultParts
	}
	sess.charName = ch.Name
	sess.charLevel = int32(ch.Level)
	sess.charExp = int32(ch.Experience)
	sess.charCurrency = int32(ch.Money)
	sess.charClassType = int32(ch.ClassType)
	sess.charAvatarParts = avatarParts
	sess.charAvatarMats = avatarMats
	sess.charAvatarID = int32(avatarID)
}

func stubChar(charID, accountID int64) *domain.Character {
	return &domain.Character{
		CharID: charID, AccountID: accountID,
		Name: "Unknown", Level: 1, Experience: 0,
		AvatarParts: AvatarDefaultParts, AvatarMaterials: AvatarDefaultParts,
	}
}

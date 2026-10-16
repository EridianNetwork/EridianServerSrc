package photon

import (
	"log"
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"

	"eridian/internal/db"
)

const lootSpawnDelay = 100 * time.Millisecond

func (s *EnetServer) hostRunPendingLootSpawns(room *Room) {
	if len(room.pendingLootSpawns) == 0 {
		return
	}
	now := time.Now()
	remaining := room.pendingLootSpawns[:0]
	for _, p := range room.pendingLootSpawns {
		if now.Before(p.at) {
			remaining = append(remaining, p)
			continue
		}
		s.hostSpawnLoot(room, p.sess, p.level, p.pid, p.chestPos)
	}
	room.pendingLootSpawns = remaining
}

type lootEntry struct {
	Pid            int64  `json:"pid"`
	GO             string `json:"go"`
	LootingType    int    `json:"lootingType"`
	LootingSubType int    `json:"lootingSubType"`
	WeaponSlots    int    `json:"weaponSlots"`
	ItemSlots      int    `json:"itemSlots"`
}

var (
	levelLootOnce sync.Once
	levelLoot     map[string][]lootEntry
)

func levelLootEntries(level string) []lootEntry {
	levelLootOnce.Do(func() { loadLowercaseJSON("data/level_loot.json", &levelLoot) })
	return levelLoot[strings.ToLower(level)]
}

func lootEntryForPid(level string, pid int64) *lootEntry {
	entries := levelLootEntries(level)
	for i := range entries {
		if entries[i].Pid == pid {
			return &entries[i]
		}
	}
	return nil
}


func (s *EnetServer) hostSpawnLoot(room *Room, sess *ClientSession, level string, pid int64, chestPos []float32) {
	e := lootEntryForPid(level, pid)
	lootingType, lootingSubType := 0, 0
	if e != nil {
		lootingType, lootingSubType = e.LootingType, e.LootingSubType
	}
	charLevel := 1
	if sess.hostChar != nil && sess.hostChar.Level > 0 {
		charLevel = sess.hostChar.Level
	}
	chestID := int32(-1)
	if idx, ok := levelObjectIndex(level, pid); ok {
		chestID = int32(idx)
	}
	weaponSlots, itemSlots := 0, 0
	if e != nil && chestID >= 0 {
		weaponSlots, itemSlots = e.WeaponSlots, e.ItemSlots
	}
	// the weapons nolycome from chests that have weapon slots; the bonus weapon is rolled only there too.
	drops := rollLoot(lootingType, lootingSubType, charLevel)
	if weaponSlots > 0 {
		drops = rollBonusWeapon(drops)
	}
	kept := drops[:0]
	weaponDrops := 0
	for _, d := range drops {
		if d.ItemType == "Weapon" {
			if weaponDrops >= weaponSlots {
				continue
			}
			weaponDrops++
		}
		kept = append(kept, d)
	}
	drops = kept
	if len(drops) == 0 {
		log.Printf("[LOOT] nothing to drop for lootingType=%d subType=%d level=%d room=%s", lootingType, lootingSubType, charLevel, room.Name)
		return
	}
	band, hardOverride := lootRarityBand(lootingType)
	totalWeapons, totalItems := 0, 0
	for _, d := range drops {
		if d.ItemType == "Weapon" {
			totalWeapons++
		} else {
			totalItems++
		}
	}
	spawned, free := 0, 0
	weaponsSpawned, itemsSpawned := 0, 0
	for _, d := range drops {
		prefab, dna, item, reward, ok := lootDropPayload(d, charLevel, band, hardOverride)
		if !ok {
			continue
		}
		attachID, numWeapons, numItems := int32(-1), int32(0), int32(0)
		if d.ItemType == "Weapon" {
			if weaponsSpawned < weaponSlots {
				attachID, numWeapons = chestID, int32(min(totalWeapons, weaponSlots))
			}
			weaponsSpawned++
		} else {
			if itemsSpawned < itemSlots {
				attachID, numItems = chestID, int32(min(totalItems, itemSlots))
			}
			itemsSpawned++
		}
		pos := chestPos
		if attachID == -1 {
			pos = lootDropPos(chestPos, free)
			free++
		}
		s.hostSpawnPickableDrop(room, sess, pos, prefab, dna, item, reward, attachID, numWeapons, numItems)
		spawned++
	}
	log.Printf("[LOOT] rolled %d drop(s), spawned %d lootingType=%d subType=%d level=%d room=%s", len(drops), spawned, lootingType, lootingSubType, charLevel, room.Name)
}


const lootDropRadius = 0.9


func lootDropPos(chestPos []float32, index int) []float32 {
	if index == 0 || len(chestPos) != 3 {
		return chestPos
	}
	angle := float64(index) * 2.0 * math.Pi / 6.0
	return []float32{
		chestPos[0] + float32(math.Cos(angle))*lootDropRadius,
		chestPos[1],
		chestPos[2] + float32(math.Sin(angle))*lootDropRadius,
	}
}

const healthPackFillFraction = 0.3

func (s *EnetServer) hostApplyDropReward(room *Room, sess *ClientSession, r dropReward) {
	switch r.kind {
	case "Money":
		if sess.hostChar == nil {
			return
		}
		if err := db.AddCharacterMoney(sess.hostChar.CharID, int(r.quantity)); err != nil {
			log.Printf("[LOOT] AddCharacterMoney char=%d: %v", sess.hostChar.CharID, err)
			return
		}
		if online := s.sessionOnline(sess.userID); online != nil {
			s.sendCurrencyUpdate(online, sess.hostChar.CharID)
		}
		log.Printf("[LOOT] user=%s picked up $%d", sess.userID, r.quantity)
	case "HealthPack":
		if sess.avatarBase == 0 || sess.isDead {
			return
		}
		maxLife := sess.maxHP
		if maxLife <= 0 && sess.hostChar != nil {
			maxLife = maxLifeForCharacter(sess.hostChar)
		}
		if maxLife <= 0 {
			return
		}
		if sess.hp >= maxLife {
			return
		}
		heal := r.quantity
		if cap := int32(float64(maxLife) * healthPackFillFraction); heal > cap {
			heal = cap
		}
		newHP := sess.hp + heal
		if newHP > maxLife {
			newHP = maxLife
		}
		sess.hp = newHP
		s.hostSendRPC(room, sess.avatarBase, "RPC_RequestAllLifeAndShield", []any{
			map[int]any{0: int32(1), 1: sess.avatarBase, 2: playerLifeTable(sess.hp, maxLife, sess.shield, sess.maxShield)},
		})
		log.Printf("[LOOT] user=%s healed +%d (hp=%d/%d)", sess.userID, heal, sess.hp, maxLife)
	case "AmmoPack":
		if sess.avatarBase == 0 {
			return
		}
		if sess.ammoEst >= 360 {
			return
		}
		sess.ammoEst = 360
		s.hostSendRPC(room, sess.avatarBase+3, "RPC_OnReceiveAmmos", []any{ammoPouchesForSession(sess, true)})
		log.Printf("[LOOT] user=%s picked up ammo refill", sess.userID)
	case "GrenadeAmmo":
		if sess.avatarBase == 0 {
			return
		}
		if sess.grenadeCount >= sess.grenadeCap {
			return
		}
		sess.grenadeCount += r.quantity
		if sess.grenadeCount > sess.grenadeCap {
			sess.grenadeCount = sess.grenadeCap
		}
		s.hostSendGrenadeCount(room, sess)
		log.Printf("[LOOT] user=%s picked up grenade ammo, now %d/%d", sess.userID, sess.grenadeCount, sess.grenadeCap)
	}
}

const (
	npcAmmoDropChance       = 0.4
	npcAmmoDropChanceBadass = 0.8
)

var npcWeaponAmmoType = map[string]string{
	"SMG": "SMG", "ShotGun": "ShotGun", "SniperRifle": "SniperRifle",
	"RepeaterPistol": "Pistol", "CombatRifle": "AssaultRifle",
}

func (s *EnetServer) hostDropNpcAmmo(room *Room, npc *spawnedNpc, killer *ClientSession) {
	if npc.friendly || npc.kit == "SoldierTurretBasic" {
		return
	}
	w := npcWeaponFor(npc.kit)
	if w == nil {
		return
	}
	ammo, ok := npcWeaponAmmoType[w.Type]
	if !ok {
		return
	}
	chance := npcAmmoDropChance
	if bossKits[npc.kit] || isBadassKit(npc.kit) {
		chance = npcAmmoDropChanceBadass
	}
	if rand.Float64() >= chance {
		return
	}
	prefab, dna, item, reward, ok := lootDropPayload(rolledLoot{ItemType: "Ammo", AmmoType: ammo}, 1, rarityBand{}, false)
	if !ok {
		return
	}
	s.hostSpawnPickableDrop(room, killer, []float32{npc.pos[0], npc.pos[1], npc.pos[2]}, prefab, dna, item, reward, -1, 0, 0)
	log.Printf("[LOOT] npc %d (%s) dropped %s ammo room=%s", npc.viewBase, npc.kit, ammo, room.Name)
}

const (
	autoPickupRadius = 2.0
	autoPickupHeight = 2.5
	autoPickupDelay  = 2500 * time.Millisecond
)

func (s *EnetServer) dropNeededBy(sess *ClientSession, r dropReward) bool {
	switch r.kind {
	case "HealthPack":
		maxLife := sess.maxHP
		if maxLife <= 0 && sess.hostChar != nil {
			maxLife = maxLifeForCharacter(sess.hostChar)
		}
		return maxLife > 0 && sess.hp < maxLife
	case "AmmoPack":
		return sess.ammoEst < 360
	case "GrenadeAmmo":
		return sess.grenadeCount < sess.grenadeCap
	}
	return false
}

const pickedDropLinger = 500 * time.Millisecond

type pickedDrop struct {
	view int32
	at   time.Time
}

func (s *EnetServer) hostFlushPickedDrops(room *Room) {
	if len(room.pickedDrops) == 0 {
		return
	}
	now := time.Now()
	kept := room.pickedDrops[:0]
	for _, d := range room.pickedDrops {
		if now.Before(d.at) {
			kept = append(kept, d)
			continue
		}
		s.hostBroadcastJournaled(room, 204, map[byte]any{
			ParamPlayerNr:  HostActorNr,
			ParamEventData: map[any]any{byte(0): d.view},
		}, true)
		delete(room.dropPos, d.view)
	}
	room.pickedDrops = kept
}

func (s *EnetServer) hostTickAutoPickups(room *Room) {
	s.hostFlushPickedDrops(room)
	if len(room.dropReward) == 0 {
		return
	}
	for view, r := range room.dropReward {
		if r.kind != "HealthPack" && r.kind != "AmmoPack" && r.kind != "GrenadeAmmo" {
			continue
		}
		dp, ok := room.dropPos[view]
		if !ok {
			continue
		}
		if at, ok := room.dropSpawnedAt[view]; !ok || time.Since(at) < autoPickupDelay {
			continue
		}
		for _, uid := range room.Players {
			sess := s.sessionInRoom(uid, room.Name)
			if sess == nil || sess.avatarBase == 0 || sess.isDead || !s.dropNeededBy(sess, r) {
				continue
			}
			pos, ok := playerStreamPos(room, sess.avatarBase)
			if !ok {
				pos = room.playerPos[sess.avatarBase+1]
			}
			dx, dz, dy := pos[0]-dp[0], pos[2]-dp[2], pos[1]-dp[1]
			if dx*dx+dz*dz > autoPickupRadius*autoPickupRadius || dy > autoPickupHeight || dy < -autoPickupHeight {
				continue
			}
			s.hostSendRPC(room, interactionManagerViewID, "RPC_H2C_OnPickUpItem", []any{sess.avatarBase, view})
			delete(room.dropReward, view)
			delete(room.dropSpawnedAt, view)
			room.pickedDrops = append(room.pickedDrops, pickedDrop{view: view, at: time.Now().Add(pickedDropLinger)})
			s.hostApplyDropReward(room, sess, r)
			s.hostClusterPickup(room)
			break
		}
	}
}

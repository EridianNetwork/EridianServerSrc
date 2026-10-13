package photon

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"eridian/internal/db"
	"eridian/internal/domain"

	lua "github.com/yuin/gopher-lua"
)

const (
	PlayerStateNone byte = iota
	PlayerStatePlayerReady
	PlayerStateMapReady
	PlayerStatePlayerAndMapReady
	PlayerStateSpawning
	PlayerStateSpawned
)

var validGamemodes = []string{
	"GameModeTown",
	"GamemodePvE",
	"GameModeSurvival",
	"GameModePvP",
	"GamemodeTeamDeathmatch",
	"GamemodeTutorial",
	"GamemodeClaptrapSoccer",
}

const hostViewBase = HostActorNr * 100000

const sessionInfoViewID = hostViewBase + 1

func playerViewID(actorNr int32) int32 { return hostViewBase + 6 + actorNr }

const lomViewID = hostViewBase + 900

const (
	playerActionMove            = 1
	playerActionRun             = 2
	playerActionJump            = 4
	playerActionCrouch          = 8
	playerActionMelee           = 16
	playerActionInteract        = 32
	playerActionSwitchWeapon    = 64
	playerActionFire            = 128
	playerActionReloadWeapon    = 256
	playerActionAim             = 512
	playerActionLaunchSkill     = 1024
	playerActionThrowGrenade    = 2048
	playerActionAccessInventory = 4096

	allPlayerActions = playerActionMove | playerActionRun | playerActionJump | playerActionCrouch |
		playerActionMelee | playerActionInteract | playerActionSwitchWeapon | playerActionFire |
		playerActionReloadWeapon | playerActionAim | playerActionLaunchSkill | playerActionThrowGrenade |
		playerActionAccessInventory
)

const actionTypeLevelTriggerInteraction = 12

const actionTypeEntityPickInteraction = 4

const actionTypeThrow = 22

const actionTypeEquipWeapon = 8

const actionTypeFire = 9

const (
	actionTypeSecondWindQuickEnd = 16
	actionTypeSecondWindRevive   = 17
)

const actionTypeSkill = 14

const actionTypeMelee = 13

const (
	playerMeleeRange   = 3.2
	playerMeleeConeCos = 0.35
)

const actionTypeSwitchWeapon = 21

const bhvTaskPlayerSkill = 69

const interactionManagerViewID = hostViewBase + 2

func (s *EnetServer) handleHostRPC(sess *ClientSession, room *Room, actorNr int32, ht map[any]any) bool {
	name, _ := ht[byte(3)].(string)
	args, _ := ht[byte(4)].([]any)
	if !suppressRPCLog(name) {
		log.Printf("[HOST] RPC %q from actor=%d args=%v", name, actorNr, args)
	}

	if strings.HasPrefix(name, "RPC_RequestPickUp") {
		s.hostClusterPickup(room)
	}

	if name == "RPC_RequestPickUpWeapon" && len(args) > 0 {
		wid := asInt32(args[0])
		if !slices.Contains(sess.pickedWeapons, wid) && len(sess.pickedWeapons) < 3 {
			sess.pickedWeapons = append(sess.pickedWeapons, wid)
			log.Printf("[HOST] actor=%d picked up weapon view=%d", actorNr, wid)
		}
	}

	if name == "RPC_RequestPickUpAndSwitchWeapon" && len(args) >= 2 {
		pickID, dropID := asInt32(args[0]), asInt32(args[1])
		if i := slices.Index(sess.pickedWeapons, dropID); i >= 0 {
			sess.pickedWeapons[i] = pickID
			log.Printf("[HOST] actor=%d swapped weapon view=%d -> %d", actorNr, dropID, pickID)
		} else if !slices.Contains(sess.pickedWeapons, pickID) && len(sess.pickedWeapons) < 3 {
			sess.pickedWeapons = append(sess.pickedWeapons, pickID)
			log.Printf("[HOST] actor=%d picked up weapon (switch) view=%d", actorNr, pickID)
		}
	}

	if name == "RPC_RequestSetWeaponSlots" && len(args) >= 5 {
		sess.weaponSlotDNAs = args[:4]
		sess.activeWeaponSlot = byte(asInt32(args[4]))
	}

	if (name == "RPC_SA" || name == "RPC_SI" || name == "RPC_SO") && len(args) >= 2 && sess.avatarBase != 0 {
		wid := asInt32(args[1])
		if wid == sess.avatarBase+5 {
			sess.activeWeaponSlot = 0
		} else if i := slices.Index(sess.pickedWeapons, wid); i >= 0 {
			if gamemodeName(room) == "GamemodeTutorial" {
				sess.activeWeaponSlot = byte(i)
			} else {
				sess.activeWeaponSlot = byte(i + 1)
			}
		}
	}

	switch name {
	case "RPC_RequestPlayerReady":
		if sess.playerState >= PlayerStatePlayerReady {
			return true
		}
		charID := int64(0)
		if len(args) > 0 {
			charID = int64(asInt32(args[0]))
		}
		ch, err := db.GetCharacterByID(charID)
		if err != nil || ch == nil {
			log.Printf("[HOST] RequestPlayerReady: character %d not found: %v", charID, err)
			return true
		}
		sess.hostChar = ch
		sess.playerState = PlayerStatePlayerReady
		s.broadcastPresenceUpdate(charID)
		s.hostSpawnPlayer(room, sess, actorNr)
		alreadySpawned := room.lomSpawned
		s.hostSpawnLevelObjectManager(room)
		if alreadySpawned {
			s.sendEventTo(sess, 202, map[byte]any{
				ParamPlayerNr: HostActorNr,
				ParamEventData: map[any]any{
					byte(0): "LevelObjectManager",
					byte(4): IntArray{Items: []int32{lomViewID}},
					byte(6): int32(msNow()),
					byte(7): lomViewID,
				},
			})
		}
		s.hostSendSerialize(room)
	case "RPC_RequestMapReady":
		if sess.playerState >= PlayerStatePlayerAndMapReady {
			return true
		}
		sess.playerState = PlayerStatePlayerAndMapReady
		s.hostSendSerialize(room)
		s.hostReplayRoomState(room, sess, actorNr)
		if room.mtActive {
			s.hostSyncMissionTimer(room, sess)
		}
		s.hostStartClusters(room)
		s.hostSpawnNpcs(room)
		s.scriptEnsureLoaded(room)
		room.pendingAvatarSpawns = append(room.pendingAvatarSpawns, pendingAvatarSpawn{
			sess: sess, actorNr: actorNr, at: time.Now().Add(300 * time.Millisecond),
		})
	case "RPC_RequestRespawnPlayer":
		s.hostSpawnAvatar(room, sess, actorNr, nil)
		sess.playerState = PlayerStateSpawned
		s.hostSendSerialize(room)
	case "RPC_SetChargeShot":
		log.Printf("[HOST] actor=%d charge shot armed (RPC_SetChargeShot, no bonus applied, no verified design data for the bonus amount)", actorNr)
	case "RPC_ReportRaycastHit":
		sess.raycastPatched = true
		if len(args) >= 2 {
			wid := asInt32(args[0])
			weaponView := asInt32(ht[byte(0)])
			if s.hostThrottleRaycastDamage(sess, room.weaponType[weaponView]) {
				element := room.weaponElement[weaponView]
				if wid != 0 {
					strength := int32(-1)
					if len(args) >= 3 {
						strength = asInt32(args[2])
					}
					weakPointIndex := int32(0)
					if len(args) >= 4 {
						weakPointIndex = asInt32(args[3])
					}
					baseDamage := room.weaponDamage[weaponView]
					if baseDamage <= 0 {
						baseDamage = fireBurstDamage
					}
					wpMult := weakpointMultiplier(strength)
					if wpMult > 1 {
						wpMult *= 1 + criticalHitDamageScale(sess.hostChar)
					}
					damage := int32(baseDamage * wpMult * (1 + berserkDespBonus(sess)))
					if !s.hostDamageNpcByView(room, sess.avatarBase, wid, damage, weakPointIndex, element, false) {
						s.hostDamageStaticByView(room, sess.avatarBase, wid, damage, element)
					}
				} else if isPvPCombatMode(asInt32(room.Properties["GameMode"])) && sess.avatarBase != 0 {
					baseDamage := room.weaponDamage[weaponView]
					if baseDamage <= 0 {
						baseDamage = fireBurstDamage
					}
					pos, ok := playerStreamPos(room, sess.avatarBase)
					if !ok {
						pos = room.playerPos[sess.avatarBase+1]
					}
					s.hostDamageNearestPlayer(room, sess, pos, sess.lastYawDeg, int32(baseDamage), element)
				}
			}
		}
	case "RPC_RequestPlayerWrapup":
		log.Printf("[HOST] player ready for wrapup (portal used) user=%s room=%s", sess.userID, room.Name)
		s.hostSendWrapupDestination(room, sess)
	case "RPC_RequestPlayerForGame":
	case "RPC_SI", "RPC_SI_P", "RPC_SO", "RPC_SA", "RPC_SA_P":
		s.broadcastEvent(room, 200, map[byte]any{
			ParamPlayerNr:  actorNr,
			ParamEventData: ht,
		}, nil, nil)
		if (name == "RPC_SA" || name == "RPC_SO") && len(args) > 0 {
			packed := asInt32(args[0])
			if packed%1000 == actionTypeEntityPickInteraction {
				weaponView := packed / 1000
				if p, ok := room.dropPos[weaponView]; ok && sess.avatarBase != 0 {
					room.playerPos[sess.avatarBase+1] = p
					log.Printf("[HOST] player estimate re-anchored to weapon drop view=%d [%.1f %.1f %.1f]", weaponView, p[0], p[1], p[2])
				}
				s.hostSendRPC(room, interactionManagerViewID, "RPC_H2C_OnPickUpItem", []any{sess.avatarBase, weaponView})
				isWeapon := true
				isDrop := false
				if it, ok := room.dropItem[weaponView]; ok && it.ItemKind != domain.ItemTypeWeapon {
					isWeapon = false
				}
				if reward, ok := room.dropReward[weaponView]; ok {
					isWeapon = false
					isDrop = true
					delete(room.dropReward, weaponView)
					s.hostApplyDropReward(room, sess, reward)
				}

				backpackSlot := -1
				if !isDrop {
					backpackSlot = s.hostPersistPickup(room, sess, weaponView)
				}
				if isWeapon && len(sess.equippedWeapons) < 4 && backpackSlot >= 0 {
					s.hostEquipWeaponFromBackpack(room, sess, byte(backpackSlot), byte(len(sess.equippedWeapons)))
					log.Printf("[HOST] actor=%d picked up weapon drop=%d backpackSlot=%d equipSlot=%d", actorNr, weaponView, backpackSlot, len(sess.equippedWeapons))
				}
				s.hostClusterPickup(room)
			}
			if packed == actionTypeThrow && name == "RPC_SA" {
				s.hostThrowGrenade(room, sess)
			}
			if packed%1000 == actionTypeFire {
				var weaponView int32
				if len(args) > 1 {
					weaponView = asInt32(args[1])
				}
				s.hostTrackFire(room, sess, weaponView)
			}
			if packed%1000 == actionTypeSecondWindRevive {
				targetView := packed / 1000
				if target := sessionByAvatarBase(s, room, targetView); target != nil {
					if !target.isDead {
						log.Printf("[HOST] SecondWindRevive no-op: target view=%d (actor=%d) is not isDead room=%s", targetView, actorNr, room.Name)
					}
					s.hostRevivePlayer(room, target, false)
				} else {
					log.Printf("[HOST] SecondWindRevive: no session found for target view=%d (actor=%d) room=%s", targetView, actorNr, room.Name)
				}
			}

			if packed%1000 == actionTypeSecondWindQuickEnd {
				if target := sessionByAvatarBase(s, room, packed/1000); target != nil && target.isDead {
					log.Printf("[HOST] actor view=%d gave up (SecondWindQuickEnd), forcing respawn room=%s", target.avatarBase, room.Name)
					s.hostSpawnAvatar(room, target, room.ActorNrFor(target.userID), nil)
				}
			}

			if name == "RPC_SA" && packed%1000 == actionTypeSkill && sess.avatarBase != 0 {
				s.hostTriggerPlayerSkill(room, sess)
			}
		}
		if name == "RPC_SI" && len(args) > 0 {
			packed := asInt32(args[0])

			if packed%1000 == actionTypeMelee && sess.avatarBase != 0 {
				s.hostPlayerMelee(room, sess)
			}
			if packed%1000 == actionTypeLevelTriggerInteraction {
				id := int(packed / 1000)
				newlyOpened := s.hostOpenLevelObject(room, id)
				s.hostClusterLevelTrigger(room, id)
				objs := levelObjectEntries(getCurScnFromProps(room.Name, room.Properties))
				if id >= 0 && id < len(objs) {

					if p := objs[id].Pos; len(p) == 3 && sess.avatarBase != 0 {
						room.playerPos[sess.avatarBase+1] = [3]float32{p[0], p[1], p[2]}
						log.Printf("[HOST] player estimate re-anchored to %q [%.1f %.1f %.1f]", objs[id].GO, p[0], p[1], p[2])
					}

					if newlyOpened && objs[id].Type == "TreasureBoxTrigger" {
						level := getCurScnFromProps(room.Name, room.Properties)
						room.pendingLootSpawns = append(room.pendingLootSpawns, pendingLootSpawn{
							sess: sess, level: level, pid: objs[id].Pid, chestPos: objs[id].Pos,
							at: time.Now().Add(lootSpawnDelay),
						})
						if sess.hostChar != nil {
							bumpAchievement(sess.hostChar.CharID, achIDOpenKimBox, 1)
						}
					}
				}
			}
			if packed%1000 == actionTypeEquipWeapon && sess.avatarBase != 0 {
				param := packed / 1000
				backpackSlot := byte(param & 0xFF)
				weaponSlot := byte((param >> 8) & 0xFF)
				s.hostEquipWeaponFromBackpack(room, sess, backpackSlot, weaponSlot)
			}
			if packed%1000 == actionTypeSwitchWeapon && sess.avatarBase != 0 && sess.hostChar != nil {
				weaponSlot := byte(packed / 1000)
				if err := db.SetActiveWeaponSlot(sess.hostChar.CharID, weaponSlot); err != nil {
					log.Printf("[HOST] SwitchWeapon charID=%d slot=%d error: %v", sess.hostChar.CharID, weaponSlot, err)
				} else {
					s.hostLiveApplyActiveWeapon(sess.dbAccountID, sess.hostChar.CharID, weaponSlot)
				}
			}
		}
	default:
		return false
	}
	return true
}

func (s *EnetServer) hostBroadcastJournaled(room *Room, code byte, params map[byte]any, gated bool) {
	seq := room.JournalEvent(code, params, gated)
	evt := buildEvent(code, params)
	for _, uid := range room.Players {
		target := s.sessionInRoom(uid, room.Name)
		if target == nil {
			continue
		}
		if gated && target.playerState < PlayerStatePlayerAndMapReady {
			continue
		}
		s.sendReliable(target, evt)
		if gated {
			target.gatedSeq = seq
		}
	}
}

func (s *EnetServer) hostReplayRoomState(room *Room, sess *ClientSession, actorNr int32) {
	events := room.MissedEvents(actorNr, sess.gatedSeq)
	sess.gatedSeq = room.eventSeq
	var gated []journaledEvent
	for _, e := range events {
		if e.gated {
			gated = append(gated, e)
			continue
		}
		s.sendEventTo(sess, e.code, e.payload)
	}

	var outfitRPCs []replayRPC

	if gamemode := gamemodeName(room); isEquipCapableGamemode(gamemode) {
		rpcName := outfitRPCName(gamemode)
		for peerActorNr, uid := range room.Players {
			if peerActorNr == HostActorNr {
				continue
			}
			peer := s.sessionInRoom(uid, room.Name)
			if peer == nil || peer == sess || peer.avatarBase == 0 || peer.hostChar == nil {
				continue
			}
			outfitRPCs = append(outfitRPCs, replayRPC{viewID: peer.avatarBase + 3, name: rpcName, args: outfitRPCArgs(peer.hostChar.CharID)})
		}
	}
	if len(gated) > 0 {
		room.pendingReplayEvents = append(room.pendingReplayEvents, pendingReplayEvent{
			sess: sess, events: gated, at: time.Now().Add(300 * time.Millisecond),
		})
	}

	if len(outfitRPCs) > 0 {
		for _, d := range []time.Duration{300 * time.Millisecond, 1 * time.Second, 3 * time.Second} {
			room.pendingReplayEvents = append(room.pendingReplayEvents, pendingReplayEvent{
				sess: sess, rpcs: outfitRPCs, at: time.Now().Add(d),
			})
		}
	}
	n := 0
	for viewID, stream := range room.viewStreams {

		if viewID == controllerSyncViewID {
			continue
		}
		s.sendEventTo(sess, 206, map[byte]any{
			ParamPlayerNr: HostActorNr,
			ParamEventData: map[any]any{
				byte(0):  int32(msNow()),
				Short(1): stream,
			},
		})
		n++
	}
	if len(events) > 0 || n > 0 {
		log.Printf("[HOST] replayed %d journaled events + %d view streams to actor=%d room=%s",
			len(events), n, actorNr, room.Name)
	}
}

func isValidGamemode(name string) bool {
	for _, gm := range validGamemodes {
		if name == gm {
			return true
		}
	}
	return false
}

var realTownPlaylists = map[int32]bool{
	3: true,
	4: true,
	5: true,
}

const defaultTownPlaylistID int32 = 3

func gamemodeName(room *Room) string {
	switch asInt32(room.Properties["GameMode"]) {
	case 1:
		return "GameModeTown"
	case 2:
		return "GamemodePvE"
	case 3:
		return "GameModeSurvival"
	case 4:
		return "GameModePvP"
	case 5:
		return "GamemodeTeamDeathmatch"
	case 6:
		return "GamemodeTutorial"
	case 7:
		return "GamemodeClaptrapSoccer"
	case 8:

		return "GamemodeRV"
	}

	return "GamemodePvE"
}

var gameModeNumberByName = map[string]int32{
	"GameModeTown":           1,
	"GamemodePvE":            2,
	"GameModeSurvival":       3,
	"GameModePvP":            4,
	"GamemodeTeamDeathmatch": 5,
	"GamemodeTutorial":       6,
	"GamemodeClaptrapSoccer": 7,
	"GamemodeRV":             8,
}

func gameModeNumberForPlaylist(playlistID int32) int32 {
	p := playlistInfo(playlistID)
	if p == nil {
		return 0
	}
	return gameModeNumberByName[p.GameMode]
}

func (s *EnetServer) hostTeleportPlayer(sess *ClientSession, pos [3]float32) {
	target := sess
	if target.roomName == "" || target.hostChar == nil {
		if other := s.sessionInAnyRoom(sess.dbAccountID); other != nil {
			target = other
		}
	}
	if target.roomName == "" || target.hostChar == nil {
		log.Printf("[HOST] /tp user=%s: no active room session found (not in a room, or avatar not spawned yet)", sess.userID)
		return
	}
	room := s.rooms[target.roomName]
	if room == nil {
		log.Printf("[HOST] /tp user=%s: room %q not found", sess.userID, target.roomName)
		return
	}
	actorNr := room.ActorNrFor(target.userID)
	if actorNr == 0 {
		log.Printf("[HOST] /tp user=%s: not a player in room %q", sess.userID, target.roomName)
		return
	}
	log.Printf("[HOST] /tp user=%s actor=%d room=%s -> pos=%v", target.userID, actorNr, target.roomName, pos)
	s.hostSpawnAvatar(room, target, actorNr, &pos)
}

func (s *EnetServer) hostTravelPlayer(sess *ClientSession, playlistID, gameMode int32) {
	target := sess
	if target.roomName == "" || target.hostChar == nil {
		if other := s.sessionInAnyRoom(sess.dbAccountID); other != nil {
			target = other
		}
	}
	if target.roomName == "" || target.hostChar == nil {
		log.Printf("[HOST] /rv user=%s: no active room session found (not in a room, or avatar not spawned yet)", sess.userID)
		return
	}

	if oldRoom := s.rooms[target.roomName]; oldRoom != nil {
		s.hostTeardownClientViews(oldRoom, target)
	}
	s.removeFromRoom(target)

	roomName := fmt.Sprintf("game_%d_%d", playlistID, msNow())
	room := newRoom(roomName, map[any]any{})
	room.Properties["PlaylistId"] = playlistID
	if gameMode != 0 {
		room.Properties["GameMode"] = gameMode
	}
	s.rooms[roomName] = room

	advertisedHost := s.AdvertisedHost
	if advertisedHost == "" {
		advertisedHost = s.host
	}
	log.Printf("[HOST] /rv user=%s room=%s playlist=%d gameMode=%d scene=%s", target.userID, roomName, playlistID, gameMode, getCurScnFromProps(roomName, room.Properties))
	s.sendOpResponse(target, OpJoinRandom, 0, map[byte]any{
		ParamRoomName: roomName,
		230:           fmt.Sprintf("%s:%d", advertisedHost, s.port),
	})
}

func (s *EnetServer) hostReloadMap(sess *ClientSession) {
	target := sess
	if target.roomName == "" || target.hostChar == nil {
		if other := s.sessionInAnyRoom(sess.dbAccountID); other != nil {
			target = other
		}
	}
	room := s.rooms[target.roomName]
	if room == nil {
		log.Printf("[HOST] /reloadmap user=%s: no active room session found", sess.userID)
		return
	}
	pid := asInt32(room.Properties["PlaylistId"])
	if pid == 0 {
		log.Printf("[HOST] /reloadmap user=%s room=%s: no PlaylistId, can't resolve scene", sess.userID, room.Name)
		return
	}
	sceneIdx := asInt32(room.Properties["SceneIdx"])
	scene, ok := playlistSceneAt(pid, sceneIdx)
	if !ok {
		if s, found := playlistFirstScene(pid); found {
			scene, sceneIdx, ok = s, 0, true
		}
	}
	if !ok {
		log.Printf("[HOST] /reloadmap user=%s room=%s pid=%d: no scene for SceneIdx=%d", sess.userID, room.Name, pid, sceneIdx)
		return
	}

	var travelers []*ClientSession
	for _, uid := range room.Players {
		if ps := s.sessionInRoom(uid, room.Name); ps != nil {
			travelers = append(travelers, ps)
		}
	}
	log.Printf("[HOST] /reloadmap user=%s room=%s pid=%d scene=%s(idx %d) players=%d",
		target.userID, room.Name, pid, scene, sceneIdx, len(travelers))

	nowMS := int32(msNow())
	gamemode := gamemodeName(room)
	payload := s.sessionStatsPayload(room)
	for _, ps := range travelers {
		s.bumpMapGen(ps)
		s.hostSendRPCTo(ps, sessionInfoViewID, "RPC_RequestSendSessionStats", []any{payload})
		s.hostSendSessionInfo(ps, room, nowMS, scene, "", gamemode)
		s.pendingSceneIdx[ps.dbAccountID] = pendingScene{playlistID: pid, sceneIdx: sceneIdx}
	}
}

func (s *EnetServer) hostGrantOutfit(sess *ClientSession, query string) {
	charID := sess.selectedCharID
	if charID == 0 && sess.hostChar != nil {
		charID = sess.hostChar.CharID
	}
	if charID == 0 {
		log.Printf("[HOST] /of user=%s: no character selected on this connection", sess.userID)
		return
	}
	ch, err := db.GetCharacterByID(charID)
	if err != nil {
		log.Printf("[HOST] /of user=%s: GetCharacterByID(%d) error: %v", sess.userID, charID, err)
		return
	}

	var preset *outfitPreset
	if id, err := strconv.Atoi(strings.TrimSpace(query)); err == nil {
		preset = outfitPresetByID(id)
	} else if matches := findOutfitPresets(ch.ClassType, query); len(matches) > 0 {
		preset = matches[0]
	}
	if preset == nil {
		log.Printf("[HOST] /of user=%s query=%q: no matching real outfit for class=%d", sess.userID, query, ch.ClassType)
		return
	}

	it := &domain.InventoryItem{
		CharID:     ch.CharID,
		ItemKind:   domain.ItemTypeOutfit,
		ResourceID: preset.ID,
		ItemName:   preset.Name,
		Rarity:     itemRarityValues[preset.Rarity],
		Level:      1,
		UID:        newRandomUID("outfit"),
		PartA:      avatarTypeValues[preset.AvatarType],
		PartB:      avatarSubPartValues[preset.AvatarSubpart],
		PartC:      preset.Index,
	}
	if _, _, err := db.AddItemToBackpack(it, clampBackpackSize(ch.BackpackSize)); err != nil {
		log.Printf("[HOST] /of user=%s: AddItemToBackpack error: %v", sess.userID, err)
		return
	}
	log.Printf("[HOST] /of user=%s granted %q (id=%d) class=%d", sess.userID, preset.Name, preset.ID, ch.ClassType)
	online := s.sessionOnline(sess.userID)
	if online == nil {
		online = sess
	}
	s.sendInventorySync(online)
}

func (s *EnetServer) hostTeardownClientViews(room *Room, sess *ClientSession) {
	destroy := func(viewID int32) {
		s.sendEventTo(sess, 204, map[byte]any{
			ParamPlayerNr:  HostActorNr,
			ParamEventData: map[any]any{byte(0): viewID},
		})
	}
	if sess.avatarBase != 0 {
		destroy(sess.avatarBase)
	}
	destroy(sessionInfoViewID)
	for _, st := range room.statics {
		destroy(st.viewBase)
	}
	for _, npc := range room.npcs {
		destroy(npc.viewBase)
	}
}

func (s *EnetServer) hostNextMap(room *Room, clusterName string) {
	pid := asInt32(room.Properties["PlaylistId"])
	if pid == 0 {
		log.Printf("[HOST] NextMap (cluster %q) room=%s: no PlaylistId, can't resolve next scene", clusterName, room.Name)
		return
	}
	nextIdx := asInt32(room.Properties["SceneIdx"]) + 1
	scene, ok := playlistSceneAt(pid, nextIdx)
	if !ok {

		pid = defaultTownPlaylistID
		nextIdx = 0
		scene, ok = playlistSceneAt(pid, nextIdx)
		if !ok {
			log.Printf("[HOST] NextMap (cluster %q) room=%s: no next scene and no town fallback, staying", clusterName, room.Name)
			return
		}
	}

	var travelers []*ClientSession
	for _, uid := range room.Players {
		if sess := s.sessionInRoom(uid, room.Name); sess != nil {
			travelers = append(travelers, sess)
		}
	}
	if len(travelers) == 0 {
		log.Printf("[HOST] NextMap (cluster %q) room=%s: no live sessions to move", clusterName, room.Name)
		return
	}

	log.Printf("[HOST] NextMap (cluster %q): %s → %s (scene[%d]) room=%s players=%d",
		clusterName, getCurScnFromProps(room.Name, room.Properties), scene, nextIdx, room.Name, len(travelers))

	nowMS := int32(msNow())
	gamemode := gamemodeName(room)

	payload := s.sessionStatsPayload(room)
	for _, sess := range travelers {
		s.bumpMapGen(sess)
		s.hostSendRPCTo(sess, sessionInfoViewID, "RPC_RequestSendSessionStats", []any{payload})
		s.hostSendSessionInfo(sess, room, nowMS, scene, "", gamemode)
		s.pendingSceneIdx[sess.dbAccountID] = pendingScene{playlistID: pid, sceneIdx: nextIdx}
	}
}

type pendingScene struct {
	playlistID int32
	sceneIdx   int32
}

func (s *EnetServer) hostSendWrapupDestination(room *Room, sess *ClientSession) {
	pid := asInt32(room.Properties["PlaylistId"])
	if pid == 0 {
		log.Printf("[HOST] wrapup destination room=%s: no PlaylistId, can't resolve", room.Name)
		return
	}
	nextIdx := asInt32(room.Properties["SceneIdx"]) + 1
	scene, ok := playlistSceneAt(pid, nextIdx)
	if !ok {
		pid = defaultTownPlaylistID
		nextIdx = 0
		scene, ok = playlistSceneAt(pid, nextIdx)
		if !ok {
			log.Printf("[HOST] wrapup destination room=%s: no next scene and no town fallback, staying", room.Name)
			return
		}
	}
	log.Printf("[HOST] wrapup destination user=%s: %s → %s (scene[%d]) room=%s", sess.userID, getCurScnFromProps(room.Name, room.Properties), scene, nextIdx, room.Name)
	s.bumpMapGen(sess)
	s.hostSendSessionInfo(sess, room, int32(msNow()), scene, "", gamemodeName(room))
	s.pendingSceneIdx[sess.dbAccountID] = pendingScene{playlistID: pid, sceneIdx: nextIdx}
}

func (s *EnetServer) hostSpawnAvatar(room *Room, sess *ClientSession, actorNr int32, posOverride *[3]float32) {
	if sess.hostChar == nil {
		return
	}

	gamemode := gamemodeName(room)
	town := gamemode == "GameModeTown" || gamemode == "GamemodeRV"
	prefab := avatarPrefab(sess.hostChar, town)

	if sess.avatarBase != 0 {
		s.hostDestroyShieldEntity(room, sess)
		s.hostBroadcastJournaled(room, 204, map[byte]any{
			ParamPlayerNr:  HostActorNr,
			ParamEventData: map[any]any{byte(0): sess.avatarBase},
		}, true)
		for v := sess.avatarBase; v <= sess.avatarBase+8; v++ {
			delete(room.viewStreams, v)
		}
		delete(room.playerPos, sess.avatarBase+1)

		if sess.hunterDroidView != 0 {
			s.hostBroadcastJournaled(room, 204, map[byte]any{
				ParamPlayerNr:  HostActorNr,
				ParamEventData: map[any]any{byte(0): sess.hunterDroidView},
			}, true)
		}
		sess.hunterDroidActive = false
		sess.hunterDroidView = 0
		sess.skillPhaseNext = 0
	}
	base := room.NextAvatarViewBase()
	sess.avatarBase = base
	sess.avatarSpawnedAt = time.Now()
	sess.lastActivityAt = time.Now()
	sess.lastSkillTreeSig = ""
	sess.lastWeaponStreamSig = ""
	views := make([]int32, 5)
	for i := range views {
		views[i] = base + int32(i)
	}
	pos, rot, rotOK := spawnPosition(room, getCurScnFromProps(room.Name, room.Properties), int32(sess.teamID))
	if posOverride != nil {
		pos = *posOverride
		rotOK = false
	}
	if room.playerPos == nil {
		room.playerPos = map[int32][3]float32{}
	}
	room.playerPos[base+1] = pos
	sess.broadcastPos = pos
	const assumedSpawnDrop = 2.5
	sess.spawnFloorY = pos[1] - assumedSpawnDrop
	nowMS := int32(msNow())
	instantiateData := map[any]any{
		byte(0): prefab,
		byte(1): vector3Custom(pos[0], pos[1], pos[2]),
		byte(4): IntArray{Items: views},
		byte(5): []any{actorNr},
		byte(6): nowMS,
		byte(7): views[0],
	}

	if rotOK {
		instantiateData[byte(2)] = quaternionCustom(rot[0], rot[1], rot[2], rot[3])
	}
	s.hostBroadcastJournaled(room, 202, map[byte]any{
		ParamPlayerNr:  HostActorNr,
		ParamEventData: instantiateData,
	}, true)
	log.Printf("[HOST] spawned avatar %s views=%d-%d actor=%d pos=%v rot=%v", prefab, views[0], views[4], actorNr, pos, rot)

	if room.actionsGranted {
		now := time.Now()
		for _, d := range []time.Duration{2, 5, 10} {
			room.actionGrantAt = append(room.actionGrantAt, now.Add(d*time.Second))
		}
	}
	s.scriptFire(room, "OnPlayerSpawn", lua.LNumber(actorNr), lua.LString(sess.hostChar.Name))

	room.pendingPlayerSpawnFx = append(room.pendingPlayerSpawnFx, pendingPlayerSpawnFx{
		sess: sess, avatarBase: base, at: time.Now().Add(playerSpawnFxDelay),
	})

	tutorial := gamemodeName(room) == "GamemodeTutorial"
	sess.equippedWeapons = nil
	sess.weaponTypes = sess.weaponTypes[:0]
	switch {
	case tutorial:
		sess.activeWeaponSlot = 3
	case town:
		sess.activeWeaponSlot = 0
		weapons := equippedWeaponsOrdered(sess.hostChar.CharID)
		for i, it := range weapons {
			if i >= 4 {
				break
			}
			sess.equippedWeapons = append(sess.equippedWeapons, weaponSlotEntity{dna: weaponItemDNA(it), itemID: it.ItemID})
			sess.weaponTypes = append(sess.weaponTypes, int32(it.PartA))
		}
	default:
		weapons := equippedWeaponsOrdered(sess.hostChar.CharID)
		for i, it := range weapons {
			if i >= 4 {
				break
			}
			weaponView := base + 5 + int32(i)
			dna := weaponItemDNA(it)
			s.hostBroadcastJournaled(room, 202, map[byte]any{
				ParamPlayerNr: HostActorNr,
				ParamEventData: map[any]any{
					byte(0): "Entities/Object/Weapon/Weapon",
					byte(1): vector3Custom(pos[0], pos[1], pos[2]),
					byte(4): IntArray{Items: []int32{weaponView}},
					byte(5): []any{dna, vector3Custom(0, 0, 0), int32(-1), int32(-1), int32(0), int32(0)},
					byte(6): nowMS,
					byte(7): weaponView,
				},
			}, true)
			scheduleWeaponClaim(room, base, weaponView)
			sess.equippedWeapons = append(sess.equippedWeapons, weaponSlotEntity{view: weaponView, dna: dna, itemID: it.ItemID})
			sess.weaponTypes = append(sess.weaponTypes, int32(it.PartA))
			if room.weaponElement == nil {
				room.weaponElement = map[int32]int32{}
			}
			room.weaponElement[weaponView] = int32(it.ElementType)
			if room.weaponType == nil {
				room.weaponType = map[int32]int32{}
			}
			room.weaponType[weaponView] = int32(it.PartA)
			if room.weaponDamage == nil {
				room.weaponDamage = map[int32]float64{}
			}
			realDamage := resolveWeaponDamage(it) * (1 + weaponDamageScale(sess.hostChar))
			room.weaponDamage[weaponView] = realDamage
			log.Printf("[HOST] spawned real weapon %q slot=%d viewID=%d element=%d damage=%.1f for actor=%d", it.ItemName, it.Slot, weaponView, it.ElementType, realDamage, actorNr)
		}
		active := 0
		for i, it := range weapons {
			if it.Slot == sess.hostChar.ActiveWeaponSlot {
				active = i
				break
			}
		}
		sess.activeWeaponSlot = byte(active)
	}
	if !town {
		sess.grenadeCap = grenadeMaxFor(sess.hostChar)
		sess.ammoEst = 360
		sess.grenadeCount = sess.grenadeCap
		pouches := ammoPouchesForSession(sess, true)
		if tutorial {
			pouches = ammoPouchesForSession(sess, false)
			sess.ammoEst = 0
		}
		s.hostSendRPC(room, base+3, "RPC_OnReceiveAmmos", []any{pouches})

		for _, d := range []time.Duration{300 * time.Millisecond, 1 * time.Second, 3 * time.Second} {
			room.pendingOutfitResyncs = append(room.pendingOutfitResyncs, pendingOutfitResync{
				sess: sess, base: base, outfitArgs: []any{pouches}, rpcName: "RPC_OnReceiveAmmos", at: time.Now().Add(d),
			})
		}
	}

	if !tutorial {
		s.hostApplyEquippedShield(room, sess, base, pos)
	} else {
		sess.shield, sess.maxShield, sess.shieldDNA, sess.shieldView = 0, 0, Custom{}, 0
	}
	sess.lastDamageAt = time.Time{}
	maxLife := maxLifeForCharacter(sess.hostChar)
	sess.maxHP, sess.hp, sess.isDead = maxLife, maxLife, false
	rpcView := vitalsRPCView(room, base)
	s.hostSendRPC(room, rpcView, "RPC_RequestAllLifeAndShield", []any{
		map[int]any{0: int32(1), 1: rpcView, 2: playerLifeTable(maxLife, maxLife, sess.shield, sess.maxShield)},
	})
	for _, d := range []time.Duration{300 * time.Millisecond, 1 * time.Second, 3 * time.Second} {
		room.pendingVitalsResyncs = append(room.pendingVitalsResyncs, pendingVitalsResync{
			sess: sess, base: base, maxLife: maxLife, at: time.Now().Add(d),
		})
	}

	if !town {
		if !tutorial {
			s.hostSendBackpackItems(room, sess)
			for _, d := range []time.Duration{300 * time.Millisecond, 1 * time.Second, 3 * time.Second} {
				room.pendingItemsResyncs = append(room.pendingItemsResyncs, pendingItemsResync{
					sess: sess, base: base, at: time.Now().Add(d),
				})
			}
			outfitArgs := outfitRPCArgs(sess.hostChar.CharID)
			s.hostSendRPC(room, base+3, "RPC_ApplyOutfitsSetup", outfitArgs)
			for _, d := range []time.Duration{300 * time.Millisecond, 1 * time.Second, 3 * time.Second} {
				room.pendingOutfitResyncs = append(room.pendingOutfitResyncs, pendingOutfitResync{
					sess: sess, base: base, outfitArgs: outfitArgs, rpcName: "RPC_ApplyOutfitsSetup", at: time.Now().Add(d),
				})
			}
		}
	} else if !tutorial {
		outfitArgs := outfitRPCArgs(sess.hostChar.CharID)
		s.hostSendRPC(room, base+3, "RPC_OnUpdateOutfits", outfitArgs)
		for _, d := range []time.Duration{300 * time.Millisecond, 1 * time.Second, 3 * time.Second} {
			room.pendingOutfitResyncs = append(room.pendingOutfitResyncs, pendingOutfitResync{
				sess: sess, base: base, outfitArgs: outfitArgs, rpcName: "RPC_OnUpdateOutfits", at: time.Now().Add(d),
			})
		}
	}
}

func (s *EnetServer) hostSendBackpackItems(room *Room, sess *ClientSession) {
	backpackSize := clampBackpackSize(sess.hostChar.BackpackSize)
	itemsHT := map[int]any{0: int32(backpackSize)}
	for _, it := range backpackItemsOrdered(sess.hostChar.CharID) {
		if it.Slot >= 0 && it.Slot < backpackSize {
			itemsHT[it.Slot+1] = genericItemDNA(it)
		}
	}
	s.hostSendRPC(room, sess.avatarBase+3, "RPC_OnReceiveItems", []any{itemsHT})
}

func (s *EnetServer) hostEquipWeaponFromBackpack(room *Room, sess *ClientSession, backpackSlot, weaponSlot byte) {
	if sess.hostChar == nil || weaponSlot >= 4 {
		return
	}
	key := int(backpackSlot)<<8 | int(weaponSlot)
	if sess.lastEquipWeaponKey == key && time.Since(sess.lastEquipWeaponAt) < 500*time.Millisecond {
		return
	}
	sess.lastEquipWeaponKey = key
	sess.lastEquipWeaponAt = time.Now()
	gamemode := gamemodeName(room)
	if gamemode == "GamemodeTutorial" {
		return
	}
	if gamemode == "GameModeTown" || gamemode == "GamemodeRV" {
		s.hostEquipWeaponFromBackpackTown(room, sess, backpackSlot, weaponSlot)
		return
	}
	item, err := db.GetInventoryItemAt(sess.hostChar.CharID, int(backpackSlot), false)
	if err != nil {
		log.Printf("[HOST] EquipWeapon charID=%d backpack=%d weapon=%d lookup error: %v", sess.hostChar.CharID, backpackSlot, weaponSlot, err)
		return
	}
	ch, err := db.GetCharacterByID(sess.hostChar.CharID)
	if err != nil {
		log.Printf("[HOST] EquipWeapon charID=%d character lookup error: %v", sess.hostChar.CharID, err)
		return
	}
	if ch.Level < item.Level {
		log.Printf("[HOST] EquipWeapon rejected charID=%d: character level=%d < %q required level=%d", sess.hostChar.CharID, ch.Level, item.ItemName, item.Level)
		return
	}
	if err := db.EquipWeaponFromBackpack(sess.hostChar.CharID, backpackSlot, weaponSlot); err != nil {
		log.Printf("[HOST] EquipWeapon charID=%d backpack=%d weapon=%d error: %v", sess.hostChar.CharID, backpackSlot, weaponSlot, err)
		return
	}
	log.Printf("[HOST] user=%s equipped backpack slot=%d -> weapon slot=%d", sess.userID, backpackSlot, weaponSlot)

	sess.equipAt = time.Time{}
	previousActive := sess.activeWeaponSlot
	s.hostRespawnEquippedWeapons(room, sess)
	s.hostSendBackpackItems(room, sess)

	if weaponSlot == previousActive && sess.activeWeaponSlot == weaponSlot {
		sess.activeWeaponSlot = (weaponSlot + 1) % 4
		s.hostSendSerialize(room)
		sess.equipSlot = weaponSlot
		sess.equipAt = time.Now().Add(600 * time.Millisecond)
	} else {
		s.hostSendSerialize(room)
	}
}

func (s *EnetServer) hostEquipWeaponFromBackpackTown(room *Room, sess *ClientSession, backpackSlot, weaponSlot byte) {
	item, err := db.GetInventoryItemAt(sess.hostChar.CharID, int(backpackSlot), false)
	if err != nil {
		log.Printf("[HOST] EquipWeapon(Town) charID=%d backpack=%d weapon=%d lookup error: %v", sess.hostChar.CharID, backpackSlot, weaponSlot, err)
		return
	}
	ch, err := db.GetCharacterByID(sess.hostChar.CharID)
	if err != nil {
		log.Printf("[HOST] EquipWeapon(Town) charID=%d character lookup error: %v", sess.hostChar.CharID, err)
		return
	}
	if ch.Level < item.Level {
		log.Printf("[HOST] EquipWeapon(Town) rejected charID=%d: character level=%d < %q required level=%d", sess.hostChar.CharID, ch.Level, item.ItemName, item.Level)
		return
	}
	if err := db.EquipWeaponFromBackpack(sess.hostChar.CharID, backpackSlot, weaponSlot); err != nil {
		log.Printf("[HOST] EquipWeapon(Town) charID=%d backpack=%d weapon=%d error: %v", sess.hostChar.CharID, backpackSlot, weaponSlot, err)
		return
	}
	log.Printf("[HOST] user=%s equipped(Town) backpack slot=%d -> weapon slot=%d", sess.userID, backpackSlot, weaponSlot)

	sess.equipAt = time.Time{}
	previousActive := sess.activeWeaponSlot
	sess.equippedWeapons = nil
	sess.weaponTypes = sess.weaponTypes[:0]
	weapons := equippedWeaponsOrdered(sess.hostChar.CharID)
	for i, it := range weapons {
		if i >= 4 {
			break
		}
		sess.equippedWeapons = append(sess.equippedWeapons, weaponSlotEntity{dna: weaponItemDNA(it), itemID: it.ItemID})
		sess.weaponTypes = append(sess.weaponTypes, int32(it.PartA))
	}

	active := 0
	if ch, err := db.GetCharacterByID(sess.hostChar.CharID); err == nil {
		for i, it := range weapons {
			if it.Slot == ch.ActiveWeaponSlot {
				active = i
				break
			}
		}
	}
	sess.activeWeaponSlot = byte(active)
	s.hostSendBackpackItems(room, sess)

	if weaponSlot == previousActive && sess.activeWeaponSlot == weaponSlot {
		sess.activeWeaponSlot = (weaponSlot + 1) % 4
		s.hostSendSerialize(room)
		sess.equipSlot = byte(active)
		sess.equipAt = time.Now().Add(600 * time.Millisecond)
	} else {
		s.hostSendSerialize(room)
	}
}

func (s *EnetServer) hostRespawnEquippedWeapons(room *Room, sess *ClientSession) {
	pos, ok := playerStreamPos(room, sess.avatarBase)
	if !ok {
		return
	}
	nowMS := int32(msNow())
	old := sess.equippedWeapons
	weapons := equippedWeaponsOrdered(sess.hostChar.CharID)
	stillEquipped := make(map[int64]bool, len(weapons))
	for _, it := range weapons {
		stillEquipped[it.ItemID] = true
	}
	for _, o := range old {
		if !stillEquipped[o.itemID] {
			s.hostBroadcastJournaled(room, 204, map[byte]any{
				ParamPlayerNr:  HostActorNr,
				ParamEventData: map[any]any{byte(0): o.view},
			}, true)
			delete(room.viewStreams, o.view)
		}
	}
	fresh := make([]weaponSlotEntity, 0, len(weapons))
	sess.weaponTypes = sess.weaponTypes[:0]
	for i, it := range weapons {
		if i >= 4 {
			break
		}
		if reused := findWeaponSlotEntityByItemID(old, it.ItemID); reused != nil {
			fresh = append(fresh, *reused)
			sess.weaponTypes = append(sess.weaponTypes, int32(it.PartA))
			continue
		}
		weaponView := room.NextAvatarViewBase()
		dna := weaponItemDNA(it)
		s.hostBroadcastJournaled(room, 202, map[byte]any{
			ParamPlayerNr: HostActorNr,
			ParamEventData: map[any]any{
				byte(0): "Entities/Object/Weapon/Weapon",
				byte(1): vector3Custom(pos[0], pos[1], pos[2]),
				byte(4): IntArray{Items: []int32{weaponView}},
				byte(5): []any{dna, vector3Custom(0, 0, 0), int32(-1), int32(-1), int32(0), int32(0)},
				byte(6): nowMS,
				byte(7): weaponView,
			},
		}, true)
		scheduleWeaponClaim(room, sess.avatarBase, weaponView)
		fresh = append(fresh, weaponSlotEntity{view: weaponView, dna: dna, itemID: it.ItemID})
		sess.weaponTypes = append(sess.weaponTypes, int32(it.PartA))
		if room.weaponElement == nil {
			room.weaponElement = map[int32]int32{}
		}
		room.weaponElement[weaponView] = int32(it.ElementType)
		if room.weaponType == nil {
			room.weaponType = map[int32]int32{}
		}
		room.weaponType[weaponView] = int32(it.PartA)
		if room.weaponDamage == nil {
			room.weaponDamage = map[int32]float64{}
		}
		realDamage := resolveWeaponDamage(it) * (1 + weaponDamageScale(sess.hostChar))
		room.weaponDamage[weaponView] = realDamage
		log.Printf("[HOST] re-equipped real weapon %q slot=%d viewID=%d element=%d damage=%.1f for actor view=%d", it.ItemName, it.Slot, weaponView, it.ElementType, realDamage, sess.avatarBase)
	}
	sess.equippedWeapons = fresh

	active := 0
	if ch, err := db.GetCharacterByID(sess.hostChar.CharID); err == nil {
		for i, it := range weapons {
			if it.Slot == ch.ActiveWeaponSlot {
				active = i
				break
			}
		}
	}
	sess.activeWeaponSlot = byte(active)
}

func findWeaponSlotEntityByItemID(list []weaponSlotEntity, itemID int64) *weaponSlotEntity {
	for i := range list {
		if list[i].itemID == itemID {
			return &list[i]
		}
	}
	return nil
}

const weaponClaimDelay = 250 * time.Millisecond

func scheduleWeaponClaim(room *Room, pickerView, weaponView int32) {
	for _, d := range []time.Duration{weaponClaimDelay, 1 * time.Second, 3 * time.Second} {
		room.pendingWeaponClaims = append(room.pendingWeaponClaims, pendingWeaponClaim{
			pickerView: pickerView, weaponView: weaponView, at: time.Now().Add(d),
		})
	}
}

func (s *EnetServer) hostRunPendingWeaponClaims(room *Room) {
	if len(room.pendingWeaponClaims) == 0 {
		return
	}
	now := time.Now()
	remaining := room.pendingWeaponClaims[:0]
	for _, p := range room.pendingWeaponClaims {
		if now.Before(p.at) {
			remaining = append(remaining, p)
			continue
		}
		s.hostSendRPC(room, interactionManagerViewID, "RPC_H2C_OnPickUpItem", []any{p.pickerView, p.weaponView})
	}
	room.pendingWeaponClaims = remaining
}

func (s *EnetServer) hostDestroyShieldEntity(room *Room, sess *ClientSession) {
	if sess.shieldView == 0 {
		return
	}
	s.hostBroadcastJournaled(room, 204, map[byte]any{
		ParamPlayerNr:  HostActorNr,
		ParamEventData: map[any]any{byte(0): sess.shieldView},
	}, true)
	delete(room.viewStreams, sess.shieldView)
	sess.shieldView = 0
}

func (s *EnetServer) hostApplyEquippedShield(room *Room, sess *ClientSession, base int32, pos [3]float32) {

	s.hostDestroyShieldEntity(room, sess)
	sess.shield, sess.maxShield, sess.shieldDNA, sess.shieldView = 0, 0, Custom{}, 0
	sh, err := db.GetEquippedSingle(sess.hostChar.CharID, domain.ItemTypeShield)
	if err != nil {
		log.Printf("[HOST] GetEquippedSingle shield char=%d: %v", sess.hostChar.CharID, err)
		return
	}
	if sh == nil {
		return
	}
	sess.maxShield = int32(float64(realMaxShield(sh.PartB, sh.Level)) * (1 + maxShieldScale(sess.hostChar)))
	sess.shield = sess.maxShield
	sess.shieldDNA = shieldItemDNA(sh)
	sess.shieldView = base + 9
	s.hostBroadcastJournaled(room, 202, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): shieldPrefabForManufacturer(sh.PartA),
			byte(1): vector3Custom(pos[0], pos[1], pos[2]),
			byte(4): IntArray{Items: []int32{sess.shieldView}},
			byte(5): []any{sess.shieldDNA, vector3Custom(0, 0, 0), int32(-1), int32(-1), int32(0), int32(0)},
			byte(6): int32(msNow()),
			byte(7): sess.shieldView,
		},
	}, true)
	scheduleWeaponClaim(room, base, sess.shieldView)
}

func vitalsRPCView(room *Room, avatarBase int32) int32 {
	return avatarBase
}

func playerLifeTable(life, maxLife, shield, maxShield int32) map[int]any {
	return map[int]any{
		0: life, 1: maxLife,
		2: shield, 3: maxShield,
		4: int32(0), 5: int32(0),
		6: int32(0), 7: int32(0),
	}
}

func npcLifeTable(maxHP, currentHP int32) map[int]any {
	return map[int]any{
		0: maxHP, 1: currentHP,
		2: int32(0), 3: int32(0),
	}
}

func npcVitalsTable(kit string, maxHP, currentHP int32) map[int]any {
	switch {
	case bossHudKits[kit]:
		return map[int]any{
			0: int32(100), 1: int32(100),
			2: int32(100), 3: int32(100),
			4: int32(100), 5: int32(100),
			6: int32(100), 7: int32(100),
			8: maxHP, 9: currentHP,
			10: int32(0), 11: int32(0),
		}
	case isBadassKit(kit):
		return map[int]any{
			0: int32(100), 1: int32(100),
			2: maxHP, 3: currentHP,
			4: int32(0), 5: int32(0),
		}
	default:
		return npcLifeTable(maxHP, currentHP)
	}
}

func fullAmmoPouches() map[int]any {
	return map[int]any{
		0: Short(360), 1: Short(360),
		2: Short(120), 3: Short(120),
		4: byte(3), 5: byte(3),
		6: Short(120), 7: Short(120),
		8: Short(120), 9: Short(120),
		10: Short(120), 11: Short(120),
	}
}

func ammoPouchesForSession(sess *ClientSession, fullGuns bool) map[int]any {
	var p map[int]any
	if fullGuns {
		p = fullAmmoPouches()
	} else {
		p = emptyAmmoPouches()
	}
	p[4] = byte(sess.grenadeCap)
	p[5] = byte(sess.grenadeCount)
	return p
}

func grenadeMaxFor(ch *domain.Character) int32 {
	return grenadeMax + grenadeCapacityBonus(ch)
}

func emptyAmmoPouches() map[int]any {
	return map[int]any{
		0: Short(0), 1: Short(360),
		2: Short(0), 3: Short(120),
		4: byte(3), 5: byte(3),
		6: Short(0), 7: Short(120),
		8: Short(0), 9: Short(120),
		10: Short(0), 11: Short(120),
	}
}

func (s *EnetServer) hostKillEntity(room *Room, attackerView, victimView, damage, element int32) {
	group := map[int]any{
		0: byte(1),
		1: attackerView,
		2: packSubTypeElement(2, element),
		3: byte(1),
		4: victimView,
		5: Short(8 | 1),
		6: damage,
		7: byte(80),
	}
	s.hostSendRPC(room, sessionInfoViewID, "RPC_D", []any{map[int]any{0: group}})
	room.deferredDestroys = append(room.deferredDestroys, deferredDestroy{
		view: victimView, at: time.Now().Add(npcDeathRemoveDelay),
	})
}

const npcDeathRemoveDelay = 1500 * time.Millisecond

const claptrapRespawnStagger = 150 * time.Millisecond

func (s *EnetServer) hostDamageEntity(room *Room, attackerView, victimView, damage, weakPointIndex, element int32, isDOT bool) {
	subType, flags := hitSubTypeAndFlags(isDOT)
	group := map[int]any{
		0: byte(1),
		1: attackerView,
		2: packSubTypeElement(subType, element),
		3: byte(1),
		4: victimView,
		5: packWeakPointFlags(weakPointIndex, flags),
		6: damage,
	}
	s.hostSendRPC(room, sessionInfoViewID, "RPC_D", []any{map[int]any{0: group}})
}

func hitSubTypeAndFlags(isDOT bool) (subType, flags int32) {
	subType, flags = 2, 8
	if isDOT {
		subType = 34
		flags |= 2
	}
	return subType, flags
}

func elementalDamageOverTimeSubType(element int32) int32 {
	switch element {
	case elementFire:
		return 31
	case elementShock:
		return 32
	case elementCorrosive:
		return 33
	default:
		return 30
	}
}

func packWeakPointFlags(weakPointIndex, flags int32) Short {
	return Short(int16(weakPointIndex)<<8 | int16(flags))
}

func (s *EnetServer) hostRunDeferredDestroys(room *Room) {
	if len(room.deferredDestroys) == 0 {
		return
	}
	now := time.Now()
	remaining := room.deferredDestroys[:0]
	for _, d := range room.deferredDestroys {
		if now.Before(d.at) {
			remaining = append(remaining, d)
			continue
		}
		s.hostBroadcastJournaled(room, 204, map[byte]any{
			ParamPlayerNr:  HostActorNr,
			ParamEventData: map[any]any{byte(0): d.view},
		}, false)
		for i := range room.npcs {
			if room.npcs[i].viewBase == d.view {
				room.npcs = append(room.npcs[:i], room.npcs[i+1:]...)
				break
			}
		}
	}
	room.deferredDestroys = remaining
}

func (s *EnetServer) hostRunPendingVitalsResyncs(room *Room) {
	if len(room.pendingVitalsResyncs) == 0 {
		return
	}
	now := time.Now()
	remaining := room.pendingVitalsResyncs[:0]
	for _, p := range room.pendingVitalsResyncs {
		if now.Before(p.at) {
			remaining = append(remaining, p)
			continue
		}
		if p.sess.avatarBase != p.base {
			continue
		}
		rpcView := vitalsRPCView(room, p.base)
		s.hostSendRPC(room, rpcView, "RPC_RequestAllLifeAndShield", []any{
			map[int]any{0: int32(1), 1: rpcView, 2: playerLifeTable(p.maxLife, p.maxLife, p.sess.shield, p.sess.maxShield)},
		})
	}
	room.pendingVitalsResyncs = remaining
}

func outfitRPCName(gamemode string) string {
	if gamemode == "GameModeTown" || gamemode == "GamemodeRV" {
		return "RPC_OnUpdateOutfits"
	}
	return "RPC_ApplyOutfitsSetup"
}

func isEquipCapableGamemode(gamemode string) bool {
	switch gamemode {
	case "GameModeTown", "GamemodeRV", "GamemodePvE", "GameModePvP", "GamemodeClaptrapSoccer", "GameModeSurvival", "GamemodeTeamDeathmatch":
		return true
	}
	return false
}

type pendingOutfitResync struct {
	sess       *ClientSession
	base       int32
	outfitArgs []any
	rpcName    string
	at         time.Time
}

func (s *EnetServer) hostRunPendingOutfitResyncs(room *Room) {
	if len(room.pendingOutfitResyncs) == 0 {
		return
	}
	now := time.Now()
	remaining := room.pendingOutfitResyncs[:0]
	for _, p := range room.pendingOutfitResyncs {
		if now.Before(p.at) {
			remaining = append(remaining, p)
			continue
		}
		if p.sess.avatarBase != p.base {
			continue
		}
		s.hostSendRPC(room, p.base+3, p.rpcName, p.outfitArgs)
	}
	room.pendingOutfitResyncs = remaining
}

type pendingItemsResync struct {
	sess *ClientSession
	base int32
	at   time.Time
}

func (s *EnetServer) hostRunPendingItemsResyncs(room *Room) {
	if len(room.pendingItemsResyncs) == 0 {
		return
	}
	now := time.Now()
	remaining := room.pendingItemsResyncs[:0]
	for _, p := range room.pendingItemsResyncs {
		if now.Before(p.at) {
			remaining = append(remaining, p)
			continue
		}
		if p.sess.avatarBase != p.base {
			continue
		}
		s.hostSendBackpackItems(room, p.sess)
	}
	room.pendingItemsResyncs = remaining
}

func (s *EnetServer) hostRunPendingShieldResyncs(room *Room) {
	if len(room.pendingShieldResyncs) == 0 {
		return
	}
	now := time.Now()
	remaining := room.pendingShieldResyncs[:0]
	for _, p := range room.pendingShieldResyncs {
		if now.Before(p.at) {
			remaining = append(remaining, p)
			continue
		}
		if p.sess.avatarBase != p.base {
			continue
		}
		rpcView := vitalsRPCView(room, p.base)
		s.hostSendRPC(room, rpcView, "RPC_RequestAllLifeAndShield", []any{
			map[int]any{0: int32(1), 1: rpcView, 2: playerLifeTable(p.sess.hp, p.sess.maxHP, p.sess.shield, p.sess.maxShield)},
		})
	}
	room.pendingShieldResyncs = remaining
}

func (s *EnetServer) hostRunPendingNpcVitalsResyncs(room *Room) {
	if len(room.pendingNpcVitalsResyncs) == 0 {
		return
	}
	now := time.Now()
	remaining := room.pendingNpcVitalsResyncs[:0]
	for _, p := range room.pendingNpcVitalsResyncs {
		if now.Before(p.at) {
			remaining = append(remaining, p)
			continue
		}
		found := false
		for i := range room.npcs {
			if room.npcs[i].viewBase == p.view && room.npcs[i].hp > 0 {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		s.hostSendRPC(room, p.equipView, "RPC_SendLifeAndShield", []any{npcVitalsTable(p.kit, p.maxHP, p.hp)})
	}
	room.pendingNpcVitalsResyncs = remaining
}

const ffylDuration = 30 * time.Second

const damageAsExplosionRadius = float32(5)

func (s *EnetServer) hostDamagePlayer(room *Room, sess *ClientSession, attackerView, dmg, subType, element int32) (justDied bool) {
	if sess.isDead || sess.respawnPending {
		return false
	}
	if attackerView == 0 {
		attackerView = sess.avatarBase + 1
	}

	isNpcAttacker := sessionByAvatarBase(s, room, attackerView-1) == nil

	if isNpcAttacker {
		if evade := effectValue(sess.hostChar, "EvadeChance"); evade > 0 && rand.Float64() < evade {
			if reflectPct := effectValue(sess.hostChar, "OnEvadeReflectDmgPercent"); reflectPct > 0 {
				if reflectDmg := int32(float64(dmg) * reflectPct); reflectDmg > 0 {
					s.hostDamageNpcByView(room, sess.avatarBase, attackerView, reflectDmg, 0, element, false)
				}
			}
			return false
		}
	}
	if resist := effectValue(sess.hostChar, "LowHealthDmgResist"); resist > 0 {
		dmg = int32(float64(dmg) * (1 - resist))
		if dmg < 0 {
			dmg = 0
		}
	}
	if isNpcAttacker {
		if reflectPct := effectValue(sess.hostChar, "ReflectDamage"); reflectPct > 0 {
			if reflectDmg := int32(float64(dmg) * reflectPct); reflectDmg > 0 {
				s.hostDamageNpcByView(room, sess.avatarBase, attackerView, reflectDmg, 0, element, false)
			}
		}
	}
	if explosionPct := effectValue(sess.hostChar, "DamageAsExplosion"); explosionPct > 0 {
		if explDmg := int32(float64(dmg) * explosionPct); explDmg > 0 {
			if pos, ok := playerStreamPos(room, sess.avatarBase); ok {
				s.hostAreaDamageNpcs(room, sess.avatarBase+1, pos, damageAsExplosionRadius, explDmg, elementExplosive, false)
			}
		}
	}
	if chance := effectValue(sess.hostChar, "AutoMarkAttackerChance"); chance > 0 && rand.Float64() < chance {
		if sessionByAvatarBase(s, room, attackerView-1) == nil {
			for i := range room.npcs {
				if room.npcs[i].viewBase == attackerView && room.npcs[i].hp > 0 {
					if room.markedNpcs == nil {
						room.markedNpcs = map[int32]markedNpc{}
					}
					room.markedNpcs[attackerView] = markedNpc{until: time.Now().Add(hunterMarkDuration)}
					break
				}
			}
		}
	}
	sess.lastDamageAt = time.Now()
	shieldDmg := int32(0)
	if sess.shield > 0 {
		shieldDmg = dmg
		if shieldDmg > sess.shield {
			shieldDmg = sess.shield
		}
		sess.shield -= shieldDmg
	}
	sess.shieldRegenBaseline = sess.shield
	healthDmg := dmg - shieldDmg
	sess.hp -= healthDmg
	if sess.hp <= 0 {
		if reviveChance := effectValue(sess.hostChar, "ReviveChance"); reviveChance > 0 && rand.Float64() < reviveChance {
			reviveHP := effectValue(sess.hostChar, "ReviveHealthPoint")
			sess.hp = int32(float64(sess.maxHP) * reviveHP)
			if sess.hp < 1 {
				sess.hp = 1
			}
			log.Printf("[HOST] actor view=%d ReviveChance saved from lethal hit, hp=%d/%d room=%s", sess.avatarBase, sess.hp, sess.maxHP, room.Name)
		}
	}
	lethal := sess.hp <= 0
	wireSubType := subType
	var elementalFlags int32
	if element != elementNone {
		wireSubType = elementalDamageOverTimeSubType(element)
		elementalFlags = 2
	}
	group := map[int]any{
		0: byte(1), 1: attackerView, 2: packSubTypeElement(wireSubType, element), 3: byte(1), 4: sess.avatarBase + 1,
	}
	flags := elementalFlags
	key := 6
	if healthDmg > 0 || shieldDmg == 0 {
		flags |= 8
		group[key] = healthDmg
		key++
	}
	if shieldDmg > 0 {
		flags |= 4
		group[key] = shieldDmg
		key++
	}
	if lethal {
		sess.hp = 0
		flags |= 1
		group[key] = byte(80)
	}
	group[5] = Short(flags)
	s.hostSendRPC(room, sessionInfoViewID, "RPC_D", []any{map[int]any{0: group}})
	if lethal {
		if asInt32(room.Properties["GameMode"]) == 4 {

			s.hostKillWithDeathAnim(room, sess, 0)
		} else {
			sess.isDead = true
			sess.downedAt = time.Now()
			sess.downTaskUID = (sess.downTaskUID + 1) % 126
			if sess.hunterDroidActive {
				if sess.hunterDroidView != 0 {
					s.hostBroadcastJournaled(room, 204, map[byte]any{
						ParamPlayerNr:  HostActorNr,
						ParamEventData: map[any]any{byte(0): sess.hunterDroidView},
					}, true)
				}
				sess.hunterDroidActive = false
				sess.hunterDroidView = 0
			}
			sess.skillPhaseNext = 0
		}
	}
	return lethal
}

func (s *EnetServer) hostKillWithDeathAnim(room *Room, sess *ClientSession, extraDelay time.Duration) {
	sess.statDeaths++
	sess.respawnPending = true
	sess.downTaskUID = (sess.downTaskUID + 1) % 126
	s.hostSendPlayerDieTask(room, sess.avatarBase, sess.downTaskUID)
	room.pendingPlayerRespawns = append(room.pendingPlayerRespawns, pendingPlayerRespawn{
		sess: sess, actorNr: room.ActorNrFor(sess.userID), at: time.Now().Add(npcDeathRemoveDelay + extraDelay),
	})
}

func (s *EnetServer) hostSendPlayerDieTask(room *Room, avatarBase, taskUID int32) {
	s.broadcastEvent(room, 201, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): int32(msNow()),
			Short(1): map[any]any{
				byte(0): avatarBase + 2,
				byte(1): playerBhvTaskStream(taskUID, bhvTaskDie, 0, 0, false),
			},
		},
	}, nil, nil)
}

func (s *EnetServer) hostRunPendingPlayerRespawns(room *Room) {
	if len(room.pendingPlayerRespawns) == 0 {
		return
	}
	now := time.Now()
	remaining := room.pendingPlayerRespawns[:0]
	for _, p := range room.pendingPlayerRespawns {
		if now.Before(p.at) {
			remaining = append(remaining, p)
			continue
		}
		p.sess.respawnPending = false
		s.hostSpawnAvatar(room, p.sess, p.actorNr, nil)
	}
	room.pendingPlayerRespawns = remaining
}

func (s *EnetServer) hostRunPendingPlayerUnstuns(room *Room) {
	if len(room.pendingPlayerUnstuns) == 0 {
		return
	}
	now := time.Now()
	remaining := room.pendingPlayerUnstuns[:0]
	for _, p := range room.pendingPlayerUnstuns {
		if now.Before(p.at) {
			remaining = append(remaining, p)
			continue
		}
		if p.sess.avatarBase == 0 || p.sess.isDead || p.sess.respawnPending {
			continue
		}
		p.sess.stunTaskUID = (p.sess.stunTaskUID + 1) % 126
		s.broadcastEvent(room, 201, map[byte]any{
			ParamPlayerNr: HostActorNr,
			ParamEventData: map[any]any{
				byte(0): int32(msNow()),
				Short(1): map[any]any{
					byte(0): p.sess.avatarBase + 2,
					byte(1): playerBhvTaskStream(p.sess.stunTaskUID, bhvTaskPlayerDefault, 0, -1, false),
				},
			},
		}, nil, nil)
	}
	room.pendingPlayerUnstuns = remaining
}

const (
	playerSpawnFxDelay = 500 * time.Millisecond
	playerSpawnFxDuration = 7 * time.Second
)

func (s *EnetServer) hostRunPendingPlayerSpawnFx(room *Room) {
	if len (room.pendingPlayerSpawnFx) == 0 {
		return
	}

	now := time.Now()
	remaining := room.pendingPlayerSpawnFx[:0]
	for _, p:= range room.pendingPlayerSpawnFx {
		if now.Before(p.at) {
			remaining = append(remaining, p)
			continue
		}
		if p.sess.avatarBase != p.avatarBase || p.sess.isDead || p.sess.respawnPending {
			continue
		}
		p.sess.stunTaskUID = (p.sess.stunTaskUID + 1) % 126
		s.broadcastEvent(room, 201, map[byte]any {
			ParamPlayerNr:HostActorNr,
			ParamEventData: map[any]any {
				byte(0): int32(msNow()),
				Short(1): map[any]any {
					byte(0): p.avatarBase + 2,
					byte(1): playerBhvTaskStream(p.sess.stunTaskUID, bhvTaskPlayerSpawn, 0, -1, false),
				},
			},
		}, nil, nil)
		room.pendingPlayerUnstuns = append(room.pendingPlayerUnstuns, pendingPlayerUnstun{
			sess: p.sess, at: now.Add(playerSpawnFxDuration),
		})
		 log.Printf("[HOST] actor view=%d spawn fx (PlayerSpawn task) room=%s", p.avatarBase, room.Name)
	}
	room.pendingPlayerSpawnFx = remaining
}


func playerBhvTaskStream(uid, taskType int32, durationSec float32, phase int32, hasKilledNPC bool) []any {
	return []any{
		int32(1),
		uid,
		taskType,
		int32(0),
		false,
		int32(-1),
		int32(-1),
		int32(0),
		int32(0),
		int32(0),
		int32(-1),
		durationSec,
		phase,
		int32(-1),
		int32(-1),
		hasKilledNPC,
		false,
		false,
		vector3Custom(0, 0, 0),
		vector3Custom(0, 0, 0),
		vector3Custom(0, 0, 0),
		vector3Custom(0, 0, 0),
		int32(-1),
		"",
	}
}

func (s *EnetServer) hostTickPlayerDownTimeout(room *Room) {
	now := time.Now()
	for actorNr, uid := range room.Players {
		sess := s.sessionInRoom(uid, room.Name)
		if sess == nil || !sess.isDead || sess.avatarBase == 0 {
			continue
		}
		if now.Sub(sess.downedAt) < ffylDuration {
			continue
		}
		log.Printf("[HOST] actor view=%d FFYL timer expired with no revive, forcing respawn room=%s", sess.avatarBase, room.Name)
		sess.statDeaths++
		s.hostSpawnAvatar(room, sess, actorNr, nil)
	}
}

func sessionByAvatarBase(s *EnetServer, room *Room, view int32) *ClientSession {
	for _, uid := range room.Players {
		if sess := s.sessionInRoom(uid, room.Name); sess != nil && sess.avatarBase == view {
			return sess
		}
	}
	return nil
}

func (s *EnetServer) hostRevivePlayer(room *Room, sess *ClientSession, hasKilledNPC bool) {
	if !sess.isDead || sess.avatarBase == 0 {
		return
	}
	sess.isDead = false
	if sess.maxHP > 0 {
		sess.hp = sess.maxHP / 2
	}

	rpcView := vitalsRPCView(room, sess.avatarBase)
	s.hostSendRPC(room, rpcView, "RPC_RequestAllLifeAndShield", []any{
		map[int]any{0: int32(1), 1: rpcView, 2: playerLifeTable(sess.hp, sess.maxHP, sess.shield, sess.maxShield)},
	})
	sess.downTaskUID = (sess.downTaskUID + 1) % 126

	s.hostSendReviveCancel(room, sess.avatarBase, sess.downTaskUID, hasKilledNPC)

	for _, d := range []time.Duration{300 * time.Millisecond, 1 * time.Second} {
		room.pendingReviveCancels = append(room.pendingReviveCancels, pendingReviveCancel{
			sess: sess, avatarBase: sess.avatarBase, downTaskUID: sess.downTaskUID, hasKilledNPC: hasKilledNPC, at: time.Now().Add(d),
		})
	}
	log.Printf("[HOST] actor view=%d revived, hp=%d/%d room=%s", sess.avatarBase, sess.hp, sess.maxHP, room.Name)
}

func (s *EnetServer) hostSendReviveCancel(room *Room, avatarBase int32, downTaskUID int32, hasKilledNPC bool) {
	s.broadcastEvent(room, 201, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): int32(msNow()),
			Short(1): map[any]any{
				byte(0): avatarBase + 2,
				byte(1): playerBhvTaskStream(downTaskUID, bhvTaskPlayerRevive, 0, -1, hasKilledNPC),
			},
		},
	}, nil, nil)
}

func (s *EnetServer) hostRunPendingReviveCancels(room *Room) {
	if len(room.pendingReviveCancels) == 0 {
		return
	}
	now := time.Now()
	remaining := room.pendingReviveCancels[:0]
	for _, p := range room.pendingReviveCancels {
		if now.Before(p.at) {
			remaining = append(remaining, p)
			continue
		}

		if p.sess.avatarBase != p.avatarBase || p.sess.isDead || p.sess.downTaskUID != p.downTaskUID {
			continue
		}
		s.hostSendReviveCancel(room, p.avatarBase, p.downTaskUID, p.hasKilledNPC)
	}
	room.pendingReviveCancels = remaining
}

func (s *EnetServer) hostTriggerPlayerSkill(room *Room, sess *ClientSession) {
	phase := sess.skillPhaseNext

	if sess.isDead {
		log.Printf("[HOST] actor view=%d PlayerSkill rejected: in FFYL room=%s", sess.avatarBase, room.Name)
		return
	}
	if phase == 0 && time.Now().Before(sess.skillCooldownEndAt) {
		log.Printf("[HOST] actor view=%d PlayerSkill rejected: on cooldown (%.1fs left) room=%s", sess.avatarBase, time.Until(sess.skillCooldownEndAt).Seconds(), room.Name)
		return
	}
	if phase == 0 {
		sess.statSkillUses++
	}
	sess.skillTaskUID = (sess.skillTaskUID + 1) % 126
	sess.skillPhaseNext = 1 - sess.skillPhaseNext
	s.broadcastEvent(room, 201, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): int32(msNow()),
			Short(1): map[any]any{
				byte(0): sess.avatarBase + 2,
				byte(1): playerBhvTaskStream(sess.skillTaskUID, bhvTaskPlayerSkill, 0, phase, false),
			},
		},
	}, nil, nil)
	log.Printf("[HOST] actor view=%d triggered PlayerSkill phase=%d room=%s", sess.avatarBase, phase, room.Name)

	if sess.hostChar != nil && avatarClassName(sess.hostChar) == "Siren" {
		if phase == 0 {
			room.pendingFireBirdSpawns = append(room.pendingFireBirdSpawns, pendingFireBirdSpawn{
				sess: sess, at: time.Now().Add(fireBirdSpawnDelay),
			})
		} else {
			s.hostDetonateFireBird(room, sess)
		}
	}

	if sess.hostChar != nil && avatarClassName(sess.hostChar) == "Hunter" {
		if phase == 0 {
			room.pendingHunterDroidSpawns = append(room.pendingHunterDroidSpawns, pendingHunterDroidSpawn{
				sess: sess, at: time.Now().Add(hunterDroidSpawnDelay),
			})
		} else {
			s.hostRetrieveHunterDroid(room, sess, true)
		}
	}

	if sess.hostChar != nil && avatarClassName(sess.hostChar) == "Soldier" {
		if phase == 0 {
			room.pendingTurretSpawns = append(room.pendingTurretSpawns, pendingTurretSpawn{
				sess: sess, at: time.Now().Add(turretSpawnDelay),
			})
		} else {
			s.hostRetrieveTurret(room, sess, true)
		}
	}

	if phase == 0 && sess.hostChar != nil && avatarClassName(sess.hostChar) == "Berserker" {
		dur := time.Duration(float64(berserkDuration) * (1 + effectValue(sess.hostChar, "BerserkRageDuration")))
		sess.berserkActive = true
		sess.berserkEndAt = time.Now().Add(dur)
		if pos, ok := room.playerPos[sess.avatarBase+1]; ok {
			radius := berserkStompRadius * (1 + float32(effectValue(sess.hostChar, "BerserkStompRadius")))
			dmg := int32(float64(berserkStompDamage) * (1 + effectValue(sess.hostChar, "BerserkStompDamage")))
			s.hostAreaDamageNpcs(room, sess.avatarBase, pos, radius, dmg, elementNone, false)
		}
	}

	if phase == 1 && sess.hostChar != nil && avatarClassName(sess.hostChar) == "Berserker" {
		sess.berserkActive = false
		sess.skillCooldownEndAt = time.Now().Add(s.berserkCooldown(sess.hostChar))
		refillHP := effectValue(sess.hostChar, "SkillExitRefillHP")
		refillShield := effectValue(sess.hostChar, "SkillExitRefillShield")
		if refillHP > 0 || refillShield > 0 {
			if sess.maxHP > 0 {
				sess.hp += int32(float64(sess.maxHP) * refillHP)
				if sess.hp > sess.maxHP {
					sess.hp = sess.maxHP
				}
			}
			if sess.maxShield > 0 {
				sess.shield += int32(float64(sess.maxShield) * refillShield)
				if sess.shield > sess.maxShield {
					sess.shield = sess.maxShield
				}
				sess.shieldRegenBaseline = sess.shield
			}
			s.hostSendRPC(room, sess.avatarBase, "RPC_RequestAllLifeAndShield", []any{
				map[int]any{0: int32(1), 1: sess.avatarBase, 2: playerLifeTable(sess.hp, sess.maxHP, sess.shield, sess.maxShield)},
			})
		}
	}
}

func (s *EnetServer) hostResetSkillPhaseAfterTimeout(room *Room, sess *ClientSession) {
	sess.skillTaskUID = (sess.skillTaskUID + 1) % 126
	sess.skillPhaseNext = 0
	s.broadcastEvent(room, 201, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): int32(msNow()),
			Short(1): map[any]any{
				byte(0): sess.avatarBase + 2,
				byte(1): playerBhvTaskStream(sess.skillTaskUID, bhvTaskPlayerSkill, 0, 1, false),
			},
		},
	}, nil, nil)
}

func (s *EnetServer) hostPlayerMelee(room *Room, sess *ClientSession) {
	ppos, ok := room.playerPos[sess.avatarBase+1]
	if !ok {
		return
	}
	yaw := sess.lastYawDeg * math.Pi / 180
	fx, fz := math.Sin(yaw), math.Cos(yaw)

	ratio := s.PlayerMeleeDamageRatio
	if ratio <= 0 {
		ratio = 1.5
	}
	dmg := int32(s.playerWeaponDamageAtLevel(sess) * ratio * (1 + berserkDespBonus(sess)))

	if sess.berserkActive && time.Now().Before(sess.berserkEndAt) {
		if punches := effectValue(sess.hostChar, "BerserkPunchCount"); punches > 0 {
			dmg = int32(float64(dmg) * (1 + punches))
		}
	}
	if dmg < 1 {
		dmg = 1
	}

	hits := 0
	for i := range room.npcs {
		npc := &room.npcs[i]
		if npc.hp <= 0 || npc.kit == "ClapTrapTutor" {
			continue
		}
		dx := float64(npc.pos[0] - ppos[0])
		dz := float64(npc.pos[2] - ppos[2])
		dist := math.Hypot(dx, dz)
		if dist > playerMeleeRange || dist < 0.01 {
			continue
		}
		if (dx/dist)*fx+(dz/dist)*fz < playerMeleeConeCos {
			continue
		}
		if s.hostDamageNpcByView(room, sess.avatarBase, npc.viewBase, dmg, 0, elementNone, false) {
			hits++
		}
	}
	log.Printf("[HOST] player melee actor=%d dmg=%d hits=%d room=%s", room.ActorNrFor(sess.userID), dmg, hits, room.Name)
}

const (
	defaultFireBirdCooldown    = 30 * time.Second
	defaultBerserkCooldown     = 30 * time.Second
	defaultTurretCooldownShort = 10 * time.Second
	defaultTurretCooldownLong  = 30 * time.Second
	defaultHunterCooldownShort = 15 * time.Second
	defaultHunterCooldownLong  = 20 * time.Second
)

const (
	berserkDuration    = 15 * time.Second
	berserkStompRadius = float32(5)
	berserkStompDamage = int32(60)
)

func (s *EnetServer) fireBirdCooldown() time.Duration {
	if s.FireBirdCooldownSeconds > 0 {
		return time.Duration(s.FireBirdCooldownSeconds * float64(time.Second))
	}
	return defaultFireBirdCooldown
}

func (s *EnetServer) berserkCooldown(ch *domain.Character) time.Duration {
	base := defaultBerserkCooldown
	if s.BerserkCooldownSeconds > 0 {
		base = time.Duration(s.BerserkCooldownSeconds * float64(time.Second))
	}
	return time.Duration(float64(base) * (1 + skillCooldownScale(ch, "BerserkCoolDownDuration")))
}

func (s *EnetServer) turretCooldownShort() time.Duration {
	if s.TurretCooldownShortSeconds > 0 {
		return time.Duration(s.TurretCooldownShortSeconds * float64(time.Second))
	}
	return defaultTurretCooldownShort
}

func (s *EnetServer) turretCooldownLong(ch *domain.Character) time.Duration {
	base := defaultTurretCooldownLong
	if s.TurretCooldownLongSeconds > 0 {
		base = time.Duration(s.TurretCooldownLongSeconds * float64(time.Second))
	}
	return time.Duration(float64(base) * (1 + skillCooldownScale(ch, "TurretCooldown")))
}

func (s *EnetServer) hunterCooldownShort() time.Duration {
	if s.HunterCooldownShortSeconds > 0 {
		return time.Duration(s.HunterCooldownShortSeconds * float64(time.Second))
	}
	return defaultHunterCooldownShort
}

func (s *EnetServer) hunterCooldownLong(ch *domain.Character) time.Duration {
	base := defaultHunterCooldownLong
	if s.HunterCooldownLongSeconds > 0 {
		base = time.Duration(s.HunterCooldownLongSeconds * float64(time.Second))
	}
	return time.Duration(float64(base) * (1 + skillCooldownScale(ch, "DroidCooldown")))
}

const (
	skillEventTurretSpawned       = 28
	skillEventTurretCooldownStart = 30
	skillEventHunterDroidSpawned  = 33
	skillEventHunterDroidEnd      = 34
	skillEventHunterCooldownStart = 35
)

const fireBirdSpawnDelay = 600 * time.Millisecond

func (s *EnetServer) hostRunPendingFireBirdSpawns(room *Room) {
	if len(room.pendingFireBirdSpawns) == 0 {
		return
	}
	now := time.Now()
	remaining := room.pendingFireBirdSpawns[:0]
	for _, p := range room.pendingFireBirdSpawns {
		if now.Before(p.at) {
			remaining = append(remaining, p)
			continue
		}
		if p.sess.avatarBase != 0 && !p.sess.isDead {
			s.hostSpawnFireBird(room, p.sess)
		}
	}
	room.pendingFireBirdSpawns = remaining
}

func (s *EnetServer) hostSpawnFireBird(room *Room, sess *ClientSession) {
	base := hostViewBase + 1000 + room.npcViewCtr*10
	room.npcViewCtr++
	pos, ok := room.playerPos[sess.avatarBase+1]
	if !ok {
		return
	}
	baseDmg := s.playerWeaponDamageAtLevel(sess)
	sess.fireBirdBaseDamage = baseDmg
	s.hostFireBirdIgnitePath(room, sess, pos, baseDmg)
	angles := []any{int32(rand.Intn(360)), int32(rand.Intn(360)), int32(rand.Intn(360))}
	s.hostBroadcastJournaled(room, 202, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): "BKT|NPC|BloodWing",
			byte(1): vector3Custom(pos[0], pos[1]+1.5, pos[2]),
			byte(4): IntArray{Items: []int32{base}},
			byte(5): append([]any{sess.avatarBase}, angles...),
			byte(6): int32(msNow()),
			byte(7): base,
		},
	}, true)
	sess.fireBirdActive = true
	dur := time.Duration(float64(fireBirdDuration) * (1 + effectValue(sess.hostChar, "FirebirdDuration")))
	sess.fireBirdEndAt = time.Now().Add(dur)
	sess.fireBirdLastTick = time.Time{} // eligible to tick immediately
	log.Printf("[HOST] actor view=%d spawned FireBird view=%d baseDmg=%.1f room=%s", sess.avatarBase, base, baseDmg, room.Name)
}

const (
	fireBirdFireRadius      = float32(3)
	fireBirdDuration        = 10 * time.Second
	fireBirdExplosionRadius = float32(5)

	fireBirdBaseDamagePct = 0.45
	fireBirdExplosionPct  = 0.80

	fireBirdDotTickInterval = 1 * time.Second
)

func (s *EnetServer) hostTickFireBirds(room *Room) {
	now := time.Now()
	for _, uid := range room.Players {
		sess := s.sessionInRoom(uid, room.Name)
		if sess == nil || !sess.fireBirdActive || sess.avatarBase == 0 {
			continue
		}
		ppos, ok := room.playerPos[sess.avatarBase+1]
		if !ok {
			continue
		}
		if now.After(sess.fireBirdEndAt) {
			sess.fireBirdActive = false
			cd := time.Duration(float64(s.fireBirdCooldown()) * (1 + effectValue(sess.hostChar, "FirebirdExplodeCooldown")))
			sess.skillCooldownEndAt = now.Add(cd)
			s.hostResetSkillPhaseAfterTimeout(room, sess)
			explDmg := int32(sess.fireBirdBaseDamage * fireBirdExplosionPct * (1 + effectValue(sess.hostChar, "FirebirdExplosionDamage")))
			s.hostAreaDamageNpcs(room, sess.avatarBase, ppos, fireBirdExplosionRadius, explDmg, elementFire, false)
			s.hostFireBirdExplosionExtras(room, sess, ppos, explDmg)
			log.Printf("[HOST] actor view=%d FireBird exploded dmg=%d room=%s", sess.avatarBase, explDmg, room.Name)
			continue
		}
		if now.Sub(sess.fireBirdLastTick) < fireBirdDotTickInterval {
			continue
		}
		sess.fireBirdLastTick = now
		dotDmg := int32(sess.fireBirdBaseDamage * fireBirdBaseDamagePct)
		s.hostAreaDamageNpcs(room, sess.avatarBase, ppos, fireBirdFireRadius, dotDmg, elementFire, true)
	}
}

const fireBirdChainRadius = float32(2)

func (s *EnetServer) hostFireBirdExplosionExtras(room *Room, sess *ClientSession, center [3]float32, explDmg int32) {
	if refillPct := effectValue(sess.hostChar, "RefillShield"); refillPct > 0 && sess.maxShield > 0 {
		sess.shield += int32(float64(sess.maxShield) * refillPct)
		if sess.shield > sess.maxShield {
			sess.shield = sess.maxShield
		}
		sess.shieldRegenBaseline = sess.shield
		s.hostSendRPC(room, sess.avatarBase, "RPC_RequestAllLifeAndShield", []any{
			map[int]any{0: int32(1), 1: sess.avatarBase, 2: playerLifeTable(sess.hp, sess.maxHP, sess.shield, sess.maxShield)},
		})
	}
	chainNum := int(effectValue(sess.hostChar, "FirebirdExplodeChainNum"))
	if chainNum <= 0 {
		return
	}
	r2 := fireBirdChainRadius * fireBirdChainRadius
	hit := 0
	for i := range room.npcs {
		if hit >= chainNum {
			break
		}
		npc := &room.npcs[i]
		if npc.hp <= 0 || npc.friendly {
			continue
		}
		dx, dz := npc.pos[0]-center[0], npc.pos[2]-center[2]
		if dx*dx+dz*dz > r2 || (dx == 0 && dz == 0) {
			continue // skip the exact center point, already hit by the main blast
		}
		s.hostDamageNpcByView(room, sess.avatarBase, npc.viewBase, explDmg, 0, elementFire, false)
		hit++
	}
}

func (s *EnetServer) hostDetonateFireBird(room *Room, sess *ClientSession) {
	if !sess.fireBirdActive {
		return
	}
	sess.fireBirdActive = false
	cd := time.Duration(float64(s.fireBirdCooldown()) * (1 + effectValue(sess.hostChar, "FirebirdExplodeCooldown")))
	sess.skillCooldownEndAt = time.Now().Add(cd)
	if ppos, ok := room.playerPos[sess.avatarBase+1]; ok {
		explDmg := int32(sess.fireBirdBaseDamage * fireBirdExplosionPct * (1 + effectValue(sess.hostChar, "FirebirdExplosionDamage")))
		s.hostAreaDamageNpcs(room, sess.avatarBase, ppos, fireBirdExplosionRadius, explDmg, elementFire, false)
		s.hostFireBirdExplosionExtras(room, sess, ppos, explDmg)
	}
	log.Printf("[HOST] actor view=%d manually detonated FireBird room=%s", sess.avatarBase, room.Name)
}

const fireBirdThrowDistance = 15.0

func (s *EnetServer) hostFireBirdIgnitePath(room *Room, sess *ClientSession, castPos [3]float32, baseDmg float64) {
	yawRad := sess.lastYawDeg * math.Pi / 180
	dirX, dirZ := float32(math.Sin(yawRad)), float32(math.Cos(yawRad))
	end := [3]float32{
		castPos[0] + dirX*fireBirdThrowDistance,
		castPos[1],
		castPos[2] + dirZ*fireBirdThrowDistance,
	}
	setting, _ := elementalDOTFor(elementFire) // always present. See elementalDOTTable
	dotDPS := int32(baseDmg * fireBirdBaseDamagePct)
	now := time.Now()
	for i := range room.npcs {
		npc := &room.npcs[i]
		if npc.hp <= 0 || npc.friendly {
			continue
		}
		if d, _ := segPointDistXZ(castPos, end, npc.pos[0], npc.pos[2]); d > fireBirdFireRadius {
			continue
		}
		npc.dotElement = elementFire
		npc.dotAttackerView = sess.avatarBase
		npc.dotDPS = dotDPS
		npc.dotEndAt = now.Add(setting.duration)
		if npc.dotNextTickAt.IsZero() || npc.dotNextTickAt.Before(now) {
			npc.dotNextTickAt = now.Add(elementalDOTTickInterval)
		}
		log.Printf("[HOST] actor view=%d FireBird throw ignites npc %d (%s) room=%s",
			sess.avatarBase, npc.viewBase, npc.kit, room.Name)
	}
}

type elementalDOTSetting struct {
	procChance float64
	duration   time.Duration
	dpsRatio   float64
}

var elementalDOTTable = map[int32]elementalDOTSetting{
	elementFire:      {procChance: 0.25, duration: 5 * time.Second, dpsRatio: 0.5},
	elementShock:     {procChance: 0.15, duration: 2 * time.Second, dpsRatio: 0.7},
	elementCorrosive: {procChance: 0.35, duration: 8 * time.Second, dpsRatio: 0.3},
}

func elementalDOTFor(element int32) (elementalDOTSetting, bool) {
	s, ok := elementalDOTTable[element]
	return s, ok
}

const elementalDOTTickInterval = 500 * time.Millisecond

func npcMaybeStartElementalDOT(npc *spawnedNpc, attackerView, element, hitDamage int32, now time.Time, procBoost float64, forceProc bool) {
	setting, ok := elementalDOTFor(element)
	if !ok {
		return // elementNone or elementExplosive, no persistent status for these
	}
	alreadyActive := npc.dotElement == element && now.Before(npc.dotEndAt)
	if !alreadyActive && !forceProc && rand.Float64() > setting.procChance*(1+procBoost) {
		return
	}
	npc.dotElement = element
	npc.dotAttackerView = attackerView
	npc.dotDPS = int32(float64(hitDamage) * setting.dpsRatio)
	if npc.dotDPS < 1 {
		npc.dotDPS = 1
	}
	npc.dotEndAt = now.Add(setting.duration)
	if npc.dotNextTickAt.IsZero() || npc.dotNextTickAt.Before(now) {
		npc.dotNextTickAt = now.Add(elementalDOTTickInterval)
	}
}

func (s *EnetServer) hostTickElementalDOT(room *Room) {
	now := time.Now()
	for i := range room.npcs {
		npc := &room.npcs[i]
		if npc.dotElement == elementNone {
			continue
		}
		if npc.hp <= 0 || now.After(npc.dotEndAt) {
			npc.dotElement = elementNone
			continue
		}
		if now.Before(npc.dotNextTickAt) {
			continue
		}
		npc.dotNextTickAt = now.Add(elementalDOTTickInterval)
		tickDmg := npc.dotDPS / int32(time.Second/elementalDOTTickInterval)
		if tickDmg < 1 {
			tickDmg = 1
		}
		s.hostDamageNpcByView(room, npc.dotAttackerView, npc.viewBase, tickDmg, 0, npc.dotElement, true)
	}
}

func (s *EnetServer) hostAreaDamageNpcs(room *Room, attackerView int32, center [3]float32, radius float32, damage, element int32, isDOT bool) {
	r2 := radius * radius
	for i := range room.npcs {
		npc := &room.npcs[i]
		if npc.hp <= 0 || npc.friendly {
			continue
		}
		dx, dz := center[0]-npc.pos[0], center[2]-npc.pos[2]
		if dx*dx+dz*dz > r2 {
			continue
		}
		s.hostDamageNpcByView(room, attackerView, npc.viewBase, damage, 0, element, isDOT)
	}
}

func (s *EnetServer) hostSendRPC(room *Room, viewID int32, name string, args []any) {
	s.broadcastEvent(room, 200, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): viewID,
			byte(2): int32(msNow()),
			byte(3): name,
			byte(4): args,
		},
	}, nil, nil)
}

func (s *EnetServer) hostSendRPCTo(sess *ClientSession, viewID int32, name string, args []any) {
	s.sendEventTo(sess, 200, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): viewID,
			byte(2): int32(msNow()),
			byte(3): name,
			byte(4): args,
		},
	})
}

func clampByte(v int32) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

func clampShort(v int32) Short {
	if v < -32768 {
		return -32768
	}
	if v > 32767 {
		return 32767
	}
	return Short(v)
}

func missionTimerRemaining(room *Room) float64 {
	if !room.mtActive {
		return 0
	}
	if room.mtRunning {
		remaining := room.mtDuration - (float64(msNow())/1000 - room.mtStart)
		if remaining < 0 {
			return 0
		}
		return remaining
	}
	return room.mtDuration - (room.mtPause - room.mtStart)
}

func (s *EnetServer) roomTopDamage(room *Room) int32 {
	var top int32 = 1
	for _, uid := range room.Players {
		if target := s.sessionInRoom(uid, room.Name); target != nil && target.statTotalDamage > top {
			top = target.statTotalDamage
		}
	}
	return top
}

func matchScore(room *Room, sess *ClientSession, topDamage int32) int32 {
	if topDamage <= 0 {
		topDamage = 1
	}
	share := float64(sess.statTotalDamage) / float64(topDamage)
	timeBonus := missionTimerRemaining(room) * scoringPVE.TimeScoreConversionRate
	return int32(timeBonus * share)
}

func (s *EnetServer) sessionStatsPayload(room *Room) []any {
	loadScoringConfig()
	curScn := getCurScnFromProps(room.Name, room.Properties)
	duration := time.Since(room.CreatedAt)
	isTeamMode := isTeamGameMode(asInt32(room.Properties["GameMode"]))
	timeLimit, endScore := int32(0), int32(0)
	if isTeamMode {
		remaining := s.claptrapRemainingSeconds(room)
		if remaining < claptrapMinDisplaySeconds {
			remaining = claptrapMinDisplaySeconds
		}
		timeLimit = int32(duration.Seconds()) + remaining
		endScore = s.claptrapTargetScore()
	}
	out := []any{
		curScn,
		asInt32(room.Properties["PlaylistId"]),
		byte(asInt32(room.Properties["Difficulty"])),
		float32(duration.Seconds()),
		timeLimit,
		endScore,
		Short(0),
		byte(1),
		wireTeamID(room.claptrapLastZoneOwnerTeam, isTeamMode),
		room.claptrapLastScoreValue,
		gamemodeName(room),
	}
	guildExp := int32(guildInstanceExp(room, duration))
	topDamage := s.roomTopDamage(room)

	var sheets [][]any
	for _, uid := range room.Players {
		target := s.sessionInRoom(uid, room.Name)
		if target == nil || target.hostChar == nil {
			continue
		}
		sheets = append(sheets, []any{
			int32(target.hostChar.CharID),
			int32(wireTeamID(target.teamID, isTeamMode)),
			false,
			displayNameFor(target, target.hostChar),
			int32(target.hostChar.Level),
			Short(0), Short(0), Short(0),
			false, false,
			clampShort(matchScore(room, target, topDamage)), Short(0),
			clampByte(target.statKills), clampByte(target.statCriticalKills), clampByte(target.statDeaths),
			target.statTotalDamage, target.statCritDmg, int32(0),
			clampByte(target.statBadassKills), clampByte(target.statBossKills),
			clampShort(target.statHits), clampShort(target.statCriticalHits), Short(0), Short(0), Short(0), Short(0), clampShort(target.statCriticalKills), clampShort(target.statSkillUses), // hit/critHit/sameVictim/meleeKill/nemesis/grenadeKill/headshotKill/skill counts
			target.statExtraExp, target.statExtraMoney, int32(0), guildExp,
			byte(0), byte(0), byte(0),
			byte(0),
			float32(0), float32(0), float32(0), float32(0),
			byte(0),
			byte(0),
		})
	}
	if isTeamMode {
		for team := int32(0); team < 2; team++ {
			wireID := int32(wireTeamID(byte(team), true))
			score := room.claptrapTeamScore[team]
			if team == int32(room.claptrapLastScoreTeam) {
				score -= int32(room.claptrapLastScoreValue)
			}
			sheets = append(sheets, []any{
				wireID, wireID, true, "", int32(0),
				Short(0), Short(0), Short(0),
				false, false,
				clampShort(score), Short(0),
				byte(0), byte(0), byte(0),
				int32(0), int32(0), int32(0),
				byte(0), byte(0),
				Short(0), Short(0), Short(0), Short(0), Short(0), Short(0), Short(0), Short(0),
				int32(0), int32(0), int32(0), int32(0),
				byte(0), byte(0), byte(0),
				byte(0),
				float32(0), float32(0), float32(0), float32(0),
				byte(0),
				byte(0),
			})
		}
	}
	out = append(out, byte(len(sheets))) // [11] sheet count (byte)
	for _, sh := range sheets {
		out = append(out, sh...)
	}
	return out
}

func (s *EnetServer) hostSendUIMessage(room *Room, text string, duration float32) {
	s.hostSendRPC(room, sessionInfoViewID, "RPC_RequestDisplayUIMessage",
		[]any{false, float32(0), duration, text, byte(0)})
}

var starterWeaponBlob, _ = hex.DecodeString(
	"0200000001000000010000000000000000000000000000000000000000000000" +
		"0900000000000000000001000000020000000300000004000000050000000600" +
		"0000070000000800000000000600000053004d004700")

func starterWeaponItemDNA() Custom {
	return Custom{Code: 66, Data: starterWeaponBlob}
}

func weaponItemDNA(it *domain.InventoryItem) Custom {
	name := utf16LEBytes(it.ItemName)
	buf := make([]byte, 0, 36+9*4+len(name))
	putI32 := func(v int32) { buf = binary.LittleEndian.AppendUint32(buf, uint32(v)) }
	putI32(2) // ItemType.Weapon
	putI32(int32(it.ResourceID))
	putI32(int32(it.Level))
	putI32(int32(it.PartA)) // WeaponType
	putI32(int32(it.Rarity))
	putI32(int32(it.GenerationSource))
	putI32(0) // starLevel
	putI32(0) // starExperience
	putI32(9) // subPartsCode.Length
	sub := weaponItemSubparts(it)
	for i := int32(0); i < 9; i++ {
		putI32(int32(it.PartA)<<24 | i<<16 | (int32(sub[i]) & 0xFFFF))
	}
	putI32(0) // extraEffectCount
	putI32(int32(len(name)))
	buf = append(buf, name...)
	return Custom{Code: 66, Data: buf}
}

const weaponSubPartMaterial = 8

func utf16LEBytes(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(out[i*2:], u)
	}
	return out
}

type weaponSlotEntity struct {
	view   int32
	dna    Custom
	itemID int64
}

func equippedWeaponsOrdered(charID int64) []*domain.InventoryItem {
	items, err := db.GetInventory(charID)
	if err != nil {
		return nil
	}
	var out []*domain.InventoryItem
	for _, it := range items {
		if it.IsEquipped && it.ItemKind == domain.ItemTypeWeapon {
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })
	return out
}

func backpackItemsOrdered(charID int64) []*domain.InventoryItem {
	items, err := db.GetInventory(charID)
	if err != nil {
		return nil
	}
	var out []*domain.InventoryItem
	for _, it := range items {
		if !it.IsEquipped {
			out = append(out, it)
		}
	}
	return out
}

const sessionOutfitSlotCount = 5

func equippedOutfitsBySlot(charID int64) [sessionOutfitSlotCount]*domain.InventoryItem {
	var out [sessionOutfitSlotCount]*domain.InventoryItem
	items, err := db.GetInventory(charID)
	if err != nil {
		return out
	}
	for _, it := range items {
		if it.IsEquipped && it.ItemKind == domain.ItemTypeOutfit && it.Slot >= 0 && it.Slot < sessionOutfitSlotCount {
			out[it.Slot] = it
		}
	}
	return out
}

func outfitRPCArgs(charID int64) []any {
	args := make([]any, sessionOutfitSlotCount)
	for i, it := range equippedOutfitsBySlot(charID) {
		if it != nil {
			args[i] = outfitItemDNA(it)
		}
	}
	return args
}

func avatarClassName(ch *domain.Character) string {
	class := "Siren"
	switch ch.AvatarID {
	case 1242413768:
		class = "Siren"
	case 1267549099:
		class = "Soldier"
	case 3556519174:
		class = "Berserker"
	case 3169502637:
		class = "Hunter"
	default:
		switch ch.ClassType {
		case 1:
			class = "Soldier"
		case 2:
			class = "Berserker"
		case 3:
			class = "Hunter"
		}
	}
	return class
}

func avatarPrefab(ch *domain.Character, town bool) string {
	class := avatarClassName(ch)
	if town {
		return "Entities/Player_Mecanim/" + class + "/CHR_Town_" + class + "_Root"
	}
	return "Entities/Player_Mecanim/" + class + "/CHR_" + class + "_Root"
}

func (s *EnetServer) hostSpawnStaticEntities(room *Room) {
	if room.staticsSpawned {
		return
	}
	room.staticsSpawned = true
	level := getCurScnFromProps(room.Name, room.Properties)
	nowMS := int32(msNow())

	spawnStatic := func(i int32, prefab string, x, y, z float32, rot []float32) {
		base := hostViewBase + 300 + i*10 // 3 views each disjoint range
		views := []int32{base, base + 1, base + 2}
		data := map[any]any{
			byte(0): prefab,
			byte(1): vector3Custom(x, y, z),
			byte(4): IntArray{Items: views},
			byte(6): nowMS,
			byte(7): views[0],
		}
		if len(rot) == 4 {
			data[byte(2)] = quaternionCustom(rot[0], rot[1], rot[2], rot[3])
		}
		s.hostBroadcastJournaled(room, 202, map[byte]any{
			ParamPlayerNr:  HostActorNr,
			ParamEventData: data,
		}, false)
	}

	i := int32(0)
	for _, e := range levelSpawnEntries(level) {
		if e.Script != "StaticEntitySpawner" || len(e.Pos) != 3 {
			continue
		}
		prefab := "Entities/Object/ExplosiveDestructibleObj/ExposiveDestructible_BarrelXML"
		isDestoryable := strings.Contains(e.GO, "Destroyable")
		d := destroyableSettingsFor(level, e.GO)
		if d != nil {
			isDestoryable = true
		}
		if isDestoryable {
			prefab = "Entities/Object/DestoryableObj/DestoryableObj"
		}
		spawnStatic(i, prefab, e.Pos[0], e.Pos[1], e.Pos[2], e.Rot)
		hp, receiveShoot, receiveMelee, receiveArea := int32(100), true, true, true
		if d != nil {
			hp, receiveShoot, receiveMelee, receiveArea = d.InitHP, d.ReceiveShoot, d.ReceiveMelee, d.ReceiveArea
		}
		room.statics = append(room.statics, staticEnt{
			viewBase:     hostViewBase + 300 + i*10,
			goName:       e.GO,
			pos:          [3]float32{e.Pos[0], e.Pos[1], e.Pos[2]},
			hp:           hp,
			maxHP:        hp,
			isBarrel:     !isDestoryable,
			receiveShoot: receiveShoot,
			receiveMelee: receiveMelee,
			receiveArea:  receiveArea,
		})
		i++
	}
	if i == 0 {
		return
	}
	const extras = 3
	for k := int32(0); k < extras; k++ {
		spawnStatic(i+k, "Entities/Object/ExplosiveDestructibleObj/ExposiveDestructible_BarrelXML", 0, -500, 0, nil)
	}
	log.Printf("[HOST] spawned %d+%d static entities room=%s level=%s", i, extras, room.Name, level)
}

func npcKitForSpawn(level, goName string) string {
	switch {
	case strings.Contains(goName, "Skag"):
		return "SkagPup"
	case strings.Contains(goName, "ClapTrap"):
		return "ClapTrapTutor"
	case strings.Contains(goName, "cluster_AI"), strings.Contains(goName, "CombatZone"):
		if strings.Contains(strings.ToLower(level), "skag") {
			return "SkagPup"
		}
		return "BanditKiller"
	}
	return ""
}

func groundHint(level string, x, z, refY float32) (float32, bool) {
	if y, ok := navmeshGroundHeight(level, x, z, refY); ok {
		return y, true
	}
	return groundHintApprox(level, x, z)
}

func groundHintApprox(level string, x, z float32) (float32, bool) {
	bestDist := float32(1 << 30)
	bestY := float32(0)
	found := false
	consider := func(px, py, pz float32) {
		dx, dz := px-x, pz-z
		d := dx*dx + dz*dz
		if d < bestDist {
			bestDist = d
			bestY = py
			found = true
		}
	}
	for _, e := range levelSpawnEntries(level) {
		if len(e.Pos) != 3 {
			continue
		}
		if e.Script != "StaticEntitySpawner" && e.Script != "PlayerSpawnCluster" {
			continue
		}
		consider(e.Pos[0], e.Pos[1], e.Pos[2])
	}
	for _, c := range levelClusterDefs(level) {
		if c.Collider != nil && len(c.Pos) == 3 {
			consider(c.Pos[0], c.Pos[1], c.Pos[2])
		}
	}
	return bestY, found
}

var waveNamePattern = regexp.MustCompile(`(CombatZone\d+)_Wave(\d+)`)

func parseWaveSpawn(goName string) (zone string, wave int, ok bool) {
	m := waveNamePattern.FindStringSubmatch(goName)
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, false
	}
	return m[1], n, true
}

const waveInterval = 25 * time.Second

func (s *EnetServer) hostSpawnNpcs(room *Room) {
	if room.npcsSpawned {
		return
	}
	room.npcsSpawned = true
	level := getCurScnFromProps(room.Name, room.Properties)

	clusterOwned := map[string]bool{}
	if room.clusters != nil {
		for _, rt := range room.clusters.list {
			if rt.def.Type == "Cluster" {
				clusterOwned[rt.def.Name] = true
			}
		}
	}

	zones := map[string]map[int][]spawnEntry{}
	var immediate []spawnEntry
	for _, e := range levelSpawnEntries(level) {
		if e.Script != "SpawnSlotContainer" || len(e.Pos) != 3 {
			continue
		}
		if clusterOwned[e.GO] {
			continue
		}
		if npcKitForSpawn(level, e.GO) == "" {
			continue
		}
		if zone, wave, ok := parseWaveSpawn(e.GO); ok {
			if zones[zone] == nil {
				zones[zone] = map[int][]spawnEntry{}
			}
			zones[zone][wave] = append(zones[zone][wave], e)
			continue
		}
		immediate = append(immediate, e)
	}

	room.pendingImmediateSpawns = append(room.pendingImmediateSpawns, immediate...)
	n := len(immediate)
	for zone, byWave := range zones {
		waves := make([]int, 0, len(byWave))
		for w := range byWave {
			waves = append(waves, w)
		}
		sort.Ints(waves)
		for _, e := range byWave[waves[0]] {
			s.hostSpawnOneNpc(room, level, e)
			n++
		}
		for i := 1; i < len(waves); i++ {
			room.pendingWaves = append(room.pendingWaves, pendingWave{
				zone:    zone,
				wave:    waves[i],
				entries: byWave[waves[i]],
				dueAt:   time.Now().Add(time.Duration(i) * waveInterval),
			})
		}
	}
	if n > 0 || len(room.pendingWaves) > 0 {
		log.Printf("[HOST] queued %d NPCs (%d immediate spread over ticks, +%d waves queued) room=%s level=%s",
			n, len(room.pendingImmediateSpawns), len(room.pendingWaves), room.Name, level)
	}
}

func (s *EnetServer) hostSpawnOneNpc(room *Room, level string, e spawnEntry) {
	kit := npcKitForSpawn(level, e.GO)
	if kit == "" {
		return
	}
	pos := [3]float32{e.Pos[0], e.Pos[1], e.Pos[2]}
	base := s.hostSpawnNpcAt(room, level, kit, pos, nil, 0)
	npc := &room.npcs[len(room.npcs)-1]
	if npc.viewBase != base {
		return
	}
	if e.Patrol != nil && len(e.Patrol.Points) > 0 {
		npc.patrolPolicy = e.Patrol.Policy
		npc.patrolIdx = -1
		npc.patrolPoints = make([][3]float32, len(e.Patrol.Points))
		npc.patrolWaits = make([]float32, len(e.Patrol.Points))
		for i, p := range e.Patrol.Points {
			npc.patrolPoints[i] = [3]float32{p[0], p[1], p[2]}
			npc.patrolWaits[i] = p[3]
		}
		return
	}
	npcAssignDefaultWander(npc, kit, pos, level)
}

func npcAssignDefaultWander(npc *spawnedNpc, kit string, spawnPos [3]float32, level string) {
	cfg := npcConfigFor(kit)
	if cfg == nil || cfg.PatrolRadius <= 0 {
		return
	}
	const wanderPoints = 4
	const maxWanderAttempts = 8
	waitMin, waitMax := cfg.StayIdleMin, cfg.StayIdleMax
	if waitMax <= 0 {
		waitMin, waitMax = 1, 4 // fallback if the kit's config has no explicit stay-idle range
	}
	var points [][3]float32
	var waits []float32
	for i := 0; i < wanderPoints; i++ {
		for attempt := 0; attempt < maxWanderAttempts; attempt++ {
			angle := randRange(0, 2*math.Pi)
			dist := randRange(cfg.PatrolRadius*0.5, cfg.PatrolRadius)
			cand := [3]float32{
				spawnPos[0] + float32(dist*math.Sin(angle)),
				spawnPos[1],
				spawnPos[2] + float32(dist*math.Cos(angle)),
			}
			if _, ok := navmeshFindPath(level, spawnPos[0], spawnPos[2], cand[0], cand[2], spawnPos[1]); ok {
				points = append(points, cand)
				waits = append(waits, float32(randRange(waitMin, waitMax)))
				break
			}
		}
	}
	if len(points) < 2 {
		return
	}
	npc.patrolPolicy = "Random"
	npc.patrolIdx = -1
	npc.patrolPoints = points
	npc.patrolWaits = waits
}

func (s *EnetServer) hostSpawnNpcAt(room *Room, level string, kit string, pos [3]float32, instantiationData []any, viewCount int32) int32 {
	if viewCount <= 0 {
		viewCount = 4
	}
	base := hostViewBase + 1000 + room.npcViewCtr*10
	room.npcViewCtr++
	views := make([]int32, viewCount)
	for i := range views {
		views[i] = base + int32(i)
	}
	hpMult := s.npcHPScaleForKit(room, kit)
	hp := int32(float32(npcMaxHP(kit)) * levelHPModifier(level) * hpMult)
	npc := spawnedNpc{
		viewBase: base, kit: kit, pos: pos, groundY: pos[1], hp: hp, maxHP: hp, coverIdx: -1,
		aggroRadiusScale: float32(randRange(0.85, 1.15)),
		viewCount:        viewCount,
		spawnedAt:        time.Now(),
	}
	if gy, ok := groundHint(level, pos[0], pos[2], pos[1]); ok && gy < npc.pos[1] {
		npc.groundY = gy
	}
	if w := npcWeaponFor(kit); w != nil { // humanoid gunner: give it real held weapon
		npc.weaponView = s.hostSpawnNpcWeapon(room, pos, npcWeaponItemDNA(w))
		log.Printf("[HOST] spawned real weapon %q (type=%s) for npc kit=%q viewID=%d room=%s", w.Name, w.Type, kit, npc.weaponView, room.Name)
	}
	if attackerView, ok := roomActiveAggroAttacker(room); ok {
		npc.forcedAggroView = attackerView
		npc.forcedAggroUntil = time.Now().Add(forcedAggroWindow)
	}
	room.npcs = append(room.npcs, npc)
	data := map[any]any{
		byte(0): "BKT|NPC|" + kit,
		byte(1): vector3Custom(pos[0], pos[1], pos[2]),
		byte(4): IntArray{Items: views},
		byte(6): int32(msNow()),
		byte(7): views[0],
	}
	if instantiationData != nil {
		data[byte(5)] = instantiationData
	}
	s.hostBroadcastJournaled(room, 202, map[byte]any{
		ParamPlayerNr:  HostActorNr,
		ParamEventData: data,
	}, true)
	if viewCount >= 2 {
		equipView := base + viewCount - 1
		s.hostSendRPC(room, equipView, "RPC_SendLifeAndShield", []any{npcVitalsTable(kit, hp, hp)})
		room.pendingNpcVitalsResyncs = append(room.pendingNpcVitalsResyncs, pendingNpcVitalsResync{
			view: base, equipView: equipView, kit: kit, maxHP: hp, hp: hp, at: time.Now().Add(300 * time.Millisecond),
		})
	}
	return base
}

func (s *EnetServer) hostSpawnPropAt(room *Room, prefab string, pos [3]float32, viewCount int32, rot []float32) int32 {
	if viewCount < 1 {
		viewCount = 1
	}
	base := hostViewBase + 1000 + room.npcViewCtr*10
	room.npcViewCtr++
	views := make([]int32, viewCount)
	for i := range views {
		views[i] = base + int32(i)
	}
	data := map[any]any{
		byte(0): prefab,
		byte(1): vector3Custom(pos[0], pos[1], pos[2]),
		byte(4): IntArray{Items: views},
		byte(6): int32(msNow()),
		byte(7): views[0],
	}
	if len(rot) == 4 {
		data[byte(2)] = quaternionCustom(rot[0], rot[1], rot[2], rot[3])
	}
	s.hostBroadcastJournaled(room, 202, map[byte]any{
		ParamPlayerNr:  HostActorNr,
		ParamEventData: data,
	}, false)
	return base
}

const claptrapPickupRadius = 3.0

const claptrapPickupCooldown = 5 * time.Second

const claptrapScore3Value = 3

const (
	defaultClaptrapTargetScore  = 10
	defaultClaptrapRoundSeconds = 180
)

func (s *EnetServer) claptrapTargetScore() int32 {
	if s.ClaptrapTargetScore <= 0 {
		return defaultClaptrapTargetScore
	}
	return s.ClaptrapTargetScore
}

const claptrapMinDisplaySeconds = 15

func (s *EnetServer) claptrapRemainingSeconds(room *Room) int32 {
	if room.clusters != nil {
		for _, rt := range room.clusters.list {
			if rt.def.Name != "Match_Timer" {
				continue
			}
			if rt.phase == clusterActive && !rt.completeAt.IsZero() {
				if remain := time.Until(rt.completeAt); remain > 0 {
					return int32(remain.Seconds())
				}
				return 0
			}
			if rt.def.Duration > 0 {
				return int32(rt.def.Duration)
			}
			break
		}
	}
	return s.claptrapRoundFallbackSeconds()
}

func (s *EnetServer) claptrapRoundFallbackSeconds() int32 {
	if s.ClaptrapRoundSeconds <= 0 {
		return defaultClaptrapRoundSeconds
	}
	return int32(s.ClaptrapRoundSeconds)
}

func (s *EnetServer) hostSendClaptrapRoundResult(room *Room, isFinalRound bool) {
	payload := s.sessionStatsPayload(room)
	log.Printf("[HOST] Claptrap round result room=%s isFinal=%v totals=%v timeLimit=%v endScore=%v claptrapTeamID=%v claptrapScore=%v",
		room.Name, isFinalRound, room.claptrapTeamScore, payload[4], payload[5], payload[8], payload[9])
	s.hostSendRPC(room, sessionInfoViewID, "RPC_RequestSyncSessionStats", []any{payload})
	s.hostSendRPC(room, sessionInfoViewID, "RPC_ShowRoundResultToClient", []any{isFinalRound})
}

func claptrapDistSq(a, b [3]float32) float32 {
	dx, dy, dz := a[0]-b[0], a[1]-b[1], a[2]-b[2]
	return dx*dx + dy*dy + dz*dz
}

func claptrapZoneContains(pos, center, halfExtents [3]float32) bool {
	for i := 0; i < 3; i++ {
		d := pos[i] - center[i]
		if d < 0 {
			d = -d
		}
		if d > halfExtents[i] {
			return false
		}
	}
	return true
}

func claptrapSpawnPosition(level string) ([3]float32, bool) {
	for _, e := range levelSpawnEntries(level) {
		if e.Script == "SpawnPointSettings" && e.GO == "ClaptrapSpawnPoint" && len(e.Pos) == 3 {
			return [3]float32{e.Pos[0], e.Pos[1], e.Pos[2]}, true
		}
	}
	return [3]float32{}, false
}

type claptrapScoreZone struct {
	Name         string
	Pos          [3]float32
	HalfExtents  [3]float32
	TeamID       int32
	ScenePid     int64
	ConditionPid int64
}

func claptrapScoreZones(level string) []claptrapScoreZone {
	var out []claptrapScoreZone
	for _, c := range levelClusterDefs(level) {
		if c.Type != "Scene" || !strings.Contains(c.Name, "Score") || c.Collider == nil || len(c.Collider.Size) != 3 || len(c.Pos) != 3 {
			continue
		}
		var team int32
		switch {
		case strings.HasPrefix(c.Name, "TeamA"):
			team = 0
		case strings.HasPrefix(c.Name, "TeamB"):
			team = 1
		default:
			continue
		}
		z := claptrapScoreZone{
			Name:        c.Name,
			Pos:         [3]float32{c.Pos[0], c.Pos[1], c.Pos[2]},
			HalfExtents: [3]float32{c.Collider.Size[0] / 2, c.Collider.Size[1] / 2, c.Collider.Size[2] / 2},
			TeamID:      team,
			ScenePid:    c.Pid,
		}
		if len(c.Conditions) > 0 && c.Conditions[0].Type == "ClusterCondition_TriggerZone" {
			z.ConditionPid = c.Conditions[0].Pid
		}
		out = append(out, z)
	}
	return out
}

func (s *EnetServer) hostSpawnClaptrap(room *Room) {
	if room.claptrapViewBase != 0 || gamemodeName(room) != "GamemodeClaptrapSoccer" {
		return
	}
	level := getCurScnFromProps(room.Name, room.Properties)
	pos, ok := claptrapSpawnPosition(level)
	if !ok {
		log.Printf("[HOST] hostSpawnClaptrap: no ClaptrapSpawnPoint for level %q", level)
		return
	}
	room.claptrapViewBase = s.hostSpawnPropAt(room, "Entities/Object/Item/Claptrap", pos, 1, nil)
	room.claptrapPos = pos
	room.claptrapCarrier = nil
	log.Printf("[HOST] spawned Claptrap view=%d room=%s pos=%v", room.claptrapViewBase, room.Name, pos)
}

func (s *EnetServer) hostTickClaptrapSoccer(room *Room) {
	if room.claptrapViewBase == 0 || gamemodeName(room) != "GamemodeClaptrapSoccer" {
		return
	}
	level := getCurScnFromProps(room.Name, room.Properties)

	if room.claptrapCarrier == nil {
		if time.Now().Before(room.claptrapPickupCooldownUntil) {
			return
		}
		for _, uid := range room.Players {
			target := s.sessionInRoom(uid, room.Name)
			if target == nil || target.avatarBase == 0 || target.isDead {
				continue
			}
			pos := room.playerPos[target.avatarBase+1]
			if claptrapDistSq(pos, room.claptrapPos) > claptrapPickupRadius*claptrapPickupRadius {
				continue
			}
			room.claptrapCarrier = target
			s.hostSendRPC(room, target.avatarBase, "RPC_RequestPickUpClaptrap", []any{room.claptrapViewBase})
			log.Printf("[HOST] Claptrap picked up by actor view=%d team=%d room=%s", target.avatarBase, target.teamID, room.Name)
			break
		}
		return
	}

	carrier := room.claptrapCarrier
	pos := room.playerPos[carrier.avatarBase+1]

	if carrier.isDead {
		s.hostSendRPC(room, carrier.avatarBase, "RPC_RequestDropClaptrap", []any{
			vector3Custom(pos[0], pos[1], pos[2]), quaternionCustom(0, 0, 0, 1), vector3Custom(0, 0, 0),
		})
		room.claptrapPos = pos
		room.claptrapCarrier = nil
		log.Printf("[HOST] Claptrap dropped (carrier died) room=%s pos=%v", room.Name, pos)
		return
	}

	for _, z := range claptrapScoreZones(level) {
		if !claptrapZoneContains(pos, z.Pos, z.HalfExtents) {
			continue
		}
		if z.TeamID == int32(carrier.teamID) {
			continue
		}
		scoringTeam := int32(carrier.teamID)
		room.claptrapTeamScore[scoringTeam] += claptrapScore3Value
		room.claptrapLastScoreTeam = byte(scoringTeam)
		room.claptrapLastZoneOwnerTeam = byte(z.TeamID)
		room.claptrapLastScoreValue = byte(claptrapScore3Value)
		log.Printf("[HOST] Claptrap delivered by team=%d into zone=%q (+%d) room=%s totals=%v",
			scoringTeam, z.Name, claptrapScore3Value, room.Name, room.claptrapTeamScore)
		if target := s.claptrapTargetScore(); !room.pvpMatchEnded && room.claptrapTeamScore[scoringTeam] >= target {
			room.pvpMatchEnded = true
			log.Printf("[CLUSTER] ClaptrapSoccer match won by team=%d score=%d (target=%d) room=%s",
				scoringTeam, room.claptrapTeamScore[scoringTeam], target, room.Name)
			s.hostFinishPvPMatch(room)
		}
		s.hostTriggerClaptrapScoreBomb(room, z.Pos)
		if room.clusters != nil && z.ScenePid != 0 && z.ConditionPid != 0 {
			if rt := room.clusters.byPid[z.ScenePid]; rt != nil {
				rt.fired[z.ConditionPid] = true
			}
		}
		s.hostSendRPC(room, carrier.avatarBase, "RPC_RequestDropClaptrap", []any{
			vector3Custom(room.claptrapPos[0], room.claptrapPos[1], room.claptrapPos[2]),
			quaternionCustom(0, 0, 0, 1), vector3Custom(0, 0, 0),
		})
		room.claptrapCarrier = nil
		room.claptrapPickupCooldownUntil = time.Now().Add(claptrapPickupCooldown)
		return
	}
}

func (s *EnetServer) hostTriggerClaptrapScoreBomb(room *Room, zonePos [3]float32) {
	s.broadcastEvent(room, 201, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): int32(msNow()),
			Short(1): map[any]any{
				byte(0): room.claptrapViewBase + 1,
				byte(1): grenadeTaskStream(bhvTaskBlowUp, 0),
			},
		},
	}, nil, nil)
	killed := 0
	for _, uid := range room.Players {
		target := s.sessionInRoom(uid, room.Name)
		if target == nil || target.avatarBase == 0 || target.isDead || target.respawnPending {
			continue
		}
		if !s.hostDamagePlayer(room, target, sessionInfoViewID, target.hp+target.shield+1, attackSubTypeLegacy, elementExplosive) {
			continue
		}
		if target.isDead {
			target.isDead = false
			s.hostKillWithDeathAnim(room, target, time.Duration(killed)*claptrapRespawnStagger)
		}
		killed++
	}
	log.Printf("[HOST] Claptrap goal bomb at %v, killed %d player(s) room=%s", zonePos, killed, room.Name)
}

const immediateSpawnBatchSize = 3

func (s *EnetServer) hostAdvanceImmediateNpcSpawns(room *Room) bool {
	if len(room.pendingImmediateSpawns) == 0 {
		return false
	}
	level := getCurScnFromProps(room.Name, room.Properties)
	n := immediateSpawnBatchSize
	if n > len(room.pendingImmediateSpawns) {
		n = len(room.pendingImmediateSpawns)
	}
	for _, e := range room.pendingImmediateSpawns[:n] {
		s.hostSpawnOneNpc(room, level, e)
	}
	room.pendingImmediateSpawns = room.pendingImmediateSpawns[n:]
	return true
}

func (s *EnetServer) hostAdvanceWaves(room *Room) bool {
	if len(room.pendingWaves) == 0 {
		return false
	}
	level := getCurScnFromProps(room.Name, room.Properties)
	now := time.Now()
	spawned := false
	remaining := room.pendingWaves[:0]
	for _, pw := range room.pendingWaves {
		if now.Before(pw.dueAt) {
			remaining = append(remaining, pw)
			continue
		}
		for _, e := range pw.entries {
			s.hostSpawnOneNpc(room, level, e)
		}
		log.Printf("[HOST] wave released zone=%s wave=%d room=%s npcs=%d", pw.zone, pw.wave, room.Name, len(pw.entries))
		spawned = true
	}
	room.pendingWaves = remaining
	return spawned
}

type pendingAvatarSpawn struct {
	sess    *ClientSession
	actorNr int32
	at      time.Time
}

func (s *EnetServer) hostRunPendingAvatarSpawns(room *Room) {
	if len(room.pendingAvatarSpawns) == 0 {
		return
	}
	now := time.Now()
	remaining := room.pendingAvatarSpawns[:0]
	for _, p := range room.pendingAvatarSpawns {
		if now.Before(p.at) {
			remaining = append(remaining, p)
			continue
		}
		s.hostFinishMapReady(room, p.sess, p.actorNr)
	}
	room.pendingAvatarSpawns = remaining
}

func (s *EnetServer) hostFinishMapReady(room *Room, sess *ClientSession, actorNr int32) {
	s.hostSpawnAvatar(room, sess, actorNr, nil)
	sess.playerState = PlayerStateSpawned
	s.hostSpawnClaptrap(room)
	s.hostSendSerialize(room)
	if len(room.levelObjectStates) > 0 {
		s.hostSendLevelObjectState(room)
	}
	if playlistID, ok := room.Properties["PlaylistId"].(int32); ok && realTownPlaylists[playlistID] && sess.hostChar != nil {
		if err := db.UpdateCharacterLastTown(sess.hostChar.CharID, playlistID); err != nil {
			log.Printf("[HOST] UpdateCharacterLastTown charID=%d: %v", sess.hostChar.CharID, err)
		}
		bumpTownVisited(sess.hostChar.CharID, playlistID)
	}
	needsActionGrant := gamemodeName(room) == "GameModeTown" || gamemodeName(room) == "GamemodeRV" ||
		gamemodeName(room) == "GamemodeClaptrapSoccer" ||
		(gamemodeName(room) == "GamemodeTutorial" && (room.clusters == nil || s.TutorialFreeActions))
	if needsActionGrant {
		room.lastPlayerActions = int32(allPlayerActions)
		room.actionsGranted = true
		s.hostSendRPC(room, sessionInfoViewID, "RPC_RequestSetPlayerActions", []any{int32(allPlayerActions)})
		now := time.Now()
		for _, d := range []time.Duration{2, 5, 10, 20} {
			room.actionGrantAt = append(room.actionGrantAt, now.Add(d*time.Second))
		}
	}
}

type replayRPC struct {
	viewID int32
	name   string
	args   []any
}

type pendingReplayEvent struct {
	sess   *ClientSession
	events []journaledEvent
	rpcs   []replayRPC
	at     time.Time
}

func (s *EnetServer) hostRunPendingReplayEvents(room *Room) {
	if len(room.pendingReplayEvents) == 0 {
		return
	}
	now := time.Now()
	remaining := room.pendingReplayEvents[:0]
	for _, p := range room.pendingReplayEvents {
		if now.Before(p.at) {
			remaining = append(remaining, p)
			continue
		}
		for _, e := range p.events {
			s.sendEventTo(p.sess, e.code, e.payload)
		}
		for _, r := range p.rpcs {
			s.hostSendRPCTo(p.sess, r.viewID, r.name, r.args)
		}
	}
	room.pendingReplayEvents = remaining
}

func (s *EnetServer) hostRetryActionGrants(room *Room) {
	if len(room.actionGrantAt) == 0 {
		return
	}
	now := time.Now()
	remaining := room.actionGrantAt[:0]
	for _, at := range room.actionGrantAt {
		if now.Before(at) {
			remaining = append(remaining, at)
			continue
		}
		s.hostSendRPC(room, sessionInfoViewID, "RPC_RequestSetPlayerActions", []any{room.lastPlayerActions})
		log.Printf("[HOST] re-sent RPC_RequestSetPlayerActions(%d) room=%s", room.lastPlayerActions, room.Name)
	}
	room.actionGrantAt = remaining
}

const npcGravityDropPerSec = 6.0

func (s *EnetServer) hostTickNpcGravity(room *Room, dt float64) bool {
	moved := false
	for i := range room.npcs {
		npc := &room.npcs[i]
		if npc.grounded {
			continue
		}
		npc.pos[1] -= float32(npcGravityDropPerSec * dt)
		if npc.pos[1] <= npc.groundY {
			npc.pos[1] = npc.groundY
			npc.grounded = true
		}
		moved = true
	}
	return moved
}

func vector3Custom(x, y, z float32) Custom {
	data := make([]byte, 12)
	binary.BigEndian.PutUint32(data[0:], math.Float32bits(x))
	binary.BigEndian.PutUint32(data[4:], math.Float32bits(y))
	binary.BigEndian.PutUint32(data[8:], math.Float32bits(z))
	return Custom{Code: 86, Data: data}
}

type spawnEntry struct {
	Script              string       `json:"script"`
	GO                  string       `json:"go"`
	Pos                 []float32    `json:"pos"`
	Rot                 []float32    `json:"rot"`
	Patrol              *patrolEntry `json:"patrol,omitempty"`
	TeamID              int32        `json:"teamId,omitempty"`
	Score               int32        `json:"score,omitempty"`
	ColliderCenter      []float32    `json:"colliderCenter,omitempty"`
	ColliderHalfExtents []float32    `json:"colliderHalfExtents,omitempty"`
}

func (e *spawnEntry) SetPos(x, y, z float32) { e.Pos = []float32{x, y, z} }

func (e *spawnEntry) SetRot(x, y, z, w float32) { e.Rot = []float32{x, y, z, w} }

type patrolEntry struct {
	Policy string       `json:"policy"`
	Points [][4]float32 `json:"points"`
}

var (
	spawnsOnce  sync.Once
	levelSpawns map[string][]spawnEntry
)

func reloadLevelSpawns() {
	spawnsOnce = sync.Once{}
	levelSpawns = nil
}

func levelSpawnEntries(level string) []spawnEntry {
	spawnsOnce.Do(func() {
		levelSpawns = map[string][]spawnEntry{}
		raw, err := os.ReadFile("data/level_spawns.json")
		if err != nil {
			log.Printf("[HOST] level_spawns.json not loaded: %v", err)
			return
		}
		parsed := map[string][]spawnEntry{}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			log.Printf("[HOST] level_spawns.json parse error: %v", err)
			return
		}
		for k, v := range parsed {
			lk := strings.ToLower(k)
			levelSpawns[lk] = append(levelSpawns[lk], v...)
		}
		applyOverrides("data/level_spawns.json", levelSpawns)
	})
	level = strings.ToLower(level)
	if e, ok := levelSpawns[level]; ok {
		return e
	}
	if i := strings.LastIndex(level, "_part"); i > 0 {
		return levelSpawns[level[:i]]
	}
	return nil
}

func spawnEntryRot(e *spawnEntry) (rot [4]float32, ok bool) {
	if e == nil || len(e.Rot) != 4 {
		return rot, false
	}
	return [4]float32{e.Rot[0], e.Rot[1], e.Rot[2], e.Rot[3]}, true
}

func spawnPosition(room *Room, level string, team int32) (pos [3]float32, rot [4]float32, rotOK bool) {
	entries := levelSpawnEntries(level)
	var first, checkpoint, playerSpawn *spawnEntry
	var playerSpawnClusters []*spawnEntry
	for i := range entries {
		e := &entries[i]
		if len(e.Pos) != 3 {
			continue
		}
		if e.Script == "PlayerSpawnCluster" {
			playerSpawnClusters = append(playerSpawnClusters, e)
			continue
		}
		if e.Script != "SpawnSlotContainer" {
			continue
		}
		if playerSpawn == nil && strings.Contains(e.GO, "PlayerSpawn") {
			playerSpawn = e
		}
		if checkpoint == nil && strings.Contains(e.GO, "Checkpoint") {
			checkpoint = e
		}
		if first == nil {
			first = e
		}
	}
	if len(playerSpawnClusters) > 0 {
		anchorEntry := playerSpawnClusters[0]
		teamMatched := false
		wantSuffix := "teama"
		if team == 1 {
			wantSuffix = "teamb"
		}
		for _, e := range playerSpawnClusters {
			if strings.Contains(strings.ToLower(e.GO), wantSuffix) {
				anchorEntry = e
				teamMatched = true
				break
			}
		}
		if !teamMatched {
			matched := false
			for _, e := range playerSpawnClusters {
				if strings.Contains(e.GO, "PlayerSpawn") {
					anchorEntry = e
					matched = true
					break
				}
			}
			if !matched {
				excluded := []string{"transition", "checkpoint", "newspawn", "respawn", "telepot", "teleport", "stage"}
				for _, e := range playerSpawnClusters {
					lower := strings.ToLower(e.GO)
					skip := false
					for _, kw := range excluded {
						if strings.Contains(lower, kw) {
							skip = true
							break
						}
					}
					if skip {
						continue
					}
					anchorEntry = e
					break
				}
			}
		}
		anchor := [3]float32{anchorEntry.Pos[0], anchorEntry.Pos[1], anchorEntry.Pos[2]}
		if cp, cpRot, cpRotOK, ok := activeCheckpointPos(room, anchor); ok {
			return cp, cpRot, cpRotOK
		}
		r, ok := spawnEntryRot(anchorEntry)
		return anchor, r, ok
	}
	for _, e := range []*spawnEntry{playerSpawn, checkpoint, first} {
		if e != nil {
			r, ok := spawnEntryRot(e)
			return [3]float32{e.Pos[0], e.Pos[1], e.Pos[2]}, r, ok
		}
	}
	return [3]float32{}, rot, false
}

func quaternionCustom(x, y, z, w float32) Custom {
	data := make([]byte, 16)
	binary.LittleEndian.PutUint32(data[0:], math.Float32bits(w))
	binary.LittleEndian.PutUint32(data[4:], math.Float32bits(x))
	binary.LittleEndian.PutUint32(data[8:], math.Float32bits(y))
	binary.LittleEndian.PutUint32(data[12:], math.Float32bits(z))
	return Custom{Code: 81, Data: data}
}

func quatFromEulerDegrees(xDeg, yDeg, zDeg float32) [4]float32 {
	half := func(deg float32) (s, c float64) {
		rad := float64(deg) * math.Pi / 180
		return math.Sin(rad / 2), math.Cos(rad / 2)
	}
	sx, cx := half(xDeg)
	sy, cy := half(yDeg)
	sz, cz := half(zDeg)
	qx := [4]float64{sx, 0, 0, cx}
	qy := [4]float64{0, sy, 0, cy}
	qz := [4]float64{0, 0, sz, cz}
	mul := func(a, b [4]float64) [4]float64 {
		return [4]float64{
			a[3]*b[0] + a[0]*b[3] + a[1]*b[2] - a[2]*b[1],
			a[3]*b[1] - a[0]*b[2] + a[1]*b[3] + a[2]*b[0],
			a[3]*b[2] + a[0]*b[1] - a[1]*b[0] + a[2]*b[3],
			a[3]*b[3] - a[0]*b[0] - a[1]*b[1] - a[2]*b[2],
		}
	}
	q := mul(mul(qy, qx), qz)
	return [4]float32{float32(q[0]), float32(q[1]), float32(q[2]), float32(q[3])}
}

func (s *EnetServer) hostSpawnPlayer(room *Room, sess *ClientSession, actorNr int32) {
	pv := playerViewID(actorNr)
	nowMS := int32(msNow())
	s.hostBroadcastJournaled(room, 202, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): "Player",
			byte(4): IntArray{Items: []int32{pv}},
			byte(6): nowMS,
			byte(7): pv,
		},
	}, false)
	s.hostSendRPC(room, pv, "RPC_OnReceiveExp", []any{int32(sess.hostChar.Experience)})
	log.Printf("[HOST] spawned Player viewID=%d for actor=%d char=%q exp=%d", pv, actorNr, sess.hostChar.Name, sess.hostChar.Experience)
}

func (s *EnetServer) hostSpawnLevelObjectManager(room *Room) {
	if room.lomSpawned {
		return
	}
	room.lomSpawned = true
	nowMS := int32(msNow())
	s.hostBroadcastJournaled(room, 202, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): "LevelObjectManager",
			byte(4): IntArray{Items: []int32{lomViewID}},
			byte(6): nowMS,
			byte(7): lomViewID,
		},
	}, false)
	log.Printf("[HOST] spawned LevelObjectManager viewID=%d room=%s", lomViewID, room.Name)
}

func (s *EnetServer) hostOpenLevelObject(room *Room, levelObjectID int) bool {
	if room.levelObjectStates == nil {
		room.levelObjectStates = map[int]bool{}
	}
	newlyOpened := !room.levelObjectStates[levelObjectID]
	room.levelObjectStates[levelObjectID] = true
	s.hostSpawnLevelObjectManager(room) // defensive: should already be spawned
	s.hostSendLevelObjectState(room)
	if newlyOpened {
		predicted := "?"
		objs := levelObjectEntries(getCurScnFromProps(room.Name, room.Properties))
		if levelObjectID >= 0 && levelObjectID < len(objs) {
			predicted = objs[levelObjectID].GO
		}
		log.Printf("[HOST] level object %d activated room=%s (predicted: %q)", levelObjectID, room.Name, predicted)
	}
	return newlyOpened
}

func (s *EnetServer) hostTrackFire(room *Room, sess *ClientSession, weaponView int32) {
	if sess.ammoEst > 0 {
		sess.ammoEst -= 6
	}
	if sess.avatarBase == 0 {
		return
	}
	pos, ok := playerStreamPos(room, sess.avatarBase)
	if !ok {
		pos = room.playerPos[sess.avatarBase+1]
	}
	if room.firesNear == nil {
		room.firesNear = map[int32]int{}
	}
	for i := range room.statics {
		dx, dz := pos[0]-room.statics[i].pos[0], pos[2]-room.statics[i].pos[2]
		if dx*dx+dz*dz <= 30*30 {
			room.firesNear[room.statics[i].viewBase]++
		}
	}
	if sess.raycastPatched {
		return
	}
	element := room.weaponElement[weaponView]
	s.hostDamageNearestNpc(room, sess.avatarBase, pos, sess.lastYawDeg, element)
}

func (s *EnetServer) hostThrottleRaycastDamage(sess *ClientSession, weaponType int32) bool {
	now := time.Now()
	interval := weaponFireInterval(weaponType)
	if sess.berserkActive && now.Before(sess.berserkEndAt) {
		if bonus := effectValue(sess.hostChar, "BerserkFireRate"); bonus > 0 {
			interval = time.Duration(float64(interval) / (1 + bonus))
		}
	}
	if now.Sub(sess.lastRaycastDamageAt) < interval {
		return false
	}
	sess.lastRaycastDamageAt = now
	return true
}

func berserkDespBonus(sess *ClientSession) float64 {
	if sess == nil || sess.maxHP <= 0 {
		return 0
	}
	perGrade := effectValue(sess.hostChar, "DespHealthPercent")
	if perGrade <= 0 {
		return 0
	}
	missingFrac := 1 - float64(sess.hp)/float64(sess.maxHP)
	if missingFrac < 0 {
		missingFrac = 0
	}
	return perGrade * missingFrac
}

func npcHealthBarVisuallyEmpty(hp, maxHP int32) bool {
	if hp <= 0 || maxHP <= 0 {
		return false
	}
	num := int32(math.Ceil(100.0 * float64(hp) / float64(maxHP)))
	return num <= 1
}

func (s *EnetServer) hostDamageNpcByView(room *Room, attackerView, targetView, damage, weakPointIndex, element int32, isDOT bool) bool {
	for i := range room.npcs {
		if room.npcs[i].viewBase != targetView {
			continue
		}
		if room.npcs[i].kit == "ClapTrapTutor" || room.npcs[i].hp <= 0 {
			return false
		}
		npc := &room.npcs[i]
		if m, ok := room.markedNpcs[targetView]; ok && time.Now().Before(m.until) {
			bonus := float64(hunterTrackshotBonus)
			if attacker := sessionByAvatarBase(s, room, attackerView); attacker != nil {
				bonus *= 1 + effectValue(attacker.hostChar, "DamamgeBonusToMarked")
			}
			damage += int32(bonus)
		}
		if m, ok := room.markedNpcs[targetView]; ok && time.Now().Before(m.until) && !isDOT {
			if attacker := sessionByAvatarBase(s, room, attackerView); attacker != nil && attacker.maxHP > 0 {
				if drain := effectValue(attacker.hostChar, "DrainMarkedLife"); drain > 0 {
					heal := int32(float64(damage) * drain)
					if heal > 0 {
						attacker.hp += heal
						if attacker.hp > attacker.maxHP {
							attacker.hp = attacker.maxHP
						}
						s.hostSendRPC(room, attacker.avatarBase, "RPC_RequestAllLifeAndShield", []any{
							map[int]any{0: int32(1), 1: attacker.avatarBase, 2: playerLifeTable(attacker.hp, attacker.maxHP, attacker.shield, attacker.maxShield)},
						})
					}
				}
			}
		}
		if !isDOT && element == elementShock {
			if attacker := sessionByAvatarBase(s, room, attackerView); attacker != nil {
				if bonus := effectValue(attacker.hostChar, "ElementDamage"); bonus > 0 {
					damage = int32(float64(damage) * (1 + bonus))
				}
			}
		}
		if !isDOT {
			if attacker := sessionByAvatarBase(s, room, attackerView); attacker != nil {
				if transferPct := effectValue(attacker.hostChar, "TransferDamageToMarked"); transferPct > 0 {
					transferDmg := int32(float64(damage) * transferPct)
					if transferDmg > 0 {
						for mv, m := range room.markedNpcs {
							if mv == targetView || !time.Now().Before(m.until) {
								continue
							}
							s.hostApplyTransferredDamage(room, attackerView, mv, transferDmg, element)
						}
					}
				}
			}
		}
		if !isDOT && npc.hp > 0 {
			if attacker := sessionByAvatarBase(s, room, attackerView); attacker != nil && attacker.hostChar != nil {
				if dazeChance := effectValue(attacker.hostChar, "DazeEnemyChance"); dazeChance > 0 && rand.Float64() < dazeChance {
					durMult := float32(1 + effectValue(attacker.hostChar, "DazeDuration"))
					npc.dazedUntil = time.Now().Add(time.Duration(3 * float64(time.Second) * float64(durMult)))
					npc.dazeEffectUID = (npc.dazeEffectUID + 1) % 126
					npc.dazeDurationMult = durMult
					npc.moving, npc.path = false, nil
					npcSetTask(npc, bhvTaskIdle, -1, npc.pos, -1)
				} else if confuseChance := effectValue(attacker.hostChar, "ConfuseChance"); confuseChance > 0 && rand.Float64() < confuseChance {
					npc.confusedUntil = time.Now().Add(3 * time.Second)
					npc.moving, npc.path = false, nil
					npcSetTask(npc, bhvTaskIdle, -1, npc.pos, -1)
				}
			}
		}
		npc.forcedAggroView = attackerView
		npc.forcedAggroUntil = time.Now().Add(forcedAggroWindow)
		s.hostAlertNearbyNpcs(room, npc, attackerView)
		s.hostRecordDamageStat(room, attackerView, damage, weakPointIndex >= 0, isDOT)
		npc.hp -= damage
		if npc.hp < 0 {
			npc.hp = 0
		}
		if npc.hp > 0 && !npc.furyActive {
			cfg := npcConfigFor(npc.kit)
			var furyHP float64
			if cfg != nil {
				furyHP = cfg.FuryHealthPoint
			}
			if npcFuryThresholdCrossed(npc.hp, furyHP) {
				npc.furyActive = true
				log.Printf("[AI] npc %d (%s) enters Fury (hp=%d threshold=%.0f) room=%s", npc.viewBase, npc.kit, npc.hp, furyHP, room.Name)
			}
		}
		if npc.hp > 0 && npcHealthBarVisuallyEmpty(npc.hp, npc.maxHP) {
			npc.hp = 0
		}
		if npc.hp > 0 {
			s.hostDamageEntity(room, attackerView, npc.viewBase, damage, weakPointIndex, element, isDOT)
			s.hostSendRPC(room, npc.equipView(), "RPC_SendLifeAndShield", []any{npcVitalsTable(npc.kit, npc.maxHP, npc.hp)})
			if !isDOT {
				attacker := sessionByAvatarBase(s, room, attackerView)
				var procBoost float64
				if attacker != nil {
					procBoost = effectValue(attacker.hostChar, "WpnElementalProc")
				}
				npcMaybeStartElementalDOT(npc, attackerView, element, damage, time.Now(), procBoost, false)
				if attacker != nil && element != elementFire {
					if ignite := effectValue(attacker.hostChar, "IgniteEnemyChance"); ignite > 0 && rand.Float64() < ignite {
						npcMaybeStartElementalDOT(npc, attackerView, elementFire, damage, time.Now(), 0, true)
					}
				}
			}
			s.npcMaybeWarcry(room, npc, damage, weakPointIndex, isDOT)
			return true
		}
		s.hostKillNpc(room, npc, attackerView, damage, element, weakPointIndex >= 0)
		return true
	}
	return false
}

func (s *EnetServer) hostApplyTransferredDamage(room *Room, attackerView, targetView, damage, element int32) {
	for i := range room.npcs {
		npc := &room.npcs[i]
		if npc.viewBase != targetView || npc.hp <= 0 {
			continue
		}
		npc.hp -= damage
		if npc.hp < 0 {
			npc.hp = 0
		}
		if npc.hp > 0 && npcHealthBarVisuallyEmpty(npc.hp, npc.maxHP) {
			npc.hp = 0
		}
		if npc.hp > 0 {
			s.hostSendRPC(room, npc.equipView(), "RPC_SendLifeAndShield", []any{npcVitalsTable(npc.kit, npc.maxHP, npc.hp)})
			return
		}
		s.hostKillNpc(room, npc, attackerView, damage, element, false)
		return
	}
}

func (s *EnetServer) hostAlertNearbyNpcs(room *Room, source *spawnedNpc, attackerView int32) {
	cutoff := float32(npcAlertRadius * npcAlertRadius)
	now := time.Now()
	for i := range room.npcs {
		other := &room.npcs[i]
		if other == source || other.hp <= 0 || other.friendly || other.kit == "ClapTrapTutor" {
			continue
		}
		dx, dz := other.pos[0]-source.pos[0], other.pos[2]-source.pos[2]
		if dx*dx+dz*dz > cutoff {
			continue
		}
		other.forcedAggroView = attackerView
		other.forcedAggroUntil = now.Add(forcedAggroWindow)
	}
}

func roomActiveAggroAttacker(room *Room) (int32, bool) {
	now := time.Now()
	for i := range room.npcs {
		npc := &room.npcs[i]
		if npc.hp > 0 && npc.forcedAggroView != 0 && now.Before(npc.forcedAggroUntil) {
			return npc.forcedAggroView, true
		}
	}
	return 0, false
}

func (s *EnetServer) hostDamageStaticByView(room *Room, attackerView, targetView, damage, element int32) bool {
	for i := range room.statics {
		st := &room.statics[i]
		if st.viewBase != targetView {
			continue
		}
		if st.hp <= 0 || !st.receiveShoot {
			return false
		}
		st.hp -= damage
		if st.hp < 0 {
			st.hp = 0
		}
		if st.hp > 0 {
			s.hostDamageEntity(room, attackerView, st.viewBase, damage, -1, element, false)
			return true
		}
		if st.isBarrel {
			s.hostExplodeBarrel(room, attackerView, st)
		} else {
			s.hostKillEntity(room, attackerView, st.viewBase, damage, element)
		}
		log.Printf("[HOST] static %q destroyed viewID=%d room=%s", st.goName, st.viewBase, room.Name)
		return true
	}
	return false
}

func npcMaxHP(kit string) int32 {
	if cfg := npcConfigFor(kit); cfg != nil && cfg.HealthPoint > 0 {
		return cfg.HealthPoint
	}
	return 200
}

func npcMeleeDamage(kit string) int32 {
	if cfg := npcConfigFor(kit); cfg != nil && cfg.MeleeDamage > 0 {
		return cfg.MeleeDamage
	}
	return 15
}

func npcMeleeDamageForLevel(kit, level string) int32 {
	return int32(float32(npcMeleeDamage(kit)) * levelDamageModifier(level))
}

func npcChargeDamage(kit string) int32 {
	if cfg := npcConfigFor(kit); cfg != nil && cfg.ChargeDamage > 0 {
		return cfg.ChargeDamage
	}
	return npcMeleeDamage(kit) * 2
}

func npcChargeDamageForLevel(kit, level string) int32 {
	return int32(float32(npcChargeDamage(kit)) * levelDamageModifier(level))
}

func npcJumpDamage(kit string) int32 {
	if cfg := npcConfigFor(kit); cfg != nil && cfg.JumpDamage > 0 {
		return cfg.JumpDamage
	}
	return npcMeleeDamage(kit)
}

func npcJumpDamageForLevel(kit, level string) int32 {
	return int32(float32(npcJumpDamage(kit)) * levelDamageModifier(level))
}

const attackSubTypeGeneric int32 = 21

const attackSubTypeLegacy int32 = 2

func npcMeleeSubType(kit string) int32 {
	cfg := npcConfigFor(kit)
	if cfg == nil || len(cfg.MeleeSubTypes) == 0 {
		return attackSubTypeGeneric
	}
	return cfg.MeleeSubTypes[rand.Intn(len(cfg.MeleeSubTypes))]
}

func npcChargeSubType(kit string) int32 {
	if cfg := npcConfigFor(kit); cfg != nil && cfg.ChargeSubType != 0 {
		return cfg.ChargeSubType
	}
	return attackSubTypeGeneric
}

func npcJumpSubType(kit string) int32 {
	if cfg := npcConfigFor(kit); cfg != nil && cfg.JumpSubType != 0 {
		return cfg.JumpSubType
	}
	return attackSubTypeGeneric
}

func npcChargeCheckRadius(kit string) float64 {
	if cfg := npcConfigFor(kit); cfg != nil && cfg.ChargeCheckRadius > 0 {
		return cfg.ChargeCheckRadius
	}
	return npcAttackRangeFor(kit) + 1
}

func npcAttackRangeFor(kit string) float64 {
	r := npcAttackRange
	if cfg := npcConfigFor(kit); cfg != nil && cfg.MeleeRange > 0 {
		r = cfg.MeleeRange
	}
	if r > meleeRangeCap {
		r = meleeRangeCap
	}
	return r
}

const (
	npcSpacingRangeMinFallback = 3.0
	npcSpacingRangeMaxFallback = 5.0
)

const npcSpacingMaxIterations = 8

func npcApplySpacing(room *Room, npc *spawnedNpc, dest [3]float32) [3]float32 {
	minRange, maxRange := npcSpacingRangeMinFallback, npcSpacingRangeMaxFallback
	if cfg := npcConfigFor(npc.kit); cfg != nil && cfg.SpacingRangeMin > 0 {
		minRange, maxRange = cfg.SpacingRangeMin, cfg.SpacingRangeMax
	}
	for iter := 0; iter < npcSpacingMaxIterations; iter++ {
		moved := false
		for i := range room.npcs {
			other := &room.npcs[i]
			if other == npc || other.hp <= 0 || other.friendly {
				continue
			}
			dx, dz := dest[0]-other.pos[0], dest[2]-other.pos[2]
			d := math.Sqrt(float64(dx*dx + dz*dz))
			if d >= minRange {
				continue
			}
			var nx, nz float32
			if d < 0.01 {
				angle := float64(npc.viewBase%360) * math.Pi / 180
				nx, nz = float32(math.Cos(angle)), float32(math.Sin(angle))
				d = 0
			} else {
				nx, nz = float32(float64(dx)/d), float32(float64(dz)/d)
			}
			push := float32(maxRange) - float32(d)
			dest[0] += nx * push
			dest[2] += nz * push
			moved = true
			break
		}
		if !moved {
			break
		}
	}
	return dest
}

const npcHardStackDist = 1.5

func npcIsOverlapping(room *Room, npc *spawnedNpc, thresh float64) bool {
	for i := range room.npcs {
		other := &room.npcs[i]
		if other == npc || other.hp <= 0 || other.friendly {
			continue
		}
		dx, dz := npc.pos[0]-other.pos[0], npc.pos[2]-other.pos[2]
		if float64(dx*dx+dz*dz) < thresh*thresh {
			return true
		}
	}
	return false
}

const npcSpacingSafetyMargin = 0.3

func npcClampDestToRange(dest, center [3]float32, maxDist float64) [3]float32 {
	dx, dz := dest[0]-center[0], dest[2]-center[2]
	d := math.Sqrt(float64(dx*dx + dz*dz))
	if d <= maxDist || d < 0.01 {
		return dest
	}
	scale := float32(maxDist / d)
	dest[0] = center[0] + dx*scale
	dest[2] = center[2] + dz*scale
	return dest
}

func npcRunSpeed(kit string) float64 {
	cfg := npcConfigFor(kit)
	if kit == "ClapTrapTutor" {
		if cfg != nil && cfg.WalkSpeed > 0 {
			return cfg.WalkSpeed
		}
		return 1.5
	}
	if npcUsesTrueRunPace(kit) {
		return cfg.RunSpeed
	}
	if cfg != nil && cfg.CombatSpeed > 0 {
		return cfg.CombatSpeed
	}
	return 3
}

func npcChargeSpeed(kit string) float64 {
	if cfg := npcConfigFor(kit); cfg != nil && cfg.ChargeSpeed > 0 {
		return cfg.ChargeSpeed
	}
	return npcRunSpeed(kit)
}

func npcUsesTrueRunPace(kit string) bool {
	if kit == "ClapTrapTutor" {
		return false
	}
	if npcForceFullRunPaceKits[kit] {
		return true
	}
	cfg := npcConfigFor(kit)
	if cfg == nil || cfg.WalkSpeed <= 0 || cfg.RunSpeed <= 0 {
		return false
	}
	return cfg.CombatSpeed < cfg.WalkSpeed*1.3
}

var npcForceFullRunPaceKits = map[string]bool{
	"SpitterQueen": true,
}

func npcPatrolSpeed(kit string) float64 {
	if cfg := npcConfigFor(kit); cfg != nil && cfg.WalkSpeed > 0 {
		return cfg.WalkSpeed
	}
	return 1.5
}

func npcMoveSpeed(npc *spawnedNpc) float64 {
	speed := npcPatrolSpeed(npc.kit)
	switch {
	case npc.taskType == bhvTaskChargeAttack || npc.taskType == bhvTaskPoisonDash:
		speed = npcChargeSpeed(npc.kit)
	case npc.taskSpeed == 2:
		speed = npcRunSpeed(npc.kit)
	}
	if npc.furyActive {
		speed *= npcFurySpeedMult
	}
	if npc.markSlowFrac > 0 && time.Now().Before(npc.markSlowUntil) {
		speed *= float64(1 - npc.markSlowFrac)
	}
	return speed
}

func npcMoveWireSpeed(kit string, taskSpeed int32) (movementSpeed, stress int32) {
	if taskSpeed != 2 {
		return 0, 0
	}
	if npcUsesTrueRunPace(kit) {
		return 2, 1
	}
	return 0, 1
}

const npcHitRadius = 15.0

const npcHitConeCosMin = 0.9397

const fireBurstDamage = 25

func packSubTypeElement(subType, element int32) Short {
	return Short(subType<<3 | element&7)
}

const (
	npcAggroRadius = 20.0
	npcAttackRange = 3.0
	npcMeleeEvery  = 2 * time.Second

	npcAttackCommitLock = 1100 * time.Millisecond
	npcMeleeSwingLock   = 650 * time.Millisecond

	meleeRangeCap = 2.2

	navmeshRepathInterval = 400 * time.Millisecond

	npcEquipmentStreamGracePeriod = 3 * time.Second

	avatarStreamGracePeriod = 3 * time.Second

	forcedAggroWindow = 10 * time.Second

	npcAlertRadius = 25.0
)

type aiTarget struct {
	view int32
	pos  [3]float32
	sess *ClientSession
}

func (s *EnetServer) hostTickNpcAI(room *Room, dt float64) bool {
	if len(room.npcs) == 0 {
		return false
	}
	level := m2level(room)
	var players []aiTarget
	for _, uid := range room.Players {
		sess := s.sessionInRoom(uid, room.Name)
		if sess == nil || sess.avatarBase == 0 || sess.isDead {
			continue
		}
		pos, ok := playerStreamPos(room, sess.avatarBase)
		if !ok {
			pos = room.playerPos[sess.avatarBase+1]
		}
		players = append(players, aiTarget{sess.avatarBase, pos, sess})
	}
	now := time.Now()
	acted := false
	for i := range room.npcs {
		npc := &room.npcs[i]
		if npc.hp > 0 && !npc.moving {
			if npcSyncGround(room, npc, dt) {
				acted = true
			}
		}
	}
	if len(players) == 0 {
		return acted
	}
	if room.cutsceneActive {
		return acted
	}
	for i := range room.npcs {
		npc := &room.npcs[i]
		if npc.hp <= 0 {
			continue
		}
		if npc.kit == "ClapTrapTutor" {
			continue
		}
		if npc.friendly {
			continue
		}
		if npc.jumping {
			if s.hostTickJumpAttack(room, npc, players, now) {
				acted = true
			}
			continue
		}
		if npc.taskType == bhvTaskChargeAttack && npc.moving && !npc.chargeHit {
			for j := range players {
				if players[j].view != npc.chargeTargetView {
					continue
				}
				dx, dz := players[j].pos[0]-npc.pos[0], players[j].pos[2]-npc.pos[2]
				hitRadius := npcChargeCheckRadius(npc.kit)
				if dx*dx+dz*dz <= float32(hitRadius*hitRadius) {
					npc.chargeHit = true
					_, dmgMult := s.npcCombatScale(room)
					dmg := int32(float32(npcChargeDamageForLevel(npc.kit, level)) * dmgMult)
					if s.hostDamagePlayer(room, players[j].sess, npc.viewBase, dmg, npcChargeSubType(npc.kit), elementNone) {
						log.Printf("[HOST] actor view=%d died (killed by npc %d %q charge-attack) room=%s", players[j].view, npc.viewBase, npc.kit, room.Name)
					}
					npc.moving, npc.path = false, nil
					npcSetTask(npc, bhvTaskChargeAttackHit, npc.chargeTargetView, npc.pos, 0)
					npc.attackBusyUntil = now.Add(npcMeleeSwingLock)
					acted = true
				}
				break
			}
			continue
		}
		if npc.taskType == bhvTaskPoisonDash && npc.moving && !npc.poisonDashHit {
			for j := range players {
				if players[j].view != npc.chargeTargetView {
					continue
				}
				dx, dz := players[j].pos[0]-npc.pos[0], players[j].pos[2]-npc.pos[2]
				if dx*dx+dz*dz <= float32(npcPoisonDashHitRadius*npcPoisonDashHitRadius) {
					npc.poisonDashHit = true
					_, dmgMult := s.npcCombatScale(room)
					dmg := int32(float32(npcPoisonDashDamage(npc.kit)) * levelDamageModifier(level) * dmgMult)
					if s.hostDamagePlayer(room, players[j].sess, npc.viewBase, dmg, attackSubTypeLegacy, elementCorrosive) {
						log.Printf("[HOST] actor view=%d died (killed by npc %d %q poison-dash) room=%s", players[j].view, npc.viewBase, npc.kit, room.Name)
					}
					npc.moving, npc.path = false, nil
					acted = true
				}
				break
			}
			continue
		}
		if now.Before(npc.dazedUntil) || now.Before(npc.confusedUntil) {
			continue
		}
		best := -1
		usedForcedAggro := false
		if npc.forcedAggroView != 0 {
			if !now.Before(npc.forcedAggroUntil) {
				npc.forcedAggroView = 0
			} else {
				for j := range players {
					if players[j].view == npc.forcedAggroView {
						best = j
						usedForcedAggro = true
						break
					}
				}
				if best < 0 {
					npc.forcedAggroView = 0
				}
			}
		}
		if best < 0 && npc.coverIdx >= 0 && npc.coverAggroView != 0 {
			if !now.Before(npc.coverAggroUntil) {
				npc.coverAggroView = 0
			} else {
				for j := range players {
					if players[j].view == npc.coverAggroView {
						best = j
						break
					}
				}
				if best < 0 {
					npc.coverAggroView = 0
				}
			}
		}
		if best < 0 {
			scale := npc.aggroRadiusScale
			if scale == 0 {
				scale = 1
			}
			base := npcAggroRadius
			if cfg := npcConfigFor(npc.kit); cfg != nil && cfg.DetectionRng > 0 {
				base = cfg.DetectionRng
			}
			radius := base * float64(scale)
			cutoff := float32(radius * radius)
			for j := range players {
				dx, dz := players[j].pos[0]-npc.pos[0], players[j].pos[2]-npc.pos[2]
				if d := dx*dx + dz*dz; d <= cutoff {
					cutoff = d
					best = j
				}
			}
		}
		var bestD float32
		if best >= 0 {
			dx, dz := players[best].pos[0]-npc.pos[0], players[best].pos[2]-npc.pos[2]
			bestD = dx*dx + dz*dz
		}
		if best < 0 {
			hadEngagement := npc.moving || !npc.nextRangedAtkAt.IsZero() || now.Before(npc.attackBusyUntil)
			if hadEngagement && !npc.patrolling {
				npc.moving, npc.path = false, nil
				npcSetTask(npc, bhvTaskIdle, -1, npc.pos, -1)
				npc.nextRangedAtkAt = time.Time{}
				npc.attackBusyUntil = time.Time{}
				acted = true
			}
			npc.pendingAggroAt = time.Time{}
			npc.aggroEngaged = false
			releaseCover(room, npc)
			if s.hostTickNpcPatrol(npc, m2level(room), now) {
				acted = true
			}
			continue
		}
		npc.patrolling = false
		p := players[best]

		if now.Before(npc.attackBusyUntil) {
			if npc.moving {
				npc.moving, npc.path = false, nil
			}
			dx, dz := float64(p.pos[0]-npc.pos[0]), float64(p.pos[2]-npc.pos[2])
			npc.yawDeg = float32(math.Mod(math.Atan2(dx, dz)*180/math.Pi+360, 360))
			acted = true
			continue
		}

		attackRange := npcAttackRangeFor(npc.kit)
		alreadyInRealRange := bestD <= float32(attackRange*attackRange)
		engaged := npc.moving || alreadyInRealRange || !npc.nextRangedAtkAt.IsZero()
		if engaged && !npc.moving && now.After(npc.nextGrenadeRollAt) {
			if cfg := npcConfigFor(npc.kit); cfg != nil && cfg.ThrowGrenadeChance > 0 {
				dist := math.Sqrt(float64(bestD))
				if dist > attackRange && dist <= npcThrowGrenadeMaxDist {
					npc.nextGrenadeRollAt = now.Add(npcThrowGrenadeEvery)
					if rand.Float64() < cfg.ThrowGrenadeChance {
						s.npcThrowGrenadeAttack(room, npc, p, now)
						acted = true
						continue
					}
				}
			}
		}
		if engaged && now.After(npc.nextAtkAt) {
			chosenAtk := npcChooseAttack(npcConfigFor(npc.kit), math.Sqrt(float64(bestD)), attackRange, room.bossPhase)
			if chosenAtk == atkJump {
				log.Printf("[AI] npc %d (%s) chose atkJump realDistToPlayer=%.2f attackRange=%.2f room=%s",
					npc.viewBase, npc.kit, math.Sqrt(float64(bestD)), attackRange, room.Name)
			}
			if chosenAtk != atkNone && chosenAtk != atkMelee &&
				chosenAtk != atkJump && chosenAtk != atkCharge && chosenAtk != atkPoisonDash {
				npc.attackBusyUntil = now.Add(npcAttackCommitLock)
			}
			switch chosenAtk {
			case atkJump:
				if now.After(npc.jumpCooldownUntil) {
					s.npcStartJumpAttack(room, npc, p, now)
					acted = true
					continue
				}
			case atkCharge:
				s.npcStartChargeAttack(room, npc, p, now)
				acted = true
				continue
			case atkSpit:
				s.npcDoSpitAttack(room, npc, p, level, now)
				acted = true
				continue
			case atkThrowAxe:
				s.npcThrowAxeAttack(room, npc, p, level, now)
				acted = true
				continue
			case atkSuicide:
				s.npcSuicideBombAttack(room, npc, level, now)
				acted = true
				continue
			case atkPoisonDash:
				s.npcDoPoisonDash(room, npc, p, level, now)
				acted = true
				continue
			case atkPoisonSpit:
				s.npcDoPoisonSpit(room, npc, p, level, now)
				acted = true
				continue
			case atkPoisonCrush:
				s.npcDoPoisonCrush(room, npc, p, level, now)
				acted = true
				continue
			case atkStun:
				s.npcStunAttack(room, npc, p, level, now)
				acted = true
				continue
			case atkWebTrap:
				s.npcWebTrapAttack(room, npc, p, now)
				acted = true
				continue
			case atkRailgun:
				s.npcRailgunAttack(room, npc, p, now)
				acted = true
				continue
			case atkMortar:
				s.npcMortarAttack(room, npc, p, now)
				acted = true
				continue
			case atkLaserSweep:
				s.npcLaserSweepAttack(room, npc, p, level, now)
				acted = true
				continue
			case atkJet:
				s.npcJetAttack(room, npc, p, now)
				acted = true
				continue
			}
		}
		var forceRelocate bool
		if alreadyInRealRange {
			forceRelocate = npcIsOverlapping(room, npc, npcHardStackDist)
		} else {
			crowdCheck := npcApplySpacing(room, npc, npc.pos)
			forceRelocate = crowdCheck[0] != npc.pos[0] || crowdCheck[2] != npc.pos[2]
		}
		ringDist := math.Max(0.5, float64(attackRange)-npcSpacingSafetyMargin)
		var dest [3]float32
		if alreadyInRealRange && forceRelocate {
			angle := float64(npc.viewBase%360) * math.Pi / 180
			dest = [3]float32{
				p.pos[0] + float32(math.Cos(angle))*float32(ringDist),
				p.pos[1],
				p.pos[2] + float32(math.Sin(angle))*float32(ringDist),
			}
		} else {
			dest = p.pos
			if apx, apz := npc.pos[0]-p.pos[0], npc.pos[2]-p.pos[2]; apx != 0 || apz != 0 {
				if apd := math.Sqrt(float64(apx*apx + apz*apz)); apd >= 0.01 {
					scale := float32(ringDist / apd)
					dest[0] = p.pos[0] + apx*scale
					dest[2] = p.pos[2] + apz*scale
				}
			}
			dest = npcApplySpacing(room, npc, dest)
			dest = npcClampDestToRange(dest, p.pos, ringDist)
		}
		if forceRelocate {
			ddx, ddz := dest[0]-npc.pos[0], dest[2]-npc.pos[2]
			if ddx*ddx+ddz*ddz < 0.0025 {
				forceRelocate = false
			}
		}
		engage := attackRange
		if bestD > float32(engage*engage) || forceRelocate {
			if !npc.moving && usedForcedAggro {
				npc.pendingAggroAt = time.Time{}
				if !npc.aggroEngaged && now.Sub(npc.lastForcedAggroLogAt) > 2*time.Second {
					npc.lastForcedAggroLogAt = now
					npc.aggroEngaged = true
					log.Printf("[AI] npc %d (%s) forced-aggro (was shot) on view=%d dist=%.1f room=%s",
						npc.viewBase, npc.kit, p.view, math.Sqrt(float64(bestD)), room.Name)
				}
			} else if !npc.moving && !alreadyInRealRange && !npc.aggroEngaged {
				if npc.pendingAggroAt.IsZero() {
					delayMaxMs := 500.0
					if cfg := npcConfigFor(npc.kit); cfg != nil && cfg.SelectTargetTimer > 0 {
						delayMaxMs = cfg.SelectTargetTimer * 1000
					}
					npc.pendingAggroAt = now.Add(time.Duration(randRange(0, delayMaxMs)) * time.Millisecond)
				}
				if now.Before(npc.pendingAggroAt) {
					continue
				}
				npc.pendingAggroAt = time.Time{}
				npc.aggroEngaged = true
				log.Printf("[AI] npc %d (%s) aggro on view=%d dist=%.1f room=%s",
					npc.viewBase, npc.kit, p.view, math.Sqrt(float64(bestD)), room.Name)
			}
			if npcWeaponFor(npc.kit) != nil {
				if a, handled := s.hostTickNpcCoverEngage(room, npc, p, now); handled {
					if a {
						acted = true
					}
					continue
				}
				if a, handled := s.hostTickNpcRangedEngage(room, npc, p, now); handled {
					if a {
						acted = true
					}
					continue
				}
			}
			if len(npc.path) == 0 || now.After(npc.nextRepathAt) {
				if wps, ok := navmeshFindPath(m2level(room), npc.pos[0], npc.pos[2], dest[0], dest[2], npc.pos[1]); ok {
					npc.path = wps
				} else {
					npc.path = [][3]float32{dest}
				}
				npc.nextRepathAt = now.Add(navmeshRepathInterval)
			}
			npc.moving = true
			npcSetTask(npc, npcMoveTask(npc.kit), -1, dest, 2)
			continue
		}
		npc.moving, npc.path = false, nil
		dx, dz := float64(p.pos[0]-npc.pos[0]), float64(p.pos[2]-npc.pos[2])
		npc.yawDeg = float32(math.Mod(math.Atan2(dx, dz)*180/math.Pi+360, 360))
		acted = true
		if bestD > float32(attackRange*attackRange) {
			continue
		}
		if now.Before(npc.nextAtkAt) {
			continue
		}
		npc.nextAtkAt = now.Add(npcMeleeEvery)
		npc.attackBusyUntil = now.Add(npcMeleeSwingLock)
		npc.taskUID = (npc.taskUID + 1) % 126
		npc.taskType = bhvTaskMeleeAttack
		if hammerAttackKits[npc.kit] {
			npc.taskType = bhvTaskHammerAttack
		}
		if swordSlashAttackKits[npc.kit] {
			npc.taskType = bhvTaskSwordSlash
		}
		if flyMeleeAttackKits[npc.kit] {
			npc.taskType = bhvTaskFlyMeleeAttack
		}
		npc.taskTarget = p.view
		_, dmgMult := s.npcCombatScale(room)
		dmg := int32(float32(npcMeleeDamageForLevel(npc.kit, level)) * dmgMult)
		if s.hostDamagePlayer(room, p.sess, npc.viewBase, dmg, npcMeleeSubType(npc.kit), elementNone) {
			log.Printf("[HOST] actor view=%d died (killed by npc %d %q) room=%s", p.view, npc.viewBase, npc.kit, room.Name)
		}
	}
	return acted
}

type npcAttackKind int

const (
	atkNone npcAttackKind = iota
	atkMelee
	atkJump
	atkCharge
	atkSpit
	atkThrowAxe
	atkSuicide
	atkPoisonDash
	atkPoisonSpit
	atkPoisonCrush
	atkWarcry
	atkStun
	atkWebTrap
	atkRailgun
	atkJet
	atkMortar
	atkLaserSweep
)

func npcChooseAttack(cfg *npcConfig, dist, meleeRange float64, bossPhase int) npcAttackKind {
	type cand struct {
		kind   npcAttackKind
		weight float64
	}
	var cands []cand
	if cfg != nil {
		dashW, spitPoisonW, crushW := npcPoisonPhaseWeightsFor(cfg, bossPhase)
		if dashW > 0 && dist >= cfg.PoisonDashTriggerDist && dist <= cfg.PoisonDashDist {
			cands = append(cands, cand{atkPoisonDash, dashW})
		}
		if spitPoisonW > 0 && dist <= cfg.PoisonSpitDist {
			cands = append(cands, cand{atkPoisonSpit, spitPoisonW})
		}
		if crushW > 0 && dist <= cfg.PoisonCrushDist {
			cands = append(cands, cand{atkPoisonCrush, crushW})
		}
		if cfg.JumpWeight > 0 && dist >= cfg.JumpDistanceMin && dist <= cfg.JumpDistanceMax && dist > meleeRange {
			cands = append(cands, cand{atkJump, cfg.JumpWeight})
		}
		_, railgunW, jetW, chargeW, mortarW, laserSweepW := npcKnoxxPhaseWeightsFor(cfg, bossPhase)
		if chargeW > 0 && dist >= cfg.ChargeDistanceMin && dist <= cfg.ChargeDistanceMax {
			cands = append(cands, cand{atkCharge, chargeW})
		}
		if railgunW > 0 {
			cands = append(cands, cand{atkRailgun, railgunW})
		}
		if mortarW > 0 {
			cands = append(cands, cand{atkMortar, mortarW})
		}
		if laserSweepW > 0 {
			cands = append(cands, cand{atkLaserSweep, laserSweepW})
		}
		if jetW > 0 && dist >= cfg.JetDistanceMin && dist <= cfg.JetDistanceMax {
			cands = append(cands, cand{atkJet, jetW})
		}
		if cfg.SpitWeight > 0 && dist >= cfg.SpitDistanceMin && dist <= cfg.SpitDistanceMax {
			cands = append(cands, cand{atkSpit, cfg.SpitWeight})
		}
		if cfg.ThrowAxeDistanceMin > 0 && dist >= cfg.ThrowAxeDistanceMin && dist <= cfg.ThrowAxeDistanceMax {
			cands = append(cands, cand{atkThrowAxe, 1})
		}
		if cfg.StunDamage > 0 && dist <= meleeRange {
			cands = append(cands, cand{atkStun, npcStunWeight})
		}
		if cfg.WebTrapWeight > 0 && dist > meleeRange {
			cands = append(cands, cand{atkWebTrap, cfg.WebTrapWeight})
		}
	}
	suicideEligible := cfg != nil && cfg.SuicideDistance > 0 && dist <= cfg.SuicideDistance
	if suicideEligible {
		cands = append(cands, cand{atkSuicide, 1})
	} else if dist <= meleeRange {
		w := 0.8
		if cfg != nil && cfg.MeleeWeight > 0 {
			w = cfg.MeleeWeight
		}
		cands = append(cands, cand{atkMelee, w})
	}
	if len(cands) == 0 {
		return atkNone
	}
	total := 0.0
	for _, c := range cands {
		total += c.weight
	}
	r := rand.Float64() * total
	for _, c := range cands {
		r -= c.weight
		if r <= 0 {
			return c.kind
		}
	}
	return cands[len(cands)-1].kind
}

const (
	npcThrowGrenadeMaxDist    = 25.0
	npcThrowGrenadeEvery      = 4 * time.Second
	npcGrenadeFuse            = 2 * time.Second
	npcGrenadeThrowFlightTime = 700 * time.Millisecond
)

func (s *EnetServer) npcThrowGrenadeAttack(room *Room, npc *spawnedNpc, p aiTarget, now time.Time) {
	npc.moving, npc.path = false, nil
	dx, dz := float64(p.pos[0]-npc.pos[0]), float64(p.pos[2]-npc.pos[2])
	npc.yawDeg = float32(math.Mod(math.Atan2(dx, dz)*180/math.Pi+360, 360))

	npc.nextAtkAt = now.Add(npcThrowGrenadeEvery)
	npc.taskUID = (npc.taskUID + 1) % 126
	npc.taskType = bhvTaskThrowAttack
	npc.taskTarget = p.view

	hand := [3]float32{npc.pos[0], npc.pos[1] + 1.5, npc.pos[2]}
	t := float32(npcGrenadeThrowFlightTime.Seconds())
	vel := [3]float32{
		(p.pos[0] - hand[0]) / t,
		(p.pos[1]-hand[1])/t + 0.5*9.81*t,
		(p.pos[2] - hand[2]) / t,
	}

	view := room.NextAvatarViewBase()
	views := []int32{view, view + 1, view + 2}
	s.hostBroadcastJournaled(room, 202, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): "Entities/Object/Launchable/Grenade",
			byte(1): vector3Custom(hand[0], hand[1], hand[2]),
			byte(4): IntArray{Items: views},
			byte(5): []any{npc.viewBase},
			byte(6): int32(msNow()),
			byte(7): view,
		},
	}, false)
	s.broadcastEvent(room, 201, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): int32(msNow()),
			Short(1): map[any]any{
				byte(0): view + 2,
				byte(1): []any{
					vector3Custom(hand[0], hand[1], hand[2]),
					quaternionCustom(0, 0, 0, 1),
					vector3Custom(vel[0], vel[1], vel[2]),
					vector3Custom(4, 8, 2),
				},
			},
			Short(2): map[any]any{
				byte(0): view + 1,
				byte(1): grenadeTaskStream(bhvTaskCountDown, float32(npcGrenadeFuse.Seconds())),
			},
		},
	}, nil, nil)

	room.pendingNpcGrenades = append(room.pendingNpcGrenades, pendingNpcGrenade{
		view: view, npcView: npc.viewBase, pos: p.pos, at: now.Add(npcGrenadeFuse),
	})
	log.Printf("[AI] npc %d (%s) throws a grenade at view=%d room=%s", npc.viewBase, npc.kit, p.view, room.Name)
}

func (s *EnetServer) hostTickNpcGrenades(room *Room) bool {
	if len(room.pendingNpcGrenades) == 0 {
		return false
	}
	now := time.Now()
	remaining := room.pendingNpcGrenades[:0]
	exploded := false
	for _, g := range room.pendingNpcGrenades {
		if now.Before(g.at) {
			remaining = append(remaining, g)
			continue
		}
		s.hostExplodeNpcGrenade(room, g)
		exploded = true
	}
	room.pendingNpcGrenades = remaining
	return exploded
}

func (s *EnetServer) hostExplodeNpcGrenade(room *Room, g pendingNpcGrenade) {
	s.broadcastEvent(room, 201, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): int32(msNow()),
			Short(1): map[any]any{
				byte(0): g.view + 1,
				byte(1): grenadeTaskStream(bhvTaskBlowUp, 0),
			},
		},
	}, nil, nil)
	const r2 = float32(playerGrenadeBlowUpRadius * playerGrenadeBlowUpRadius)
	level := m2level(room)
	_, dmgMult := s.npcCombatScale(room)
	rating := float32(powerRatingForLevel(s.roomEffectiveInstanceLevel(room)))
	dmg := int32(float32(grenadeBlowUpDamage) * levelDamageModifier(level) * dmgMult * rating)
	for _, uid := range room.Players {
		target := s.sessionInRoom(uid, room.Name)
		if target == nil || target.avatarBase == 0 {
			continue
		}
		ppos, ok := playerStreamPos(room, target.avatarBase)
		if !ok {
			ppos = room.playerPos[target.avatarBase+1]
		}
		dx, dz := g.pos[0]-ppos[0], g.pos[2]-ppos[2]
		if dx*dx+dz*dz > r2 {
			continue
		}
		if s.hostDamagePlayer(room, target, g.npcView, dmg, attackSubTypeLegacy, elementExplosive) {
			log.Printf("[HOST] actor view=%d died (killed by npc %d grenade) room=%s", target.avatarBase, g.npcView, room.Name)
		}
	}
	room.deferredDestroys = append(room.deferredDestroys, deferredDestroy{
		view: g.view, at: time.Now().Add(grenadeDestroyDelay),
	})
	log.Printf("[HOST] npc grenade exploded viewID=%d room=%s", g.view, room.Name)
}

const (
	npcWarcryRadius = 6.0
	npcWarcryEvery  = 4 * time.Second
)

func npcWarcryDamage(kit string) int32 {
	if cfg := npcConfigFor(kit); cfg != nil && cfg.WarcryDamage > 0 {
		return cfg.WarcryDamage
	}
	return 60
}

const (
	npcWarcryBurstFraction = 0.25
	npcWarcryEnrageHP      = 0.5
)

func (s *EnetServer) npcMaybeWarcry(room *Room, npc *spawnedNpc, damage, weakPointIndex int32, isDOT bool) {
	if isDOT || npc.maxHP <= 0 {
		return
	}
	cfg := npcConfigFor(npc.kit)
	if cfg == nil || cfg.WarcryDamage <= 0 {
		return
	}
	now := time.Now()
	if now.Before(npc.nextWarcryAt) {
		return
	}
	bigHit := weakPointIndex >= 0 || float64(damage) >= float64(npc.maxHP)*npcWarcryBurstFraction
	enrage := !npc.warcriedEnrage && float64(npc.hp) <= float64(npc.maxHP)*npcWarcryEnrageHP
	if !bigHit && !enrage {
		return
	}
	if enrage {
		npc.warcriedEnrage = true
	}
	s.npcWarcryAttack(room, npc, m2level(room), now)
}

func (s *EnetServer) npcWarcryAttack(room *Room, npc *spawnedNpc, level string, now time.Time) {
	npc.moving, npc.path = false, nil
	npc.nextWarcryAt = now.Add(npcWarcryEvery)
	npc.attackBusyUntil = now.Add(npcAttackCommitLock)
	npc.taskUID = (npc.taskUID + 1) % 126
	npc.taskType = bhvTaskWarcry
	npc.taskTarget = npc.viewBase

	_, dmgMult := s.npcCombatScale(room)
	dmg := int32(float32(npcWarcryDamage(npc.kit)) * levelDamageModifier(level) * dmgMult)
	r2 := float32(npcWarcryRadius * npcWarcryRadius)
	for _, uid := range room.Players {
		target := s.sessionInRoom(uid, room.Name)
		if target == nil || target.avatarBase == 0 || target.isDead {
			continue
		}
		pos, ok := playerStreamPos(room, target.avatarBase)
		if !ok {
			pos = room.playerPos[target.avatarBase+1]
		}
		dx, dz := npc.pos[0]-pos[0], npc.pos[2]-pos[2]
		if dx*dx+dz*dz > r2 {
			continue
		}
		if s.hostDamagePlayer(room, target, npc.viewBase, dmg, attackSubTypeLegacy, elementNone) {
			log.Printf("[HOST] actor view=%d died (killed by npc %d %q warcry) room=%s", target.avatarBase, npc.viewBase, npc.kit, room.Name)
		}
	}
	log.Printf("[AI] npc %d (%s) warcries room=%s", npc.viewBase, npc.kit, room.Name)
}

const npcStunWeight = 0.4

func npcStunDamage(kit string) int32 {
	if cfg := npcConfigFor(kit); cfg != nil && cfg.StunDamage > 0 {
		return cfg.StunDamage
	}
	return 30
}

func npcStunTime(kit string) float64 {
	if cfg := npcConfigFor(kit); cfg != nil && cfg.StunTime > 0 {
		return cfg.StunTime
	}
	return 1.2
}

func npcStunCooldown(kit string) float64 {
	if cfg := npcConfigFor(kit); cfg != nil && cfg.StunCooldown > 0 {
		return cfg.StunCooldown
	}
	return 5
}

func (s *EnetServer) npcStunAttack(room *Room, npc *spawnedNpc, p aiTarget, level string, now time.Time) {
	npc.moving, npc.path = false, nil
	dx, dz := float64(p.pos[0]-npc.pos[0]), float64(p.pos[2]-npc.pos[2])
	npc.yawDeg = float32(math.Mod(math.Atan2(dx, dz)*180/math.Pi+360, 360))

	npc.nextAtkAt = now.Add(time.Duration(npcStunCooldown(npc.kit) * float64(time.Second)))
	npc.taskUID = (npc.taskUID + 1) % 126
	npc.taskType = bhvTaskMeleeAttack
	npc.taskTarget = p.view

	_, dmgMult := s.npcCombatScale(room)
	dmg := int32(float32(npcStunDamage(npc.kit)) * levelDamageModifier(level) * dmgMult)
	killed := s.hostDamagePlayer(room, p.sess, npc.viewBase, dmg, attackSubTypeLegacy, elementNone)
	if killed {
		log.Printf("[HOST] actor view=%d died (killed by npc %d %q stun slam) room=%s", p.view, npc.viewBase, npc.kit, room.Name)
		return
	}
	if p.sess.avatarBase == 0 {
		return
	}
	p.sess.stunTaskUID = (p.sess.stunTaskUID + 1) % 126
	s.broadcastEvent(room, 201, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): int32(msNow()),
			Short(1): map[any]any{
				byte(0): p.sess.avatarBase + 2,
				byte(1): playerBhvTaskStream(p.sess.stunTaskUID, bhvTaskPlayerStun, float32(npcStunTime(npc.kit)), -1, false),
			},
		},
	}, nil, nil)
	room.pendingPlayerUnstuns = append(room.pendingPlayerUnstuns, pendingPlayerUnstun{
		sess: p.sess, at: now.Add(time.Duration(npcStunTime(npc.kit) * float64(time.Second))),
	})
	log.Printf("[AI] npc %d (%s) stuns view=%d room=%s", npc.viewBase, npc.kit, p.view, room.Name)
}

func npcWebTrapCooldown(kit string) float64 {
	if cfg := npcConfigFor(kit); cfg != nil && cfg.WebTrapCooldown > 0 {
		return cfg.WebTrapCooldown
	}
	return 7
}

func (s *EnetServer) npcWebTrapAttack(room *Room, npc *spawnedNpc, p aiTarget, now time.Time) {
	npc.moving, npc.path = false, nil
	dx, dz := float64(p.pos[0]-npc.pos[0]), float64(p.pos[2]-npc.pos[2])
	npc.yawDeg = float32(math.Mod(math.Atan2(dx, dz)*180/math.Pi+360, 360))

	npc.nextAtkAt = now.Add(time.Duration(npcWebTrapCooldown(npc.kit) * float64(time.Second)))
	npc.taskUID = (npc.taskUID + 1) % 126
	npc.taskType = bhvTaskWebTrap
	npc.taskTarget = p.view
	npc.taskDest = p.pos
	log.Printf("[AI] npc %d (%s) throws web trap at view=%d room=%s", npc.viewBase, npc.kit, p.view, room.Name)
}

const npcFurySpeedMult = 1.4

func npcFuryThresholdCrossed(hp int32, furyHP float64) bool {
	return furyHP > 0 && hp > 0 && float64(hp) <= furyHP
}

const (
	jumpAttackTakeoffDur  = 300 * time.Millisecond
	jumpAttackAirSpeed    = 10.0 // u/s
	jumpAttackLandRecover = 400 * time.Millisecond
	jumpAttackMinAirTime  = 150 * time.Millisecond
	jumpAttackArcHeight   = 1.8
	jumpAttackFullArcDist = 4.0
	jumpAttackMinArcScale = 0.35
)

var npcJumpArcHeightScale = map[string]float32{
	"BanditApollyon": 0.5,
}

func jumpArcHeightScaleFor(kit string) float32 {
	if scale, ok := npcJumpArcHeightScale[kit]; ok {
		return scale
	}
	return 1
}

func (s *EnetServer) npcStartJumpAttack(room *Room, npc *spawnedNpc, p aiTarget, now time.Time) {
	attackRange := npcAttackRangeFor(npc.kit)
	ringDist := math.Max(0.5, attackRange-0.3)
	land := p.pos
	if dx, dz := float64(npc.pos[0]-p.pos[0]), float64(npc.pos[2]-p.pos[2]); dx != 0 || dz != 0 {
		if d := math.Sqrt(dx*dx + dz*dz); d >= 0.01 {
			scale := float32(ringDist / d)
			land[0] = p.pos[0] + float32(dx)*scale
			land[2] = p.pos[2] + float32(dz)*scale
		}
	}
	npc.jumping = true
	npc.jumpFrom = npc.pos
	npc.jumpTo = land
	npc.jumpTargetView = p.view
	npc.jumpDidDamage = false
	npc.moving, npc.path = false, nil

	dist := math.Hypot(float64(land[0]-npc.pos[0]), float64(land[2]-npc.pos[2]))
	airDur := time.Duration(dist / jumpAttackAirSpeed * float64(time.Second))
	if airDur < jumpAttackMinAirTime {
		airDur = jumpAttackMinAirTime
	}
	arcScale := dist / jumpAttackFullArcDist
	if arcScale > 1 {
		arcScale = 1
	} else if arcScale < jumpAttackMinArcScale {
		arcScale = jumpAttackMinArcScale
	}
	npc.jumpArcHeight = jumpAttackArcHeight * float32(arcScale) * jumpArcHeightScaleFor(npc.kit)
	npc.jumpAirStart = now.Add(jumpAttackTakeoffDur)
	npc.jumpAirEnd = npc.jumpAirStart.Add(airDur)
	npc.jumpLandUntil = npc.jumpAirEnd.Add(jumpAttackLandRecover)
	npc.nextAtkAt = npc.jumpLandUntil
	if cfg := npcConfigFor(npc.kit); cfg != nil && cfg.JumpCooldown > 0 {
		npc.jumpCooldownUntil = now.Add(time.Duration(cfg.JumpCooldown * float64(time.Second)))
	}

	npc.taskUID = (npc.taskUID + 1) % 126
	npc.taskType = bhvTaskJumpAttack
	if hammerAttackKits[npc.kit] {
		npc.taskType = bhvTaskHammerJumpAttack
	}
	npc.taskTarget = p.view
	npc.taskDest = land
	dx, dz := float64(p.pos[0]-npc.pos[0]), float64(p.pos[2]-npc.pos[2])
	npc.yawDeg = float32(math.Mod(math.Atan2(dx, dz)*180/math.Pi+360, 360))
	log.Printf("[AI] npc %d (%s) jump-attacks view=%d dist=%.1f airDur=%s arcHeight=%.2f from=[%.1f %.1f %.1f] to=[%.1f %.1f %.1f] room=%s",
		npc.viewBase, npc.kit, p.view, dist, airDur, npc.jumpArcHeight,
		npc.jumpFrom[0], npc.jumpFrom[1], npc.jumpFrom[2], npc.jumpTo[0], npc.jumpTo[1], npc.jumpTo[2], room.Name)
}

func (s *EnetServer) hostTickJumpAttack(room *Room, npc *spawnedNpc, players []aiTarget, now time.Time) bool {
	switch {
	case now.Before(npc.jumpAirStart):
		return false
	case now.Before(npc.jumpAirEnd):
		total := npc.jumpAirEnd.Sub(npc.jumpAirStart)
		frac := float32(now.Sub(npc.jumpAirStart)) / float32(total)
		if frac > 1 {
			frac = 1
		}
		npc.pos[0] = npc.jumpFrom[0] + (npc.jumpTo[0]-npc.jumpFrom[0])*frac
		npc.pos[2] = npc.jumpFrom[2] + (npc.jumpTo[2]-npc.jumpFrom[2])*frac
		baseY := npc.jumpFrom[1] + (npc.jumpTo[1]-npc.jumpFrom[1])*frac
		npc.pos[1] = baseY + npc.jumpArcHeight*4*frac*(1-frac)
		return true
	case now.Before(npc.jumpLandUntil):
		if !npc.jumpDidDamage {
			npc.jumpDidDamage = true
			npc.pos = npc.jumpTo
			if gy, ok := groundHint(m2level(room), npc.pos[0], npc.pos[2], npc.pos[1]); ok {
				npc.pos[1] = gy
				npc.groundY = gy
			}
			log.Printf("[AI] npc %d (%s) jump-attack landed at [%.1f %.1f %.1f] actualAirTime=%s (wanted %s) room=%s",
				npc.viewBase, npc.kit, npc.pos[0], npc.pos[1], npc.pos[2], now.Sub(npc.jumpAirStart), npc.jumpAirEnd.Sub(npc.jumpAirStart), room.Name)
			for j := range players {
				if players[j].view != npc.jumpTargetView {
					continue
				}
				dx, dz := players[j].pos[0]-npc.pos[0], players[j].pos[2]-npc.pos[2]
				landRange := npcAttackRangeFor(npc.kit) + 2
				if dx*dx+dz*dz <= float32(landRange*landRange) {
					_, dmgMult := s.npcCombatScale(room)
					dmg := int32(float32(npcJumpDamageForLevel(npc.kit, m2level(room))) * dmgMult)
					if s.hostDamagePlayer(room, players[j].sess, npc.viewBase, dmg, npcJumpSubType(npc.kit), elementNone) {
						log.Printf("[HOST] actor view=%d died (killed by npc %d %q jump-attack) room=%s", players[j].view, npc.viewBase, npc.kit, room.Name)
					}
				}
				break
			}
		}
		return true
	default:
		npc.jumping = false
		return true
	}
}

func (s *EnetServer) npcStartChargeAttack(room *Room, npc *spawnedNpc, p aiTarget, now time.Time) {
	behind := 2.0
	if cfg := npcConfigFor(npc.kit); cfg != nil && cfg.ChargeDistanceBehindTarget > 0 {
		behind = cfg.ChargeDistanceBehindTarget
	}
	dx, dz := float64(p.pos[0]-npc.pos[0]), float64(p.pos[2]-npc.pos[2])
	dest := p.pos
	if d := math.Sqrt(dx*dx + dz*dz); d >= 0.01 {
		scale := float32((d + behind) / d)
		dest[0] = npc.pos[0] + float32(dx)*scale
		dest[2] = npc.pos[2] + float32(dz)*scale
	}
	npc.path = [][3]float32{dest}
	npc.moving = true
	npc.taskSpeed = 2
	npc.chargeHit = false
	npc.chargeTargetView = p.view
	npc.taskUID = (npc.taskUID + 1) % 126
	npc.taskType = bhvTaskChargeAttack
	npc.taskTarget = p.view
	npc.taskDest = dest

	travel := math.Hypot(float64(dest[0]-npc.pos[0]), float64(dest[2]-npc.pos[2])) / npcChargeSpeed(npc.kit)
	npc.nextAtkAt = now.Add(time.Duration(travel*float64(time.Second)) + time.Second)
	log.Printf("[AI] npc %d (%s) charge-attacks view=%d room=%s", npc.viewBase, npc.kit, p.view, room.Name)
}

const npcSpitSpeed = 18.0

func (s *EnetServer) npcDoSpitAttack(room *Room, npc *spawnedNpc, p aiTarget, level string, now time.Time) {
	npc.moving, npc.path = false, nil
	dx, dz := float64(p.pos[0]-npc.pos[0]), float64(p.pos[2]-npc.pos[2])
	npc.yawDeg = float32(math.Mod(math.Atan2(dx, dz)*180/math.Pi+360, 360))

	idle := 1.2
	if cfg := npcConfigFor(npc.kit); cfg != nil && cfg.SpitIdleTime > 0 {
		idle = cfg.SpitIdleTime
	}
	npc.nextAtkAt = now.Add(time.Duration(idle * float64(time.Second)))

	npc.taskUID = (npc.taskUID + 1) % 126
	npc.taskType = bhvTaskSpit
	npc.taskTarget = p.view

	dist := math.Hypot(dx, dz)
	travelTime := time.Duration(dist / npcSpitSpeed * float64(time.Second))
	if travelTime < 100*time.Millisecond {
		travelTime = 100 * time.Millisecond
	}

	_, dmgMult := s.npcCombatScale(room)
	dmg := int32(float32(npcMeleeDamageForLevel(npc.kit, level)) * dmgMult)
	room.pendingSpitDamages = append(room.pendingSpitDamages, pendingSpitDamage{
		sess: p.sess, targetView: p.view, attackerView: npc.viewBase, npcKit: npc.kit, dmg: dmg, at: now.Add(travelTime),
	})
	log.Printf("[AI] npc %d (%s) spits at view=%d dist=%.1f travelTime=%s room=%s", npc.viewBase, npc.kit, p.view, dist, travelTime, room.Name)
}

func (s *EnetServer) hostTickPendingSpitDamages(room *Room) {
	if len(room.pendingSpitDamages) == 0 {
		return
	}
	now := time.Now()
	remaining := room.pendingSpitDamages[:0]
	for _, d := range room.pendingSpitDamages {
		if now.Before(d.at) {
			remaining = append(remaining, d)
			continue
		}
		if d.sess.avatarBase != 0 && !d.sess.isDead {
			if s.hostDamagePlayer(room, d.sess, d.attackerView, d.dmg, attackSubTypeLegacy, elementCorrosive) {
				log.Printf("[HOST] actor view=%d died (killed by npc %d %q spit) room=%s", d.targetView, d.attackerView, d.npcKit, room.Name)
			}
		}
	}
	room.pendingSpitDamages = remaining
}

func (s *EnetServer) hostTickPendingNpcAreaHits(room *Room) {
	if len(room.pendingNpcAreaHits) == 0 {
		return
	}
	now := time.Now()
	remaining := room.pendingNpcAreaHits[:0]
	for _, h := range room.pendingNpcAreaHits {
		if now.Before(h.at) {
			remaining = append(remaining, h)
			continue
		}
		r2 := h.radius * h.radius
		_, dmgMult := s.npcCombatScale(room)
		for _, uid := range room.Players {
			target := s.sessionInRoom(uid, room.Name)
			if target == nil || target.avatarBase == 0 || target.isDead {
				continue
			}
			pos, ok := playerStreamPos(room, target.avatarBase)
			if !ok {
				pos = room.playerPos[target.avatarBase+1]
			}
			dx, dz := h.pos[0]-pos[0], h.pos[2]-pos[2]
			if dx*dx+dz*dz > r2 {
				continue
			}
			dmg := h.dmg
			if h.hpFraction > 0 && target.hostChar != nil {
				dmg = int32(float64(maxLifeForCharacter(target.hostChar)) * h.hpFraction)
			}
			dmg = int32(float32(dmg) * dmgMult)
			if s.hostDamagePlayer(room, target, h.attackerView, dmg, attackSubTypeLegacy, h.element) {
				log.Printf("[HOST] actor view=%d died (killed by npc %d %q area hit) room=%s", target.avatarBase, h.attackerView, h.npcKit, room.Name)
			}
		}
	}
	room.pendingNpcAreaHits = remaining
}

var lanceTurretZone2SpawnPos = [3]float32{0.0, 18.952, -34.79}

func (s *EnetServer) hostTeleportPlayersToLanceTurretZone2(room *Room) {
	pos := lanceTurretZone2SpawnPos
	for actorNr, uid := range room.Players {
		psess := s.sessionInRoom(uid, room.Name)
		if psess == nil {
			continue
		}
		s.hostSpawnAvatar(room, psess, actorNr, &pos)
		psess.playerState = PlayerStateSpawned
		log.Printf("[HOST] teleported actor=%d to lance turret zone 2 pos=%v room=%s", actorNr, pos, room.Name)
	}
	s.hostSendSerialize(room)
}

const (
	npcRailgunCooldown    = 3 * time.Second
	npcMortarCooldown     = 30 * time.Second
	npcLaserSweepCooldown = 30 * time.Second
)

func (s *EnetServer) npcRailgunAttack(room *Room, npc *spawnedNpc, p aiTarget, now time.Time) {
	npc.moving, npc.path = false, nil
	dx, dz := float64(p.pos[0]-npc.pos[0]), float64(p.pos[2]-npc.pos[2])
	npc.yawDeg = float32(math.Mod(math.Atan2(dx, dz)*180/math.Pi+360, 360))

	cfg := npcConfigFor(npc.kit)
	speed, radius, dmgFrac := 20.0, 1.5, 0.15
	if cfg != nil {
		if cfg.PlasmaBallSpeed > 0 {
			speed = cfg.PlasmaBallSpeed
		}
		if cfg.PlasmaBallRadius > 0 {
			radius = cfg.PlasmaBallRadius
		}
		if cfg.PlasmaBallDamage > 0 {
			dmgFrac = cfg.PlasmaBallDamage
		}
	}
	dist := math.Hypot(dx, dz)
	travelTime := time.Duration(dist / speed * float64(time.Second))
	if travelTime < 150*time.Millisecond {
		travelTime = 150 * time.Millisecond
	}

	npc.nextAtkAt = now.Add(npcRailgunCooldown)
	npc.taskUID = (npc.taskUID + 1) % 126
	npc.taskType = bhvTaskRailgunAttack
	npc.taskTarget = p.view

	room.pendingNpcAreaHits = append(room.pendingNpcAreaHits, pendingNpcAreaHit{
		attackerView: npc.viewBase, npcKit: npc.kit, pos: p.pos, radius: float32(radius),
		hpFraction: dmgFrac, element: elementNone, at: now.Add(travelTime),
	})
	log.Printf("[AI] npc %d (%s) fires railgun at view=%d dist=%.1f room=%s", npc.viewBase, npc.kit, p.view, dist, room.Name)
}

func (s *EnetServer) npcMortarAttack(room *Room, npc *spawnedNpc, p aiTarget, now time.Time) {
	npc.moving, npc.path = false, nil
	dx, dz := float64(p.pos[0]-npc.pos[0]), float64(p.pos[2]-npc.pos[2])
	npc.yawDeg = float32(math.Mod(math.Atan2(dx, dz)*180/math.Pi+360, 360))

	cfg := npcConfigFor(npc.kit)
	dmg, amount, radius, speed := int32(30), 3, 2.5, 5.0
	if cfg != nil {
		if cfg.MortarShellDamage > 0 {
			dmg = cfg.MortarShellDamage
		}
		if cfg.MortarShellAmount > 0 {
			amount = int(cfg.MortarShellAmount)
		}
		if cfg.MortarShellRadius > 0 {
			radius = cfg.MortarShellRadius
		}
		if cfg.MortarShellSpeed > 0 {
			speed = cfg.MortarShellSpeed
		}
	}
	dist := math.Hypot(dx, dz)
	baseTravel := time.Duration(dist / speed * float64(time.Second))
	if baseTravel < 300*time.Millisecond {
		baseTravel = 300 * time.Millisecond
	}

	npc.nextAtkAt = now.Add(npcMortarCooldown)
	npc.taskUID = (npc.taskUID + 1) % 126
	npc.taskType = bhvTaskMortarAttack
	npc.taskTarget = p.view

	_, dmgMult := s.npcCombatScale(room)
	for i := 0; i < amount; i++ {
		landPos := [3]float32{
			p.pos[0] + float32(randRange(-radius, radius)),
			p.pos[1],
			p.pos[2] + float32(randRange(-radius, radius)),
		}
		room.pendingNpcAreaHits = append(room.pendingNpcAreaHits, pendingNpcAreaHit{
			attackerView: npc.viewBase, npcKit: npc.kit, pos: landPos, radius: float32(radius),
			dmg: int32(float32(dmg) * dmgMult), element: elementNone,
			at: now.Add(baseTravel + time.Duration(i)*200*time.Millisecond),
		})
	}
	log.Printf("[AI] npc %d (%s) fires a %d-shell mortar barrage at view=%d dist=%.1f room=%s", npc.viewBase, npc.kit, amount, p.view, dist, room.Name)
}

func (s *EnetServer) npcJetAttack(room *Room, npc *spawnedNpc, p aiTarget, now time.Time) {
	dx, dz := float64(p.pos[0]-npc.pos[0]), float64(p.pos[2]-npc.pos[2])
	npc.yawDeg = float32(math.Mod(math.Atan2(dx, dz)*180/math.Pi+360, 360))

	cfg := npcConfigFor(npc.kit)
	dmg, radius, speed, idle := int32(90), 8.0, 15.0, 1.0
	if cfg != nil {
		if cfg.JetAttackDamage > 0 {
			dmg = cfg.JetAttackDamage
		}
		if cfg.JetDamageRadius > 0 {
			radius = cfg.JetDamageRadius
		}
		if cfg.JetFlyingSpeed > 0 {
			speed = cfg.JetFlyingSpeed
		}
		if cfg.JetIdleTime > 0 {
			idle = cfg.JetIdleTime
		}
	}
	dist := math.Hypot(dx, dz)
	travelTime := time.Duration(dist / speed * float64(time.Second))
	if travelTime < 200*time.Millisecond {
		travelTime = 200 * time.Millisecond
	}

	npc.moving, npc.path = false, nil
	npc.nextAtkAt = now.Add(travelTime + time.Duration(idle*float64(time.Second)))
	npc.taskUID = (npc.taskUID + 1) % 126
	npc.taskType = bhvTaskJetAttack
	npc.taskTarget = p.view
	npc.taskVec1 = p.pos

	_, dmgMult := s.npcCombatScale(room)
	room.pendingNpcAreaHits = append(room.pendingNpcAreaHits, pendingNpcAreaHit{
		attackerView: npc.viewBase, npcKit: npc.kit, pos: p.pos, radius: float32(radius),
		dmg: int32(float32(dmg) * dmgMult), element: elementNone, at: now.Add(travelTime),
	})
	log.Printf("[AI] npc %d (%s) jet-attacks view=%d dist=%.1f room=%s", npc.viewBase, npc.kit, p.view, dist, room.Name)
}

func (s *EnetServer) npcLaserSweepAttack(room *Room, npc *spawnedNpc, p aiTarget, level string, now time.Time) {
	npc.moving, npc.path = false, nil
	dx, dz := float64(p.pos[0]-npc.pos[0]), float64(p.pos[2]-npc.pos[2])
	npc.yawDeg = float32(math.Mod(math.Atan2(dx, dz)*180/math.Pi+360, 360))

	cfg := npcConfigFor(npc.kit)
	dmg := int32(20)
	if cfg != nil && cfg.LaserSweepDamage > 0 {
		dmg = cfg.LaserSweepDamage
	}

	npc.nextAtkAt = now.Add(npcLaserSweepCooldown)
	npc.taskUID = (npc.taskUID + 1) % 126
	npc.taskType = bhvTaskLaserSweep
	npc.taskTarget = p.view

	_, dmgMult := s.npcCombatScale(room)
	dmg = int32(float32(dmg) * dmgMult * float32(levelDamageModifier(level)))
	if s.hostDamagePlayer(room, p.sess, npc.viewBase, dmg, attackSubTypeLegacy, elementNone) {
		log.Printf("[HOST] actor view=%d died (killed by npc %d %q laser sweep) room=%s", p.view, npc.viewBase, npc.kit, room.Name)
	}
	log.Printf("[AI] npc %d (%s) laser-sweeps view=%d room=%s", npc.viewBase, npc.kit, p.view, room.Name)
}

func npcPoisonDashDamage(kit string) int32 {
	if cfg := npcConfigFor(kit); cfg != nil && cfg.PoisonDashDamage > 0 {
		return cfg.PoisonDashDamage
	}
	return 40
}

func npcPoisonSpitDamage(kit string) int32 {
	if cfg := npcConfigFor(kit); cfg != nil && cfg.PoisonSpitDamage > 0 {
		return cfg.PoisonSpitDamage
	}
	return 60
}

func npcPoisonSpitAngle(kit string) float64 {
	if cfg := npcConfigFor(kit); cfg != nil && cfg.PoisonSpitAngle > 0 {
		return cfg.PoisonSpitAngle
	}
	return 50
}

func npcPoisonCrushDamage(kit string) int32 {
	if cfg := npcConfigFor(kit); cfg != nil && cfg.PoisonCrushDamage > 0 {
		return cfg.PoisonCrushDamage
	}
	return 75
}

func npcPoisonCrushRadius(kit string) float64 {
	if cfg := npcConfigFor(kit); cfg != nil && cfg.PoisonCrushDist > 0 {
		return cfg.PoisonCrushDist
	}
	return 11
}

const (
	npcPoisonDashDuration  = 1733 * time.Millisecond
	npcPoisonSpitDuration  = 3333 * time.Millisecond
	npcPoisonCrushDuration = 1667 * time.Millisecond
	npcPoisonHitFraction   = 0.5
)

const npcPoisonDashHitRadius = 2.0

func (s *EnetServer) npcDoPoisonDash(room *Room, npc *spawnedNpc, p aiTarget, level string, now time.Time) {
	dx, dz := float64(p.pos[0]-npc.pos[0]), float64(p.pos[2]-npc.pos[2])
	npc.yawDeg = float32(math.Mod(math.Atan2(dx, dz)*180/math.Pi+360, 360))

	cfg := npcConfigFor(npc.kit)
	cooldown := 2.0
	if cfg != nil && cfg.PoisonDashCooldown > 0 {
		cooldown = cfg.PoisonDashCooldown
	}

	npc.path = [][3]float32{p.pos}
	npc.moving = true
	npc.taskSpeed = 2
	npc.poisonDashHit = false
	npc.chargeTargetView = p.view
	npc.taskUID = (npc.taskUID + 1) % 126
	npc.taskType = bhvTaskPoisonDash
	npc.taskTarget = p.view
	npc.taskDest = p.pos

	travel := math.Hypot(dx, dz) / npcChargeSpeed(npc.kit)
	npc.nextAtkAt = now.Add(time.Duration(travel*float64(time.Second)) + time.Duration(cooldown*float64(time.Second)))
	log.Printf("[AI] npc %d (%s) poison-dashes at view=%d room=%s", npc.viewBase, npc.kit, p.view, room.Name)
}

func (s *EnetServer) npcDoPoisonSpit(room *Room, npc *spawnedNpc, p aiTarget, level string, now time.Time) {
	npc.moving, npc.path = false, nil
	dx, dz := float64(p.pos[0]-npc.pos[0]), float64(p.pos[2]-npc.pos[2])
	npc.yawDeg = float32(math.Mod(math.Atan2(dx, dz)*180/math.Pi+360, 360))

	cfg := npcConfigFor(npc.kit)
	cooldown := 2.0
	if cfg != nil && cfg.PoisonSpitCooldown > 0 {
		cooldown = cfg.PoisonSpitCooldown
	}
	npc.nextAtkAt = now.Add(time.Duration(cooldown * float64(time.Second)))

	npc.taskUID = (npc.taskUID + 1) % 126
	npc.taskType = bhvTaskPoisonSpit
	npc.taskTarget = p.view

	_, dmgMult := s.npcCombatScale(room)
	dmg := int32(float32(npcPoisonSpitDamage(npc.kit)) * levelDamageModifier(level) * dmgMult)
	room.pendingPoisonAttacks = append(room.pendingPoisonAttacks, pendingPoisonAttack{
		sess: p.sess, targetView: p.view, attackerView: npc.viewBase, npcKit: npc.kit, center: npc.pos,
		dirX: dx, dirZ: dz, angle: npcPoisonSpitAngle(npc.kit), dmg: dmg, at: now.Add(time.Duration(float64(npcPoisonSpitDuration) * npcPoisonHitFraction)),
	})
	log.Printf("[AI] npc %d (%s) poison-spits at view=%d room=%s", npc.viewBase, npc.kit, p.view, room.Name)
}

func (s *EnetServer) npcDoPoisonCrush(room *Room, npc *spawnedNpc, p aiTarget, level string, now time.Time) {
	npc.moving, npc.path = false, nil
	dx, dz := float64(p.pos[0]-npc.pos[0]), float64(p.pos[2]-npc.pos[2])
	npc.yawDeg = float32(math.Mod(math.Atan2(dx, dz)*180/math.Pi+360, 360))

	cfg := npcConfigFor(npc.kit)
	cooldown := 2.0
	if cfg != nil && cfg.PoisonCrushCooldown > 0 {
		cooldown = cfg.PoisonCrushCooldown
	}
	npc.nextAtkAt = now.Add(time.Duration(cooldown * float64(time.Second)))

	npc.taskUID = (npc.taskUID + 1) % 126
	npc.taskType = bhvTaskPoisonCrush
	npc.taskTarget = p.view

	_, dmgMult := s.npcCombatScale(room)
	dmg := int32(float32(npcPoisonCrushDamage(npc.kit)) * levelDamageModifier(level) * dmgMult)
	room.pendingPoisonAttacks = append(room.pendingPoisonAttacks, pendingPoisonAttack{
		attackerView: npc.viewBase, npcKit: npc.kit, aoe: true, center: npc.pos, radius: npcPoisonCrushRadius(npc.kit),
		dmg: dmg, at: now.Add(time.Duration(float64(npcPoisonCrushDuration) * npcPoisonHitFraction)),
	})
	log.Printf("[AI] npc %d (%s) poison-crushes room=%s", npc.viewBase, npc.kit, room.Name)
}

func (s *EnetServer) hostTickPendingPoisonAttacks(room *Room) {
	if len(room.pendingPoisonAttacks) == 0 {
		return
	}
	now := time.Now()
	remaining := room.pendingPoisonAttacks[:0]
	for _, d := range room.pendingPoisonAttacks {
		if now.Before(d.at) {
			remaining = append(remaining, d)
			continue
		}
		if d.aoe {
			for _, uid := range room.Players {
				target := s.sessionInRoom(uid, room.Name)
				if target == nil || target.avatarBase == 0 || target.isDead {
					continue
				}
				pos, ok := playerStreamPos(room, target.avatarBase)
				if !ok {
					pos = room.playerPos[target.avatarBase+1]
				}
				dx, dz := pos[0]-d.center[0], pos[2]-d.center[2]
				if float64(dx*dx+dz*dz) > d.radius*d.radius {
					continue
				}
				if s.hostDamagePlayer(room, target, d.attackerView, d.dmg, attackSubTypeLegacy, elementCorrosive) {
					log.Printf("[HOST] actor view=%d died (killed by npc %d %q poison-crush) room=%s", target.avatarBase+1, d.attackerView, d.npcKit, room.Name)
				}
			}
			continue
		}
		if d.sess == nil || d.sess.avatarBase == 0 || d.sess.isDead {
			continue
		}
		pos, ok := playerStreamPos(room, d.sess.avatarBase)
		if !ok {
			pos = room.playerPos[d.sess.avatarBase+1]
		}
		if d.angle > 0 && d.angle < 360 {
			curDx, curDz := float64(pos[0]-d.center[0]), float64(pos[2]-d.center[2])
			origMag, curMag := math.Hypot(d.dirX, d.dirZ), math.Hypot(curDx, curDz)
			if origMag > 0.01 && curMag > 0.01 {
				cosAngle := (d.dirX*curDx + d.dirZ*curDz) / (origMag * curMag)
				if cosAngle < math.Cos(d.angle/2*math.Pi/180) {
					continue
				}
			}
		}
		if s.hostDamagePlayer(room, d.sess, d.attackerView, d.dmg, attackSubTypeLegacy, elementCorrosive) {
			log.Printf("[HOST] actor view=%d died (killed by npc %d %q poison-attack) room=%s", d.targetView, d.attackerView, d.npcKit, room.Name)
		}
	}
	room.pendingPoisonAttacks = remaining
}

const npcThrowAxeDamage = 30

const npcThrowAxeEvery = 2 * time.Second

func (s *EnetServer) npcThrowAxeAttack(room *Room, npc *spawnedNpc, p aiTarget, level string, now time.Time) {
	npc.moving, npc.path = false, nil
	dx, dz := float64(p.pos[0]-npc.pos[0]), float64(p.pos[2]-npc.pos[2])
	npc.yawDeg = float32(math.Mod(math.Atan2(dx, dz)*180/math.Pi+360, 360))

	npc.nextAtkAt = now.Add(npcThrowAxeEvery)

	npc.taskUID = (npc.taskUID + 1) % 126
	npc.taskType = bhvTaskThrowAttack
	npc.taskTarget = p.view

	_, dmgMult := s.npcCombatScale(room)
	dmg := int32(float32(npcThrowAxeDamage) * levelDamageModifier(level) * dmgMult)
	if s.hostDamagePlayer(room, p.sess, npc.viewBase, dmg, attackSubTypeLegacy, elementNone) {
		log.Printf("[HOST] actor view=%d died (killed by npc %d %q thrown axe) room=%s", p.view, npc.viewBase, npc.kit, room.Name)
	}
	log.Printf("[AI] npc %d (%s) throws axe at view=%d room=%s", npc.viewBase, npc.kit, p.view, room.Name)
}

func npcSuicideDamage(kit string) int32 {
	if cfg := npcConfigFor(kit); cfg != nil && cfg.BlowUpDamage > 0 {
		return cfg.BlowUpDamage
	}
	return grenadeBlowUpDamage
}

func npcSuicideRadius(kit string) float64 {
	if cfg := npcConfigFor(kit); cfg != nil && cfg.BlowUpRadius > 0 {
		return cfg.BlowUpRadius
	}
	return grenadeBlowUpRadius
}

const suicideBombFuse = 800 * time.Millisecond

func (s *EnetServer) npcSuicideBombAttack(room *Room, npc *spawnedNpc, level string, now time.Time) {
	npc.moving, npc.path = false, nil
	npcSetTask(npc, bhvTaskSuicide, 0, npc.pos, 0)
	s.hostSendSerialize(room)

	view := room.NextAvatarViewBase()
	views := []int32{view, view + 1, view + 2}
	s.hostBroadcastJournaled(room, 202, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): "Entities/Object/Launchable/Grenade",
			byte(1): vector3Custom(npc.pos[0], npc.pos[1]+0.5, npc.pos[2]),
			byte(4): IntArray{Items: views},
			byte(5): []any{npc.viewBase},
			byte(6): int32(msNow()),
			byte(7): view,
		},
	}, false)
	s.broadcastEvent(room, 201, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): int32(msNow()),
			Short(1): map[any]any{
				byte(0): view + 1,
				byte(1): grenadeTaskStream(bhvTaskCountDown, float32(suicideBombFuse.Seconds())),
			},
		},
	}, nil, nil)

	room.pendingSuicideBombs = append(room.pendingSuicideBombs, pendingSuicideBomb{
		view: view, npcView: npc.viewBase, pos: npc.pos, at: now.Add(suicideBombFuse),
	})
	log.Printf("[AI] npc %d (%s) primes a bomb room=%s", npc.viewBase, npc.kit, room.Name)
}

func (s *EnetServer) hostTickSuicideBombs(room *Room) bool {
	if len(room.pendingSuicideBombs) == 0 {
		return false
	}
	now := time.Now()
	remaining := room.pendingSuicideBombs[:0]
	exploded := false
	for _, b := range room.pendingSuicideBombs {
		if now.Before(b.at) {
			remaining = append(remaining, b)
			continue
		}
		s.hostExplodeSuicideBomb(room, b)
		exploded = true
	}
	room.pendingSuicideBombs = remaining
	return exploded
}

func (s *EnetServer) hostExplodeSuicideBomb(room *Room, b pendingSuicideBomb) {
	s.broadcastEvent(room, 201, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): int32(msNow()),
			Short(1): map[any]any{
				byte(0): b.view + 1,
				byte(1): grenadeTaskStream(bhvTaskBlowUp, 0),
			},
		},
	}, nil, nil)

	var bomber *spawnedNpc
	for i := range room.npcs {
		if room.npcs[i].viewBase == b.npcView {
			bomber = &room.npcs[i]
			break
		}
	}
	kit := ""
	if bomber != nil {
		kit = bomber.kit
	}
	radius := npcSuicideRadius(kit)
	r2 := float32(radius * radius)
	_, dmgMult := s.npcCombatScale(room)
	dmg := int32(float32(npcSuicideDamage(kit)) * levelDamageModifier(m2level(room)) * dmgMult)

	for i := range room.npcs {
		other := &room.npcs[i]
		if other.viewBase == b.npcView || other.hp <= 0 {
			continue
		}
		dx, dz := b.pos[0]-other.pos[0], b.pos[2]-other.pos[2]
		if dx*dx+dz*dz > r2 {
			continue
		}
		other.hp -= dmg
		if other.hp < 0 {
			other.hp = 0
		}
		if other.hp > 0 {
			continue
		}
		s.hostKillNpc(room, other, 0, dmg, elementExplosive, false)
	}
	for _, uid := range room.Players {
		target := s.sessionInRoom(uid, room.Name)
		if target == nil || target.avatarBase == 0 {
			continue
		}
		ppos, ok := playerStreamPos(room, target.avatarBase)
		if !ok {
			ppos = room.playerPos[target.avatarBase+1]
		}
		dx, dz := b.pos[0]-ppos[0], b.pos[2]-ppos[2]
		if dx*dx+dz*dz > r2 {
			continue
		}
		if s.hostDamagePlayer(room, target, 0, dmg, attackSubTypeLegacy, elementNone) {
			log.Printf("[HOST] actor view=%d died (killed by suicide bomb) room=%s", target.avatarBase, room.Name)
		}
	}
	if bomber != nil && bomber.hp > 0 {
		bomber.hp = 0
		s.hostKillNpc(room, bomber, 0, dmg, elementExplosive, false)
	}
	room.deferredDestroys = append(room.deferredDestroys, deferredDestroy{
		view: b.view, at: time.Now().Add(grenadeDestroyDelay),
	})
	log.Printf("[HOST] suicide bomb exploded viewID=%d room=%s", b.view, room.Name)
}

const npcDefaultFireRange = 30.0

func (s *EnetServer) hostTickNpcRangedEngage(room *Room, npc *spawnedNpc, p aiTarget, now time.Time) (acted, handled bool) {
	fireRange := npcDefaultFireRange
	bestMax := 0.0
	if cfg := npcConfigFor(npc.kit); cfg != nil {
		if cfg.FireRange > 0 {
			fireRange = cfg.FireRange
		} else if cfg.DetectionRng > 0 {
			fireRange = cfg.DetectionRng
		}
		bestMax = cfg.BestAttackDistanceMax
	}
	dx, dz := p.pos[0]-npc.pos[0], p.pos[2]-npc.pos[2]
	distSq := dx*dx + dz*dz
	if distSq > float32(fireRange*fireRange) {
		return false, false
	}
	if bestMax > 0 && distSq > float32(bestMax*bestMax) {
		return false, false
	}
	if navmeshWallBlocked(m2level(room), npc.pos, p.pos) {
		return false, false
	}
	npc.moving, npc.path = false, nil
	npc.yawDeg = float32(math.Mod(math.Atan2(float64(dx), float64(dz))*180/math.Pi+360, 360))
	if now.Before(npc.nextRangedAtkAt) {
		return true, true
	}

	cfg := npcConfigFor(npc.kit)
	fireDur, idleDur := 0.5, 0.8
	switch {
	case cfg != nil && cfg.BurstChainedFireMax > 0:
		fireDur = randRange(cfg.BurstChainedFireMin, cfg.BurstChainedFireMax)
		idleDur = randRange(cfg.BurstChainedIdleMin, cfg.BurstChainedIdleMax)
	case cfg != nil && cfg.BurstSingleMax > 0:
		fireDur = randRange(cfg.BurstSingleMin, cfg.BurstSingleMax)
		idleDur = 0
	}
	npc.nextRangedAtkAt = now.Add(time.Duration((fireDur + idleDur) * float64(time.Second)))

	npc.taskUID = (npc.taskUID + 1) % 126
	npc.taskType = npcFireTask(npc.kit)
	npc.taskTarget = p.view

	_, dmgMult := s.npcCombatScale(room)
	dmg := int32(float32(npcMeleeDamageForLevel(npc.kit, m2level(room))) * dmgMult)
	if s.hostDamagePlayer(room, p.sess, npc.viewBase, dmg, attackSubTypeLegacy, elementNone) {
		log.Printf("[HOST] actor view=%d died (killed by npc %d %q ranged) room=%s", p.view, npc.viewBase, npc.kit, room.Name)
	}
	log.Printf("[AI] npc %d (%s) fires burst at view=%d dist=%.1f room=%s",
		npc.viewBase, npc.kit, p.view, math.Sqrt(float64(distSq)), room.Name)
	return true, true
}

const (
	npcDefaultSeekCoverDistance = 5.0
	npcDefaultCoverIdleMin      = 0.8
	npcDefaultCoverIdleMax      = 1.5
	npcDefaultCoverPeakMin      = 1.0
	npcDefaultCoverPeakMax      = 2.0
)

func releaseCover(room *Room, npc *spawnedNpc) {
	if npc.coverIdx >= 0 && room.coverOccupants != nil {
		delete(room.coverOccupants, npc.coverIdx)
	}
	npc.coverIdx = -1
	npc.nextCoverRollAt = time.Time{}
	npc.inCover = false
	npc.exposed = false
	npc.coverWallFailStreak = 0
}

const coverWallFailThreshold = 3

const coverAvoidWindow = 20 * time.Second

const coverRerollInterval = 2 * time.Second

const coverAggroLockWindow = 6 * time.Second

func (s *EnetServer) hostTickNpcCoverEngage(room *Room, npc *spawnedNpc, p aiTarget, now time.Time) (acted, handled bool) {
	if !npcCoverCapableKits[npc.kit] {
		return false, false
	}
	cfg := npcConfigFor(npc.kit)

	if npc.coverIdx < 0 && now.After(npc.nextCoverRollAt) {
		npc.nextCoverRollAt = now.Add(coverRerollInterval)
		chance := 0.5
		if cfg != nil && cfg.InitialCoverChance > 0 {
			chance = cfg.InitialCoverChance
		}
		if rand.Float64() < chance {
			s.npcClaimNearestCover(room, npc, cfg, now)
		}
	}
	if npc.coverIdx < 0 {
		return false, false
	}
	npc.coverAggroView = p.view
	npc.coverAggroUntil = now.Add(coverAggroLockWindow)

	points := levelCoverPoints(m2level(room))
	if npc.coverIdx >= len(points) {
		releaseCover(room, npc)
		return false, false
	}
	cp := points[npc.coverIdx]

	if !npc.inCover {
		if npc.moving {
			return false, true
		}
		npc.inCover = true
		npc.exposed = false
		npc.yawDeg = float32(math.Mod(math.Atan2(float64(cp.Facing[0]), float64(cp.Facing[2]))*180/math.Pi+360, 360))
		npcSetTask(npc, bhvTaskCoverIdle, -1, npc.pos, -1)
		idleMin, idleMax := npcDefaultCoverIdleMin, npcDefaultCoverIdleMax
		if cfg != nil && cfg.CoverIdleDangerMax > 0 {
			idleMin, idleMax = cfg.CoverIdleDangerMin, cfg.CoverIdleDangerMax
		}
		npc.nextCoverPhaseAt = now.Add(time.Duration(randRange(idleMin, idleMax) * float64(time.Second)))
		return true, true
	}

	if now.Before(npc.nextCoverPhaseAt) {
		return false, true
	}

	if !npc.exposed {
		npc.exposed = true
		dx, dz := p.pos[0]-npc.pos[0], p.pos[2]-npc.pos[2]
		npc.yawDeg = float32(math.Mod(math.Atan2(float64(dx), float64(dz))*180/math.Pi+360, 360))
		peakMin, peakMax := npcDefaultCoverPeakMin, npcDefaultCoverPeakMax
		if cfg != nil && cfg.CoverPeakOutCombatMax > 0 {
			peakMin, peakMax = cfg.CoverPeakOutCombatMin, cfg.CoverPeakOutCombatMax
		}
		npc.nextCoverPhaseAt = now.Add(time.Duration(randRange(peakMin, peakMax) * float64(time.Second)))

		if !navmeshWallBlocked(m2level(room), cp.Pos, p.pos) {
			npc.coverWallFailStreak = 0
			npc.taskUID = (npc.taskUID + 1) % 126
			npc.taskType = bhvTaskCoverFire
			npc.taskTarget = p.view
			_, dmgMult := s.npcCombatScale(room)
			dmg := int32(float32(npcMeleeDamageForLevel(npc.kit, m2level(room))) * dmgMult)
			if s.hostDamagePlayer(room, p.sess, npc.viewBase, dmg, attackSubTypeLegacy, elementNone) {
				log.Printf("[HOST] actor view=%d died (killed by npc %d %q from cover) room=%s", p.view, npc.viewBase, npc.kit, room.Name)
			}
			log.Printf("[AI] npc %d (%s) peeks from cover and fires at view=%d room=%s", npc.viewBase, npc.kit, p.view, room.Name)
		} else {
			npcSetTask(npc, bhvTaskCoverPeakOut, -1, npc.pos, -1)
			npc.coverWallFailStreak++
			if npc.coverWallFailStreak >= coverWallFailThreshold {
				log.Printf("[AI] npc %d (%s) abandons cover point %d, wall blocks line to player room=%s",
					npc.viewBase, npc.kit, npc.coverIdx, room.Name)
				npc.avoidCoverIdx = npc.coverIdx
				npc.avoidCoverUntil = now.Add(coverAvoidWindow)
				releaseCover(room, npc)
			}
		}
		return true, true
	}

	npc.exposed = false
	npcSetTask(npc, bhvTaskCoverIdle, -1, npc.pos, -1)
	idleMin, idleMax := npcDefaultCoverIdleMin, npcDefaultCoverIdleMax
	if cfg != nil && cfg.CoverIdleDangerMax > 0 {
		idleMin, idleMax = cfg.CoverIdleDangerMin, cfg.CoverIdleDangerMax
	}
	npc.nextCoverPhaseAt = now.Add(time.Duration(randRange(idleMin, idleMax) * float64(time.Second)))
	return true, true
}

func (s *EnetServer) npcClaimNearestCover(room *Room, npc *spawnedNpc, cfg *npcConfig, now time.Time) {
	points := levelCoverPoints(m2level(room))
	if len(points) == 0 {
		return
	}
	seekDist := npcDefaultSeekCoverDistance
	if cfg != nil && cfg.SeekCoverDistance > 0 {
		seekDist = cfg.SeekCoverDistance
	}
	seekDistSq := float32(seekDist * seekDist)

	avoidActive := now.Before(npc.avoidCoverUntil)
	best := -1
	bestDist := float32(1 << 30)
	for i, cp := range points {
		if avoidActive && i == npc.avoidCoverIdx {
			continue
		}
		if room.coverOccupants != nil {
			if _, taken := room.coverOccupants[i]; taken {
				continue
			}
		}
		dx, dz := cp.Pos[0]-npc.pos[0], cp.Pos[2]-npc.pos[2]
		d := dx*dx + dz*dz
		if d > seekDistSq || d >= bestDist {
			continue
		}
		best, bestDist = i, d
	}
	if best < 0 {
		return
	}
	cp := points[best]
	wps, ok := navmeshFindPath(m2level(room), npc.pos[0], npc.pos[2], cp.Pos[0], cp.Pos[2], npc.pos[1])
	if !ok {
		return
	}
	if room.coverOccupants == nil {
		room.coverOccupants = map[int]int32{}
	}
	room.coverOccupants[best] = npc.viewBase
	npc.coverIdx = best
	npc.path = wps
	npc.moving = true
	npcSetTask(npc, npcMoveTask(npc.kit), -1, cp.Pos, 2)
	log.Printf("[AI] npc %d (%s) heads for cover point %d dist=%.1f room=%s",
		npc.viewBase, npc.kit, best, math.Sqrt(float64(bestDist)), room.Name)
}

func randRange(lo, hi float64) float64 {
	if hi <= lo {
		return lo
	}
	return lo + rand.Float64()*(hi-lo)
}

func (s *EnetServer) hostTickNpcPatrol(npc *spawnedNpc, level string, now time.Time) bool {
	if len(npc.patrolPoints) == 0 || npc.moving {
		return false
	}
	if npc.patrolling {
		wait := float32(3)
		if npc.patrolIdx >= 0 && npc.patrolIdx < len(npc.patrolWaits) {
			wait = npc.patrolWaits[npc.patrolIdx]
		}
		npc.patrolWaitUntil = now.Add(time.Duration(wait * float32(time.Second)))
		npc.patrolling = false
		return false
	}
	if now.Before(npc.patrolWaitUntil) {
		return false
	}
	next := npc.patrolIdx
	for tried := 0; tried < len(npc.patrolPoints); tried++ {
		if npc.patrolPolicy == "Random" && len(npc.patrolPoints) > 1 {
			next = rand.Intn(len(npc.patrolPoints))
			for next == npc.patrolIdx && len(npc.patrolPoints) > 1 {
				next = rand.Intn(len(npc.patrolPoints))
			}
		} else {
			next++
			if next >= len(npc.patrolPoints) {
				next = 0
			}
		}
		dest := npc.patrolPoints[next]
		wps, ok := navmeshFindPath(level, npc.pos[0], npc.pos[2], dest[0], dest[2], npc.pos[1])
		if !ok {
			continue
		}
		npc.patrolIdx = next
		npc.path = wps
		npc.moving = true
		npc.patrolling = true
		npcSetTask(npc, npcMoveTask(npc.kit), -1, dest, 0)
		return true
	}
	return false
}

const (
	bhvTaskIdle        = 1
	bhvTaskDie         = 2
	bhvTaskMoveTo      = 4
	bhvTaskSlideMoveTo = 8

	bhvTaskHoverPatrol    = 65
	bhvTaskFlyMeleeAttack = 67
	bhvTaskMeleeAttack    = 22
	bhvTaskCountDown      = 37
	bhvTaskBlowUp         = 38

	bhvTaskJumpAttack      = 23
	bhvTaskChargeAttack    = 26
	bhvTaskChargeAttackHit = 27
	bhvTaskSpit            = 47

	bhvTaskHammerJumpAttack = 76
	bhvTaskHammerAttack     = 77

	bhvTaskWebTrap = 79

	bhvTaskSwordSlash = 104

	bhvTaskDazeEffect  = 58
	bhvTaskLayerEffect = 3

	bhvTaskThrowAttack = 59

	bhvTaskSuicide = 41

	bhvTaskPoisonDash  = 95
	bhvTaskPoisonSpit  = 96
	bhvTaskPoisonCrush = 97

	bhvTaskWarcry = 33

	bhvTaskPlayerStun = 103

	bhvTaskTutorialAction = 92

	bhvTaskFire         = 10
	bhvTaskFireFullBody = 74

	bhvTaskCoverIdle    = 12
	bhvTaskCoverFire    = 13
	bhvTaskCoverPeakOut = 16
	bhvTaskCoverPeakIn  = 17

	bhvTaskRocketAttack = 68

	bhvTaskRailgunAttack = 61
	bhvTaskMortarAttack  = 62
	bhvTaskJetAttack     = 63
	bhvTaskLaserSweep    = 71

	bhvTaskPlayerDown   = 99
	bhvTaskPlayerRevive = 100

	bhvTaskPlayerDefault = 35
	bhvTaskPlayerSpawn	 = 50
)

var npcCoverCapableKits = map[string]bool{
	"BanditKiller": true, "BadassBanditKiller": true, "BanditRaider": true,
	"CrimsonLanceInfantry": true, "DrBlofeld": true, "MajorVanCleef": true,
	"BanditScout": true, "CrimsonLanceSniper": true,
	"CrimsonLanceMedic": true,
}

var jumpCurveTypeForKit = map[string]int32{
	"SpiderantWidowmaker": 1, // ESpiderWidommakeJumpCurve
	"BanditApollyon":      7, // EApollyonJumpCurve
}

var hammerAttackKits = map[string]bool{
	"BanditApollyon": true,
}

var swordSlashAttackKits = map[string]bool{
	"CrimsonLanceAssassin": true,
}

var flyMeleeAttackKits = map[string]bool{
	"Rakk": true,
}

var npcFireTaskOverride = map[string]int32{
	"SoldierTurretBasic": bhvTaskFireFullBody,
}

func npcFireTask(kit string) int32 {
	if t, ok := npcFireTaskOverride[kit]; ok {
		return t
	}
	return bhvTaskFire
}

const (
	bhvFireStyleBurstChained = int32(1)
	bhvFireStyleBurstSingle  = int32(0)
)

func npcFireStyleForKit(kit string) int32 {
	cfg := npcConfigFor(kit)
	if cfg != nil && cfg.BurstChainedFireMax > 0 {
		return bhvFireStyleBurstChained
	}
	return bhvFireStyleBurstSingle
}

func bhvTaskStream(npc *spawnedNpc) []any {
	vec0 := vector3Custom(npc.taskDest[0], npc.taskDest[1], npc.taskDest[2])
	vec1 := vector3Custom(0, 0, 0)
	fireStyle := int32(-1)
	int1 := int32(-1)
	float0 := float32(-1)
	bool1 := false
	int4 := npc.taskAction
	int3 := int32(-1)
	taskLayer := int32(0)
	switch npc.taskType {
	case bhvTaskMeleeAttack:
		vec0 = vector3Custom(-1, 0, 0)
	case bhvTaskHammerAttack:
	case bhvTaskSwordSlash:
		int1 = 0
		int4 = 10
	case bhvTaskHammerJumpAttack:
	case bhvTaskFire, bhvTaskFireFullBody:
		vec0 = vector3Custom(100, 0, 0)
		fireStyle = npcFireStyleForKit(npc.kit)
		int1 = 1
		if npc.taskType == bhvTaskFire {
			taskLayer = 1
		}
	case bhvTaskCoverFire:
		taskLayer = 1
		fireStyle = npcFireStyleForKit(npc.kit)
		int1 = 1
	case bhvTaskRailgunAttack:
		if cfg := npcConfigFor(npc.kit); cfg != nil {
			speed, radius, duration, dmgFrac := 20.0, 1.5, 2.0, 0.15
			if cfg.PlasmaBallSpeed > 0 {
				speed = cfg.PlasmaBallSpeed
			}
			if cfg.PlasmaBallRadius > 0 {
				radius = cfg.PlasmaBallRadius
			}
			if cfg.PlasmaBallDuration > 0 {
				duration = cfg.PlasmaBallDuration
			}
			if cfg.PlasmaBallDamage > 0 {
				dmgFrac = cfg.PlasmaBallDamage
			}
			float0 = float32(dmgFrac)
			vec0 = vector3Custom(float32(speed), float32(radius), float32(duration))
		}
	case bhvTaskJetAttack:
		vec0 = vector3Custom(10, 0, 0)
		vec1 = vector3Custom(npc.taskVec1[0], npc.taskVec1[1], npc.taskVec1[2])
	case bhvTaskRocketAttack:
		taskLayer = 1
	case bhvTaskJumpAttack:
		int1 = jumpCurveTypeForKit[npc.kit]
		int4 = 0
	case bhvTaskSpit:
		vec0 = vector3Custom(0, 0, 0)
		int1 = 0
	case bhvTaskThrowAttack:
		int1 = 2
	case bhvTaskSuicide:
		float0 = 0
	case bhvTaskPoisonDash:
		int4 = npcPoisonDashDamage(npc.kit)
	case bhvTaskPoisonSpit:
		vec0 = vector3Custom(5, float32(npcPoisonSpitAngle(npc.kit)), 0)
		int3 = npc.taskTarget
		int4 = npcPoisonSpitDamage(npc.kit)
	case bhvTaskPoisonCrush:
		vec0 = vector3Custom(float32(npcPoisonCrushRadius(npc.kit)), 0, 0)
		int4 = npcPoisonCrushDamage(npc.kit)
	case bhvTaskDie:
		int1 = 0
		cfg := npcConfigFor(npc.kit)
		if cfg != nil && cfg.NormalDeathAnimCount > 1 {
			int1 = int32(rand.Intn(int(cfg.NormalDeathAnimCount)))
		}
		if npc.taskDieHeadshot && cfg != nil && cfg.HeadShotDeathAnimCount > 0 {
			bool1 = true
			int1 = int32(rand.Intn(int(cfg.HeadShotDeathAnimCount)))
		}
		float0 = npc.taskForce
	}
	wireSpeed, wireStress := npcMoveWireSpeed(npc.kit, npc.taskSpeed)
	return []any{
		int32(1),
		npc.taskUID,
		npc.taskType,
		taskLayer,
		false,
		int32(-1),
		fireStyle,
		int32(0),
		wireStress,
		wireSpeed,
		int32(-1),
		float0,
		int1,
		npc.taskTarget,
		int3,
		bool1,
		false,
		false,
		vec0,
		vec1,
		vector3Custom(0, 0, 0),
		vector3Custom(0, 0, 0),
		int4,
		"",
	}
}

func bhvDazeEffectStream(npc *spawnedNpc) []any {
	return []any{
		int32(1),
		npc.dazeEffectUID,
		int32(bhvTaskDazeEffect),
		int32(bhvTaskLayerEffect),
		false,
		int32(-1),
		int32(-1),
		int32(0),
		int32(0),
		int32(0),
		int32(-1),
		float32(-1),
		int32(-1),
		int32(0),
		int32(-1),
		false,
		false,
		false,
		vector3Custom(npc.dazeDurationMult, 0, 0),
		vector3Custom(0, 0, 0),
		vector3Custom(0, 0, 0),
		vector3Custom(0, 0, 0),
		int32(0),
		"",
	}
}

func npcMoveTask(kit string) int32 {
	switch kit {
	case "GuardianWraith", "BadassGuardianWraith":
		return bhvTaskSlideMoveTo
	case "Rakk":
		return bhvTaskHoverPatrol
	default:
		return bhvTaskMoveTo
	}
}

func npcSetTask(npc *spawnedNpc, typ, target int32, dest [3]float32, speed int32) {
	if npc.taskType == typ && npc.taskTarget == target && npc.taskSpeed == speed {
		dx, dz := npc.taskDest[0]-dest[0], npc.taskDest[2]-dest[2]
		if dx*dx+dz*dz < 2*2 {
			return
		}
		npc.taskDest = dest
		return
	}
	npc.taskUID = (npc.taskUID + 1) % 126
	npc.taskType = typ
	npc.taskTarget = target
	npc.taskDest = dest
	npc.taskSpeed = speed
	npc.taskAction = 0
}

func npcSetTutorialAction(npc *spawnedNpc, action int32) {
	npc.taskUID = (npc.taskUID + 1) % 126
	npc.taskType = bhvTaskTutorialAction
	npc.taskTarget = -1
	npc.taskDest = npc.pos
	npc.taskSpeed = -1
	npc.taskAction = action
}

func (s *EnetServer) hostDamageNearestNpc(room *Room, attackerView int32, shooterPos [3]float32, shooterYawDeg float64, element int32) {
	yawRad := shooterYawDeg * math.Pi / 180
	dirX, dirZ := float32(math.Sin(yawRad)), float32(math.Cos(yawRad))

	best := -1
	bestDist := float32(npcHitRadius * npcHitRadius)
	for i := range room.npcs {
		if room.npcs[i].kit == "ClapTrapTutor" || room.npcs[i].hp <= 0 {
			continue
		}
		dx, dz := room.npcs[i].pos[0]-shooterPos[0], room.npcs[i].pos[2]-shooterPos[2]
		d := dx*dx + dz*dz
		if d > bestDist {
			continue
		}
		if d > 0.25 {
			dist := float32(math.Sqrt(float64(d)))
			cosAngle := (dx*dirX + dz*dirZ) / dist
			if cosAngle < npcHitConeCosMin {
				continue
			}
		}
		bestDist = d
		best = i
	}
	if best < 0 {
		return
	}
	npc := &room.npcs[best]
	npc.forcedAggroView = attackerView
	npc.forcedAggroUntil = time.Now().Add(forcedAggroWindow)
	s.hostAlertNearbyNpcs(room, npc, attackerView)
	s.hostRecordDamageStat(room, attackerView, fireBurstDamage, false, false)
	npc.hp -= fireBurstDamage
	if npc.hp < 0 {
		npc.hp = 0
	}
	if npc.hp > 0 && npcHealthBarVisuallyEmpty(npc.hp, npc.maxHP) {
		npc.hp = 0
	}
	if npc.hp > 0 {
		s.hostDamageEntity(room, attackerView, npc.viewBase, fireBurstDamage, 0, element, false)
		s.hostSendRPC(room, npc.equipView(), "RPC_SendLifeAndShield", []any{npcVitalsTable(npc.kit, npc.maxHP, npc.hp)})
		npcMaybeStartElementalDOT(npc, attackerView, element, fireBurstDamage, time.Now(), 0, false)
		return
	}
	s.hostKillNpc(room, npc, attackerView, fireBurstDamage, element, false)
}

func (s *EnetServer) hostDamageNearestPlayer(room *Room, attacker *ClientSession, shooterPos [3]float32, shooterYawDeg float64, damage, element int32) bool {
	yawRad := shooterYawDeg * math.Pi / 180
	dirX, dirZ := float32(math.Sin(yawRad)), float32(math.Cos(yawRad))
	teamMode := isTeamGameMode(asInt32(room.Properties["GameMode"]))

	var best *ClientSession
	bestDist := float32(npcHitRadius * npcHitRadius)
	for _, uid := range room.Players {
		target := s.sessionInRoom(uid, room.Name)
		if target == nil || target == attacker || target.avatarBase == 0 || target.isDead {
			continue
		}
		if teamMode && target.teamID == attacker.teamID {
			continue
		}
		pos := room.playerPos[target.avatarBase+1]
		dx, dz := pos[0]-shooterPos[0], pos[2]-shooterPos[2]
		d := dx*dx + dz*dz
		if d > bestDist {
			continue
		}
		if d > 0.25 {
			dist := float32(math.Sqrt(float64(d)))
			cosAngle := (dx*dirX + dz*dirZ) / dist
			if cosAngle < npcHitConeCosMin {
				continue
			}
		}
		bestDist = d
		best = target
	}
	if best == nil {
		return false
	}
	s.hostRecordDamageStat(room, attacker.avatarBase, damage, false, false)
	if s.hostDamagePlayer(room, best, attacker.avatarBase+1, damage, attackSubTypeLegacy, element) {
		attacker.pvpKillCount++
		s.hostCheckPvPKillTarget(room)
	}
	return true
}

func npcDeathForce(npc *spawnedNpc, damage, element int32) float32 {
	if element == elementExplosive {
		return 70 + rand.Float32()*25
	}
	heavyHit := npc.maxHP > 0 && damage*2 >= npc.maxHP
	knockbackOdds := float32(0.15)
	if npc.moving || heavyHit {
		knockbackOdds = 0.85
	}
	if rand.Float32() < knockbackOdds {
		return 40 + rand.Float32()*50
	}
	return rand.Float32() * 20
}

func npcDeathOrigin(npc *spawnedNpc) [3]float32 {
	if npc.moving {
		rad := float64(npc.yawDeg) * math.Pi / 180
		dist := 1.5 + rand.Float64()*2
		return [3]float32{
			npc.pos[0] - float32(math.Sin(rad))*float32(dist),
			npc.pos[1],
			npc.pos[2] - float32(math.Cos(rad))*float32(dist),
		}
	}
	ang := rand.Float64() * 2 * math.Pi
	dist := float32(1.5 + rand.Float64()*2.5)
	return [3]float32{npc.pos[0] + float32(math.Cos(ang))*dist, npc.pos[1], npc.pos[2] + float32(math.Sin(ang))*dist}
}

func (s *EnetServer) hostKillNpc(room *Room, npc *spawnedNpc, attackerView, damage, element int32, criticalHit bool) {
	if m, ok := room.markedNpcs[npc.viewBase]; ok && time.Now().Before(m.until) {
		delete(room.markedNpcs, npc.viewBase)
		burstDamage, burstElement := hunterExplosionForElement(element)
		attacker := sessionByAvatarBase(s, room, attackerView)
		if attacker != nil {
			burstDamage = int32(float64(burstDamage) * (1 + effectValue(attacker.hostChar, "MarkedExplodeOnKill")))
		}
		s.hostAreaDamageNpcs(room, attackerView, npc.pos, hunterMarkExplosionRadius, burstDamage, burstElement, false)
		if attacker != nil && attacker.hunterDroidActive {
			extend := effectValue(attacker.hostChar, "OnKillDroidDuration")
			if extend > 0 {
				attacker.hunterDroidEndAt = attacker.hunterDroidEndAt.Add(time.Duration(extend * float64(time.Second)))
			}
		}
	}
	npcSetTask(npc, bhvTaskDie, 0, npcDeathOrigin(npc), 0)
	npc.taskForce = npcDeathForce(npc, damage, element)
	npc.taskDieHeadshot = criticalHit
	releaseCover(room, npc)
	s.hostKillEntity(room, attackerView, npc.viewBase, damage, element)
	if attacker := sessionByAvatarBase(s, room, attackerView); attacker != nil && attacker.isDead {
		s.hostRevivePlayer(room, attacker, true)
		log.Printf("[HOST] actor view=%d got a Second Wind from a kill while downed room=%s", attacker.avatarBase, room.Name)
	}
	s.hostSendSerialize(room)
	if npc.weaponView != 0 {
		room.deferredDestroys = append(room.deferredDestroys, deferredDestroy{
			view: npc.weaponView, at: time.Now().Add(npcDeathRemoveDelay),
		})
	}
	log.Printf("[HOST] npc %d (%s) killed by actor=%d room=%s", npc.viewBase, npc.kit, attackerView, room.Name)
	s.hostRecordNpcKill(room, attackerView, npc.kit, criticalHit)
	s.scriptFire(room, "OnNpcDeath",
		lua.LNumber(npc.viewBase), lua.LString(npc.kit),
		lua.LNumber(s.scriptActorForView(room, attackerView)))
}

func (s *EnetServer) hostRecordDamageStat(room *Room, attackerView, damage int32, critical, isDOT bool) {
	if attackerView == 0 {
		return
	}
	sess := sessionByAvatarBase(s, room, attackerView)
	if sess == nil {
		return
	}
	sess.statTotalDamage += damage
	if critical {
		sess.statCritDmg += damage
	}
	if !isDOT {
		sess.statHits++
		if critical {
			sess.statCriticalHits++
		}
	}
}

func (s *EnetServer) hostRecordNpcKill(room *Room, attackerView int32, kit string, criticalHit bool) {
	if attackerView == 0 {
		return
	}
	sess := sessionByAvatarBase(s, room, attackerView)
	if sess == nil || sess.hostChar == nil {
		return
	}
	sess.statKills++
	if criticalHit {
		sess.statCriticalKills++
	}
	switch {
	case bossKits[kit]:
		sess.statBossKills++
		bumpAchievement(sess.hostChar.CharID, achIDKillBoss, 1)
	case isBadassKit(kit):
		sess.statBadassKills++
		bumpAchievement(sess.hostChar.CharID, achIDKillBadass, 1)
	}
	if xp := s.npcKillXP(kit); xp > 0 {
		s.hostGrantExp(sess, xp)
	}
	s.hostAdvanceKillQuestTasks(sess, sess.hostChar.CharID)
}

func (s *EnetServer) xpMultiplier() float64 {
	if s.XPMultiplier > 0 {
		return s.XPMultiplier
	}
	return 1
}

func (s *EnetServer) npcKillXP(kit string) int {
	if s.NpcKillXPBase <= 0 {
		return 0
	}
	v := s.NpcKillXPBase
	switch {
	case bossKits[kit]:
		if s.NpcKillXPBossMult > 0 {
			v *= s.NpcKillXPBossMult
		}
	case isBadassKit(kit):
		if s.NpcKillXPBadassMult > 0 {
			v *= s.NpcKillXPBadassMult
		}
	}
	return int(math.Round(v))
}

func (s *EnetServer) hostGrantExp(sess *ClientSession, xp int) bool {
	ch := sess.hostChar
	if ch == nil || xp == 0 {
		return ch != nil
	}
	xp = int(math.Round(float64(xp) * s.xpMultiplier()))
	if xp == 0 {
		return true
	}
	oldLevel := levelForExp(ch.Experience)
	ch.Experience += xp
	newLevel := levelForExp(ch.Experience)
	ch.Level = newLevel
	if err := db.UpdateCharacterExpLevel(ch.CharID, newLevel, ch.Experience); err != nil {
		log.Printf("[HOST] hostGrantExp: persist char=%d error: %v", ch.CharID, err)
		return false
	}
	log.Printf("[HOST] char=%d +%d xp → exp=%d level=%d", ch.CharID, xp, ch.Experience, newLevel)
	online := s.sessionOnline(sess.userID)
	if online == nil {
		return true
	}
	s.sendEventTo(online, EvExperienceUpdated, map[byte]any{0: int32(ch.Experience)})
	if newLevel > oldLevel {
		s.sendEventTo(online, EvLevelUp, map[byte]any{
			0: int32(ch.CharID), 1: int32(ch.Experience), 2: int32(newLevel),
		})
	}
	return true
}

func (s *EnetServer) hostTickPendingEquips(room *Room) {
	now := time.Now()
	for _, uid := range room.Players {
		sess := s.sessionInRoom(uid, room.Name)
		if sess == nil || sess.equipAt.IsZero() || now.Before(sess.equipAt) {
			continue
		}
		sess.equipAt = time.Time{}
		sess.activeWeaponSlot = sess.equipSlot
		s.hostSendSerialize(room)
		log.Printf("[HOST] auto-equip: active slot → %d actor view=%d room=%s", sess.equipSlot, sess.avatarBase, room.Name)
	}
}

func (s *EnetServer) hostTickSupplyStations(room *Room) {
	level := getCurScnFromProps(room.Name, room.Properties)
	type refillPt struct {
		name string
		pos  [3]float32
	}
	var pts []refillPt
	for _, e := range levelSpawnEntries(level) {
		if e.Script == "SupplyStation" && len(e.Pos) == 3 {
			pts = append(pts, refillPt{e.GO, [3]float32{e.Pos[0], e.Pos[1], e.Pos[2]}})
		}
	}
	if room.clusters != nil {
		for _, rt := range room.clusters.list {
			if strings.Contains(rt.def.Name, "ProvideAmmo") && rt.def.Type == "Scene" && len(rt.def.Pos) == 3 {
				pts = append(pts, refillPt{rt.def.Name, [3]float32{rt.def.Pos[0], rt.def.Pos[1], rt.def.Pos[2]}})
			}
		}
	}
	if len(pts) == 0 {
		return
	}
	now := time.Now()
	for _, uid := range room.Players {
		sess := s.sessionInRoom(uid, room.Name)
		if sess == nil || sess.avatarBase == 0 || sess.isDead || now.Sub(sess.lastSupplyAt) < 2*time.Second {
			continue
		}
		pos, ok := playerStreamPos(room, sess.avatarBase)
		if !ok {
			pos = room.playerPos[sess.avatarBase+1]
		}
		for _, p := range pts {
			dx, dz := pos[0]-p.pos[0], pos[2]-p.pos[2]
			if dx*dx+dz*dz > 20*20 {
				continue
			}
			if sess.ammoEst >= 300 {
				continue
			}
			sess.lastSupplyAt = now
			sess.ammoEst = 360
			s.hostSendRPC(room, sess.avatarBase+3, "RPC_OnReceiveAmmos", []any{ammoPouchesForSession(sess, true)})
			maxLife := int32(120)
			if sess.hostChar != nil {
				maxLife = maxLifeForCharacter(sess.hostChar)
			}
			sess.shield = sess.maxShield
			s.hostSendRPC(room, sess.avatarBase, "RPC_RequestAllLifeAndShield", []any{
				map[int]any{0: int32(1), 1: sess.avatarBase, 2: playerLifeTable(maxLife, maxLife, sess.shield, sess.maxShield)},
			})
			sess.maxHP, sess.hp, sess.isDead = maxLife, maxLife, false
			log.Printf("[HOST] supply refill actor view=%d at %q room=%s", sess.avatarBase, p.name, room.Name)
			break
		}
	}
}

func (s *EnetServer) hostTickShieldRegen(room *Room) {
	now := time.Now()
	for _, uid := range room.Players {
		sess := s.sessionInRoom(uid, room.Name)
		if sess == nil || sess.avatarBase == 0 || sess.isDead || sess.maxShield <= 0 || sess.shield >= sess.maxShield {
			continue
		}
		elapsed := now.Sub(sess.lastDamageAt) - shieldRegenDelay
		if elapsed <= 0 {
			continue
		}
		elapsed = time.Duration(float64(elapsed) * (1 + effectValue(sess.hostChar, "ShieldRechargeRate")))
		newShield := shieldAfterRegen(sess.shieldRegenBaseline, sess.maxShield, elapsed)
		if newShield <= sess.shield {
			continue
		}
		sess.shield = newShield
		s.hostSendRPC(room, sess.avatarBase, "RPC_RequestAllLifeAndShield", []any{
			map[int]any{0: int32(1), 1: sess.avatarBase, 2: playerLifeTable(sess.hp, sess.maxHP, sess.shield, sess.maxShield)},
		})
	}
}

func (s *EnetServer) hostTickHealthRegen(room *Room) {
	now := time.Now()
	for _, uid := range room.Players {
		sess := s.sessionInRoom(uid, room.Name)
		if sess == nil || sess.avatarBase == 0 || sess.isDead || sess.maxHP <= 0 || sess.hp >= sess.maxHP {
			continue
		}
		regenRate := effectValue(sess.hostChar, "FlexibleTimeRegenHealthSpeed") + effectValue(sess.hostChar, "FixedTimeRegenHealthSpeed")
		if regenRate <= 0 {
			continue
		}
		if now.Sub(sess.lastDamageAt) < shieldRegenDelay {
			continue
		}
		if now.Sub(sess.lastHealthRegenTickAt) < time.Second {
			continue
		}
		sess.lastHealthRegenTickAt = now
		sess.hp += int32(float64(sess.maxHP) * regenRate)
		if sess.hp > sess.maxHP {
			sess.hp = sess.maxHP
		}
		s.hostSendRPC(room, sess.avatarBase, "RPC_RequestAllLifeAndShield", []any{
			map[int]any{0: int32(1), 1: sess.avatarBase, 2: playerLifeTable(sess.hp, sess.maxHP, sess.shield, sess.maxShield)},
		})
	}
}

const (
	grenadeFuse               = 2 * time.Second
	grenadeBlowUpRadius       = 10.0
	grenadeBlowUpDamage       = 90
	grenadeDestroyDelay       = 700 * time.Millisecond
	playerGrenadeBlowUpRadius = grenadeBlowUpRadius / 2
)

const grenadeSpawnDelay = 450 * time.Millisecond

const grenadeThrowSpeed = 13.0

const grenadeHandForwardOffset = 1.0

func grenadeThrowVectors(yawDeg float64, pitch16 int16) (dir, handOffset [3]float32) {
	yaw := yawDeg * math.Pi / 180
	pitch := (float64(pitch16)/100 - 10) * math.Pi / 180
	dir = [3]float32{
		float32(math.Sin(yaw) * math.Cos(pitch)),
		float32(-math.Sin(pitch)),
		float32(math.Cos(yaw) * math.Cos(pitch)),
	}
	handOffset = [3]float32{
		float32(math.Sin(yaw)) * grenadeHandForwardOffset,
		0,
		float32(math.Cos(yaw)) * grenadeHandForwardOffset,
	}
	return dir, handOffset
}

const grenadeMax int32 = 3

func (s *EnetServer) hostSendGrenadeCount(room *Room, sess *ClientSession) {
	if sess.avatarBase == 0 {
		return
	}
	log.Printf("[HOST] actor view=%d sending RPC_H2C_SetGrenadeCount max=%d cur=%d room=%s",
		sess.avatarBase, sess.grenadeCap, sess.grenadeCount, room.Name)
	s.hostSendRPC(room, sess.avatarBase+3, "RPC_H2C_SetGrenadeCount", []any{byte(sess.grenadeCap), byte(sess.grenadeCount)})
}

func (s *EnetServer) hostThrowGrenade(room *Room, sess *ClientSession) {
	if sess.avatarBase == 0 {
		return
	}
	if sess.grenadeCount <= 0 {
		log.Printf("[HOST] actor view=%d Throw rejected: out of grenades room=%s", sess.avatarBase, room.Name)
		return
	}
	sess.grenadeCount--
	s.hostSendGrenadeCount(room, sess)
	now := time.Now()
	room.pendingGrenades = append(room.pendingGrenades, pendingGrenade{
		sess: sess, ownerView: sess.avatarBase,
		spawnAt: now.Add(grenadeSpawnDelay),
		at:      now.Add(grenadeSpawnDelay + grenadeFuse),
	})
}

func (s *EnetServer) hostLaunchGrenade(room *Room, g *pendingGrenade) {
	sess := g.sess
	if sess == nil || sess.avatarBase == 0 || sess.roomName != room.Name {
		return
	}
	pos, ok := playerStreamPos(room, sess.avatarBase)
	if !ok {
		pos = room.playerPos[sess.avatarBase+1]
	}
	dir, handOffset := grenadeThrowVectors(sess.lastYawDeg, sess.lastPitch16)
	vel := [3]float32{dir[0] * grenadeThrowSpeed, dir[1] * grenadeThrowSpeed, dir[2] * grenadeThrowSpeed}
	hand := [3]float32{pos[0] + handOffset[0], pos[1] + 1.5, pos[2] + handOffset[2]}

	base := room.NextAvatarViewBase()
	g.view = base
	g.spawned = true
	g.pos = grenadeLandingEstimate(m2level(room), hand, vel)
	views := []int32{base, base + 1, base + 2}
	s.hostBroadcastJournaled(room, 202, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): "Entities/Object/Launchable/Grenade",
			byte(1): vector3Custom(hand[0], hand[1], hand[2]),
			byte(4): IntArray{Items: views},
			byte(5): []any{sess.avatarBase},
			byte(6): int32(msNow()),
			byte(7): base,
		},
	}, false)
	s.broadcastEvent(room, 201, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): int32(msNow()),
			Short(1): map[any]any{
				byte(0): base + 2,
				byte(1): []any{
					vector3Custom(hand[0], hand[1], hand[2]),
					quaternionCustom(0, 0, 0, 1),
					vector3Custom(vel[0], vel[1], vel[2]),
					vector3Custom(4, 8, 2),
				},
			},
			Short(2): map[any]any{
				byte(0): base + 1,
				byte(1): grenadeTaskStream(bhvTaskCountDown, float32(grenadeFuse.Seconds())),
			},
		},
	}, nil, nil)
	log.Printf("[HOST] grenade %d launched from [%.1f %.1f %.1f] vel=[%.1f %.1f %.1f] landing≈[%.1f %.1f %.1f] room=%s",
		base, hand[0], hand[1], hand[2], vel[0], vel[1], vel[2], g.pos[0], g.pos[1], g.pos[2], room.Name)
}

func grenadeLandingEstimate(level string, start, vel [3]float32) [3]float32 {
	pos := start
	v := vel
	const dt = float32(0.066)
	for range 60 {
		pos[0] += v[0] * dt
		pos[1] += v[1] * dt
		pos[2] += v[2] * dt
		v[1] -= 9.81 * dt
		gy, ok := navmeshGroundHeight(level, pos[0], pos[2], pos[1])
		if !ok {
			gy, ok = groundHint(level, pos[0], pos[2], pos[1])
		}
		if ok && v[1] < 0 && pos[1] <= gy {
			pos[1] = gy
			return pos
		}
	}
	return pos
}

func grenadeTaskStream(taskType int32, floatArg float32) []any {
	return []any{
		int32(1), int32(0), taskType, int32(0), false, int32(-1), int32(-1),
		int32(0), int32(0), int32(0), int32(-1), floatArg, int32(-1), int32(-1),
		int32(-1), false, false, false,
		vector3Custom(0, 0, 0), vector3Custom(0, 0, 0), vector3Custom(0, 0, 0), vector3Custom(0, 0, 0),
		int32(0), "",
	}
}

func (s *EnetServer) hostTickGrenades(room *Room) bool {
	if len(room.pendingGrenades) == 0 {
		return false
	}
	now := time.Now()
	remaining := room.pendingGrenades[:0]
	exploded := false
	for i := range room.pendingGrenades {
		g := room.pendingGrenades[i]
		if !g.spawned {
			if now.Before(g.spawnAt) {
				remaining = append(remaining, g)
				continue
			}
			s.hostLaunchGrenade(room, &g)
			if !g.spawned {
				continue
			}
			remaining = append(remaining, g)
			continue
		}
		if now.Before(g.at) {
			remaining = append(remaining, g)
			continue
		}
		s.hostExplodeGrenade(room, g)
		exploded = true
	}
	room.pendingGrenades = remaining
	return exploded
}

func (s *EnetServer) hostExplodeGrenade(room *Room, g pendingGrenade) {
	s.broadcastEvent(room, 201, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): int32(msNow()),
			Short(1): map[any]any{
				byte(0): g.view + 1,
				byte(1): grenadeTaskStream(bhvTaskBlowUp, 0),
			},
		},
	}, nil, nil)
	const r2 = float32(playerGrenadeBlowUpRadius * playerGrenadeBlowUpRadius)
	blowUpDamage := int32(float64(grenadeBlowUpDamage) * float64(levelDamageModifier(m2level(room))))
	if g.sess != nil && g.sess.hostChar != nil {
		blowUpDamage = int32(float64(blowUpDamage) * powerRatingForLevel(int(g.sess.hostChar.Level)))
		blowUpDamage = int32(float64(blowUpDamage) * (1 + effectValue(g.sess.hostChar, "GrenadeDmg")))
	}
	for i := range room.npcs {
		npc := &room.npcs[i]
		if npc.hp <= 0 {
			continue
		}
		dx, dz := g.pos[0]-npc.pos[0], g.pos[2]-npc.pos[2]
		if dx*dx+dz*dz > r2 {
			continue
		}
		npc.hp -= blowUpDamage
		if npc.hp < 0 {
			npc.hp = 0
		}
		if npc.hp > 0 && npcHealthBarVisuallyEmpty(npc.hp, npc.maxHP) {
			npc.hp = 0
		}
		if npc.hp > 0 {
			continue
		}
		s.hostRecordDamageStat(room, g.ownerView, blowUpDamage, false, false)
		s.hostKillNpc(room, npc, g.ownerView, blowUpDamage, elementExplosive, false)
	}
	for _, uid := range room.Players {
		target := s.sessionInRoom(uid, room.Name)
		if target == nil || target.avatarBase == 0 {
			continue
		}
		ppos, ok := playerStreamPos(room, target.avatarBase)
		if !ok {
			ppos = room.playerPos[target.avatarBase+1]
		}
		dx, dz := g.pos[0]-ppos[0], g.pos[2]-ppos[2]
		if dx*dx+dz*dz > r2 {
			continue
		}
		s.hostRecordDamageStat(room, g.ownerView, blowUpDamage, false, false)
		if s.hostDamagePlayer(room, target, g.ownerView+1, blowUpDamage, attackSubTypeLegacy, elementExplosive) && g.sess != nil && target != g.sess {
			g.sess.pvpKillCount++
			s.hostCheckPvPKillTarget(room)
		}
	}
	room.deferredDestroys = append(room.deferredDestroys, deferredDestroy{
		view: g.view, at: time.Now().Add(grenadeDestroyDelay),
	})
	log.Printf("[HOST] grenade exploded viewID=%d room=%s", g.view, room.Name)
}

func (s *EnetServer) hostExplodeBarrel(room *Room, attackerView int32, st *staticEnt) {
	s.broadcastEvent(room, 201, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): int32(msNow()),
			Short(1): map[any]any{
				byte(0): st.viewBase + 1,
				byte(1): grenadeTaskStream(bhvTaskBlowUp, 0),
			},
		},
	}, nil, nil)
	const r2 = float32(grenadeBlowUpRadius * grenadeBlowUpRadius)
	_, dmgMult := s.npcCombatScale(room)
	scaledDamage := int32(float32(grenadeBlowUpDamage) * dmgMult)
	for i := range room.npcs {
		npc := &room.npcs[i]
		if npc.hp <= 0 {
			continue
		}
		dx, dz := st.pos[0]-npc.pos[0], st.pos[2]-npc.pos[2]
		if dx*dx+dz*dz > r2 {
			continue
		}
		npc.hp -= grenadeBlowUpDamage
		if npc.hp < 0 {
			npc.hp = 0
		}
		if npc.hp > 0 && npcHealthBarVisuallyEmpty(npc.hp, npc.maxHP) {
			npc.hp = 0
		}
		if npc.hp > 0 {
			s.hostDamageEntity(room, attackerView, npc.viewBase, grenadeBlowUpDamage, 0, elementExplosive, false)
			s.hostSendRPC(room, npc.equipView(), "RPC_SendLifeAndShield", []any{npcVitalsTable(npc.kit, npc.maxHP, npc.hp)})
			npcMaybeStartElementalDOT(npc, attackerView, elementFire, grenadeBlowUpDamage, time.Now(), 0, false)
			continue
		}
		s.hostRecordDamageStat(room, attackerView, grenadeBlowUpDamage, false, false)
		s.hostKillNpc(room, npc, attackerView, grenadeBlowUpDamage, elementExplosive, false)
	}
	for _, uid := range room.Players {
		target := s.sessionInRoom(uid, room.Name)
		if target == nil || target.avatarBase == 0 {
			continue
		}
		ppos, ok := playerStreamPos(room, target.avatarBase)
		if !ok {
			ppos = room.playerPos[target.avatarBase+1]
		}
		dx, dz := st.pos[0]-ppos[0], st.pos[2]-ppos[2]
		if dx*dx+dz*dz > r2 {
			continue
		}
		if s.hostDamagePlayer(room, target, attackerView, scaledDamage, attackSubTypeLegacy, elementNone) {
			log.Printf("[HOST] actor view=%d died (killed by exploding barrel) room=%s", target.avatarBase, room.Name)
		}
	}
	room.deferredDestroys = append(room.deferredDestroys, deferredDestroy{
		view: st.viewBase, at: time.Now().Add(grenadeDestroyDelay),
	})
}

var starterWeaponDropItem = &domain.InventoryItem{
	ItemKind: domain.ItemTypeWeapon, ResourceID: 1, Level: 1,
	PartA: 0, ItemName: "SMG", IsWeapon: true, GenerationSource: domain.GenerationSourceLoot,
}

func (s *EnetServer) hostDropItemInWorld(onlineSess *ClientSession, item *domain.InventoryItem) {
	roomSess := s.sessionInAnyRoom(onlineSess.dbAccountID)
	if roomSess == nil || roomSess.roomName == "" {
		return
	}
	room, ok := s.rooms[roomSess.roomName]
	if !ok {
		return
	}
	gamemode := gamemodeName(room)
	if gamemode != "GameModeTown" && gamemode != "GamemodeRV" {
		return
	}
	s.hostSpawnPickableDrop(room, roomSess, nil, "Entities/Object/Weapon/Weapon", weaponItemDNA(item), item, nil, -1, 0, 0)
}

func (s *EnetServer) hostSpawnWeaponDrop(room *Room, sess *ClientSession, chestPos []float32, attachID int32) {
	s.hostSpawnPickableDrop(room, sess, chestPos, "Entities/Object/Weapon/Weapon", starterWeaponItemDNA(), starterWeaponDropItem, nil, attachID, 0, 0)
}

func (s *EnetServer) hostSpawnPickableDrop(room *Room, sess *ClientSession, chestPos []float32, prefab string, dna Custom, item *domain.InventoryItem, reward *dropReward, attachID, numWeaponSpawned, numItemSpawned int32) {
	var pos [3]float32
	if len(chestPos) == 3 {
		pos = [3]float32{chestPos[0], chestPos[1], chestPos[2]}
	} else if p, ok := playerStreamPos(room, sess.avatarBase); ok {
		pos = p
	} else {
		return
	}
	view := room.NextAvatarViewBase()
	const lootDropHeightOffset = 0
	spawnY := pos[1]
	if attachID == -1 {
		spawnY += lootDropHeightOffset
	}
	s.hostBroadcastJournaled(room, 202, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): prefab,
			byte(1): vector3Custom(pos[0], spawnY, pos[2]),
			byte(4): IntArray{Items: []int32{view}},
			byte(5): []any{dna, vector3Custom(0, 0, 0), int32(-1), attachID, numWeaponSpawned, numItemSpawned},
			byte(6): int32(msNow()),
			byte(7): view,
		},
	}, true)
	if room.dropPos == nil {
		room.dropPos = map[int32][3]float32{}
	}
	room.dropPos[view] = pos
	if item != nil {
		if room.dropItem == nil {
			room.dropItem = map[int32]*domain.InventoryItem{}
		}
		room.dropItem[view] = item
	}
	if reward != nil {
		if room.dropReward == nil {
			room.dropReward = map[int32]dropReward{}
		}
		room.dropReward[view] = *reward
	}
}

func (s *EnetServer) hostPersistPickup(room *Room, sess *ClientSession, view int32) int {
	it, ok := room.dropItem[view]
	if !ok {
		return -1
	}
	delete(room.dropItem, view)
	if sess.hostChar == nil {
		return -1
	}
	ch, err := db.GetCharacterByID(sess.hostChar.CharID)
	backpackSize := 20
	if err == nil {
		backpackSize = clampBackpackSize(ch.BackpackSize)
	}
	granted := *it
	granted.CharID = sess.hostChar.CharID
	granted.UID = newRandomUID("drop")
	_, slot, err := db.AddItemToBackpack(&granted, backpackSize)
	if err != nil {
		log.Printf("[HOST] pickup persist error user=%s view=%d: %v", sess.userID, view, err)
		return -1
	}
	log.Printf("[HOST] user=%s picked up %q (kind=%d) view=%d -> backpack slot=%d", sess.userID, granted.ItemName, granted.ItemKind, view, slot)
	invHT := realInventoryHashtable(sess.hostChar.CharID)
	s.sendEventTo(sess, EvInventoryUpdated, map[byte]any{0: invHT})
	if online := s.sessionOnline(sess.userID); online != nil {
		s.sendEventTo(online, EvInventoryUpdated, map[byte]any{0: invHT})
	}
	gamemode := gamemodeName(room)
	if gamemode != "GameModeTown" && gamemode != "GamemodeRV" && sess.avatarBase != 0 {
		s.hostSendBackpackItems(room, sess)
	}

	return slot
}

func (s *EnetServer) hostSpawnNpcWeapon(room *Room, pos [3]float32, dna Custom) int32 {
	view := room.NextAvatarViewBase()
	s.hostBroadcastJournaled(room, 202, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): "Entities/Object/Weapon/Weapon",
			byte(1): vector3Custom(pos[0], pos[1], pos[2]),
			byte(4): IntArray{Items: []int32{view}},
			byte(5): []any{dna, vector3Custom(0, 0, 0), int32(HostActorNr), int32(-1), int32(0), int32(0)},
			byte(6): int32(msNow()),
			byte(7): view,
		},
	}, true)
	return view
}

func npcEquipmentStream(weaponView int32, level byte) []any {
	return []any{
		level,
		byte(0),
		false,
		"",
		false,
		int32(0),
		weaponView,
	}
}

func npcBossEquipmentStream(weaponView int32, level byte, phase, phaseMin, phaseMax byte) []any {
	return append(npcEquipmentStream(weaponView, level), phase, phaseMin, phaseMax)
}

func bossPhaseFor(hp, maxHP int32) (phase, phaseMin, phaseMax byte) {
	if maxHP <= 0 {
		return 1, 66, 100
	}
	pct := hp * 100 / maxHP
	switch {
	case pct > 66:
		return 1, 66, 100
	case pct > 33:
		return 2, 33, 66
	default:
		return 3, 0, 33
	}
}

func (s *EnetServer) hostSendLevelObjectState(room *Room) {
	nowMS := int32(msNow())
	s.broadcastEvent(room, 201, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): nowMS,
			Short(1): map[any]any{
				byte(0): int32(lomViewID),
				byte(1): levelObjectBits(room),
			},
		},
	}, nil, nil)
}

func levelObjectBits(room *Room) []any {
	objs := levelObjectEntries(getCurScnFromProps(room.Name, room.Properties))
	maxID := len(objs) - 1
	for id := range room.levelObjectStates {
		if id > maxID {
			maxID = id
		}
	}
	bits := make([]byte, maxID/8+1+16)
	for i, e := range objs {
		if e.Initial == 1 {
			bits[i/8] |= 1 << uint(i%8)
		}
	}
	for id, activated := range room.levelObjectStates {
		if activated {
			bits[id/8] |= 1 << uint(id%8)
		} else {
			bits[id/8] &^= 1 << uint(id%8)
		}
	}
	stream := make([]any, len(bits))
	for i, b := range bits {
		stream[i] = b
	}
	return stream
}

func (s *EnetServer) hostSendSessionInfo(sess *ClientSession, room *Room, nowMS int32, instanceName, mapName, gamemode string) {
	sig := fmt.Sprintf("%s|%s|%s|%d", instanceName, mapName, gamemode, sess.mapGen)
	if sess.sessionInfoSent && sig == sess.lastSessionInfoSig {
		return
	}
	sess.sessionInfoSent = true
	sess.lastSessionInfoSig = sig
	timeLimit, endScore, endKillCount := int32(0), int32(0), int32(0)
	switch gamemode {
	case "GamemodeTeamDeathmatch", "GameModePvP":
		timeLimit = s.pvpMatchTimeLimit(room)
		endKillCount = s.pvpKillTarget()
	case "GamemodeClaptrapSoccer":
		remaining := s.claptrapRemainingSeconds(room)
		if remaining < claptrapMinDisplaySeconds {
			remaining = claptrapMinDisplaySeconds
		}
		timeLimit = int32(time.Since(room.CreatedAt).Seconds()) + remaining
		endScore = s.claptrapTargetScore()
	}
	s.sendEventTo(sess, 201, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): nowMS,
			Short(1): map[any]any{
				byte(0): int32(sessionInfoViewID),
				byte(1): []any{
					instanceName,
					byte(0),
					Double(float64(nowMS) / 1000),
					false,
					sess.mapGen,
					mapName,
					gamemode,
					Short(0),
					Short(timeLimit),
					Short(endScore),
					Short(endKillCount),
					float32(0), float32(0),
				},
			},
		},
	})
}

func npcSendsMovementSync(npc *spawnedNpc) bool {
	if npc.viewCount != 0 && npc.viewCount < 4 {
		return false
	}
	return !(npc.jumping && hammerAttackKits[npc.kit])
}

func displayNameFor(sess *ClientSession, ch *domain.Character) string {
	name := sess.accountUsername
	if name == "" {
		if ch != nil && ch.Name != "" {
			name = ch.Name
		} else {
			name = sess.userID
		}
	}
	if sess.guildTag != "" {
		return sess.guildTag + name
	}
	return name
}

func (s *EnetServer) hostSendSerialize(room *Room) {
	nowMS := int32(msNow())
	curScn := getCurScnFromProps(room.Name, room.Properties)
	gamemode := gamemodeName(room)
	instanceLevel := byte(s.roomEffectiveInstanceLevel(room))

	for _, uid := range room.Players {
		if target := s.sessionInRoom(uid, room.Name); target != nil {
			if _, waiting := s.pendingSceneIdx[target.dbAccountID]; waiting {
				continue
			}
			s.hostSendSessionInfo(target, room, nowMS, curScn, curScn, gamemode)
		}
	}

	outer := map[any]any{
		byte(0): nowMS,
	}

	roomGameMode := asInt32(room.Properties["GameMode"])
	idx := int16(1)
	var peerMoveSyncs []peerMoveSync
	for actorNr, uid := range room.Players {
		target := s.sessionInRoom(uid, room.Name)
		if target == nil || target.hostChar == nil {
			continue
		}
		ch := target.hostChar
		teamIDWire := wireTeamID(target.teamID, isTeamGameMode(roomGameMode))
		if roomGameMode == 4 {
			teamIDWire = byte(actorNr)
		}
		outer[Short(idx)] = map[any]any{
			byte(0): playerViewID(actorNr),
			byte(1): []any{
				actorNr,
				displayNameFor(target, ch),
				int32(target.accountID),
				int32(ch.CharID),
				int32(ch.Experience),
				teamIDWire,
				byte(0),
				target.playerState,
				false,
				byte(0),
				avatarDNACustom(ch),
			},
		}
		idx++

		if target.avatarBase != 0 && (gamemode == "GameModeTown" || gamemode == "GamemodeRV") {
			items := []any{byte(0)} // flags
			for slot := 0; slot < 4; slot++ {
				if slot < len(target.equippedWeapons) {
					items = append(items, target.equippedWeapons[slot].dna)
				} else {
					items = append(items, nil)
				}
			}
			items = append(items, target.activeWeaponSlot)
			if sig := fmt.Sprintf("%v", items); !target.streamGraceElapsed() || sig != target.lastWeaponStreamSig {
				outer[Short(idx)] = map[any]any{
					byte(0): target.avatarBase + 3,
					byte(1): items,
				}
				idx++
				target.lastWeaponStreamSig = sig
			}
		}
		if target.avatarBase != 0 && gamemode != "GameModeTown" && gamemode != "GamemodeRV" {
			var slots []int32
			var dnas []Custom
			for _, we := range target.equippedWeapons {
				slots = append(slots, we.view)
				dnas = append(dnas, we.dna)
			}
			slots = append(slots, target.pickedWeapons...)
			var shieldDNA any
			if target.shieldDNA.Data != nil {
				shieldDNA = target.shieldDNA
			}
			items := []any{
				byte(0),
				target.shieldView, shieldDNA,
			}
			for slot := 0; slot < 4; slot++ {
				if slot >= len(slots) {
					items = append(items, int32(0), nil)
					continue
				}
				dna := starterWeaponItemDNA()
				if slot < len(dnas) {
					dna = dnas[slot]
				}
				items = append(items, slots[slot], dna)
			}
			items = append(items, target.activeWeaponSlot)
			if sig := fmt.Sprintf("%v", items); !target.streamGraceElapsed() || sig != target.lastWeaponStreamSig {
				outer[Short(idx)] = map[any]any{
					byte(0): target.avatarBase + 3,
					byte(1): items,
				}
				idx++
				target.lastWeaponStreamSig = sig
			}
		}

		if target.avatarBase != 0 {
			peerMoveSyncs = append(peerMoveSyncs, peerMoveSync{sess: target, avatarBase: target.avatarBase})

			if target.isDead {
				outer[Short(idx)] = map[any]any{
					byte(0): target.avatarBase + 2,
					byte(1): playerBhvTaskStream(target.downTaskUID, bhvTaskPlayerDown, float32(ffylDuration.Seconds()), -1, false),
				}
				idx++
			}

			if target.hostChar != nil {
				stream := skillTreeStatusStream(target.hostChar)
				if sig := fmt.Sprintf("%v", stream); !target.streamGraceElapsed() || sig != target.lastSkillTreeSig {
					outer[Short(idx)] = map[any]any{
						byte(0): target.avatarBase + 4,
						byte(1): []any{stream},
					}
					idx++
					target.lastSkillTreeSig = sig
				}
			}
		}
	}

	for i := range room.npcs {
		npc := &room.npcs[i]
		if npcSendsMovementSync(npc) {
			speed := Short(0)
			if npc.moving {
				speed = Short(int16(npcMoveSpeed(npc) * 100))
			}
			outer[Short(idx)] = map[any]any{
				byte(0): npc.viewBase + 2,
				byte(1): []any{
					vector3Custom(npc.pos[0], npc.pos[1], npc.pos[2]),
					Short(int16(npc.yawDeg * 100)),
					Short(0),
					speed,
					Short(0),
				},
			}
			idx++
		}
		if npc.taskType != 0 {
			outer[Short(idx)] = map[any]any{
				byte(0): npc.viewBase + 1,
				byte(1): bhvTaskStream(npc),
			}
			idx++
		}
		if npc.dazedUntil.After(time.Now()) {
			outer[Short(idx)] = map[any]any{
				byte(0): npc.viewBase + 1,
				byte(1): bhvDazeEffectStream(npc),
			}
			idx++
		}
		if npc.kit != "ClapTrapTutor" && time.Since(npc.spawnedAt) >= npcEquipmentStreamGracePeriod {
			var payload []any
			var sig string
			if bossHudKits[npc.kit] {
				phase, phaseMin, phaseMax := bossPhaseFor(npc.hp, npc.maxHP)
				if int(phase) > room.bossPhase {
					room.bossPhase = int(phase)
				}
				payload = npcBossEquipmentStream(npc.weaponView, instanceLevel, phase, phaseMin, phaseMax)
				sig = fmt.Sprintf("boss|%d|%d|%d|%d|%d", npc.weaponView, instanceLevel, phase, phaseMin, phaseMax)
			} else {
				payload = npcEquipmentStream(npc.weaponView, instanceLevel)
				sig = fmt.Sprintf("%d|%d", npc.weaponView, instanceLevel)
			}
			if sig != npc.lastEquipStreamSig {
				outer[Short(idx)] = map[any]any{
					byte(0): npc.equipView(),
					byte(1): payload,
				}
				idx++
				npc.lastEquipStreamSig = sig
			}
		}
	}

	if room.lomSpawned {
		outer[Short(idx)] = map[any]any{
			byte(0): int32(lomViewID),
			byte(1): levelObjectBits(room),
		}
		idx++
	}

	const serializeBatchSize = 40
	for start := int16(1); start < idx; start += serializeBatchSize {
		end := start + serializeBatchSize
		if end > idx {
			end = idx
		}
		batch := map[any]any{byte(0): nowMS}
		for k := start; k < end; k++ {
			batch[Short(k)] = outer[Short(k)]
		}
		s.broadcastEvent(room, 201, map[byte]any{
			ParamPlayerNr:  HostActorNr,
			ParamEventData: batch,
		}, nil, nil)
	}

	for _, pm := range peerMoveSyncs {
		bpos := pm.sess.broadcastPos
		payload := map[byte]any{
			ParamPlayerNr: HostActorNr,
			ParamEventData: map[any]any{
				byte(0): nowMS,
				Short(1): map[any]any{
					byte(0): pm.avatarBase + 1,
					byte(1): []any{
						vector3Custom(bpos[0], bpos[1], bpos[2]),
						Short(int16(pm.sess.lastYawDeg * 100)),
						pm.sess.lastInputFrame,
						pm.sess.lastMoveFlg,
						Short(pm.sess.lastPitch16),
					},
				},
			},
		}
		if s.UnreliableMovementSync {
			s.broadcastEventUnreliable(room, 201, payload, pm.sess, nil)
		} else {
			s.broadcastEvent(room, 201, payload, pm.sess, nil)
		}
		if room.moveSyncDumps < 10 {
			room.moveSyncDumps++
			log.Printf("[HOST] TEMP sent MovementSync view=%d owner_actor=%d owner_user=%s room=%s pos=[%.1f %.1f %.1f] flags=%08b payload=%#v",
				pm.avatarBase+1, room.ActorNrFor(pm.sess.userID), pm.sess.userID, room.Name, bpos[0], bpos[1], bpos[2], pm.sess.lastMoveFlg, payload)
		}
	}
}

type peerMoveSync struct {
	sess       *ClientSession
	avatarBase int32
}

func avatarDNACustom(ch *domain.Character) Custom {
	fbxs := parseByteCSV(ch.AvatarParts)
	mats := parseByteCSV(ch.AvatarMaterials)
	for len(mats) < len(fbxs) {
		mats = append(mats, 0)
	}
	data := make([]byte, 0, 10+2*len(fbxs))
	data = binary.LittleEndian.AppendUint16(data, uint16(ch.ClassType))
	data = binary.LittleEndian.AppendUint32(data, uint32(len(fbxs)))
	data = binary.LittleEndian.AppendUint32(data, ch.AvatarID)
	data = append(data, fbxs...)
	data = append(data, mats[:len(fbxs)]...)
	return Custom{Code: 68, Data: data}
}

func parseByteCSV(s string) []byte {
	parts := strings.Split(s, ",")
	out := make([]byte, 0, len(parts))
	for _, p := range parts {
		n, _ := strconv.Atoi(strings.TrimSpace(p))
		out = append(out, byte(n))
	}
	return out
}

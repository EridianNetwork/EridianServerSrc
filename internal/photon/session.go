package photon

import (
	"fmt"
	"log"
	"math/rand"
	"slices"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

var playlistIDSceneMap = map[int32]string{
	10000: "lvl_town",
}

var playlistSceneMap = map[string]string{
	"lvl_Floasm_Small":         "lvl_Floasm_Small",
	"lvl_Tutorial_Part1_4x":    "lvl_Tutorial_Part1_4x",
	"lvl_autotest_1":           "lvl_autotest_1",
	"lvl_td_Arid_Small":        "lvl_td_Arid_Small",
	"lvl_td_Arid_Medium":       "lvl_td_Arid_Medium",
	"lvl_NE_Arid":              "lvl_NE_Arid",
	"lvl_Arid_SkagGully_Part1": "lvl_Arid_SkagGully_Part1",
	"lvl_NE_Arid_SafeHouse":    "lvl_NE_Arid_SafeHouse",
	"lvl_PvP_Shipment":         "lvl_PvP_Shipment",
}

func getCurScnFromProps(roomName string, props map[any]any) string {
	if props != nil {
		if raw, ok := props["PlaylistId"]; ok {
			pid := asInt32(raw)
			if idx := asInt32(props["SceneIdx"]); idx > 0 {
				if scene, found := playlistSceneAt(pid, idx); found {
					return scene
				}
			}
			if scene, found := playlistFirstScene(pid); found {
				return scene
			}
			if scene, found := playlistIDSceneMap[pid]; found {
				return scene
			}
		}
	}
	for key, scene := range playlistSceneMap {
		if strings.Contains(roomName, key) {
			return scene
		}
	}
	if roomName != "" && !strings.HasPrefix(roomName, "room_") && !strings.HasPrefix(roomName, "town_") {
		return roomName
	}
	return "lvl_NE_Arid_Hub"
}

func (s *EnetServer) opCreateRoom(sess *ClientSession, opCode byte, params map[byte]any) {
	roomName := fmt.Sprintf("%v", params[ParamRoomName])
	if roomName == "" || roomName == "<nil>" {
		roomName = fmt.Sprintf("room_%d", rand.Intn(9000)+1000)
	}
	roomProps, _ := params[ParamRoomProps].(map[any]any)

	s.dropStaleRoomSessions(sess)
	if _, exists := s.rooms[roomName]; !exists {
		s.rooms[roomName] = newRoom(roomName, anyMapToAny(roomProps))
	}
	room := s.rooms[roomName]
	actorNr := room.AddPlayer(sess.userID)
	sess.roomName = roomName
	s.bumpMapGen(sess)
	s.activeGameSession[sess.userID] = sess

	log.Printf("[PHOTON] CreateRoom %s user=%s actorNr=%d", roomName, sess.userID, actorNr)
	s.sendHostJoinEvent(sess)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{
		ParamRoomName:  roomName,
		ParamPlayerNr:  actorNr,
		ParamActorList: actorList(room),
		ParamRoomProps: room.Properties,
	})
	s.sendJoinEvents(sess, room, actorNr)
	s.hostSyncPresenceRoom(sess, room)
}

func (s *EnetServer) opCancelJoin(sess *ClientSession, opCode byte, params map[byte]any) {
	log.Printf("[PHOTON] CancelJoin user=%s", sess.userID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}

func (s *EnetServer) opJoinRoom(sess *ClientSession, opCode byte, params map[byte]any) {
	roomName := fmt.Sprintf("%v", params[ParamRoomName])
	s.dropStaleRoomSessions(sess)
	if _, exists := s.rooms[roomName]; !exists {
		s.rooms[roomName] = newRoom(roomName, nil)
	}
	room := s.rooms[roomName]
	actorNr := room.AddPlayer(sess.userID)
	sess.roomName = roomName
	s.bumpMapGen(sess)
	s.activeGameSession[sess.userID] = sess
	delete(s.pendingSceneIdx, sess.dbAccountID)
	if team, ok := s.pendingLobbyTeam[sess.dbAccountID]; ok {
		if isTeamGameMode(asInt32(room.Properties["GameMode"])) {
			sess.teamID = team
		} else {
			delete(s.pendingLobbyTeam, sess.dbAccountID)
		}
	}
	delete(s.pendingGroupRoom, sess.userID)

	log.Printf("[PHOTON] JoinRoom %s user=%s actorNr=%d", roomName, sess.userID, actorNr)

	s.broadcastEvent(room, 255, map[byte]any{
		ParamPlayerNr:    actorNr,
		ParamPlayerProps: map[any]any{},
	}, sess, nil)

	advertisedHost := s.AdvertisedHost
	if advertisedHost == "" {
		advertisedHost = s.host
	}
	s.sendHostJoinEvent(sess)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{
		ParamPlayerNr:    actorNr,
		ParamRoomName:    roomName,
		ParamRoomProps:   room.Properties,
		ParamPlayerProps: map[any]any{},
		230:              fmt.Sprintf("%s:%d", advertisedHost, s.port),
	})
	s.sendJoinEvents(sess, room, actorNr)
	s.hostSyncPresenceRoom(sess, room)
}

func (s *EnetServer) opJoinRandom(sess *ClientSession, opCode byte, params map[byte]any) {
	roomProps, _ := params[ParamRoomProps].(map[any]any)
	log.Printf("[PHOTON] JoinRandom raw params: %v", params)
	log.Printf("[PHOTON] JoinRandom room props: %v", roomProps)

	playlistID := int32(10000)
	if roomProps != nil {
		if pid, ok := roomProps["PlaylistId"]; ok {
			playlistID = asInt32(pid)
		}
	}

	base := fmt.Sprintf("town_%d", playlistID)
	roomName := base
	info := playlistInfo(playlistID)
	isSharedTownHub := playlistID == 10000 || (info != nil && info.GameMode == "GameModeTown")
	if !isSharedTownHub {
		roomName = fmt.Sprintf("%s_%s", base, newRandomUID("i"))
	} else {
		for i := 2; ; i++ {
			room, ok := s.rooms[roomName]
			if !ok || len(room.Players)-1 < MaxPlayers {
				break
			}
			roomName = fmt.Sprintf("%s_%d", base, i)
		}
	}
	if _, exists := s.rooms[roomName]; !exists {
		room := newRoom(roomName, anyMapToAny(roomProps))
		room.Properties["PlaylistId"] = playlistID
		if gm := gameModeNumberForPlaylist(playlistID); gm != 0 {
			room.Properties["GameMode"] = gm
		}
		s.rooms[roomName] = room
		log.Printf("[PHOTON] JoinRandom created room %s user=%s playlistID=%d gameMode=%v", roomName, sess.userID, playlistID, room.Properties["GameMode"])
	}
	log.Printf("[PHOTON] JoinRandom redirect user=%s room=%s", sess.userID, roomName)
	advertisedHost := s.AdvertisedHost
	if advertisedHost == "" {
		advertisedHost = s.host
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{
		ParamRoomName: roomName,
		230:           fmt.Sprintf("%s:%d", advertisedHost, s.port),
	})
}

func (s *EnetServer) opLeave(sess *ClientSession, opCode byte, params map[byte]any) {
	log.Printf("[PHOTON] Leave user=%s room=%s", sess.userID, sess.roomName)
	s.removeFromRoom(sess)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}

func (s *EnetServer) opRaiseEvent(sess *ClientSession, opCode byte, params map[byte]any) {
	eventCode := byte(asInt32(params[ParamEventCode]))
	eventData := params[ParamEventData]
	if eventCode != 201 && eventCode != 206 && (eventCode != 200 || logVerbose) {
		log.Printf("[PHOTON] opRaiseEvent code=%d data_type=%T data=%v", eventCode, eventData, eventData)
	}
	var targets []int32
	if t, ok := params[ParamTargetActors]; ok {
		if arr, ok := t.([]int32); ok {
			targets = arr
		}
	}

	if sess.roomName == "" {
		return
	}
	room := s.rooms[sess.roomName]
	if room == nil {
		return
	}
	actorNr := room.ActorNrFor(sess.userID)

	rpcHandled := false
	if eventCode == 200 {
		if ht, ok := eventData.(map[any]any); ok {
			rpcHandled = s.handleHostRPC(sess, room, actorNr, ht)
		}
	}
	if rpcHandled {
		return
	}

	relayData := eventData
	if eventCode == 201 || eventCode == 206 {
		if !sess.streamsSeen {
			sess.streamsSeen = true
			log.Printf("[PHOTON] first view stream (ev %d) from actor=%d user=%s, client serialize loop is alive", eventCode, actorNr, sess.userID)
		}
		if room.streamDumps < 3 {
			room.streamDumps++
			log.Printf("[PHOTON] stream dump ev=%d actor=%d data=%#v", eventCode, actorNr, eventData)
		}
		if room.nonControllerDumps < 6 {
			if outerPeek, ok := eventData.(map[any]any); ok {
				for k, v := range outerPeek {
					if stream, ok := v.(map[any]any); ok {
						if vid := asInt32(stream[byte(0)]); vid != 0 && vid != controllerSyncViewID && vid != sessionInfoViewID {
							room.nonControllerDumps++
							log.Printf("[PHOTON] non-controller stream dump ev=%d actor=%d key=%#v viewID=%d data=%#v", eventCode, actorNr, k, vid, stream)
						}
					}
				}
			}
		}
		if outer, ok := eventData.(map[any]any); ok {
			filtered := map[any]any{}
			for k, v := range outer {
				if _, isByte := k.(byte); isByte {
					filtered[k] = v
					continue
				}
				stream, ok := v.(map[any]any)
				if !ok {
					filtered[k] = v
					continue
				}
				streamViewID := asInt32(stream[byte(0)])
				if streamViewID != controllerSyncViewID && streamViewID != sessionInfoViewID {
					filtered[k] = v
				}
				viewID := asInt32(stream[byte(0)])
				if viewID == controllerSyncViewID {
					items, ok := stream[byte(1)].([]any)
					if !ok {
						items = []any{stream[byte(1)]}
					}
					for _, item := range items {
						if frames, ok := item.([]byte); ok {
							s.applyInputFrames(room, sess, frames)
							continue
						}
					}
					s.applyRealPosition(room, sess, items)
				}
				if viewID == sessionInfoViewID {
					continue
				}
				if _, full := stream[byte(1)]; !full {
					continue
				}
				room.viewStreams[viewID] = stream
			}
			relayData = filtered
		}
	}

	out := map[byte]any{
		ParamEventData: relayData,
		ParamPlayerNr:  actorNr,
	}
	if eventCode == 201 || eventCode == 206 {
		targets = nil
	}
	s.broadcastEvent(room, eventCode, out, sess, targets)
}

func (s *EnetServer) opSetProps(sess *ClientSession, opCode byte, params map[byte]any) {
	if sess.roomName != "" {
		if room := s.rooms[sess.roomName]; room != nil {
			if props, ok := params[ParamRoomProps].(map[any]any); ok {
				for k, v := range props {
					room.Properties[k] = v
				}
			}
		}
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}

func (s *EnetServer) opGetProps(sess *ClientSession, opCode byte, params map[byte]any) {
	props := map[any]any{}
	if sess.roomName != "" {
		if room := s.rooms[sess.roomName]; room != nil {
			props = room.Properties
		}
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{ParamRoomProps: props})
}

func (s *EnetServer) sendHostJoinEvent(sess *ClientSession) {
	s.sendEventTo(sess, 255, map[byte]any{
		ParamPlayerNr:    HostActorNr,
		ParamPlayerProps: map[any]any{},
	})
}

func (s *EnetServer) sendJoinEvents(sess *ClientSession, room *Room, actorNr int32) {
	actorListArr := actorList(room)

	s.sendEventTo(sess, 255, map[byte]any{
		ParamPlayerNr:    actorNr,
		ParamPlayerProps: map[any]any{},
		ParamActorList:   IntArray{Items: actorListArr},
	})

	const curscnKey = "yawSkhXz0eqhCLhCiU88Og=="
	curScn := getCurScnFromProps(room.Name, room.Properties)
	log.Printf("[PHOTON] sendJoinEvents room=%s curScn=%s", room.Name, curScn)
	room.Properties["curScn"] = curScn
	room.Properties[curscnKey] = curScn
	room.Properties[byte(249)] = true

	s.broadcastEvent(room, 253, map[byte]any{
		ParamRedisNr: int32(0),
		ParamTargetActors: map[any]any{
			curscnKey: curScn,
			byte(249): true,
		},
	}, nil, nil)

	nowMS := int32(msNow())
	sessionViews := make([]int32, 6)
	for i := range sessionViews {
		sessionViews[i] = hostViewBase + 1 + int32(i)
	}
	ev202Session := map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): "Session",
			byte(4): IntArray{Items: sessionViews},
			byte(6): nowMS,
			byte(7): sessionViews[0],
		},
	}
	s.sendEventTo(sess, 202, ev202Session)

	s.broadcastEvent(room, EvGroupGameStarted, map[byte]any{ParamPlayerNr: HostActorNr}, nil, nil)

}

func (s *EnetServer) opChangeGroups(sess *ClientSession, opCode byte, params map[byte]any) {
	if sess.roomName == "" {
		return
	}
	room := s.rooms[sess.roomName]
	if room == nil {
		return
	}
	groups, _ := params[238].([]byte)
	if params[238] == nil || slices.Contains(groups, byte(1)) {
		s.hostSpawnStaticEntities(room)
	}
}

func (s *EnetServer) opKickPlayerFromGame(sess *ClientSession, opCode byte, params map[byte]any) {
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
	if sess.roomName == "" {
		return
	}
	room := s.rooms[sess.roomName]
	if room == nil {
		return
	}
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		return
	}
	targetCharID := int64(asInt32(arr[0]))

	var targetSess *ClientSession
	var targetActorNr int32
	for actorNr, userID := range room.Players {
		for _, candidate := range s.sessions {
			if candidate.userID == userID && candidate.selectedCharID == targetCharID {
				targetSess = candidate
				targetActorNr = actorNr
				break
			}
		}
		if targetSess != nil {
			break
		}
	}
	if targetSess == nil {
		log.Printf("[PHOTON] KickPlayerFromGame: charID=%d not found in room=%s", targetCharID, sess.roomName)
		return
	}

	s.evictFromRoom(targetSess, targetActorNr, room)
	log.Printf("[PHOTON] KickPlayerFromGame charID=%d actorNr=%d kicked by user=%s", targetCharID, targetActorNr, sess.userID)
}

func (s *EnetServer) evictFromRoom(targetSess *ClientSession, targetActorNr int32, room *Room) {
	s.sendEventTo(targetSess, EvKickedFromGame, map[byte]any{})
	wasMaster := room.IsMasterActor(targetActorNr)
	room.RemovePlayer(targetActorNr)
	if targetSess.avatarBase != 0 {
		s.hostBroadcastJournaled(room, 204, map[byte]any{
			ParamPlayerNr:  HostActorNr,
			ParamEventData: map[any]any{byte(0): targetSess.avatarBase},
		}, true)
		for v := targetSess.avatarBase; v <= targetSess.avatarBase+5; v++ {
			delete(room.viewStreams, v)
		}
	}
	s.broadcastEvent(room, 254, map[byte]any{ParamPlayerNr: targetActorNr}, nil, nil)
	if newMaster := room.LowestActorNr(); wasMaster && newMaster != 0 {
		s.broadcastEvent(room, EvGameOwnerChanged, map[byte]any{ParamPlayerNr: newMaster}, nil, nil)
		log.Printf("[PHOTON] owner evicted room=%s ownership passed to actorNr=%d", room.Name, newMaster)
	}
	s.scriptFire(room, "OnPlayerLeave", lua.LNumber(targetActorNr))
	if room.LowestActorNr() == 0 {
		room.scriptClose()
		delete(s.rooms, room.Name)
		log.Printf("[PHOTON] room=%s emptied, instance destroyed", room.Name)
	}
	targetSess.roomName = ""
	targetSess.avatarBase = 0
	targetSess.playerState = PlayerStateNone
	targetSess.hostChar = nil
	targetSess.gatedSeq = 0
	targetSess.streamsSeen = false
}

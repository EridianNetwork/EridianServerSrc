package photon

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"time"

	"eridian/internal/db"
)

type LobbyRecord struct {
	ID          int
	Owner       int64
	TotalSlots  int
	IsGroupGame bool
	SessionID   string
	GameMode    int32
	Playlist    int32
	Difficulty  int32
	Members     []*ClientSession
}

func (s *EnetServer) hostSyncPresenceRoom(sess *ClientSession, room *Room) {
	online := s.sessionOnline(sess.userID)
	if online == nil {
		return
	}
	playlistID, _ := room.Properties["PlaylistId"].(int32)
	online.playlistId = playlistID
	if realTownPlaylists[playlistID] {
		online.currentTown = playlistID
	} else {
		online.currentTown = -1
	}
}

func sessPresenceHT(sess *ClientSession) map[int]any {
	ht := map[int]any{
		0:  int32(sess.selectedCharID),
		1:  true,
		2:  int32(time.Now().Unix()),
		3:  sess.currentTown,
		4:  sess.charLevel,
		5:  sess.charExp,
		6:  sess.charCurrency,
		7:  sess.charClassType,
		8:  sess.charAvatarParts,
		9:  sess.charAvatarMats, 
		10: sess.charAvatarID,   
		11: sess.charName,
		13: sess.groupID,
		14: int32(sess.lobbyID),
		15: int32(0),       
		16: sess.playlistId,
	}
	addPvPPresenceFields(ht, sess.selectedCharID)
	return ht
}

func addPvPPresenceFields(ht map[int]any, charID int64) {
	if charID == 0 {
		return
	}
	st, err := db.GetPvPStats(charID)
	if err != nil {
		return
	}
	ht[17] = int32(st.Matches)
	ht[18] = int32(st.Wins)
	ht[19] = int32(st.Losses)
	ht[20] = int32(st.Kills)
	ht[21] = int32(st.Deaths)
	ht[22] = int32(st.Honor)
}

func serializeLobby(l *LobbyRecord) map[int]any {
	members := map[int]any{}
	for i, m := range l.Members {
		members[i] = sessPresenceHT(m)
	}
	return map[int]any{
		0: int32(l.ID),
		1: int32(l.Owner),
		2: int32(l.TotalSlots),
		3: l.IsGroupGame,
		4: l.SessionID,
		5: members,
		6: map[int]any{},
		7: l.GameMode,
		8: l.Playlist,
		9: l.Difficulty,
	}
}

func (s *EnetServer) broadcastToAll(eventCode byte, params map[byte]any, exclude *ClientSession) {
	evt := buildEvent(eventCode, params)
	for _, sess := range s.sessions {
		if sess == exclude || sess.appName != "Online" {
			continue
		}
		s.sendPacket(sess, buildCmdSendReliable(0, sess.nextSeq(), evt))
	}
}

func (s *EnetServer) lobbyBroadcast(l *LobbyRecord, eventCode byte, params map[byte]any, exclude *ClientSession) {
	for _, m := range l.Members {
		if m == exclude {
			continue
		}
		s.sendEventTo(m, eventCode, params)
	}
}

func removeMemberFromLobby(l *LobbyRecord, sess *ClientSession) {
	for i, m := range l.Members {
		if m == sess {
			l.Members = append(l.Members[:i], l.Members[i+1:]...)
			return
		}
	}
}

func (s *EnetServer) cleanLeaveLobby(sess *ClientSession) {
	lobby := s.lobbies[sess.lobbyID]
	if lobby == nil {
		sess.lobbyID = 0
		return
	}
	charID := int32(sess.selectedCharID)
	removeMemberFromLobby(lobby, sess)
	sess.lobbyID = 0

	if len(lobby.Members) == 0 {
		delete(s.lobbies, lobby.ID)
		return
	}
	s.lobbyBroadcast(lobby, 193, map[byte]any{0: charID}, nil)
	if lobby.Owner == int64(charID) {
		lobby.Owner = lobby.Members[0].selectedCharID
		s.lobbyBroadcast(lobby, 190, map[byte]any{0: int32(lobby.Owner)}, nil)
	}
}

func (s *EnetServer) opJoinLobby(sess *ClientSession, opCode byte, params map[byte]any) {
	log.Printf("[PHOTON] JoinLobby user=%s", sess.userID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
	s.sendEventTo(sess, 230, map[byte]any{222: map[any]any{}})
}

func (s *EnetServer) opLeaveLobby(sess *ClientSession, opCode byte, params map[byte]any) {
	if sess.lobbyID != 0 {
		s.cleanLeaveLobby(sess)
		s.sendOpResponse(sess, opCode, 0, map[byte]any{})
		return
	}
	log.Printf("[PHOTON] LeaveLobby user=%s", sess.userID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}

func (s *EnetServer) opStartLobbyGame(sess *ClientSession, opCode byte, params map[byte]any) {
	if lobby := s.lobbies[sess.lobbyID]; lobby == nil {
		if lobbyID, ok := s.pendingLobbyID[sess.dbAccountID]; ok {
			if owned := s.lobbies[lobbyID]; owned != nil {
				sess.lobbyID = owned.ID
			}
		} else if owned := s.findOwnedLobby(sess.selectedCharID); owned != nil {
			sess.lobbyID = owned.ID
		}
	}
	advertisedHost := s.AdvertisedHost
	if advertisedHost == "" {
		advertisedHost = s.host
	}

	if lobby := s.lobbies[sess.lobbyID]; lobby != nil && lobby.SessionID != "" {
		if room := s.rooms[lobby.SessionID]; room != nil {
			log.Printf("[PHOTON] StartLobbyGame user=%s lobbyID=%d already started, redirecting to room=%s",
				sess.userID, lobby.ID, lobby.SessionID)
			s.sendOpResponse(sess, opCode, 0, map[byte]any{
				ParamRoomName: lobby.SessionID,
				230:           fmt.Sprintf("%s:%d", advertisedHost, s.port),
			})
			return
		}
	}
	gameMode := asInt32(params[0])
	playlistID := asInt32(params[1])
	roomName := fmt.Sprintf("game_%d_%d", playlistID, msNow())
	room := newRoom(roomName, map[any]any{})
	room.Properties["GameMode"] = gameMode
	room.Properties["PlaylistId"] = playlistID
	if idx, ok := s.debugSceneIdx[playlistID]; ok {
		room.Properties["SceneIdx"] = idx
	}
	if pending, ok := s.pendingSceneIdx[sess.dbAccountID]; ok && pending.playlistID == playlistID {
		delete(s.pendingSceneIdx, sess.dbAccountID)
		room.Properties["SceneIdx"] = pending.sceneIdx
	}
	if diff, ok := s.pendingLobbyDifficulty[sess.dbAccountID]; ok {
		room.Properties["Difficulty"] = diff
		delete(s.pendingLobbyDifficulty, sess.dbAccountID)
	} else if lobby := s.lobbies[sess.lobbyID]; lobby != nil {
		room.Properties["Difficulty"] = lobby.Difficulty
	}
	s.rooms[roomName] = room
	log.Printf("[PHOTON] StartLobbyGame user=%s room=%s mode=%d playlist=%d scene=%s",
		sess.userID, roomName, gameMode, playlistID, getCurScnFromProps(roomName, room.Properties))
	s.sendOpResponse(sess, opCode, 0, map[byte]any{
		ParamRoomName: roomName,
		230:           fmt.Sprintf("%s:%d", advertisedHost, s.port),
	})

	if lobby := s.lobbies[sess.lobbyID]; lobby != nil {
		lobby.SessionID = roomName
		s.assignLobbyTeams(lobby)
		for _, m := range lobby.Members {
			if m == sess {
				continue
			}
			s.pendingGroupRoom[m.userID] = roomName
			s.sendEventTo(m, EvLobbyGameStarted, map[byte]any{0: int32(lobby.ID)})
		}
	}
}

func isTeamGameMode(gameMode int32) bool {
	switch gameMode {
	case 5, 7, 8:
		return true
	default:
		return false
	}
}

func wireTeamID(team byte, isTeamMode bool) byte {
	if !isTeamMode {
		return 0
	}
	return team + 1
}

func isPvPCombatMode(gameMode int32) bool {
	switch gameMode {
	case 4, 5, 7:
		return true
	default:
		return false
	}
}

func (s *EnetServer) assignLobbyTeams(lobby *LobbyRecord) {
	if !isTeamGameMode(lobby.GameMode) {
		return
	}
	byGroup := map[int32][]*ClientSession{}
	var clusters [][]*ClientSession
	for _, m := range lobby.Members {
		if m.groupID == 0 {
			clusters = append(clusters, []*ClientSession{m})
			continue
		}
		byGroup[m.groupID] = append(byGroup[m.groupID], m)
	}
	var groupClusters [][]*ClientSession
	for _, members := range byGroup {
		groupClusters = append(groupClusters, members)
	}

	sort.SliceStable(groupClusters, func(i, j int) bool { return len(groupClusters[i]) > len(groupClusters[j]) })
	clusters = append(groupClusters, clusters...)

	var teamSize [2]int
	for _, cluster := range clusters {
		team := byte(0)
		if teamSize[1] < teamSize[0] {
			team = 1
		}
		for _, m := range cluster {
			m.teamID = team
			s.pendingLobbyTeam[m.dbAccountID] = team
		}
		teamSize[team] += len(cluster)
	}
	log.Printf("[PHOTON] assigned teams lobbyID=%d mode=%d members=%d teamA=%d teamB=%d",
		lobby.ID, lobby.GameMode, len(lobby.Members), teamSize[0], teamSize[1])
}

func (s *EnetServer) opAcceptLobbyGameInvitation(sess *ClientSession, opCode byte, params map[byte]any) {
	roomName := s.pendingGroupRoom[sess.userID]
	if roomName == "" {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	delete(s.pendingGroupRoom, sess.userID)
	room := s.rooms[roomName]
	playlistID := int32(0)
	if room != nil {
		playlistID = asInt32(room.Properties["PlaylistId"])
	}
	log.Printf("[PHOTON] AcceptLobbyGameInvitation user=%s room=%s", sess.userID, roomName)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: roomName, 1: playlistID})
}

func (s *EnetServer) opCreateLobby(sess *ClientSession, opCode byte, params map[byte]any) {
	log.Printf("[PHOTON] CreateLobby raw params: %v", params)
	arr := toSlice(params[112])
	isGroupGame := false
	if len(arr) > 0 {
		if b, ok := arr[0].(bool); ok {
			isGroupGame = b
		}
	}
	s.lobbyCounter++
	lobby := &LobbyRecord{
		ID:    s.lobbyCounter,
		Owner: sess.selectedCharID,
		TotalSlots:  2,
		IsGroupGame: isGroupGame,
		Members:     []*ClientSession{sess},
	}
	s.lobbies[lobby.ID] = lobby
	sess.lobbyID = lobby.ID
	s.pendingLobbyID[sess.dbAccountID] = lobby.ID

	lobbyHT := serializeLobby(lobby)
	log.Printf("[PHOTON] CreateLobby user=%s lobbyID=%d selectedCharID=%d dbAccountID=%d", sess.userID, lobby.ID, sess.selectedCharID, sess.dbAccountID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: lobbyHT})
}

func (s *EnetServer) opOpenLobby(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	gameMode := int32(0)
	playlist := int32(0)
	difficulty := int32(0)
	if len(arr) > 0 {
		gameMode = asInt32(arr[0])
	}
	if len(arr) > 1 {
		playlist = asInt32(arr[1])
	}
	if len(arr) > 2 {
		difficulty = asInt32(arr[2])
	}
	lobby := s.lobbies[sess.lobbyID]
	if lobby == nil || lobby.Owner != sess.selectedCharID {
		if owned := s.findOwnedLobby(sess.selectedCharID); owned != nil {
			lobby = owned
			sess.lobbyID = lobby.ID
		}
	}
	if lobby == nil {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	lobby.GameMode = gameMode
	lobby.Playlist = playlist
	lobby.Difficulty = difficulty
	s.pendingLobbyDifficulty[sess.dbAccountID] = difficulty
	if info := playlistInfo(playlist); info != nil {
		slots := info.MinPlayerCountToStart
		if slots <= 0 {
			slots = info.MaxPlayerCount
		}
		if slots > 0 {
			lobby.TotalSlots = slots
		}
	}


	if gameMode >= 4 && gameMode != 6 {
		if other := s.findOpenPvPLobby(lobby, gameMode, playlist, difficulty); other != nil {
			delete(s.lobbies, lobby.ID)
			other.Members = append(other.Members, sess)
			sess.lobbyID = other.ID
			s.pendingLobbyID[sess.dbAccountID] = other.ID
			log.Printf("[PHOTON] OpenLobby user=%s merged solo lobbyID=%d into lobbyID=%d (%d/%d)",
				sess.userID, lobby.ID, other.ID, len(other.Members), other.TotalSlots)
			s.lobbyBroadcast(other, 194, map[byte]any{0: sessPresenceHT(sess)}, sess)
			s.sendOpResponse(sess, opCode, 0, map[byte]any{0: serializeLobby(other)})
			return
		}
	}

	s.pendingLobbyID[sess.dbAccountID] = lobby.ID
	lobbyHT := serializeLobby(lobby)
	log.Printf("[PHOTON] OpenLobby user=%s lobbyID=%d mode=%d playlist=%d difficulty=%d",
		sess.userID, lobby.ID, gameMode, playlist, difficulty)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: lobbyHT})

}

func (s *EnetServer) findOpenPvPLobby(mine *LobbyRecord, gameMode, playlist, difficulty int32) *LobbyRecord {
	var best *LobbyRecord
	for id, l := range s.lobbies {
		if id == mine.ID || l.GameMode != gameMode || l.Playlist != playlist || l.Difficulty != difficulty {
			continue
		}
		if len(l.Members) >= l.TotalSlots {
			continue
		}
		if best == nil || l.ID < best.ID {
			best = l
		}
	}
	return best
}

func (s *EnetServer) findOwnedLobby(charID int64) *LobbyRecord {
	var best *LobbyRecord
	for _, l := range s.lobbies {
		if l.Owner != charID {
			continue
		}
		if best == nil || l.ID > best.ID {
			best = l
		}
	}
	return best
}

func (s *EnetServer) opGetLobby(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	lobbyID := sess.lobbyID
	if len(arr) > 0 {
		lobbyID = int(asInt32(arr[0]))
	}
	lobby := s.lobbies[lobbyID]
	if lobby == nil {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: serializeLobby(lobby)})
}

func (s *EnetServer) opCloseLobby(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	lobbyID := sess.lobbyID
	if len(arr) > 0 {
		lobbyID = int(asInt32(arr[0]))
	}
	lobby := s.lobbies[lobbyID]
	if lobby == nil {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{})
		return
	}
	log.Printf("[PHOTON] CloseLobby user=%s lobbyID=%d", sess.userID, lobbyID)
	s.lobbyBroadcast(lobby, 191, map[byte]any{}, nil)
	for _, m := range lobby.Members {
		m.lobbyID = 0
	}
	delete(s.lobbies, lobby.ID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}


func (s *EnetServer) opInviteToLobby(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	if len(arr) == 0 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	targetCharID := int64(asInt32(arr[0]))

	lobby := s.lobbies[sess.lobbyID]
	if lobby == nil {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	if len(lobby.Members) >= lobby.TotalSlots {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	for _, m := range lobby.Members {
		if m.selectedCharID == targetCharID {
			s.sendOpResponse(sess, opCode, -1, map[byte]any{}) // already in the lobby
			return
		}
	}
	if _, err := db.GetCharacterByID(targetCharID); err != nil {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}

	if _, err := s.pushMessage(sess.selectedCharID, targetCharID, "lobby_invite", strconv.Itoa(lobby.ID)); err != nil {
		log.Printf("[PHOTON] InviteToLobby error: %v", err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	log.Printf("[PHOTON] InviteToLobby user=%s lobbyID=%d target=%d", sess.userID, lobby.ID, targetCharID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}

func (s *EnetServer) resolveLobbyInvite(sess *ClientSession, opCode byte, params map[byte]any) (*db.Message, *LobbyRecord) {
	arr := toSlice(params[112])
	if len(arr) == 0 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return nil, nil
	}
	msgID := int64(asInt32(arr[0]))
	msg, err := db.GetMessageByID(msgID)
	if err != nil || msg == nil || msg.Type != "lobby_invite" || msg.ReceiverID != sess.selectedCharID {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return nil, nil
	}
	lobbyID, err := strconv.Atoi(msg.Payload)
	if err != nil {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return nil, nil
	}
	lobby := s.lobbies[lobbyID]
	if lobby == nil {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return nil, nil
	}
	return msg, lobby
}

func (s *EnetServer) opAcceptLobbyInvite(sess *ClientSession, opCode byte, params map[byte]any) {
	msg, lobby := s.resolveLobbyInvite(sess, opCode, params)
	if msg == nil {
		return
	}
	if len(lobby.Members) >= lobby.TotalSlots {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	lobby.Members = append(lobby.Members, sess)
	sess.lobbyID = lobby.ID
	db.DeleteMessage(msg.MessageID)
	log.Printf("[PHOTON] AcceptLobbyInvite user=%s lobbyID=%d", sess.userID, lobby.ID)
	// event 194 = PlayerJoinLobby, carries the new member's Presence
	s.lobbyBroadcast(lobby, 194, map[byte]any{0: sessPresenceHT(sess)}, sess)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: serializeLobby(lobby)})
}

func (s *EnetServer) opRejectLobbyInvite(sess *ClientSession, opCode byte, params map[byte]any) {
	msg, lobby := s.resolveLobbyInvite(sess, opCode, params)
	if msg == nil {
		return
	}
	db.DeleteMessage(msg.MessageID)
	log.Printf("[PHOTON] RejectLobbyInvite user=%s lobbyID=%d", sess.userID, lobby.ID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}

func (s *EnetServer) opKickFromLobby(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	if len(arr) == 0 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	targetCharID := int64(asInt32(arr[0]))

	lobby := s.lobbies[sess.lobbyID]
	if lobby == nil || lobby.Owner != sess.selectedCharID || targetCharID == sess.selectedCharID {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	var target *ClientSession
	for _, m := range lobby.Members {
		if m.selectedCharID == targetCharID {
			target = m
			break
		}
	}
	if target == nil {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}

	s.lobbyBroadcast(lobby, EvPlayerLeftLobby, map[byte]any{0: int32(targetCharID)}, nil)
	removeMemberFromLobby(lobby, target)
	target.lobbyID = 0
	log.Printf("[PHOTON] KickFromLobby user=%s lobbyID=%d target=%d", sess.userID, lobby.ID, targetCharID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(targetCharID)})
}

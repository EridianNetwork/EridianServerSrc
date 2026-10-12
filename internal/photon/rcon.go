package photon

import (
	"encoding/json"
	"eridian/internal/db"
	"eridian/internal/domain"
	"eridian/pkg/config"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"
)

func (s *EnetServer) StartRconServer(host string, port int, password string) {
	if port <= 0 {
		return
	}
	s.rconPassword = password
	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", s.rconCORS(s.handleRconLogin))
	mux.HandleFunc("/api/summary", s.rconAuthed(s.handleRconSummary))
	mux.HandleFunc("/api/rooms", s.rconAuthed(s.handleRconRooms))
	mux.HandleFunc("/api/sessions", s.rconAuthed(s.handleRconSessions))
	mux.HandleFunc("/api/config", s.rconAuthed(s.handleRconConfig))
	mux.HandleFunc("/api/accounts", s.rconAuthed(s.handleRconAccounts))
	mux.HandleFunc("/api/accounts/create", s.rconAuthed(s.handleRconCreateAccount))
	mux.HandleFunc("/api/accounts/flags", s.rconAuthed(s.handleRconAccountFlags))
	mux.HandleFunc("/api/accounts/delete", s.rconAuthed(s.handleRconDeleteAccount))
	mux.HandleFunc("/api/characters", s.rconAuthed(s.handleRconCharacters))
	mux.HandleFunc("/api/kick", s.rconAuthed(s.handleRconKick))
	mux.HandleFunc("/api/bans", s.rconAuthed(s.handleRconBans))
	mux.HandleFunc("/api/bans/add", s.rconAuthed(s.handleRconBanAdd))
	mux.HandleFunc("/api/bans/remove", s.rconAuthed(s.handleRconBanRemove))
	mux.HandleFunc("/api/announce", s.rconAuthed(s.handleRconAnnounce))
	mux.HandleFunc("/api/say", s.rconAuthed(s.handleRconSay))
	mux.HandleFunc("/api/reload", s.rconAuthed(s.handleRconReload))
	if password == "" {
		log.Printf("[RCON] WARNING: rcon.password is empty in config, the admin API has NO login gate")
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	go func() {
		log.Printf("[RCON] admin API on http://%s (see ../rcon)", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("[RCON] server error: %v", err)
		}
	}()
}
func (s *EnetServer) rconCORS(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-RCON-Password")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		h(w, r)
	}
}
func (s *EnetServer) rconAuthed(h http.HandlerFunc) http.HandlerFunc {
	return s.rconCORS(func(w http.ResponseWriter, r *http.Request) {
		if s.rconPassword != "" && r.Header.Get("X-RCON-Password") != s.rconPassword {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h(w, r)
	})
}
func (s *EnetServer) handleRconLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	ok := s.rconPassword == "" || body.Password == s.rconPassword
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
	}
	writeJSON(w, map[string]bool{"ok": ok})
}
func (s *EnetServer) runOnDispatch(fn func()) {
	done := make(chan struct{})
	s.rconActions <- func() {
		fn()
		close(done)
	}
	<-done
}
func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[RCON] response encode error: %v", err)
	}
}

type rconSummaryPayload struct {
	Rooms         int `json:"rooms"`
	Sessions      int `json:"sessions"`
	PlayersInRoom int `json:"playersInRoom"`
	Npcs          int `json:"npcs"`
}

func (s *EnetServer) handleRconSummary(w http.ResponseWriter, r *http.Request) {
	var payload rconSummaryPayload
	s.runOnDispatch(func() {
		players, npcs := 0, 0
		for _, room := range s.rooms {
			players += rconRealPlayerCount(room)
			npcs += len(room.npcs)
		}
		payload = rconSummaryPayload{
			Rooms:         len(s.rooms),
			Sessions:      len(s.rconFilteredSessions()),
			PlayersInRoom: players,
			Npcs:          npcs,
		}
	})
	writeJSON(w, payload)
}
func rconRealPlayerCount(room *Room) int {
	n := len(room.Players)
	if _, ok := room.Players[HostActorNr]; ok {
		n--
	}
	return n
}
func (s *EnetServer) rconFilteredSessions() []*ClientSession {
	latest := map[int64]*ClientSession{}
	var noAccount []*ClientSession
	for _, sess := range s.sessions {
		if sess.appName == "Online" {
			continue
		}
		if sess.dbAccountID == 0 {
			noAccount = append(noAccount, sess)
			continue
		}
		if cur, ok := latest[sess.dbAccountID]; !ok || sess.lastRecv.After(cur.lastRecv) {
			latest[sess.dbAccountID] = sess
		}
	}
	out := make([]*ClientSession, 0, len(latest)+len(noAccount))
	for _, sess := range latest {
		out = append(out, sess)
	}
	return append(out, noAccount...)
}

type rconPlayer struct {
	ActorNr     int32  `json:"actorNr"`
	UserID      string `json:"userId"`
	Username    string `json:"username,omitempty"`
	DbAccountID int64  `json:"-"`
	CharName    string `json:"charName,omitempty"`
	CharLevel   int32  `json:"charLevel,omitempty"`
	ClassType   int32  `json:"classType,omitempty"`
	AvatarBase  int32  `json:"avatarBase,omitempty"`
}
type rconRoom struct {
	Name       string       `json:"name"`
	GameMode   string       `json:"gameMode"`
	Level      string       `json:"level"`
	Difficulty int32        `json:"difficulty"`
	PlaylistID int32        `json:"playlistId"`
	CreatedAt  time.Time    `json:"createdAt"`
	NpcCount   int          `json:"npcCount"`
	NpcAlive   int          `json:"npcAlive"`
	Players    []rconPlayer `json:"players"`
}

func (s *EnetServer) handleRconRooms(w http.ResponseWriter, r *http.Request) {
	out := make([]rconRoom, 0)
	s.runOnDispatch(func() {
		out = s.rconRoomsSnapshot()
	})
	cache := map[int64]string{}
	for i := range out {
		for j := range out[i].Players {
			out[i].Players[j].Username = rconResolveUsername(cache, out[i].Players[j].DbAccountID)
		}
	}
	writeJSON(w, out)
}
func rconResolveUsername(cache map[int64]string, dbAccountID int64) string {
	if dbAccountID == 0 {
		return ""
	}
	if u, ok := cache[dbAccountID]; ok {
		return u
	}
	u := ""
	if acc, err := db.GetAccountByID(dbAccountID); err == nil && acc != nil {
		u = acc.Username
	}
	cache[dbAccountID] = u
	return u
}
func (s *EnetServer) rconRoomsSnapshot() []rconRoom {
	out := make([]rconRoom, 0, len(s.rooms))
	for _, room := range s.rooms {
		npcAlive := 0
		for i := range room.npcs {
			if room.npcs[i].hp > 0 {
				npcAlive++
			}
		}
		rr := rconRoom{
			Name:       room.Name,
			GameMode:   gamemodeName(room),
			Level:      getCurScnFromProps(room.Name, room.Properties),
			Difficulty: asInt32(room.Properties["Difficulty"]),
			PlaylistID: asInt32(room.Properties["PlaylistId"]),
			CreatedAt:  room.CreatedAt,
			NpcCount:   len(room.npcs),
			NpcAlive:   npcAlive,
			Players:    make([]rconPlayer, 0, len(room.Players)),
		}
		for actorNr, uid := range room.Players {
			if actorNr == HostActorNr {
				continue
			}
			p := rconPlayer{ActorNr: actorNr, UserID: uid}
			if sess := s.sessionInRoom(uid, room.Name); sess != nil {
				p.AvatarBase = sess.avatarBase
				p.DbAccountID = sess.dbAccountID
				if sess.hostChar != nil {
					p.CharName = sess.hostChar.Name
					p.CharLevel = int32(sess.hostChar.Level)
					p.ClassType = int32(sess.hostChar.ClassType)
				}
			}
			rr.Players = append(rr.Players, p)
		}
		out = append(out, rr)
	}
	return out
}

type rconSession struct {
	Addr        string    `json:"addr"`
	AppName     string    `json:"appName"`
	UserID      string    `json:"userId"`
	Username    string    `json:"username,omitempty"`
	DbAccountID int64     `json:"dbAccountId"`
	RoomName    string    `json:"roomName,omitempty"`
	CharName    string    `json:"charName,omitempty"`
	CharLevel   int32     `json:"charLevel,omitempty"`
	ConnectedAt time.Time `json:"connectedAt"`
	LastRecv    time.Time `json:"lastRecv"`
	IdleSeconds int       `json:"idleSeconds"`
}

func (s *EnetServer) handleRconSessions(w http.ResponseWriter, r *http.Request) {
	out := make([]rconSession, 0)
	s.runOnDispatch(func() {
		now := time.Now()
		for _, sess := range s.rconFilteredSessions() {
			rs := rconSession{
				Addr:        sess.addr,
				AppName:     sess.appName,
				UserID:      sess.userID,
				DbAccountID: sess.dbAccountID,
				RoomName:    sess.roomName,
				ConnectedAt: sess.connectedAt,
				LastRecv:    sess.lastRecv,
				IdleSeconds: int(now.Sub(sess.lastRecv).Seconds()),
			}
			if sess.hostChar != nil {
				rs.CharName = sess.hostChar.Name
				rs.CharLevel = int32(sess.hostChar.Level)
			}
			out = append(out, rs)
		}
	})
	cache := map[int64]string{}
	for i := range out {
		out[i].Username = rconResolveUsername(cache, out[i].DbAccountID)
	}
	writeJSON(w, out)
}
func paginationParams(r *http.Request) (search string, limit, offset int) {
	search = r.URL.Query().Get("q")
	limit = 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = v
	}
	if limit > 200 {
		limit = 200
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v > 0 {
		offset = v
	}
	return search, limit, offset
}

type rconPage[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

func (s *EnetServer) handleRconConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]bool{"newAccountCanPlay": config.LiveNewAccountCanPlay()})
	case http.MethodPost:
		var body struct {
			NewAccountCanPlay bool `json:"newAccountCanPlay"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if err := config.SetNewAccountCanPlay(body.NewAccountCanPlay); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("[RCON] game.new_account_can_play set to %v", body.NewAccountCanPlay)
		writeJSON(w, map[string]bool{"ok": true, "newAccountCanPlay": body.NewAccountCanPlay})
	default:
		http.Error(w, "GET or POST only", http.StatusMethodNotAllowed)
	}
}
func (s *EnetServer) handleRconAccounts(w http.ResponseWriter, r *http.Request) {
	search, limit, offset := paginationParams(r)
	accounts, total, err := db.ListAccounts(search, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, rconPage[*domain.Account]{Items: accounts, Total: total})
}
func (s *EnetServer) handleRconCharacters(w http.ResponseWriter, r *http.Request) {
	search, limit, offset := paginationParams(r)
	chars, total, err := db.ListCharacters(search, limit, offset, r.URL.Query().Get("sort"), r.URL.Query().Get("dir") == "desc")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, rconPage[*db.CharacterSummary]{Items: chars, Total: total})
}
func (s *EnetServer) handleRconCreateAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Email    string `json:"email"`
		IsTester bool   `json:"isTester"`
		CanPlay  bool   `json:"canPlay"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" || body.Password == "" {
		http.Error(w, "missing username/password", http.StatusBadRequest)
		return
	}
	exists, err := db.AccountExists(body.Username)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if exists {
		http.Error(w, "username already exists", http.StatusConflict)
		return
	}
	id, err := db.CreateAccountWithFlags(body.Username, body.Password, body.Email, body.IsTester, body.CanPlay)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("[RCON] created account id=%d username=%q isTester=%v canPlay=%v", id, body.Username, body.IsTester, body.CanPlay)
	writeJSON(w, map[string]int64{"accountId": id})
}
func (s *EnetServer) handleRconAccountFlags(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		AccountID int64 `json:"accountId"`
		IsTester  bool  `json:"isTester"`
		CanPlay   bool  `json:"canPlay"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AccountID == 0 {
		http.Error(w, "missing accountId", http.StatusBadRequest)
		return
	}
	if err := db.SetAccountTester(body.AccountID, body.IsTester); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := db.SetAccountCanPlay(body.AccountID, body.CanPlay); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("[RCON] account id=%d flags set isTester=%v canPlay=%v", body.AccountID, body.IsTester, body.CanPlay)
	writeJSON(w, map[string]bool{"ok": true})
}
func (s *EnetServer) handleRconDeleteAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		AccountID int64 `json:"accountId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AccountID == 0 {
		http.Error(w, "missing accountId", http.StatusBadRequest)
		return
	}
	s.runOnDispatch(func() {
		for _, sess := range s.sessionsByAccount(body.AccountID) {
			if sess.roomName == "" {
				continue
			}
			room := s.rooms[sess.roomName]
			if room == nil {
				continue
			}
			for actorNr, uid := range room.Players {
				if uid == sess.userID {
					s.hostKickPlayer(sess, room, actorNr, "Your account has been deleted", true)
					break
				}
			}
		}
	})
	if err := db.DeleteAccountCascade(body.AccountID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("[RCON] deleted account id=%d (characters/inventory/quest progress/etc. cascaded)", body.AccountID)
	writeJSON(w, map[string]bool{"ok": true})
}
func (s *EnetServer) hostKickPlayer(sess *ClientSession, room *Room, actorNr int32, message string, forceDisconnect bool) {
	s.hostSendRPCTo(sess, sessionInfoViewID, "RPC_RequestDisplayUIMessage", []any{false, float32(0), float32(6), message, byte(0)})
	alreadyHome := gamemodeName(room) == "GameModeTown" || gamemodeName(room) == "GamemodeRV"
	if !forceDisconnect && !alreadyHome {
		s.hostSendRPCTo(sess, sessionInfoViewID, "RPC_SendPlayerToTown", []any{})
	}
	s.evictFromRoom(sess, actorNr, room)
	if forceDisconnect || alreadyHome {
		s.handleDisconnect(sess)
	}
}
func (s *EnetServer) handleRconKick(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Room    string `json:"room"`
		ActorNr int32  `json:"actorNr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var kickErr string
	s.runOnDispatch(func() {
		room := s.rooms[body.Room]
		if room == nil {
			kickErr = "room not found"
			return
		}
		uid, ok := room.Players[body.ActorNr]
		if !ok {
			kickErr = "actorNr not in room"
			return
		}
		sess := s.sessionInRoom(uid, room.Name)
		if sess == nil {
			kickErr = "session not found"
			return
		}
		s.hostKickPlayer(sess, room, body.ActorNr, "You have been kicked", false)
		log.Printf("[RCON] kicked userId=%s actorNr=%d room=%s", uid, body.ActorNr, body.Room)
	})
	if kickErr != "" {
		http.Error(w, kickErr, http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
func (s *EnetServer) handleRconBans(w http.ResponseWriter, r *http.Request) {
	list, err := db.ListBannedIPs()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, list)
}
func (s *EnetServer) handleRconBanAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		IP        string `json:"ip"`
		AccountID int64  `json:"accountId"`
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var kicked int
	s.runOnDispatch(func() {
		if body.IP == "" && body.AccountID != 0 {
			for _, sess := range s.sessionsByAccount(body.AccountID) {
				if sess.udpAddr != nil {
					body.IP = sess.udpAddr.IP.String()
					break
				}
			}
		}
		if net.ParseIP(body.IP) == nil {
			body.IP = ""
			return
		}
		body.IP = net.ParseIP(body.IP).String()
		var victims []*ClientSession
		for _, sess := range s.sessions {
			if sess.udpAddr != nil && sess.udpAddr.IP.String() == body.IP {
				victims = append(victims, sess)
			}
		}
		for _, sess := range victims {
			if room := s.rooms[sess.roomName]; room != nil {
				for actorNr, uid := range room.Players {
					if uid == sess.userID {
						s.hostKickPlayer(sess, room, actorNr, "You have been banned", true)
						break
					}
				}
			}
			if s.sessions[sess.addr] == sess {
				s.handleDisconnect(sess)
			}
		}
		kicked = len(victims)
	})
	if body.IP == "" {
		http.Error(w, "need a valid ip, or an accountId with a live session", http.StatusBadRequest)
		return
	}
	if err := db.BanIP(body.IP, body.Reason); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("[RCON] banned ip=%s reason=%q (%d live session(s) dropped)", body.IP, body.Reason, kicked)
	writeJSON(w, map[string]any{"ok": true, "ip": body.IP, "kicked": kicked})
}
func (s *EnetServer) handleRconBanRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.IP == "" {
		http.Error(w, "missing ip", http.StatusBadRequest)
		return
	}
	if err := db.UnbanIP(body.IP); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("[RCON] unbanned ip=%s", body.IP)
	writeJSON(w, map[string]bool{"ok": true})
}
func (s *EnetServer) handleRconAnnounce(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Text == "" {
		http.Error(w, "missing text", http.StatusBadRequest)
		return
	}
	s.runOnDispatch(func() {
		const announcementTypeBroadcast = int32(1)
		s.broadcastToAll(EvAnnouncements, map[byte]any{
			0: map[int]any{0: map[int]any{0: body.Text, 1: announcementTypeBroadcast}},
		}, nil)
		log.Printf("[RCON] announce: %q", body.Text)
	})
	writeJSON(w, map[string]bool{"ok": true})
}
func (s *EnetServer) handleRconSay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Text == "" {
		http.Error(w, "missing text", http.StatusBadRequest)
		return
	}
	var roomCount int
	s.runOnDispatch(func() {
		for _, room := range s.rooms {
			s.hostSendUIMessage(room, body.Text, 5)
		}
		roomCount = len(s.rooms)
	})
	log.Printf("[RCON] say: %q (%d room(s))", body.Text, roomCount)
	writeJSON(w, map[string]bool{"ok": true})
}
func (s *EnetServer) handleRconReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	s.runOnDispatch(func() {
		reloadLevelSpawns()
		reloadLevelClusterDefs()
		reloadLevelCovers()
		log.Printf("[RCON] reloaded level_spawns/level_clusters/level_covers caches (new rooms only)")
	})
	writeJSON(w, map[string]bool{"ok": true})
}

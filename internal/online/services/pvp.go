package services

import (
	"fmt"
	"log"
	"sync"
	"time"

	"eridian/internal/online"
)

// PvPRoom represents a PvP room.
type PvPRoom struct {
	ID         int64        `json:"id"`
	Name       string       `json:"name"`
	OwnerID    int64        `json:"owner_id"`
	OwnerName  string       `json:"owner_name"`
	Password   string       `json:"-"`
	IsPrivate  bool         `json:"is_private"`
	MaxPlayers int          `json:"max_players"`
	Players    []*PvPPlayer `json:"players"`
	Status     string       `json:"status"` // "waiting", "in_game", "finished"
	GameMode   string       `json:"game_mode"`
	MapName    string       `json:"map_name"`
	CreatedAt  string       `json:"created_at"`
}

// PvPPlayer represents a player in a PvP room.
type PvPPlayer struct {
	AccountID int64  `json:"account_id"`
	CharID    int64  `json:"char_id"`
	Username  string `json:"username"`
	Team      int    `json:"team"` // 0 = unassigned, 1 = team A, 2 = team B
	IsReady   bool   `json:"is_ready"`
}

// PvPManager manages the in-memory PvP rooms.
type PvPManager struct {
	mu     sync.RWMutex
	rooms  map[int64]*PvPRoom
	nextID int64
}

var pvpMgr = &PvPManager{
	rooms:  make(map[int64]*PvPRoom),
	nextID: 1,
}

// RegisterPvPService registers the PvP handlers.
func RegisterPvPService(s *online.OnlineServer) {
	s.Handle(online.ServicePvPCreate, handlePvPCreate)
	s.Handle(online.ServicePvPJoin, handlePvPJoin)
	s.Handle(online.ServicePvPLeave, handlePvPLeave)
	s.Handle(online.ServicePvPList, handlePvPList)
	s.Handle(online.ServicePvPFind, handlePvPFind)
}

func handlePvPCreate(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	if c.AccountID == 0 {
		return nil, fmt.Errorf("not_authenticated")
	}

	name, _ := data["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("name_required")
	}

	maxPlayers := 8
	if v, ok := toInt(data["max_players"]); ok && v > 0 && v <= 16 {
		maxPlayers = v
	}

	password, _ := data["password"].(string)
	isPrivate := password != ""

	gameMode, _ := data["game_mode"].(string)
	if gameMode == "" {
		gameMode = "deathmatch"
	}
	mapName, _ := data["map_name"].(string)

	pvpMgr.mu.Lock()
	defer pvpMgr.mu.Unlock()

	id := pvpMgr.nextID
	pvpMgr.nextID++

	room := &PvPRoom{
		ID:         id,
		Name:       name,
		OwnerID:    c.AccountID,
		OwnerName:  c.Username,
		Password:   password,
		IsPrivate:  isPrivate,
		MaxPlayers: maxPlayers,
		Players: []*PvPPlayer{
			{AccountID: c.AccountID, CharID: c.CharID, Username: c.Username, Team: 1, IsReady: true},
		},
		Status:    "waiting",
		GameMode:  gameMode,
		MapName:   mapName,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	pvpMgr.rooms[id] = room

	log.Printf("[PVP] room created: id=%d name=%q mode=%s owner=%d", id, name, gameMode, c.AccountID)
	return map[string]any{"room": room}, nil
}

func handlePvPJoin(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	if c.AccountID == 0 {
		return nil, fmt.Errorf("not_authenticated")
	}

	roomID, ok := toInt64(data["room_id"])
	if !ok {
		return nil, fmt.Errorf("room_id_required")
	}

	pvpMgr.mu.Lock()
	defer pvpMgr.mu.Unlock()

	room, exists := pvpMgr.rooms[roomID]
	if !exists {
		return nil, fmt.Errorf("room_not_found")
	}
	if room.Status != "waiting" {
		return nil, fmt.Errorf("game_already_started")
	}
	if len(room.Players) >= room.MaxPlayers {
		return nil, fmt.Errorf("room_full")
	}

	if room.IsPrivate {
		pwd, _ := data["password"].(string)
		if pwd != room.Password {
			return nil, fmt.Errorf("wrong_password")
		}
	}

	for _, p := range room.Players {
		if p.AccountID == c.AccountID {
			return map[string]any{"room": room}, nil
		}
	}

	// Assign team to balance
	teamA := 0
	teamB := 0
	for _, p := range room.Players {
		if p.Team == 1 {
			teamA++
		} else if p.Team == 2 {
			teamB++
		}
	}
	team := 2
	if teamA <= teamB {
		team = 1
	}

	room.Players = append(room.Players, &PvPPlayer{
		AccountID: c.AccountID,
		CharID:    c.CharID,
		Username:  c.Username,
		Team:      team,
		IsReady:   false,
	})

	log.Printf("[PVP] player %d joined room %d (team %d)", c.AccountID, roomID, team)
	return map[string]any{"room": room}, nil
}

func handlePvPLeave(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	if c.AccountID == 0 {
		return nil, fmt.Errorf("not_authenticated")
	}

	roomID, ok := toInt64(data["room_id"])
	if !ok {
		return nil, fmt.Errorf("room_id_required")
	}

	pvpMgr.mu.Lock()
	defer pvpMgr.mu.Unlock()

	room, exists := pvpMgr.rooms[roomID]
	if !exists {
		return nil, fmt.Errorf("room_not_found")
	}

	filtered := make([]*PvPPlayer, 0, len(room.Players))
	for _, p := range room.Players {
		if p.AccountID != c.AccountID {
			filtered = append(filtered, p)
		}
	}
	room.Players = filtered

	if room.OwnerID == c.AccountID && len(room.Players) > 0 {
		room.OwnerID = room.Players[0].AccountID
		room.OwnerName = room.Players[0].Username
	}

	if len(room.Players) == 0 {
		delete(pvpMgr.rooms, roomID)
		log.Printf("[PVP] deleted empty room %d", roomID)
	}

	return map[string]any{"room_id": roomID}, nil
}

func handlePvPList(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	pvpMgr.mu.RLock()
	defer pvpMgr.mu.RUnlock()

	rooms := make([]*PvPRoom, 0, len(pvpMgr.rooms))
	for _, r := range pvpMgr.rooms {
		if !r.IsPrivate && r.Status == "waiting" {
			rooms = append(rooms, r)
		}
	}
	return map[string]any{"rooms": rooms}, nil
}

func handlePvPFind(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	if c.AccountID == 0 {
		return nil, fmt.Errorf("not_authenticated")
	}

	gameMode, _ := data["game_mode"].(string)

	pvpMgr.mu.RLock()
	defer pvpMgr.mu.RUnlock()

	for _, r := range pvpMgr.rooms {
		if r.Status != "waiting" {
			continue
		}
		if gameMode != "" && r.GameMode != gameMode {
			continue
		}
		if len(r.Players) < r.MaxPlayers {
			return map[string]any{"room": r}, nil
		}
	}

	return nil, fmt.Errorf("no_room_available")
}

package services

import (
	"fmt"
	"log"
	"sync"
	"time"

	"eridian/internal/online"
)

// Lobby represents a PvE room.
type Lobby struct {
	ID         int64          `json:"id"`
	Name       string         `json:"name"`
	OwnerID    int64          `json:"owner_id"`
	OwnerName  string         `json:"owner_name"`
	Password   string         `json:"-"`
	IsPrivate  bool           `json:"is_private"`
	MaxPlayers int            `json:"max_players"`
	Players    []*LobbyPlayer `json:"players"`
	Status     string         `json:"status"` // "waiting", "in_game", "finished"
	CreatedAt  string         `json:"created_at"`
}

// LobbyPlayer represents a player in a lobby.
type LobbyPlayer struct {
	AccountID int64  `json:"account_id"`
	CharID    int64  `json:"char_id"`
	Username  string `json:"username"`
	IsReady   bool   `json:"is_ready"`
}

// LobbyManager manages the in-memory rooms.
type LobbyManager struct {
	mu      sync.RWMutex
	lobbies map[int64]*Lobby
	nextID  int64
}

var lobbyMgr = &LobbyManager{
	lobbies: make(map[int64]*Lobby),
	nextID:  1,
}

// RegisterLobbyService registers the lobby-related handlers.
func RegisterLobbyService(s *online.OnlineServer) {
	s.Handle(online.ServiceLobbyCreate, handleLobbyCreate)
	s.Handle(online.ServiceLobbyJoin, handleLobbyJoin)
	s.Handle(online.ServiceLobbyLeave, handleLobbyLeave)
	s.Handle(online.ServiceLobbyList, handleLobbyList)
	s.Handle(online.ServiceLobbyGet, handleLobbyGet)
	s.Handle(online.ServiceLobbyInvite, handleLobbyInvite)
	s.Handle(online.ServiceLobbyStart, handleLobbyStart)
}

func handleLobbyCreate(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	if c.AccountID == 0 {
		return nil, fmt.Errorf("not_authenticated")
	}

	name, _ := data["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("name_required")
	}

	maxPlayers := 4
	if v, ok := toInt(data["max_players"]); ok && v > 0 && v <= 8 {
		maxPlayers = v
	}

	password, _ := data["password"].(string)
	isPrivate := password != ""

	lobbyMgr.mu.Lock()
	defer lobbyMgr.mu.Unlock()

	id := lobbyMgr.nextID
	lobbyMgr.nextID++

	lobby := &Lobby{
		ID:         id,
		Name:       name,
		OwnerID:    c.AccountID,
		OwnerName:  c.Username,
		Password:   password,
		IsPrivate:  isPrivate,
		MaxPlayers: maxPlayers,
		Players: []*LobbyPlayer{
			{AccountID: c.AccountID, CharID: c.CharID, Username: c.Username, IsReady: true},
		},
		Status:    "waiting",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	lobbyMgr.lobbies[id] = lobby

	log.Printf("[LOBBY] created: id=%d name=%q owner=%d", id, name, c.AccountID)
	return map[string]any{"lobby": lobby}, nil
}

func handleLobbyJoin(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	if c.AccountID == 0 {
		return nil, fmt.Errorf("not_authenticated")
	}

	lobbyID, ok := toInt64(data["lobby_id"])
	if !ok {
		return nil, fmt.Errorf("lobby_id_required")
	}

	lobbyMgr.mu.Lock()
	defer lobbyMgr.mu.Unlock()

	lobby, exists := lobbyMgr.lobbies[lobbyID]
	if !exists {
		return nil, fmt.Errorf("lobby_not_found")
	}
	if lobby.Status != "waiting" {
		return nil, fmt.Errorf("game_already_started")
	}
	if len(lobby.Players) >= lobby.MaxPlayers {
		return nil, fmt.Errorf("lobby_full")
	}

	// Check password if private
	if lobby.IsPrivate {
		pwd, _ := data["password"].(string)
		if pwd != lobby.Password {
			return nil, fmt.Errorf("wrong_password")
		}
	}

	// Check if already in lobby
	for _, p := range lobby.Players {
		if p.AccountID == c.AccountID {
			return map[string]any{"lobby": lobby}, nil
		}
	}

	lobby.Players = append(lobby.Players, &LobbyPlayer{
		AccountID: c.AccountID,
		CharID:    c.CharID,
		Username:  c.Username,
		IsReady:   false,
	})

	log.Printf("[LOBBY] player %d joined lobby %d", c.AccountID, lobbyID)
	return map[string]any{"lobby": lobby}, nil
}

func handleLobbyLeave(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	if c.AccountID == 0 {
		return nil, fmt.Errorf("not_authenticated")
	}

	lobbyID, ok := toInt64(data["lobby_id"])
	if !ok {
		return nil, fmt.Errorf("lobby_id_required")
	}

	lobbyMgr.mu.Lock()
	defer lobbyMgr.mu.Unlock()

	lobby, exists := lobbyMgr.lobbies[lobbyID]
	if !exists {
		return nil, fmt.Errorf("lobby_not_found")
	}

	filtered := make([]*LobbyPlayer, 0, len(lobby.Players))
	for _, p := range lobby.Players {
		if p.AccountID != c.AccountID {
			filtered = append(filtered, p)
		}
	}
	lobby.Players = filtered

	// Transfer ownership if owner left and players remain
	if lobby.OwnerID == c.AccountID && len(lobby.Players) > 0 {
		lobby.OwnerID = lobby.Players[0].AccountID
		lobby.OwnerName = lobby.Players[0].Username
	}

	// Clean up empty lobbies
	if len(lobby.Players) == 0 {
		delete(lobbyMgr.lobbies, lobbyID)
		log.Printf("[LOBBY] deleted empty lobby %d", lobbyID)
	}

	log.Printf("[LOBBY] player %d left lobby %d", c.AccountID, lobbyID)
	return map[string]any{"lobby_id": lobbyID}, nil
}

func handleLobbyList(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	lobbyMgr.mu.RLock()
	defer lobbyMgr.mu.RUnlock()

	lobbies := make([]*Lobby, 0, len(lobbyMgr.lobbies))
	for _, l := range lobbyMgr.lobbies {
		// Filter private lobbies (only show public)
		if !l.IsPrivate && l.Status == "waiting" {
			lobbies = append(lobbies, l)
		}
	}
	return map[string]any{"lobbies": lobbies}, nil
}

func handleLobbyGet(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	lobbyID, ok := toInt64(data["lobby_id"])
	if !ok {
		return nil, fmt.Errorf("lobby_id_required")
	}

	lobbyMgr.mu.RLock()
	defer lobbyMgr.mu.RUnlock()

	lobby, exists := lobbyMgr.lobbies[lobbyID]
	if !exists {
		return nil, fmt.Errorf("lobby_not_found")
	}
	return map[string]any{"lobby": lobby}, nil
}

func handleLobbyInvite(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	if c.AccountID == 0 {
		return nil, fmt.Errorf("not_authenticated")
	}

	lobbyID, ok := toInt64(data["lobby_id"])
	if !ok {
		return nil, fmt.Errorf("lobby_id_required")
	}
	targetID, ok := toInt64(data["target_account_id"])
	if !ok {
		return nil, fmt.Errorf("target_account_id_required")
	}

	lobbyMgr.mu.RLock()
	defer lobbyMgr.mu.RUnlock()

	lobby, exists := lobbyMgr.lobbies[lobbyID]
	if !exists {
		return nil, fmt.Errorf("lobby_not_found")
	}
	if lobby.OwnerID != c.AccountID {
		return nil, fmt.Errorf("only_owner_can_invite")
	}

	log.Printf("[LOBBY] invite: player %d invited %d to lobby %d", c.AccountID, targetID, lobbyID)
	return map[string]any{
		"lobby_id":          lobbyID,
		"target_account_id": targetID,
		"invited_by":        c.AccountID,
	}, nil
}

func handleLobbyStart(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	if c.AccountID == 0 {
		return nil, fmt.Errorf("not_authenticated")
	}

	lobbyID, ok := toInt64(data["lobby_id"])
	if !ok {
		return nil, fmt.Errorf("lobby_id_required")
	}

	lobbyMgr.mu.Lock()
	defer lobbyMgr.mu.Unlock()

	lobby, exists := lobbyMgr.lobbies[lobbyID]
	if !exists {
		return nil, fmt.Errorf("lobby_not_found")
	}
	if lobby.OwnerID != c.AccountID {
		return nil, fmt.Errorf("only_owner_can_start")
	}
	if len(lobby.Players) < 1 {
		return nil, fmt.Errorf("need_at_least_one_player")
	}

	lobby.Status = "in_game"
	log.Printf("[LOBBY] game started in lobby %d by player %d", lobbyID, c.AccountID)
	return map[string]any{"lobby": lobby}, nil
}

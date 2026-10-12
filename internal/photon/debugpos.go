package photon


import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

const debugPosInterval = 200 * time.Millisecond


type PlayerPosSnapshot struct {
	ActorNr int32      `json:"actorNr"`
	UserID  string     `json:"userId"`
	Room    string     `json:"room"`
	Level   string     `json:"level"`
	Pos     [3]float32 `json:"pos"`
}


type EnemyPosSnapshot struct {
	ViewBase int32      `json:"viewBase"`
	Kit      string     `json:"kit"`
	Room     string     `json:"room"`
	Level    string     `json:"level"`
	Pos      [3]float32 `json:"pos"`
	HP       int32      `json:"hp"`
	PatrolPoints [][3]float32 `json:"patrolPoints,omitempty"`
	PatrolIdx    int          `json:"patrolIdx,omitempty"`
}


type debugPosPayload struct {
	Players []PlayerPosSnapshot `json:"players"`
	Enemies []EnemyPosSnapshot  `json:"enemies"`
}


func (s *EnetServer) StartDebugPosServer(host string, port int) {
	if port <= 0 {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debugpos/stream", s.handleDebugPosStream)
	addr := fmt.Sprintf("%s:%d", host, port)
	go func() {
		log.Printf("[DEBUGPOS] live position stream on http://%s/debugpos/stream", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("[DEBUGPOS] server error: %v", err)
		}
	}()
}

func (s *EnetServer) handleDebugPosStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ticker := time.NewTicker(debugPosInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			data, err := json.Marshal(s.debugPosPayload())
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return // client disconnected
			}
			flusher.Flush()
		}
	}
}


func (s *EnetServer) debugPosPayload() debugPosPayload {
	s.mu.RLock()
	defer s.mu.RUnlock()

	players := make([]PlayerPosSnapshot, 0, len(s.rooms))
	var enemyCount int
	for _, room := range s.rooms {
		enemyCount += len(room.npcs)
	}
	enemies := make([]EnemyPosSnapshot, 0, enemyCount)
	for _, room := range s.rooms {
		level := getCurScnFromProps(room.Name, room.Properties)
		for actorNr, userID := range room.Players {
			sess := s.sessionInRoom(userID, room.Name)
			if sess == nil || sess.avatarBase == 0 {
				continue
			}
			pos, ok := playerStreamPos(room, sess.avatarBase)
			if !ok {
				continue
			}
			players = append(players, PlayerPosSnapshot{
				ActorNr: actorNr,
				UserID:  userID,
				Room:    room.Name,
				Level:   level,
				Pos:     pos,
			})
		}
		for _, npc := range room.npcs {
			if npc.hp <= 0 {
				continue
			}
			snap := EnemyPosSnapshot{
				ViewBase: npc.viewBase,
				Kit:      npc.kit,
				Room:     room.Name,
				Level:    level,
				Pos:      npc.pos,
				HP:       npc.hp,
			}
			if len(npc.patrolPoints) > 0 {
				snap.PatrolPoints = npc.patrolPoints
				snap.PatrolIdx = npc.patrolIdx
			}
			enemies = append(enemies, snap)
		}
	}
	return debugPosPayload{Players: players, Enemies: enemies}
}

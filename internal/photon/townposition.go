package photon

import (
	"log"
	"math"
	"time"

	"eridian/internal/db"
)

// The town position is saved while in a town (periodically and when leaving the room) and used as the spawn
// point the next time that same town opens, after a login or when returning from any instance.
const townPositionSaveEvery = 10 * time.Second

func roomTownPlaylist(room *Room) (int32, bool) {
	id, ok := room.Properties["PlaylistId"].(int32)
	return id, ok && realTownPlaylists[id]
}

func (s *EnetServer) saveTownPosition(sess *ClientSession, room *Room) {
	if sess == nil || room == nil || sess.hostChar == nil || sess.avatarBase == 0 || sess.isDead {
		return
	}
	playlistID, ok := roomTownPlaylist(room)
	if !ok {
		return
	}
	pos, ok := room.playerPos[sess.avatarBase+1]
	if !ok {
		return
	}
	err := db.SaveCharacterTownPosition(sess.hostChar.CharID, db.TownPosition{
		PlaylistID: playlistID, X: float64(pos[0]), Y: float64(pos[1]), Z: float64(pos[2]), Yaw: sess.lastYawDeg,
	})
	if err != nil {
		log.Printf("[HOST] SaveCharacterTownPosition charID=%d: %v", sess.hostChar.CharID, err)
	}
	sess.townPosSavedAt = time.Now()
}

func (s *EnetServer) hostTickTownPositionSave(room *Room) {
	if _, ok := roomTownPlaylist(room); !ok {
		return
	}
	for _, uid := range room.Players {
		sess := s.sessionInRoom(uid, room.Name)
		if sess != nil && time.Since(sess.townPosSavedAt) >= townPositionSaveEvery {
			s.saveTownPosition(sess, room)
		}
	}
}

func townSpawnPosition(room *Room, sess *ClientSession) (pos [3]float32, rot [4]float32, ok bool) {
	if sess.hostChar == nil {
		return pos, rot, false
	}
	playlistID, isTown := roomTownPlaylist(room)
	if !isTown {
		return pos, rot, false
	}
	saved, err := db.GetCharacterTownPosition(sess.hostChar.CharID)
	if err != nil || saved.PlaylistID != playlistID {
		return pos, rot, false
	}
	half := saved.Yaw * math.Pi / 360
	return [3]float32{float32(saved.X), float32(saved.Y), float32(saved.Z)},
		[4]float32{0, float32(math.Sin(half)), 0, float32(math.Cos(half))}, true
}

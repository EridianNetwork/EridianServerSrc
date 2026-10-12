package photon

import (
	"fmt"
	"log"
	"strconv"
	"strings"
)

func (s *EnetServer) opChat(sess *ClientSession, opCode byte, params map[byte]any) {
	channel := int32(0)
	msg := ""
	if params[ParamObjArray] != nil {
		arr := toSlice(params[ParamObjArray])
		if len(arr) > 0 {
			channel = asInt32(arr[0])
		}
		if len(arr) > 3 {
			msg = fmt.Sprintf("%v", arr[3])
		}
	} else {
		if params[0] != nil {
			channel = asInt32(params[0])
		}
		if params[3] != nil {
			msg = fmt.Sprintf("%v", params[3])
		}
	}
	log.Printf("[PHOTON] Chat user=%s channel=%d msg=%q", sess.userID, channel, msg)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})

	// Debug/admin command
	if pos, ok := parseTeleportCommand(msg); ok {
		s.hostTeleportPlayer(sess, pos)
		return
	}
	// Debug/admin command
	if fields := strings.Fields(strings.TrimSpace(msg)); len(fields) >= 2 && fields[0] == "/of" {
		s.hostGrantOutfit(sess, strings.Join(fields[1:], " "))
		return
	}
	// Debug/admin command
	if fields := strings.Fields(strings.TrimSpace(msg)); len(fields) > 0 && fields[0] == "/reloadmap" {
		s.hostReloadMap(sess)
		return
	}
	// Debug/admin command
	if fields := strings.Fields(strings.TrimSpace(msg)); len(fields) > 0 {
		if playlistID, ok := travelCommands[fields[0]]; ok {
			gameMode := gameModeNumberForPlaylist(playlistID)
			if len(fields) > 1 {
				if n, err := strconv.Atoi(fields[1]); err == nil {
					gameMode = int32(n)
				}
			}
			s.hostTravelPlayer(sess, playlistID, gameMode)
			return
		}
	}
	
	name, charID := sess.charName, sess.selectedCharID
	if sess.hostChar != nil {
		if name == "" {
			name = sess.hostChar.Name
		}
		if charID == 0 {
			charID = sess.hostChar.CharID
		}
	}
	if name == "" {
		for _, other := range s.sessionsByAccount(sess.dbAccountID) {
			if other.charName != "" {
				name, charID = other.charName, other.selectedCharID
				break
			}
		}
	}
	chatEvt := map[byte]any{
		0: Byte(channel),
		1: int32(charID),
		2: name,
		3: msg,
		4: []byte{},
	}
	if sess.roomName != "" {
		if room := s.rooms[sess.roomName]; room != nil {
			s.broadcastEvent(room, EvChat, chatEvt, nil, nil)
			return
		}
	}
	s.broadcastToAll(EvChat, chatEvt, nil)
}

// broken command, does not work
var travelCommands = map[string]int32{
	"/rv": 33, // lvl_RV, solo variant; lvl_RV_4x (id 3333) is the 4-player one.
}


func parseTeleportCommand(msg string) ([3]float32, bool) {
	fields := strings.Fields(msg)
	if len(fields) != 4 || fields[0] != "/tp" {
		return [3]float32{}, false
	}
	var pos [3]float32
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseFloat(fields[i+1], 32)
		if err != nil {
			return [3]float32{}, false
		}
		pos[i] = float32(v)
	}
	return pos, true
}

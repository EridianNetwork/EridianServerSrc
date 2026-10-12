package photon

import (
	"log"

	"eridian/internal/db"
)








func (s *EnetServer) opRequestMatchRecord(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	charID := sess.selectedCharID
	if len(arr) > 0 {
		charID = int64(asInt32(arr[0]))
	}
	st, err := db.GetPvPStats(charID)
	if err != nil {
		log.Printf("[PHOTON] RequestMatchRecord charID=%d error: %v", charID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: map[int]any{
		0: int32(charID),
		1: int32(st.Matches),
		2: int32(st.Honor),
		3: int32(st.Wins),
		4: int32(st.Losses),
		5: int32(st.Kills),
		6: int32(st.Deaths),
		7: int32(0), 
		8: int32(0), 
	}})
}







func matchHonorGain(kills, deaths int32, won bool) int32 {
	gain := kills*10 - deaths*2
	if won {
		gain += 50
	}
	if gain < 0 {
		gain = 0
	}
	return gain
}






const matchRatingWinDelta = 25

func matchRatingDelta(won, lost bool) int32 {
	switch {
	case won:
		return matchRatingWinDelta
	case lost:
		return -matchRatingWinDelta
	default:
		return 0
	}
}







func (s *EnetServer) hostRecordPvPMatchResults(room *Room) {
	isTeamMode := isTeamGameMode(asInt32(room.Properties["GameMode"]))
	var winningTeam byte
	haveWinner := false
	if gamemodeName(room) == "GamemodeClaptrapSoccer" {
		if room.claptrapTeamScore[0] != room.claptrapTeamScore[1] {
			haveWinner = true
			if room.claptrapTeamScore[1] > room.claptrapTeamScore[0] {
				winningTeam = 1
			}
		}
	} else if isTeamMode {
		teamKills := map[byte]int32{}
		for _, uid := range room.Players {
			if sess := s.sessionInRoom(uid, room.Name); sess != nil {
				teamKills[sess.teamID] += sess.pvpKillCount
			}
		}
		if teamKills[0] != teamKills[1] {
			haveWinner = true
			if teamKills[1] > teamKills[0] {
				winningTeam = 1
			}
		}
	}

	
	var ffaWinner *ClientSession
	if !isTeamMode {
		var top *ClientSession
		tie := false
		for _, uid := range room.Players {
			sess := s.sessionInRoom(uid, room.Name)
			if sess == nil || sess.hostChar == nil {
				continue
			}
			switch {
			case top == nil || sess.pvpKillCount > top.pvpKillCount:
				top, tie = sess, false
			case sess.pvpKillCount == top.pvpKillCount:
				tie = true
			}
		}
		if top != nil && !tie && top.pvpKillCount > 0 {
			ffaWinner = top
		}
	}

	for _, uid := range room.Players {
		sess := s.sessionInRoom(uid, room.Name)
		if sess == nil || sess.hostChar == nil {
			continue
		}
		won, lost := false, false
		if isTeamMode {
			if haveWinner {
				won = sess.teamID == winningTeam
				lost = !won
			}
		} else if ffaWinner != nil {
			won = sess == ffaWinner
			lost = !won
		}
		honor := matchHonorGain(sess.pvpKillCount, sess.statDeaths, won)
		rating := matchRatingDelta(won, lost)
		if err := db.RecordPvPMatchResult(sess.hostChar.CharID, won, lost, sess.pvpKillCount, sess.statDeaths, honor, rating); err != nil {
			log.Printf("[HOST] RecordPvPMatchResult charID=%d error: %v", sess.hostChar.CharID, err)
		}
	}
}

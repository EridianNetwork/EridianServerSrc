package photon

import (
	"log"

	"eridian/internal/db"
)

// opRetrieveFriends (op 207): OnRetrieveFriendsList reads each response value as
// a Hashtable and pulls key 0 = friend char_id
func (s *EnetServer) opRetrieveFriends(sess *ClientSession, opCode byte, params map[byte]any) {
	ids, err := db.GetFriendCharIDs(sess.selectedCharID)
	if err != nil {
		log.Printf("[PHOTON] RetrieveFriends error: %v", err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	resp := map[byte]any{}
	for i, id := range ids {
		resp[byte(i)] = map[int]any{0: int32(id)}
	}
	s.sendOpResponse(sess, opCode, 0, resp)
}

// opGetAllFriendsCharacterInfo (op 144): OnGotFriendsCharacterInfo reads
// params[0] as a Hashtable of {0:id,1:name,2:level,3:avatarType} dicts
func (s *EnetServer) opGetAllFriendsCharacterInfo(sess *ClientSession, opCode byte, params map[byte]any) {
	infos, err := db.GetFriendsCharInfo(sess.selectedCharID)
	if err != nil {
		log.Printf("[PHOTON] GetAllFriendsCharacterInfo error: %v", err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	ht := map[int]any{}
	for i, f := range infos {
		ht[i] = map[int]any{
			0: int32(f.CharID),
			1: f.Name,
			2: int32(f.Level),
			3: int32(0), // AvatarType
		}
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: ht})
}

func (s *EnetServer) opFriendAdd(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		return
	}
	targetCharID := int64(asInt32(arr[0]))
	if targetCharID == sess.selectedCharID || targetCharID == 0 {
		return
	}
	if isFriend, _ := db.IsCharFriend(sess.selectedCharID, targetCharID); isFriend {
		return
	}
	if _, err := s.pushMessage(sess.selectedCharID, targetCharID, "friend_request", sess.charName); err != nil {
		log.Printf("[PHOTON] FriendAdd error: %v", err)
		return
	}
	log.Printf("[PHOTON] FriendAdd charID=%d -> charID=%d", sess.selectedCharID, targetCharID)
}

func (s *EnetServer) opFriendAccept(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		return
	}
	senderCharID := int64(asInt32(arr[0]))
	if err := db.AddCharFriend(sess.selectedCharID, senderCharID); err != nil {
		log.Printf("[PHOTON] FriendAccept error: %v", err)
		return
	}
	db.DeleteMessagesFrom(sess.selectedCharID, senderCharID, "friend_request")
	log.Printf("[PHOTON] FriendAccept charID=%d + charID=%d", sess.selectedCharID, senderCharID)

	if ch, err := db.GetCharacterByID(sess.selectedCharID); err == nil {
		selfPresence := s.presenceHT(ch.CharID, charToHashtable(ch, realInventoryHashtable(ch.CharID)), s.isCharOnline(ch.CharID), charLastSeenUnix(ch))
		for _, other := range s.sessions {
			if other.selectedCharID == senderCharID {
				s.sendEventTo(other, EvNewFriend, map[byte]any{0: selfPresence})
			}
		}
	}
	if ch, err := db.GetCharacterByID(senderCharID); err == nil {
		log.Printf("[PHOTON] GetCharacterByID error: %v", err)
		s.sendEventTo(sess, EvNewFriend, map[byte]any{0: s.presenceHT(ch.CharID, charToHashtable(ch, realInventoryHashtable(ch.CharID)), s.isCharOnline(ch.CharID), charLastSeenUnix(ch))})
	}
}

func (s *EnetServer) opFriendReject(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		return
	}
	senderCharID := int64(asInt32(arr[0]))
	db.DeleteMessagesFrom(sess.selectedCharID, senderCharID, "friend_request")
	log.Printf("[PHOTON] FriendReject charID=%d rejected charID=%d", sess.selectedCharID, senderCharID)

	for _, other := range s.sessions {
		if other.selectedCharID == senderCharID {
			s.sendEventTo(other, EvRejectFriend, map[byte]any{
				0: int32(sess.selectedCharID),
				1: sess.charName,
			})
		}
	}
}

func (s *EnetServer) opFriendRemove(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		return
	}
	friendCharID := int64(asInt32(arr[0]))
	if err := db.RemoveCharFriend(sess.selectedCharID, friendCharID); err != nil {
		log.Printf("[PHOTON] FriendRemove error: %v", err)
		return
	}
	log.Printf("[PHOTON] FriendRemove charID=%d removed charID=%d", sess.selectedCharID, friendCharID)

	for _, other := range s.sessions {
		if other.selectedCharID == friendCharID {
			s.sendEventTo(other, EvUnfriended, map[byte]any{0: int32(sess.selectedCharID)})
		}
	}
}

func (s *EnetServer) opIgnore(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		return
	}
	targetCharID := int64(asInt32(arr[0]))
	if targetCharID == sess.selectedCharID || targetCharID == 0 {
		return
	}
	if err := db.AddCharIgnore(sess.selectedCharID, targetCharID); err != nil {
		log.Printf("[PHOTON] Ignore error: %v", err)
		return
	}
	log.Printf("[PHOTON] Ignore charID=%d ignored charID=%d", sess.selectedCharID, targetCharID)
}


func (s *EnetServer) opUnignore(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		return
	}
	targetCharID := int64(asInt32(arr[0]))
	if err := db.RemoveCharIgnore(sess.selectedCharID, targetCharID); err != nil {
		log.Printf("[PHOTON] Unignore error: %v", err)
		return
	}
	log.Printf("[PHOTON] Unignore charID=%d unignored charID=%d", sess.selectedCharID, targetCharID)
}


func (s *EnetServer) opIsUserIgnored(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	charID1 := int32(sess.selectedCharID)
	charID2 := int32(0)
	if len(arr) > 0 {
		charID1 = asInt32(arr[0])
	}
	if len(arr) > 1 {
		charID2 = asInt32(arr[1])
	}
	ignored, err := db.IsCharIgnored(int64(charID1), int64(charID2))
	if err != nil {
		log.Printf("[PHOTON] IsUserIgnored error: %v", err)
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{
		0: charID1,
		1: charID2,
		2: ignored,
	})
}


func (s *EnetServer) opSendGameInvitation(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 || sess.roomName == "" {
		return
	}
	targetName, _ := arr[0].(string)
	target, err := db.GetCharacterByName(targetName)
	if err != nil || target == nil || target.CharID == sess.selectedCharID {
		return
	}
	if _, err := s.pushMessage(sess.selectedCharID, target.CharID, "game_invitation", sess.roomName); err != nil {
		log.Printf("[PHOTON] SendGameInvitation error: %v", err)
		return
	}
	log.Printf("[PHOTON] SendGameInvitation charID=%d -> charID=%d room=%s", sess.selectedCharID, target.CharID, sess.roomName)
}

package photon

import (
	"log"
	"strconv"

	"eridian/internal/db"
)

type GroupRecord struct {
	ID      int
	Owner   int64
	Members []*ClientSession
}

const maxGroupSize = 4

// groupBroadcast sends an event to all members of a group
func (s *EnetServer) groupBroadcast(g *GroupRecord, eventCode byte, params map[byte]any, exclude *ClientSession) {
	for _, m := range g.Members {
		if m == exclude {
			continue
		}
		s.sendEventTo(m, eventCode, params)
	}
}

func removeMemberFromGroup(g *GroupRecord, sess *ClientSession) {
	for i, m := range g.Members {
		if m == sess {
			g.Members = append(g.Members[:i], g.Members[i+1:]...)
			return
		}
	}
}

func (s *EnetServer) cleanLeaveGroup(sess *ClientSession) {
	group := s.groups[int(sess.groupID)]
	if group == nil {
		sess.groupID = 0
		return
	}
	charID := int32(sess.selectedCharID)
	wasOwner := group.Owner == sess.selectedCharID
	removeMemberFromGroup(group, sess)
	sess.groupID = 0

	if len(group.Members) == 0 {
		delete(s.groups, group.ID)
		s.groupBroadcast(group, EvGroupDisbanded, map[byte]any{}, nil)
		return
	}
	if wasOwner {
		group.Owner = group.Members[0].selectedCharID
		s.groupBroadcast(group, EvNewGroupLeader, map[byte]any{0: int32(group.Owner)}, nil)
		log.Printf("[PHOTON] group=%d owner left, leadership passed to charID=%d", group.ID, group.Owner)
	}
	s.groupBroadcast(group, EvGroupMemberLeft, map[byte]any{0: charID}, nil)
}


func (s *EnetServer) opRejoinGroup(sess *ClientSession, opCode byte, params map[byte]any) {
	s.opGetGroupInfo(sess, opCode, params)
}

func (s *EnetServer) opGetGroupInfo(sess *ClientSession, opCode byte, params map[byte]any) {
	group := s.groups[int(sess.groupID)]
	if group == nil {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{0: nil})
		return
	}
	members := make([]any, len(group.Members))
	for i, m := range group.Members {
		members[i] = sessPresenceHT(m)
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{
		0: int32(len(group.Members)),
		1: TypedArray{ElementType: THash, Items: members},
	})
}


func (s *EnetServer) opSendGroupInvitation(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	if len(arr) == 0 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	targetCharID := int64(asInt32(arr[0]))
	if sess.selectedCharID == 0 || targetCharID == sess.selectedCharID {
		log.Printf("[PHOTON] SendGroupInvitation rejected user=%s selectedCharID=%d target=%d", sess.userID, sess.selectedCharID, targetCharID)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	group := s.groups[int(sess.groupID)]
	justCreated := group == nil
	if group == nil {
		s.groupCounter++
		group = &GroupRecord{ID: s.groupCounter, Owner: sess.selectedCharID, Members: []*ClientSession{sess}}
		s.groups[group.ID] = group
		sess.groupID = int32(group.ID)
	}
	if len(group.Members) >= maxGroupSize {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	for _, m := range group.Members {
		if m.selectedCharID == targetCharID {
			s.sendOpResponse(sess, opCode, -1, map[byte]any{})
			return
		}
	}
	if _, err := db.GetCharacterByID(targetCharID); err != nil {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	if _, err := s.pushMessage(sess.selectedCharID, targetCharID, "group_invitation", strconv.Itoa(group.ID)); err != nil {
		log.Printf("[PHOTON] SendGroupInvitation error: %v", err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	log.Printf("[PHOTON] SendGroupInvitation user=%s groupID=%d target=%d", sess.userID, group.ID, targetCharID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(1)})
	if justCreated {
		s.hostSyncGroupUnlocks(sess)
	}
}


func (s *EnetServer) resolveGroupInvite(sess *ClientSession, opCode byte, params map[byte]any) (*db.Message, *GroupRecord) {
	arr := toSlice(params[112])
	if len(arr) == 0 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return nil, nil
	}
	msgID := int64(asInt32(arr[0]))
	msg, err := db.GetMessageByID(msgID)
	if err != nil || msg == nil || msg.Type != "group_invitation" || msg.ReceiverID != sess.selectedCharID {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return nil, nil
	}
	groupID, err := strconv.Atoi(msg.Payload)
	if err != nil {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return nil, nil
	}
	group := s.groups[groupID]
	if group == nil {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return nil, nil
	}
	return msg, group
}


func (s *EnetServer) opAcceptGroupInvitation(sess *ClientSession, opCode byte, params map[byte]any) {
	msg, group := s.resolveGroupInvite(sess, opCode, params)
	if msg == nil {
		return
	}
	if len(group.Members) >= maxGroupSize {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	if sess.groupID != 0 {
		s.cleanLeaveGroup(sess)
	}
	group.Members = append(group.Members, sess)
	sess.groupID = int32(group.ID)
	db.DeleteMessage(msg.MessageID)
	log.Printf("[PHOTON] AcceptGroupInvitation user=%s groupID=%d", sess.userID, group.ID)
	s.groupBroadcast(group, EvNewGroupMember, map[byte]any{0: sessPresenceHT(sess)}, sess)

	members := make([]any, len(group.Members))
	for i, m := range group.Members {
		members[i] = sessPresenceHT(m)
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{
		0: int32(msg.SenderID),
		1: int32(group.ID),
		2: TypedArray{ElementType: THash, Items: members},
	})

	s.hostSyncGroupUnlocks(sess)
	if len(group.Members) == 2 {
		for _, m := range group.Members {
			if m != sess && m.selectedCharID == group.Owner {
				s.hostSyncGroupUnlocks(m)
				break
			}
		}
	}
}


func (s *EnetServer) opRejectGroupInvitation(sess *ClientSession, opCode byte, params map[byte]any) {
	msg, group := s.resolveGroupInvite(sess, opCode, params)
	if msg == nil {
		return
	}
	groupID := group.ID
	db.DeleteMessage(msg.MessageID)
	log.Printf("[PHOTON] RejectGroupInvitation user=%s groupID=%d", sess.userID, groupID)
	for _, other := range s.sessions {
		if other.selectedCharID == group.Owner {
			s.sendEventTo(other, EvGroupInviteRejected, map[byte]any{
				0: int32(sess.selectedCharID),
				1: sess.charName,
			})
		}
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}

func (s *EnetServer) opLeaveGroup(sess *ClientSession, opCode byte, params map[byte]any) {
	s.cleanLeaveGroup(sess)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}

// currently broken
func (s *EnetServer) opKickPlayerFromGroup(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	if len(arr) == 0 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	targetCharID := int64(asInt32(arr[0]))
	group := s.groups[int(sess.groupID)]
	if group == nil || group.Owner != sess.selectedCharID || targetCharID == sess.selectedCharID {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	var target *ClientSession
	for _, m := range group.Members {
		if m.selectedCharID == targetCharID {
			target = m
			break
		}
	}
	if target == nil {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	s.sendEventTo(target, EvKickedFromGroup, map[byte]any{})
	removeMemberFromGroup(group, target)
	target.groupID = 0
	s.groupBroadcast(group, EvGroupMemberLeft, map[byte]any{0: int32(targetCharID)}, nil)
	log.Printf("[PHOTON] KickPlayerFromGroup user=%s groupID=%d target=%d", sess.userID, group.ID, targetCharID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(targetCharID)})
}

func (s *EnetServer) opAcceptGroupGameInvitation(sess *ClientSession, opCode byte, params map[byte]any) {
	roomName := s.pendingGroupRoom[sess.userID]
	if roomName == "" {
		log.Printf("[PHOTON] AcceptGroupGameInvitation user=%s: no pending invite in s.pendingGroupRoom (either SendGameInvitationsToGroup never reached this user, or this fired before it did)", sess.userID)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	delete(s.pendingGroupRoom, sess.userID)
	room := s.rooms[roomName]
	playlistID := int32(0)
	if room != nil {
		playlistID = asInt32(room.Properties["PlaylistId"])
	}
	log.Printf("[PHOTON] AcceptGroupGameInvitation user=%s room=%s", sess.userID, roomName)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: roomName, 1: playlistID})
}

func (s *EnetServer) opSendGameInvitationsToGroup(sess *ClientSession, opCode byte, params map[byte]any) {
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
	if sess.groupID == 0 {
		return
	}
	group := s.groups[int(sess.groupID)]
	if group == nil {
		log.Printf("[PHOTON] SendGameInvitationsToGroup user=%s: groupID=%d no longer exists in s.groups (stale/disbanded)", sess.userID, sess.groupID)
		return
	}
	if len(group.Members) < 2 {
		return
	}
	var roomName string
	if active := s.activeGameSession[sess.userID]; active != nil {
		roomName = active.roomName
	}
	if roomName == "" {
		log.Printf("[PHOTON] SendGameInvitationsToGroup user=%s groupID=%d: no active game-peer session found (s.activeGameSession empty for this user), can't tell members where to go", sess.userID, group.ID)
		return
	}
	log.Printf("[PHOTON] SendGameInvitationsToGroup user=%s groupID=%d room=%s members=%d", sess.userID, group.ID, roomName, len(group.Members))
	for _, m := range group.Members {
		if m == sess {
			continue
		}
		if active := s.activeGameSession[m.userID]; active != nil && active.roomName == roomName {
			continue
		}
		if s.pendingGroupRoom[m.userID] == roomName {
			continue
		}
		s.pendingGroupRoom[m.userID] = roomName
		s.sendEventTo(m, EvGroupGameStarted, map[byte]any{ParamPlayerNr: HostActorNr})
		log.Printf("[PHOTON] SendGameInvitationsToGroup -> notified user=%s (addr=%s) to join room=%s", m.userID, m.addr, roomName)
	}
}

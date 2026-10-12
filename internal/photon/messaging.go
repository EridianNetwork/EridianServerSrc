package photon

import (
	"log"

	"eridian/internal/db"
)

func (s *EnetServer) opDeleteMessage(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	msgID := int64(asInt32(arr[0]))
	msg, err := db.GetMessageByID(msgID)
	if err != nil || msg == nil || msg.ReceiverID != sess.selectedCharID {
		log.Printf("[PHOTON] DeleteMessage user=%s msgID=%d denied (err=%v)", sess.userID, msgID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	if err := db.DeleteMessage(msgID); err != nil {
		log.Printf("[PHOTON] DeleteMessage user=%s msgID=%d error: %v", sess.userID, msgID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	log.Printf("[PHOTON] DeleteMessage user=%s msgID=%d", sess.userID, msgID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(msgID)})
}

func (s *EnetServer) pushMessage(senderCharID, receiverCharID int64, msgType, payload string) (int64, error) {
	msgID, err := db.CreateMessage(senderCharID, receiverCharID, msgType, payload)
	if err != nil {
		return 0, err
	}
	for _, other := range s.sessions {
		if other.selectedCharID == receiverCharID {
			s.sendEventTo(other, EvNewMessage, map[byte]any{
				0: int32(msgID),
				1: int32(senderCharID),
				2: msgType,
				3: payload,
			})
		}
	}
	return msgID, nil
}

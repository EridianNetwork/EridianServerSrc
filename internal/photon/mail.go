package photon

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"eridian/internal/db"
	"eridian/internal/domain"
)

const mailLifetime = 7 * 24 * time.Hour

func mailToHashtable(m *db.MailFull) map[int]any {
	mailState := Byte(0) // Unread
	if m.IsRead {
		mailState = Byte(1) // Read
	}
	if m.IsClaimed {
		mailState = Byte(2) // GetItems
	}

	mailType := Byte(m.MailType)

	sentAt, err := time.Parse("2006-01-02 15:04:05", m.SentAt)
	if err != nil {
		sentAt = time.Now()
	}
	expiration := int32(sentAt.Add(mailLifetime).Unix())

	return map[int]any{
		0: int32(m.MailID),
		1: mailType,
		2: mailState,
		3: int32(m.SenderID),
		4: m.SenderName,
		5: int32(m.ReceiverID),
		6: m.ReceiverName,
		7: m.Body,
		8: expiration,
		9: mailItemDNA(m.ItemType, m.ItemData),
	}
}

func mailItemDNA(itemType int, itemData string) any {
	if itemType == 0 {
		return nil
	}
	var it domain.MailItem
	if itemData != "" {
		if err := json.Unmarshal([]byte(itemData), &it); err != nil {
			log.Printf("[PHOTON] mail item_data decode error: %v", err)
			return nil
		}
	}
	switch itemType {
	case domain.ItemTypeWeapon:
		name := it.WeaponName
		if name == "" {
			name = "SMG"
		}
		return map[int]any{
			0: int32(domain.ItemTypeWeapon),
			1: int32(it.Level), 2: int32(it.Rarity), 3: int32(0), 4: int32(it.WeaponID),
			5: int32(0), 6: int32(0), 7: int32(0), 8: "",
			9: name, 10: int32(it.WeaponType),
			11: int32(0), 12: int32(0), 13: int32(0), 14: int32(0), 15: int32(0),
			16: int32(0), 17: int32(0), 18: int32(0), 19: int32(0),
			20: int32(0), 21: int32(0), 22: int32(0),
		}
	case domain.ItemTypeMaterial:
		return map[int]any{
			0: int32(domain.ItemTypeMaterial),
			1: int32(1), 2: int32(0), 3: int32(0), 4: int32(0),
			5: int32(0), 6: int32(0), 7: int32(0), 8: "",
			9: int32(it.MaterialType), 10: int32(it.Quantity),
		}
	default:
		return nil
	}
}

// opGetMail (op 95): responds ok then uit pushes event 161 (MailList) with all mail
func (s *EnetServer) opGetMail(sess *ClientSession, opCode byte, params map[byte]any) {
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
	if sess.selectedCharID == 0 {
		return
	}
	mails, err := db.GetMailsByCharID(sess.selectedCharID)
	if err != nil {
		log.Printf("[PHOTON] GetMail error: %v", err)
		return
	}
	evt := map[byte]any{}
	for i := range mails {
		evt[byte(i)] = mailToHashtable(&mails[i])
	}
	s.sendEventTo(sess, EvMailList, evt)
	log.Printf("[PHOTON] GetMail charID=%d → %d mails", sess.selectedCharID, len(mails))
}

func (s *EnetServer) opSendMail(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 2 {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{0: Short(MailCharacterNotExist)})
		return
	}
	recipient := fmt.Sprintf("%v", arr[0])
	content := fmt.Sprintf("%v", arr[1])

	if len([]rune(content)) > 300 {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{0: Short(MailContentLimit)})
		return
	}

	receiverCharID, err := db.GetCharIDByName(recipient)
	if err != nil || receiverCharID == 0 || receiverCharID == sess.selectedCharID {
		log.Printf("[PHOTON] SendMail: recipient %q not found", recipient)
		s.sendOpResponse(sess, opCode, 0, map[byte]any{0: Short(MailCharacterNotExist)})
		return
	}
	if n, err := db.CountMailsByCharID(receiverCharID); err == nil && n >= 50 {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{0: Short(MailInboxFull)})
		return
	}
	mailID, err := db.CreateMail(sess.selectedCharID, receiverCharID, content)
	if err != nil {
		log.Printf("[PHOTON] SendMail error: %v", err)
		s.sendOpResponse(sess, opCode, 0, map[byte]any{0: Short(MailCharacterNotExist)})
		return
	}
	log.Printf("[PHOTON] SendMail charID=%d → charID=%d mailID=%d", sess.selectedCharID, receiverCharID, mailID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: Short(0)})

	// 167 (NewMail) receiver if online
	for _, other := range s.sessions {
		if other.selectedCharID == receiverCharID {
			if mail, err := db.GetMailByID(mailID, receiverCharID); err == nil {
				s.sendEventTo(other, EvNewMail, map[byte]any{0: mailToHashtable(mail)})
			}
			break
		}
	}
}

func (s *EnetServer) opReadMail(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{0: int32(0)})
		return
	}
	mailID := int64(asInt32(arr[0]))
	if err := db.MarkMailRead(mailID, sess.selectedCharID); err != nil {
		log.Printf("[PHOTON] ReadMail error: %v", err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{0: int32(0)})
		return
	}
	log.Printf("[PHOTON] ReadMail charID=%d mailID=%d", sess.selectedCharID, mailID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(mailID)})
}

func (s *EnetServer) opGetMailItem(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{0: int32(0)})
		return
	}
	mailID := int64(asInt32(arr[0]))
	if err := db.ClaimMailItem(mailID, sess.selectedCharID); err != nil {
		log.Printf("[PHOTON] GetMailItem error: %v", err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{0: int32(0)})
		return
	}
	log.Printf("[PHOTON] GetMailItem charID=%d mailID=%d", sess.selectedCharID, mailID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(mailID)})
}

func (s *EnetServer) opDeleteMail(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{0: int32(0)})
		return
	}
	mailID := int64(asInt32(arr[0]))
	if err := db.DeleteMail(mailID, sess.selectedCharID); err != nil {
		log.Printf("[PHOTON] DeleteMail error: %v", err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{0: int32(0)})
		return
	}
	log.Printf("[PHOTON] DeleteMail charID=%d mailID=%d", sess.selectedCharID, mailID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(mailID)})
}

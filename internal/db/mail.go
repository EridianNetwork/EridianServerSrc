package db

import "eridian/internal/domain"

// MailFull extends domain.Mail with sender/receiver names resolved from characters
type MailFull struct {
	domain.Mail
	SenderName   string
	ReceiverName string
}

func scanMail(row interface{ Scan(...any) error }, m *MailFull) error {
	var isRead, isClaimed int
	err := row.Scan(
		&m.MailID, &m.SenderID, &m.ReceiverID, &m.Subject, &m.Body,
		&m.ItemType, &m.ItemData, &isRead, &isClaimed, &m.SentAt, &m.MailType,
		&m.SenderName, &m.ReceiverName,
	)
	if err != nil {
		return err
	}
	m.IsRead = isRead != 0
	m.IsClaimed = isClaimed != 0
	return nil
}

const mailSelectCols = `
	m.mail_id, m.sender_id, m.receiver_id, m.subject, m.body,
	m.item_type, m.item_data, m.is_read, m.is_claimed, m.sent_at, m.mail_type,
	COALESCE(sc.name, ''), COALESCE(rc.name, '')`

const mailJoins = `
	FROM mail m
	LEFT JOIN characters sc ON sc.char_id = m.sender_id
	LEFT JOIN characters rc ON rc.char_id = m.receiver_id`

// returns all mails for a character
func GetMailsByCharID(charID int64) ([]MailFull, error) {
	rows, err := DB().Query(`SELECT `+mailSelectCols+mailJoins+`
		WHERE m.receiver_id = ? ORDER BY m.mail_id DESC`, charID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var mails []MailFull
	for rows.Next() {
		var m MailFull
		if err := scanMail(rows, &m); err != nil {
			return nil, err
		}
		mails = append(mails, m)
	}
	return mails, nil
}


func CountMailsByCharID(charID int64) (int, error) {
	var n int
	err := DB().QueryRow(`SELECT COUNT(*) FROM mail WHERE receiver_id = ?`, charID).Scan(&n)
	return n, err
}

func GetMailByID(mailID, charID int64) (*MailFull, error) {
	row := DB().QueryRow(`SELECT `+mailSelectCols+mailJoins+`
		WHERE m.mail_id = ? AND m.receiver_id = ?`, mailID, charID)
	var m MailFull
	if err := scanMail(row, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func CreateMail(senderCharID, receiverCharID int64, content string) (int64, error) {
	res, err := DB().Exec(
		`INSERT INTO mail (sender_id, receiver_id, body, mail_type) VALUES (?, ?, ?, 1)`,
		senderCharID, receiverCharID, content,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// no functionality built in for this currently, but it should support sending item,s to other players eventually. keen to get this going
func CreateMailWithItem(senderCharID, receiverCharID int64, subject, content string, itemType int, itemData string) (int64, error) {
	res, err := DB().Exec(
		`INSERT INTO mail (sender_id, receiver_id, subject, body, item_type, item_data, mail_type) VALUES (?, ?, ?, ?, ?, ?, 0)`,
		senderCharID, receiverCharID, subject, content, itemType, itemData,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}


func MarkMailRead(mailID, charID int64) error {
	_, err := DB().Exec(
		`UPDATE mail SET is_read = 1 WHERE mail_id = ? AND receiver_id = ?`,
		mailID, charID,
	)
	return err
}

// see ln 84
func ClaimMailItem(mailID, charID int64) error {
	_, err := DB().Exec(
		`UPDATE mail SET is_claimed = 1 WHERE mail_id = ? AND receiver_id = ?`,
		mailID, charID,
	)
	return err
}

func DeleteMail(mailID, charID int64) error {
	_, err := DB().Exec(
		`DELETE FROM mail WHERE mail_id = ? AND receiver_id = ?`,
		mailID, charID,
	)
	return err
}

// i think this might be an isse in future for finding players. no indication of CS for mailing in-game from what it appears
func GetCharIDByName(name string) (int64, error) {
	var id int64
	err := DB().QueryRow(
		`SELECT char_id FROM characters WHERE name = ? AND is_deleted = 0 LIMIT 1`, name,
	).Scan(&id)
	return id, err
}

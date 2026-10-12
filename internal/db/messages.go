package db

import "database/sql"


type Message struct {
	MessageID  int64
	SenderID   int64
	ReceiverID int64
	Type       string
	Payload    string
	IsRead     bool
	CreatedAt  string
}

func CreateMessage(senderCharID, receiverCharID int64, msgType, payload string) (int64, error) {
	res, err := DB().Exec(
		`INSERT INTO messages (sender_id, receiver_id, type, payload) VALUES (?, ?, ?, ?)`,
		senderCharID, receiverCharID, msgType, payload,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func GetMessagesByType(receiverCharID int64, msgType string) ([]Message, error) {
	rows, err := DB().Query(
		`SELECT message_id, sender_id, receiver_id, type, payload, is_read, created_at
		 FROM messages WHERE receiver_id = ? AND type = ? ORDER BY message_id DESC`,
		receiverCharID, msgType,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var isRead int
		if err := rows.Scan(&m.MessageID, &m.SenderID, &m.ReceiverID, &m.Type, &m.Payload, &isRead, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.IsRead = isRead != 0
		out = append(out, m)
	}
	return out, nil
}

func DeleteMessagesFrom(receiverCharID, senderCharID int64, msgType string) error {
	_, err := DB().Exec(
		`DELETE FROM messages WHERE receiver_id = ? AND sender_id = ? AND type = ?`,
		receiverCharID, senderCharID, msgType,
	)
	return err
}

func GetMessageByID(msgID int64) (*Message, error) {
	row := DB().QueryRow(
		`SELECT message_id, sender_id, receiver_id, type, payload, is_read, created_at
		 FROM messages WHERE message_id = ?`,
		msgID,
	)
	var m Message
	var isRead int
	if err := row.Scan(&m.MessageID, &m.SenderID, &m.ReceiverID, &m.Type, &m.Payload, &isRead, &m.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	m.IsRead = isRead != 0
	return &m, nil
}

func DeleteMessage(msgID int64) error {
	_, err := DB().Exec(`DELETE FROM messages WHERE message_id = ?`, msgID)
	return err
}

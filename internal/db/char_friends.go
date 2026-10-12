package db

// FriendCharInfo is a friend's character summary, as sent to the client (op 144)
type FriendCharInfo struct {
	CharID int64
	Name   string
	Level  int32
}
func GetFriendCharIDs(charID int64) ([]int64, error) {
	rows, err := DB().Query(`SELECT friend_id FROM char_friends WHERE char_id = ?`, charID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetFriendsCharInfo resolves id/name/level for a characters friends
func GetFriendsCharInfo(charID int64) ([]FriendCharInfo, error) {
	rows, err := DB().Query(`
		SELECT c.char_id, c.name, c.level
		FROM char_friends f
		JOIN characters c ON c.char_id = f.friend_id
		WHERE f.char_id = ?`, charID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FriendCharInfo
	for rows.Next() {
		var f FriendCharInfo
		if err := rows.Scan(&f.CharID, &f.Name, &f.Level); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// inserts a mutual friendship between two characters
func AddCharFriend(charID, friendID int64) error {
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`INSERT OR IGNORE INTO char_friends (char_id, friend_id) VALUES (?, ?)`, charID, friendID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO char_friends (char_id, friend_id) VALUES (?, ?)`, friendID, charID); err != nil {
		return err
	}
	return tx.Commit()
}

// deletes a mutual friendship between two characters
func RemoveCharFriend(charID, friendID int64) error {
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM char_friends WHERE char_id = ? AND friend_id = ?`, charID, friendID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM char_friends WHERE char_id = ? AND friend_id = ?`, friendID, charID); err != nil {
		return err
	}
	return tx.Commit()
}

// reports whether two char are friends currently
func IsCharFriend(charID, friendID int64) (bool, error) {
	var one int
	err := DB().QueryRow(`SELECT 1 FROM char_friends WHERE char_id = ? AND friend_id = ?`, charID, friendID).Scan(&one)
	if err != nil {
		return false, nil // if false, n ot friend
	}
	return true, nil
}

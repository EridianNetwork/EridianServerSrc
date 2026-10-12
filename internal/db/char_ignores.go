package db

// blocking characters
func AddCharIgnore(charID, ignoredID int64) error {
	_, err := DB().Exec(`INSERT OR IGNORE INTO char_ignores (char_id, ignored_id) VALUES (?, ?)`, charID, ignoredID)
	return err
}

func RemoveCharIgnore(charID, ignoredID int64) error {
	_, err := DB().Exec(`DELETE FROM char_ignores WHERE char_id = ? AND ignored_id = ?`, charID, ignoredID)
	return err
}

func IsCharIgnored(charID, ignoredID int64) (bool, error) {
	var one int
	err := DB().QueryRow(`SELECT 1 FROM char_ignores WHERE char_id = ? AND ignored_id = ?`, charID, ignoredID).Scan(&one)
	if err != nil {
		return false, nil // if no rows or otherwise not ignored
	}
	return true, nil
}

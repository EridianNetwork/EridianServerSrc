package db

import "eridian/internal/domain"

func GetFriends(accountID int64) ([]*domain.Friend, error) {
	rows, err := DB().Query(`
		SELECT f.friend_id, a.username, f.status, f.created_at
		FROM friends f
		JOIN accounts a ON a.account_id = f.friend_id
		WHERE f.account_id = ? AND f.status = 1
		ORDER BY a.username`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var friends []*domain.Friend
	for rows.Next() {
		f := &domain.Friend{}
		if err := rows.Scan(&f.FriendID, &f.Username, &f.Status, &f.CreatedAt); err != nil {
			return nil, err
		}
		friends = append(friends, f)
	}
	return friends, rows.Err()
}

// returns pending friend requests addressed to an account
func GetPendingFriendRequests(accountID int64) ([]*domain.FriendRequest, error) {
	rows, err := DB().Query(`
		SELECT f.account_id, a.username, f.status, f.created_at
		FROM friends f
		JOIN accounts a ON a.account_id = f.account_id
		WHERE f.friend_id = ? AND f.status = 0
		ORDER BY f.created_at DESC`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []*domain.FriendRequest
	for rows.Next() {
		r := &domain.FriendRequest{}
		if err := rows.Scan(&r.AccountID, &r.Username, &r.Status, &r.CreatedAt); err != nil {
			return nil, err
		}
		requests = append(requests, r)
	}
	return requests, rows.Err()
}

// AddFriendRequest creates a friend request (status=0)
func AddFriendRequest(requesterID, targetID int64) error {
	_, err := DB().Exec(
		"INSERT OR IGNORE INTO friends (account_id, friend_id, status) VALUES (?, ?, 0)",
		requesterID, targetID)
	return err
}

// AcceptFriendRequest accepts a friend request
func AcceptFriendRequest(requesterID, accepterID int64) error {
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Update the original request to accepted
	if _, err := tx.Exec(
		"UPDATE friends SET status = 1 WHERE account_id = ? AND friend_id = ?",
		requesterID, accepterID); err != nil {
		return err
	}

	// Create the reverse relationship
	if _, err := tx.Exec(
		"INSERT OR IGNORE INTO friends (account_id, friend_id, status) VALUES (?, ?, 1)",
		accepterID, requesterID); err != nil {
		return err
	}

	return tx.Commit()
}

// RemoveFriend deletes the friend relationship in both directions
func RemoveFriend(accountID, friendID int64) error {
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM friends WHERE account_id = ? AND friend_id = ?", accountID, friendID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM friends WHERE account_id = ? AND friend_id = ?", friendID, accountID); err != nil {
		return err
	}

	return tx.Commit()
}

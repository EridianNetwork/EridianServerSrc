package db

import (
	"errors"

	"eridian/internal/domain"
)

// means exactly what it says. no acc found when tried
var ErrAccountNotFound = errors.New("account not found")

// when set by main at startup supplies the can_play flag for accounts created
// through real registration. you can block this so you can run private servers and manually whitelist etc or betas etc
var NewAccountCanPlayFn func() bool

// inserts a new account into db
func CreateAccount(username, password, email string) (int64, error) {
	canPlay := false
	if NewAccountCanPlayFn != nil {
		canPlay = NewAccountCanPlayFn()
	}
	res, err := DB().Exec("INSERT INTO accounts (username, password, email, is_tester, can_play) VALUES (?, ?, ?, 1, ?)", username, password, email, canPlay)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// just allows a special account creation. extremely useful
func CreateAccountWithFlags(username, password, email string, isTester, canPlay bool) (int64, error) {
	res, err := DB().Exec("INSERT INTO accounts (username, password, email, is_tester, can_play) VALUES (?, ?, ?, ?, ?)",
		username, password, email, isTester, canPlay)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetAccountByUsername looks up an account by username, case insensitively
// "Frankity", "frankity", or "FRANKITY" must all find the same account. makes sign up process easier
func GetAccountByUsername(username string) (*domain.Account, error) {
	row := DB().QueryRow("SELECT account_id, username, password, email, token, is_banned, is_gm, is_tester, can_play, created_at, COALESCE(last_login, '') FROM accounts WHERE LOWER(username) = LOWER(?)", username)
	a := &domain.Account{}
	err := row.Scan(&a.AccountID, &a.Username, &a.Password, &a.Email, &a.Token, &a.IsBanned, &a.IsGM, &a.IsTester, &a.CanPlay, &a.CreatedAt, &a.LastLogin)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// looks up an account by ID
func GetAccountByID(id int64) (*domain.Account, error) {
	row := DB().QueryRow("SELECT account_id, username,  email, token, is_banned, is_gm, is_tester, can_play, created_at, COALESCE(last_login, '') FROM accounts WHERE account_id = ?", id)
	a := &domain.Account{}
	err := row.Scan(&a.AccountID, &a.Username, &a.Email, &a.Token, &a.IsBanned, &a.IsGM, &a.IsTester, &a.CanPlay, &a.CreatedAt, &a.LastLogin)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// looks up an account by session token
func GetAccountByToken(token string) (*domain.Account, error) {
	row := DB().QueryRow("SELECT account_id, username,  email, token, is_banned, is_gm, is_tester, can_play, created_at, COALESCE(last_login, '') FROM accounts WHERE token = ?", token)
	a := &domain.Account{}
	err := row.Scan(&a.AccountID, &a.Username, &a.Email, &a.Token, &a.IsBanned, &a.IsGM, &a.IsTester, &a.CanPlay, &a.CreatedAt, &a.LastLogin)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// flips an accounts is_tester flag. not used for live game much, but still has use
func SetAccountTester(accountID int64, isTester bool) error {
	_, err := DB().Exec("UPDATE accounts SET is_tester = ? WHERE account_id = ?", isTester, accountID)
	return err
}

// simialr to above, handy for banning users who are disruptive
func SetAccountCanPlay(accountID int64, canPlay bool) error {
	_, err := DB().Exec("UPDATE accounts SET can_play = ? WHERE account_id = ?", canPlay, accountID)
	return err
}

func CheckPassword(account *domain.Account, password string) bool {
	return account.Password == password
}

// updates an accounts session token
func UpdateAccountToken(accountID int64, token string) error {
	_, err := DB().Exec("UPDATE accounts SET token = ?, last_login = datetime('now') WHERE account_id = ?", token, accountID)
	return err
}

// checks if entered username already exists. this is necessary to avoid duplicate users
// which will most likely result in big big issues
func AccountExists(username string) (bool, error) {
	var count int
	err := DB().QueryRow("SELECT COUNT(*) FROM accounts WHERE username = ?", username).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}


func GetAccountSettings(accountID int64) (string, error) {
	var settings string
	err := DB().QueryRow("SELECT settings FROM accounts WHERE account_id = ?", accountID).Scan(&settings)
	return settings, err
}

func UpdateAccountSettings(accountID int64, settings string) error {
	_, err := DB().Exec("UPDATE accounts SET settings = ? WHERE account_id = ?", settings, accountID)
	return err
}

func ListAccounts(search string, limit, offset int) ([]*domain.Account, int, error) {
	like := "%" + search + "%"
	var total int
	if err := DB().QueryRow("SELECT COUNT(*) FROM accounts WHERE username LIKE ? OR email LIKE ?", like, like).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := DB().Query(`SELECT account_id, username, email, token, is_banned, is_gm, is_tester, can_play, created_at, COALESCE(last_login, '')
		FROM accounts WHERE username LIKE ? OR email LIKE ? ORDER BY created_at DESC LIMIT ? OFFSET ?`, like, like, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*domain.Account
	for rows.Next() {
		a := &domain.Account{}
		if err := rows.Scan(&a.AccountID, &a.Username, &a.Email, &a.Token, &a.IsBanned, &a.IsGM, &a.IsTester, &a.CanPlay, &a.CreatedAt, &a.LastLogin); err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

// hard-deletes accountID and everything that references it, in FKsafe order. careful touching this
func DeleteAccountCascade(accountID int64) error {
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	charRows, err := tx.Query("SELECT char_id FROM characters WHERE account_id=?", accountID)
	if err != nil {
		return err
	}
	var charIDs []int64
	for charRows.Next() {
		var id int64
		if err := charRows.Scan(&id); err != nil {
			charRows.Close()
			return err
		}
		charIDs = append(charIDs, id)
	}
	if err := charRows.Err(); err != nil {
		return err
	}
	charRows.Close()

	for _, charID := range charIDs {
		for _, q := range []string{
			"DELETE FROM inventory WHERE char_id=?",
			"DELETE FROM quest_progress WHERE char_id=?",
			"DELETE FROM pvp_stats WHERE char_id=?",
			"DELETE FROM achievement_progress WHERE char_id=?",
		} {
			if _, err := tx.Exec(q, charID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec("DELETE FROM char_friends WHERE char_id=? OR friend_id=?", charID, charID); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM char_ignores WHERE char_id=? OR ignored_id=?", charID, charID); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM mail WHERE sender_id=? OR receiver_id=?", charID, charID); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM messages WHERE sender_id=? OR receiver_id=?", charID, charID); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM guild_news WHERE poster_character_id=?", charID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("DELETE FROM characters WHERE account_id=?", accountID); err != nil {
		return err
	}

	guildRows, err := tx.Query("SELECT guild_id FROM guilds WHERE master_account_id=?", accountID)
	if err != nil {
		return err
	}
	var guildIDs []int64
	for guildRows.Next() {
		var id int64
		if err := guildRows.Scan(&id); err != nil {
			guildRows.Close()
			return err
		}
		guildIDs = append(guildIDs, id)
	}
	if err := guildRows.Err(); err != nil {
		return err
	}
	guildRows.Close()

	for _, guildID := range guildIDs {
		for _, q := range []string{
			"DELETE FROM guild_members WHERE guild_id=?",
			"DELETE FROM guild_applications WHERE guild_id=?",
			"DELETE FROM guild_invitations WHERE guild_id=?",
			"DELETE FROM guild_news WHERE guild_id=?",
		} {
			if _, err := tx.Exec(q, guildID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec("DELETE FROM guilds WHERE guild_id=?", guildID); err != nil {
			return err
		}
	}

	for _, q := range []string{
		"DELETE FROM warehouse WHERE account_id=?",
		"DELETE FROM guild_applications WHERE account_id=?",
		"DELETE FROM guild_members WHERE account_id=?",
		"DELETE FROM guild_invitations WHERE account_id=?",
		"DELETE FROM guild_news WHERE poster_account_id=?",
	} {
		if _, err := tx.Exec(q, accountID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("DELETE FROM friends WHERE account_id=? OR friend_id=?", accountID, accountID); err != nil {
		return err
	}

	res, err := tx.Exec("DELETE FROM accounts WHERE account_id=?", accountID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrAccountNotFound
	}
	return tx.Commit()
}

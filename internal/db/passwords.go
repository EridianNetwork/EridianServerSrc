package db

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"log"
	"strings"

	"eridian/internal/domain"

	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 10

// bcrypt only reads the first 72 bytes and newer versions refuse longer input, so long passwords are sha256-hex'd first
func bcryptInput(password string) []byte {
	if len(password) <= 72 {
		return []byte(password)
	}
	sum := sha256.Sum256([]byte(password))
	return []byte(hex.EncodeToString(sum[:]))
}

// HashPassword returns the bcrypt hash that gets stored in accounts.password
func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword(bcryptInput(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// IsPasswordHashed reports whether a stored value is already a bcrypt hash (60 chars, $2a$/$2b$/$2y$ prefix)
func IsPasswordHashed(stored string) bool {
	return len(stored) == 60 && (strings.HasPrefix(stored, "$2a$") || strings.HasPrefix(stored, "$2b$") || strings.HasPrefix(stored, "$2y$"))
}

// MigratePasswordHashes hashes every account whose password is still stored in plaintext. safe to run on every boot
func MigratePasswordHashes() error {
	rows, err := DB().Query("SELECT account_id, password FROM accounts")
	if err != nil {
		return err
	}
	type entry struct {
		id int64
		pw string
	}
	var todo []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.pw); err != nil {
			rows.Close()
			return err
		}
		if !IsPasswordHashed(e.pw) {
			todo = append(todo, e)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(todo) == 0 {
		return nil
	}
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, e := range todo {
		h, err := HashPassword(e.pw)
		if err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE accounts SET password = ? WHERE account_id = ?", h, e.id); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	log.Printf("[DB] hashed %d plaintext account password(s)", len(todo))
	return nil
}

// CheckPassword verifies a login attempt. a leftover plaintext row is still accepted once and rehashed on the spot
func CheckPassword(account *domain.Account, password string) bool {
	if IsPasswordHashed(account.Password) {
		return bcrypt.CompareHashAndPassword([]byte(account.Password), bcryptInput(password)) == nil
	}
	if subtle.ConstantTimeCompare([]byte(account.Password), []byte(password)) != 1 {
		return false
	}
	if h, err := HashPassword(password); err == nil {
		if _, err := DB().Exec("UPDATE accounts SET password = ? WHERE account_id = ?", h, account.AccountID); err == nil {
			account.Password = h
		}
	}
	return true
}

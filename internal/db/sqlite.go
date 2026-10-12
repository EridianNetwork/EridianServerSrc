package db

import (
	"database/sql"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"
)

var (
	pool   *sql.DB
	poolMu sync.RWMutex
)


func Open(path string) error {
	poolMu.Lock()
	defer poolMu.Unlock()

	if pool != nil {
		return nil
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return fmt.Errorf("enable WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return fmt.Errorf("set busy timeout: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		return fmt.Errorf("set synchronous mode: %w", err)
	}

	pool = db
	return nil
}

// closes the global connection
func Close() error {
	poolMu.Lock()
	defer poolMu.Unlock()

	if pool != nil {
		err := pool.Close()
		pool = nil
		return err
	}
	return nil
}

// returns the global connection
func DB() *sql.DB {
	poolMu.RLock()
	defer poolMu.RUnlock()

	if pool == nil {
		panic("db: not initialized, call db.Open first")
	}
	return pool
}

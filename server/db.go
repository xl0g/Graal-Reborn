package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"

	_ "modernc.org/sqlite"
	"golang.org/x/crypto/bcrypt"
)

var database *sql.DB

// initDB opens (or creates) the SQLite database and runs migrations.
func initDB(path string) error {
	var err error
	database, err = sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	database.SetMaxOpenConns(1) // SQLite is single-writer

	_, err = database.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			username      TEXT UNIQUE NOT NULL COLLATE NOCASE,
			password_hash TEXT NOT NULL,
			email         TEXT DEFAULT '',
			created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_x        REAL DEFAULT 480.0,
			last_y        REAL DEFAULT 320.0,
			gralats       INTEGER DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS sessions (
			token      TEXT PRIMARY KEY,
			user_id    INTEGER NOT NULL,
			username   TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS chat_history (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			username   TEXT NOT NULL,
			message    TEXT NOT NULL,
			sent_at    DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return err
	}
	// Non-fatal migrations for existing databases.
	_, _ = database.Exec(`ALTER TABLE users ADD COLUMN gralats INTEGER DEFAULT 0`)
	_, _ = database.Exec(`ALTER TABLE users ADD COLUMN playtime INTEGER DEFAULT 0`)
	return nil
}

// UserRecord carries the fields we need from the users table at login.
type UserRecord struct {
	ID       int64
	Name     string
	LastX    float64
	LastY    float64
	Gralats  int
	Playtime int // total seconds played
}

func dbCreateUser(username, password, email string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = database.Exec(
		`INSERT INTO users (username, password_hash, email) VALUES (?, ?, ?)`,
		username, string(hash), email,
	)
	return err
}

func dbAuthenticate(username, password string) (*UserRecord, error) {
	var u UserRecord
	var hash string
	err := database.QueryRow(
		`SELECT id, username, password_hash, last_x, last_y, gralats, COALESCE(playtime,0)
		 FROM users WHERE username = ?`, username,
	).Scan(&u.ID, &u.Name, &hash, &u.LastX, &u.LastY, &u.Gralats, &u.Playtime)
	if err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}
	return &u, nil
}

func dbCreateSession(userID int64, username string) (string, error) {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	_, err := database.Exec(
		`INSERT OR REPLACE INTO sessions (token, user_id, username) VALUES (?, ?, ?)`,
		token, userID, username,
	)
	return token, err
}

func dbValidateSession(token string) (*UserRecord, error) {
	var u UserRecord
	err := database.QueryRow(
		`SELECT u.id, u.username, u.last_x, u.last_y, u.gralats, COALESCE(u.playtime,0)
		 FROM sessions s JOIN users u ON s.user_id = u.id
		 WHERE s.token = ?`, token,
	).Scan(&u.ID, &u.Name, &u.LastX, &u.LastY, &u.Gralats, &u.Playtime)
	if err != nil {
		return nil, fmt.Errorf("invalid session")
	}
	return &u, nil
}

func dbUpdatePosition(userID int64, x, y float64) {
	database.Exec(`UPDATE users SET last_x = ?, last_y = ? WHERE id = ?`, x, y, userID)
}

// dbAddGralats credits n gralats to userID and returns the new total.
func dbAddGralats(userID int64, n int) (int, error) {
	var total int
	// RETURNING is supported by SQLite ≥ 3.35.
	err := database.QueryRow(
		`UPDATE users SET gralats = gralats + ? WHERE id = ? RETURNING gralats`,
		n, userID,
	).Scan(&total)
	if err != nil {
		// Fallback for older SQLite versions.
		database.Exec(`UPDATE users SET gralats = gralats + ? WHERE id = ?`, n, userID)
		database.QueryRow(`SELECT gralats FROM users WHERE id = ?`, userID).Scan(&total)
	}
	return total, nil
}

// dbAddPlaytime adds seconds to the playtime total for userID.
func dbAddPlaytime(userID int64, seconds int) {
	database.Exec(`UPDATE users SET playtime = COALESCE(playtime,0) + ? WHERE id = ?`, seconds, userID)
}

func dbSaveChat(username, message string) {
	database.Exec(
		`INSERT INTO chat_history (username, message) VALUES (?, ?)`,
		username, message,
	)
}

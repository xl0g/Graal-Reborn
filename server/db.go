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
	_, _ = database.Exec(`ALTER TABLE users ADD COLUMN gralats  INTEGER DEFAULT 0`)
	_, _ = database.Exec(`ALTER TABLE users ADD COLUMN playtime INTEGER DEFAULT 0`)
	_, _ = database.Exec(`ALTER TABLE users ADD COLUMN body     TEXT DEFAULT ''`)
	_, _ = database.Exec(`ALTER TABLE users ADD COLUMN head     TEXT DEFAULT ''`)
	_, _ = database.Exec(`ALTER TABLE users ADD COLUMN hat      TEXT DEFAULT ''`)
	_, _ = database.Exec(`ALTER TABLE users ADD COLUMN shield   TEXT DEFAULT ''`)
	_, _ = database.Exec(`ALTER TABLE users ADD COLUMN sword    TEXT DEFAULT ''`)
	_, _ = database.Exec(`ALTER TABLE users ADD COLUMN is_admin INTEGER DEFAULT 0`)

	// Inventory and world items tables.
	_, _ = database.Exec(`
		CREATE TABLE IF NOT EXISTS inventory (
			user_id  INTEGER NOT NULL,
			item_id  TEXT    NOT NULL,
			quantity INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY (user_id, item_id)
		)`)
	_, _ = database.Exec(`
		CREATE TABLE IF NOT EXISTS world_items (
			id          TEXT    PRIMARY KEY,
			name        TEXT    NOT NULL,
			sprite_path TEXT    NOT NULL DEFAULT '',
			x           REAL    NOT NULL DEFAULT 0,
			y           REAL    NOT NULL DEFAULT 0,
			price       INTEGER NOT NULL DEFAULT 0,
			item_id     TEXT    NOT NULL DEFAULT '',
			map_name    TEXT    NOT NULL DEFAULT 'maps/GraalRebornMap.tmx'
		)`)

	// Friends system
	_, _ = database.Exec(`
		CREATE TABLE IF NOT EXISTS friends (
			user_id    INTEGER NOT NULL,
			friend_id  INTEGER NOT NULL,
			status     TEXT    NOT NULL DEFAULT 'pending',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, friend_id)
		)`)

	// Guilds system
	_, _ = database.Exec(`
		CREATE TABLE IF NOT EXISTS guilds (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT    UNIQUE NOT NULL,
			tag         TEXT    NOT NULL DEFAULT '',
			leader_id   INTEGER NOT NULL,
			description TEXT    NOT NULL DEFAULT '',
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`)
	_, _ = database.Exec(`
		CREATE TABLE IF NOT EXISTS guild_members (
			guild_id   INTEGER NOT NULL,
			user_id    INTEGER NOT NULL,
			rank       TEXT    NOT NULL DEFAULT 'member',
			joined_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (guild_id, user_id)
		)`)
	_, _ = database.Exec(`ALTER TABLE users ADD COLUMN guild_id INTEGER DEFAULT 0`)

	// Quest progress
	_, _ = database.Exec(`
		CREATE TABLE IF NOT EXISTS player_quests (
			user_id    INTEGER NOT NULL,
			quest_id   TEXT    NOT NULL,
			progress   INTEGER NOT NULL DEFAULT 0,
			completed  INTEGER NOT NULL DEFAULT 0,
			started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, quest_id)
		)`)

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
	Body     string
	Head     string
	Hat      string
	Shield   string
	Sword    string
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
		`SELECT id, username, password_hash, last_x, last_y, gralats, COALESCE(playtime,0),
		        COALESCE(body,''), COALESCE(head,''), COALESCE(hat,''),
		        COALESCE(shield,''), COALESCE(sword,'')
		 FROM users WHERE username = ?`, username,
	).Scan(&u.ID, &u.Name, &hash, &u.LastX, &u.LastY, &u.Gralats, &u.Playtime, &u.Body, &u.Head, &u.Hat, &u.Shield, &u.Sword)
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
		`SELECT u.id, u.username, u.last_x, u.last_y, u.gralats, COALESCE(u.playtime,0),
		        COALESCE(u.body,''), COALESCE(u.head,''), COALESCE(u.hat,''),
		        COALESCE(u.shield,''), COALESCE(u.sword,'')
		 FROM sessions s JOIN users u ON s.user_id = u.id
		 WHERE s.token = ?`, token,
	).Scan(&u.ID, &u.Name, &u.LastX, &u.LastY, &u.Gralats, &u.Playtime, &u.Body, &u.Head, &u.Hat, &u.Shield, &u.Sword)
	if err != nil {
		return nil, fmt.Errorf("invalid session")
	}
	return &u, nil
}

func dbSaveCosmetics(userID int64, body, head, hat, shield, sword string) {
	database.Exec(`UPDATE users SET body = ?, head = ?, hat = ?, shield = ?, sword = ? WHERE id = ?`,
		body, head, hat, shield, sword, userID)
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

// ──────────────────────────────────────────────────────────────
// Admin helpers
// ──────────────────────────────────────────────────────────────

func dbIsAdmin(userID int64) bool {
	var v int
	database.QueryRow(`SELECT COALESCE(is_admin,0) FROM users WHERE id=?`, userID).Scan(&v)
	return v != 0
}

func dbGetPlayerIDByName(name string) (int64, error) {
	var id int64
	err := database.QueryRow(`SELECT id FROM users WHERE username=?`, name).Scan(&id)
	return id, err
}

// dbDeductGralats removes amount from userID's gralats if sufficient.
// Returns new total or an error if insufficient.
func dbDeductGralats(userID int64, amount int) (int, error) {
	var newTotal int
	err := database.QueryRow(
		`UPDATE users SET gralats = gralats - ? WHERE id = ? AND gralats >= ? RETURNING gralats`,
		amount, userID, amount,
	).Scan(&newTotal)
	if err != nil {
		return -1, fmt.Errorf("insufficient gralats")
	}
	return newTotal, nil
}

// ──────────────────────────────────────────────────────────────
// Inventory
// ──────────────────────────────────────────────────────────────

type inventoryRow struct {
	ItemID   string
	Quantity int
}

func dbGetInventory(userID int64) []inventoryRow {
	rows, err := database.Query(`SELECT item_id, quantity FROM inventory WHERE user_id=?`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []inventoryRow
	for rows.Next() {
		var r inventoryRow
		if rows.Scan(&r.ItemID, &r.Quantity) == nil {
			result = append(result, r)
		}
	}
	return result
}

func dbGiveItem(userID int64, itemID string, qty int) {
	database.Exec(`
		INSERT INTO inventory (user_id, item_id, quantity) VALUES (?,?,?)
		ON CONFLICT(user_id, item_id) DO UPDATE SET quantity = quantity + ?`,
		userID, itemID, qty, qty)
}

// dbRemoveItem removes one copy of itemID from a player's inventory.
// Returns false if the player didn't have the item.
func dbRemoveItem(userID int64, itemID string) bool {
	row := database.QueryRow(
		`SELECT quantity FROM inventory WHERE user_id=? AND item_id=?`, userID, itemID)
	var qty int
	if row.Scan(&qty) != nil || qty <= 0 {
		return false
	}
	if qty == 1 {
		database.Exec(`DELETE FROM inventory WHERE user_id=? AND item_id=?`, userID, itemID)
	} else {
		database.Exec(`UPDATE inventory SET quantity=quantity-1 WHERE user_id=? AND item_id=?`, userID, itemID)
	}
	return true
}

// ──────────────────────────────────────────────────────────────
// World items (admin-spawned)
// ──────────────────────────────────────────────────────────────

type worldItemDB struct {
	ID, Name, SpritePath, ItemID, MapName string
	X, Y                                  float64
	Price                                 int
}

func dbLoadWorldItems() []worldItemDB {
	rows, err := database.Query(
		`SELECT id, name, sprite_path, x, y, price, item_id, map_name FROM world_items`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []worldItemDB
	for rows.Next() {
		var w worldItemDB
		if rows.Scan(&w.ID, &w.Name, &w.SpritePath, &w.X, &w.Y,
			&w.Price, &w.ItemID, &w.MapName) == nil {
			result = append(result, w)
		}
	}
	return result
}

func dbSaveWorldItem(w worldItemDB) {
	database.Exec(
		`INSERT OR REPLACE INTO world_items
		 (id, name, sprite_path, x, y, price, item_id, map_name)
		 VALUES (?,?,?,?,?,?,?,?)`,
		w.ID, w.Name, w.SpritePath, w.X, w.Y, w.Price, w.ItemID, w.MapName)
}

func dbRemoveWorldItem(id string) {
	database.Exec(`DELETE FROM world_items WHERE id=?`, id)
}

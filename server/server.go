package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	mrand "math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	_ "modernc.org/sqlite"
	"golang.org/x/crypto/bcrypt"
)

// ============================================================
// TYPES
// ============================================================

type PlayerState struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Dir    int     `json:"dir"`
	Moving bool    `json:"moving"`
}

type NPCState struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Dir     int     `json:"dir"`
	Moving  bool    `json:"moving"`
	NPCType int     `json:"npcType"`
}

// ============================================================
// DATABASE
// ============================================================

var database *sql.DB

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
			last_x        REAL DEFAULT 800.0,
			last_y        REAL DEFAULT 640.0
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
	return err
}

type UserRecord struct {
	ID    int64
	Name  string
	LastX float64
	LastY float64
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
		`SELECT id, username, password_hash, last_x, last_y FROM users WHERE username = ?`,
		username,
	).Scan(&u.ID, &u.Name, &hash, &u.LastX, &u.LastY)
	if err != nil {
		return nil, fmt.Errorf("identifiants invalides")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, fmt.Errorf("identifiants invalides")
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
		`SELECT u.id, u.username, u.last_x, u.last_y
		 FROM sessions s JOIN users u ON s.user_id = u.id
		 WHERE s.token = ?`, token,
	).Scan(&u.ID, &u.Name, &u.LastX, &u.LastY)
	if err != nil {
		return nil, fmt.Errorf("session invalide")
	}
	return &u, nil
}

func dbUpdatePosition(userID int64, x, y float64) {
	database.Exec(`UPDATE users SET last_x = ?, last_y = ? WHERE id = ?`, x, y, userID)
}

func dbSaveChat(username, message string) {
	database.Exec(`INSERT INTO chat_history (username, message) VALUES (?, ?)`, username, message)
}

// ============================================================
// NPC SYSTEM
// ============================================================

const (
	mapWidth  = 1600.0
	mapHeight = 1280.0
)

type NPC struct {
	state   NPCState
	homeX   float64
	homeY   float64
	speed   float64
	targetX float64
	targetY float64
	timer   float64
}

func newNPC(id string, name string, x, y float64, npcType int) *NPC {
	return &NPC{
		state: NPCState{
			ID: id, Name: name,
			X: x, Y: y,
			Dir: mrand.Intn(4), Moving: false,
			NPCType: npcType,
		},
		homeX: x, homeY: y,
		speed:   70.0 + mrand.Float64()*50.0,
		targetX: x, targetY: y,
		timer:   mrand.Float64() * 3.0,
	}
}

func (n *NPC) update(dt float64) {
	dx := n.targetX - n.state.X
	dy := n.targetY - n.state.Y
	dist := math.Sqrt(dx*dx + dy*dy)

	if dist < 4.0 {
		n.state.Moving = false
		n.timer -= dt
		if n.timer <= 0 {
			// New random target near home position
			angle := mrand.Float64() * math.Pi * 2
			radius := 80.0 + mrand.Float64()*180.0
			n.targetX = n.homeX + math.Cos(angle)*radius
			n.targetY = n.homeY + math.Sin(angle)*radius
			// Clamp to map
			if n.targetX < 50 {
				n.targetX = 50
			}
			if n.targetX > mapWidth-50 {
				n.targetX = mapWidth - 50
			}
			if n.targetY < 50 {
				n.targetY = 50
			}
			if n.targetY > mapHeight-50 {
				n.targetY = mapHeight - 50
			}
			n.timer = 1.5 + mrand.Float64()*4.0
			n.state.Moving = true
		}
	} else {
		n.state.Moving = true
		step := n.speed * dt
		n.state.X += (dx / dist) * step
		n.state.Y += (dy / dist) * step
		// Direction from movement
		if math.Abs(dx) > math.Abs(dy) {
			if dx > 0 {
				n.state.Dir = 3
			} else {
				n.state.Dir = 1
			}
		} else {
			if dy > 0 {
				n.state.Dir = 2
			} else {
				n.state.Dir = 0
			}
		}
	}
}

// ============================================================
// HUB (WebSocket connection manager)
// ============================================================

type Client struct {
	conn     *websocket.Conn
	send     chan []byte
	hub      *Hub
	userID   int64
	name     string
	playerID string
	state    PlayerState
}

type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]bool
	npcs    []*NPC
}

var globalHub *Hub

func newHub() *Hub {
	h := &Hub{
		clients: make(map[*Client]bool),
	}

	npcDefs := []struct {
		name    string
		x, y    float64
		npcType int
	}{
		{"Thibaut le Villageois", 350, 320, 0},
		{"Marceline la Marchande", 850, 420, 1},
		{"Galaad le Garde", 1250, 280, 2},
		{"Eleonore la Voyageuse", 550, 850, 3},
		{"Baptiste le Fermier", 1050, 720, 4},
		{"Sylvain l'Aubergiste", 720, 560, 1},
		{"Noemie la Sorcierre", 300, 900, 0},
	}

	for i, def := range npcDefs {
		h.npcs = append(h.npcs, newNPC(
			fmt.Sprintf("npc_%d", i),
			def.name, def.x, def.y, def.npcType,
		))
	}

	return h
}

func (h *Hub) register(c *Client) {
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
	h.broadcastSystem(fmt.Sprintf("%s a rejoint le monde!", c.name))
	log.Printf("[HUB] %s connecte (ID: %s)", c.name, c.playerID)
}

func (h *Hub) unregister(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	dbUpdatePosition(c.userID, c.state.X, c.state.Y)
	h.broadcastSystem(fmt.Sprintf("%s a quitte le monde.", c.name))
	log.Printf("[HUB] %s deconnecte", c.name)
}

func (h *Hub) broadcastRaw(data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		select {
		case c.send <- data:
		default:
		}
	}
}

func (h *Hub) broadcast(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.broadcastRaw(data)
}

func (h *Hub) broadcastSystem(msg string) {
	h.broadcast(map[string]string{"type": "system", "msg": msg})
}

func (h *Hub) getGameState() ([]PlayerState, []NPCState) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	players := make([]PlayerState, 0, len(h.clients))
	for c := range h.clients {
		players = append(players, c.state)
	}

	npcs := make([]NPCState, len(h.npcs))
	for i, n := range h.npcs {
		npcs[i] = n.state
	}
	return players, npcs
}

func (h *Hub) runGameLoop() {
	ticker := time.NewTicker(time.Second / 60) // 60 Hz
	defer ticker.Stop()
	lastTime := time.Now()

	for range ticker.C {
		now := time.Now()
		dt := now.Sub(lastTime).Seconds()
		lastTime = now
		if dt > 0.1 {
			dt = 0.1
		}

		// Update NPC AI
		h.mu.Lock()
		for _, n := range h.npcs {
			n.update(dt)
		}
		h.mu.Unlock()

		// Broadcast state to all clients
		players, npcs := h.getGameState()
		h.broadcast(map[string]interface{}{
			"type":    "state",
			"players": players,
			"npcs":    npcs,
		})
	}
}

// ============================================================
// WEBSOCKET CLIENT HANDLER
// ============================================================

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("[WS] Upgrade error:", err)
		return
	}

	// First message must be auth
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var authMsg struct {
		Type  string `json:"type"`
		Token string `json:"token"`
	}
	if err := conn.ReadJSON(&authMsg); err != nil || authMsg.Type != "auth" {
		conn.WriteJSON(map[string]string{"type": "auth_error", "msg": "Authentification requise"})
		conn.Close()
		return
	}
	conn.SetReadDeadline(time.Time{})

	user, err := dbValidateSession(authMsg.Token)
	if err != nil {
		conn.WriteJSON(map[string]string{"type": "auth_error", "msg": err.Error()})
		conn.Close()
		return
	}

	playerID := fmt.Sprintf("player_%d", user.ID)
	client := &Client{
		conn:     conn,
		send:     make(chan []byte, 512),
		hub:      globalHub,
		userID:   user.ID,
		name:     user.Name,
		playerID: playerID,
		state: PlayerState{
			ID:     playerID,
			Name:   user.Name,
			X:      user.LastX,
			Y:      user.LastY,
			Dir:    2,
			Moving: false,
		},
	}

	// Send auth OK
	conn.WriteJSON(map[string]interface{}{
		"type": "auth_ok",
		"id":   playerID,
		"name": user.Name,
		"x":    user.LastX,
		"y":    user.LastY,
	})

	globalHub.register(client)
	defer func() {
		globalHub.unregister(client)
		conn.Close()
	}()

	// Writer goroutine
	go func() {
		ticker := time.NewTicker(54 * time.Second) // ping
		defer ticker.Stop()
		for {
			select {
			case data := <-client.send:
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
					return
				}
			case <-ticker.C:
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}()

	// Read loop
	conn.SetReadLimit(4096)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, rawBytes, err := conn.ReadMessage()
		if err != nil {
			break
		}
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		var base struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(rawBytes, &base); err != nil {
			continue
		}

		switch base.Type {
		case "move":
			var msg struct {
				X      float64 `json:"x"`
				Y      float64 `json:"y"`
				Dir    int     `json:"dir"`
				Moving bool    `json:"moving"`
			}
			if err := json.Unmarshal(rawBytes, &msg); err == nil {
				globalHub.mu.Lock()
				client.state.X = msg.X
				client.state.Y = msg.Y
				client.state.Dir = msg.Dir
				client.state.Moving = msg.Moving
				globalHub.mu.Unlock()
			}

		case "chat":
			var msg struct {
				Msg string `json:"msg"`
			}
			if err := json.Unmarshal(rawBytes, &msg); err == nil {
				text := strings.TrimSpace(msg.Msg)
				if len(text) > 0 && len(text) <= 200 {
					dbSaveChat(client.name, text)
					globalHub.broadcast(map[string]interface{}{
						"type": "chat",
						"from": client.name,
						"msg":  text,
					})
				}
			}
		}
	}
}

// ============================================================
// HTTP REST HANDLERS
// ============================================================

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON invalide"})
		return
	}

	body.Username = strings.TrimSpace(body.Username)
	if len(body.Username) < 3 || len(body.Username) > 20 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Nom d'utilisateur: 3-20 caracteres"})
		return
	}
	if len(body.Password) < 6 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Mot de passe: minimum 6 caracteres"})
		return
	}

	if err := dbCreateUser(body.Username, body.Password, body.Email); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Ce nom d'utilisateur est deja pris"})
		return
	}
	log.Printf("[AUTH] Nouvel utilisateur: %s", body.Username)

	user, err := dbAuthenticate(body.Username, body.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Erreur interne"})
		return
	}
	token, err := dbCreateSession(user.ID, user.Name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Erreur interne"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"token": token, "username": user.Name})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON invalide"})
		return
	}

	user, err := dbAuthenticate(strings.TrimSpace(body.Username), body.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	token, err := dbCreateSession(user.ID, user.Name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Erreur interne"})
		return
	}
	log.Printf("[AUTH] Connexion: %s", user.Name)
	writeJSON(w, http.StatusOK, map[string]interface{}{"token": token, "username": user.Name})
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// ============================================================
// MAIN
// ============================================================

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	dbPath := "game.db"
	if p := os.Getenv("DB_PATH"); p != "" {
		dbPath = p
	}

	if err := initDB(dbPath); err != nil {
		log.Fatalf("[DB] Erreur initialisation: %v", err)
	}
	log.Println("[DB] Base de donnees initialisee:", dbPath)

	globalHub = newHub()
	go globalHub.runGameLoop()
	log.Println("[HUB] Boucle de jeu demarree (20 Hz)")

	mux := http.NewServeMux()

	// API
	mux.HandleFunc("/api/register", handleRegister)
	mux.HandleFunc("/api/login", handleLogin)
	mux.HandleFunc("/ws", handleWebSocket)

	// Serve game assets (for WASM client)
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir("assets"))))

	// Serve web client (WASM build)
	staticDir := "server/static"
	if _, err := os.Stat(staticDir); err == nil {
		mux.Handle("/", http.FileServer(http.Dir(staticDir)))
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, "Go Multiplayer Server - Lancez le client natif ou compilez en WASM.")
		})
	}

	handler := corsMiddleware(mux)

	addr := ":" + port
	log.Printf("╔══════════════════════════════════════╗")
	log.Printf("║     GO MULTIPLAYER SERVER            ║")
	log.Printf("╠══════════════════════════════════════╣")
	log.Printf("║  HTTP  : http://localhost%s         ║", addr)
	log.Printf("║  WS    : ws://localhost%s/ws        ║", addr)
	log.Printf("║  API   : /api/register  /api/login  ║")
	log.Printf("╚══════════════════════════════════════╝")

	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}

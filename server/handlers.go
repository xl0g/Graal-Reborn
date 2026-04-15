package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if len(body.Username) < 3 || len(body.Username) > 20 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Username: 3-20 characters"})
		return
	}
	if len(body.Password) < 6 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Password: minimum 6 characters"})
		return
	}
	if err := dbCreateUser(body.Username, body.Password, body.Email); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Username already taken"})
		return
	}
	log.Printf("[AUTH] New user: %s", body.Username)

	user, err := dbAuthenticate(body.Username, body.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal error"})
		return
	}
	token, err := dbCreateSession(user.ID, user.Name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal error"})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
		return
	}
	user, err := dbAuthenticate(strings.TrimSpace(body.Username), body.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	token, err := dbCreateSession(user.ID, user.Name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal error"})
		return
	}
	log.Printf("[AUTH] Login: %s", user.Name)
	writeJSON(w, http.StatusOK, map[string]interface{}{"token": token, "username": user.Name})
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// handleAssetsList lists .png/.gif files in a subdirectory of assets/offline/levels/.
// Query param: dir = "bodies" | "heads" | "hats" | "shields" | "swords"
// Returns a JSON array of filenames (no path prefix).
func handleAssetsList(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	// Whitelist allowed subdirectories — prevent path traversal.
	allowed := map[string]bool{
		"bodies": true, "heads": true, "hats": true,
		"shields": true, "swords": true,
	}
	if !allowed[dir] {
		http.Error(w, "invalid dir", http.StatusBadRequest)
		return
	}
	base := filepath.Join("assets", "offline", "levels", dir)
	entries, err := os.ReadDir(base)
	if err != nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext == ".png" || ext == ".gif" {
			files = append(files, e.Name())
		}
	}
	writeJSON(w, http.StatusOK, files)
}

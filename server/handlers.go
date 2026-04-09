package main

import (
	"encoding/json"
	"log"
	"net/http"
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

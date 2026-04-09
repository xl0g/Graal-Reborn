package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

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
	log.Println("[HUB] Boucle de jeu demarree (60 Hz)")

	mux := http.NewServeMux()
	mux.HandleFunc("/api/register", handleRegister)
	mux.HandleFunc("/api/login", handleLogin)
	mux.HandleFunc("/ws", handleWebSocket)
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir("assets"))))

	staticDir := "server/static"
	if _, err := os.Stat(staticDir); err == nil {
		mux.Handle("/", http.FileServer(http.Dir(staticDir)))
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, "Go Multiplayer Server — lancez le client natif ou compilez en WASM.")
		})
	}

	addr := ":" + port
	log.Printf("╔══════════════════════════════════════╗")
	log.Printf("║     GO MULTIPLAYER SERVER            ║")
	log.Printf("╠══════════════════════════════════════╣")
	log.Printf("║  HTTP  : http://localhost%s         ║", addr)
	log.Printf("║  WS    : ws://localhost%s/ws        ║", addr)
	log.Printf("║  API   : /api/register  /api/login  ║")
	log.Printf("╚══════════════════════════════════════╝")

	if err := http.ListenAndServe(addr, corsMiddleware(mux)); err != nil {
		log.Fatal(err)
	}
}

package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

func main() {
	setAdmin := flag.String("setadmin", "", "Set a user as admin by username and exit")
	flag.Parse()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	dbPath := "game.db"
	if p := os.Getenv("DB_PATH"); p != "" {
		dbPath = p
	}

	if err := initDB(dbPath); err != nil {
		log.Fatalf("[DB] Initialization error: %v", err)
	}
	log.Println("[DB] Database initialized:", dbPath)

	// -setadmin: grant admin rights to a user and exit immediately.
	if *setAdmin != "" {
		res, err := database.Exec(`UPDATE users SET is_admin=1 WHERE username=?`, *setAdmin)
		if err != nil {
			log.Fatalf("[setadmin] DB error: %v", err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			log.Fatalf("[setadmin] User %q not found in database", *setAdmin)
		}
		log.Printf("[setadmin] User %q is now an admin.", *setAdmin)
		return
	}

	globalHub = newHub()
	go globalHub.runGameLoop()
	log.Println("[HUB] Game loop started (60 Hz)")

	mux := http.NewServeMux()
	mux.HandleFunc("/api/register", handleRegister)
	mux.HandleFunc("/api/login", handleLogin)
	mux.HandleFunc("/api/assets/list", handleAssetsList)
	mux.HandleFunc("/ws", handleWebSocket)
	// Game asset directories (served relative to the project root where the
	// server binary is expected to run).
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir("assets"))))
	mux.Handle("/Assets/", http.StripPrefix("/Assets/", http.FileServer(http.Dir("Assets"))))
	mux.Handle("/GANITEMPLATE/", http.StripPrefix("/GANITEMPLATE/", http.FileServer(http.Dir("GANITEMPLATE"))))

	staticDir := "server/static"
	staticFS := http.FileServer(http.Dir(staticDir))

	// Catch-all: serve game assets from project root (.tmx, .tsx, .png…)
	// then fall back to server/static (index.html, game.wasm, wasm_exec.js).
	// Allowed root-level extensions to avoid exposing source code / DB.
	rootAllowed := map[string]bool{
		".tmx": true,
		".tsx": true,
		".png": true,
		".gif": true,
		".wav": true,
		".mp3": true,
		".ogg": true,
	}
	rootFS := http.FileServer(http.Dir("."))

	if _, err := os.Stat(staticDir); err == nil {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// Serve root-level game data files by extension.
			path := r.URL.Path
			dot := strings.LastIndex(path, ".")
			if dot >= 0 && rootAllowed[strings.ToLower(path[dot:])] {
				// Only allow files directly at root (no path traversal).
				if !strings.Contains(path[1:], "/") {
					rootFS.ServeHTTP(w, r)
					return
				}
			}
			// Everything else (index.html, game.wasm, wasm_exec.js, …) from static.
			staticFS.ServeHTTP(w, r)
		})
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, "Go Multiplayer Server — run the native client or compile to WASM.")
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

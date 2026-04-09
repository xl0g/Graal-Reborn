# Go Multiplayer v0.1

Jeu multijoueur en temps réel écrit en Go avec Ebiten. Fonctionne en natif sur Linux et dans le navigateur (WebAssembly).

---

## Fonctionnalités

- Inscription / connexion avec comptes persistants (SQLite)
- Multijoueur en temps réel — positions synchronisées à 60 Hz
- Interpolation côté client — mouvement fluide sans téléportation
- Chat en jeu diffusé à tous les joueurs connectés
- 7 NPCs avec IA de déambulation dans leur zone
- Noms flottants au-dessus de chaque entité
- Menus Zelda-like avec fond étoilé et panneaux dorés
- Compatible Linux natif et navigateur (WASM)

---

## Lancement rapide

### 1. Compiler

```bash
./build.sh
```

Génère `game-server` et `game-client`.

### 2. Démarrer le serveur

```bash
./game-server
```

La base de données `game.db` est créée automatiquement.

### 3. Lancer le client

```bash
./game-client

# Serveur distant :
./game-client -server monserveur.com:8080
```

---

## Version web (WASM)

```bash
./build_web.sh   # compile game.wasm + copie wasm_exec.js
./game-server    # sert aussi le client web sur le même port
```

Ouvrir **http://localhost:8080** dans le navigateur.

---

## Contrôles

| Touche | Action |
|--------|--------|
| `ZQSD` / Flèches | Se déplacer |
| `T` | Ouvrir le chat |
| `Entrée` | Envoyer le message / valider un formulaire |
| `Tab` | Changer de champ dans les menus |
| `Échap` | Fermer le chat / retour au menu |

---

## Architecture

```
.
├── main.go              Point d'entrée client
├── game.go              Boucle de jeu, machine à états
├── character.go         Rendu + interpolation des entités
├── chat.go              Overlay chat
├── menu.go              Menus (accueil, connexion, inscription)
├── ui.go                Widgets (TextInput, Button, DrawPanel…)
├── network.go           WebSocket — interface partagée
├── network_native.go    WebSocket natif  (gorilla, !js)
├── network_js.go        WebSocket WASM   (syscall/js)
├── types.go             Types de messages JSON
├── assets/              Sprites (character, head, tiles)
└── server/
    ├── server.go        HTTP + WebSocket + NPC + SQLite
    └── static/          Fichiers web (index.html, game.wasm…)
```

### Protocole WebSocket (JSON)

| Direction | Type | Description |
|-----------|------|-------------|
| C → S | `auth` | Token de session après connexion |
| C → S | `move` | Position + direction + état |
| C → S | `chat` | Message de chat |
| S → C | `auth_ok` | Confirmation + position initiale |
| S → C | `state` | État de tous les joueurs et NPCs (60 Hz) |
| S → C | `chat` | Message diffusé |
| S → C | `system` | Notification (connexion, déconnexion) |

---

## Variables d'environnement (serveur)

| Variable | Défaut | Description |
|----------|--------|-------------|
| `PORT` | `8080` | Port d'écoute |
| `DB_PATH` | `game.db` | Chemin de la base SQLite |

---

## Dépendances

**Serveur** — `github.com/gorilla/websocket` · `modernc.org/sqlite` · `golang.org/x/crypto`

**Client** — `github.com/hajimehoshi/ebiten/v2` · `github.com/gorilla/websocket`

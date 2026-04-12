package main

import (
	"encoding/json"
	"fmt"
	"time"
)

// ──────────────────────────────────────────────────────────────
// Network message processing
// ──────────────────────────────────────────────────────────────

func (g *Game) processNetwork() {
	if g.conn == nil {
		return
	}
	for {
		data, ok := g.conn.TryReceive()
		if !ok {
			break
		}
		g.handleServerMsg(data)
	}
}

func (g *Game) handleServerMsg(data []byte) {
	var msg ServerMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	switch msg.Type {
	case "auth_ok":
		g.localID = msg.ID
		g.localGralats = msg.GralatN
		g.localPlaytime = msg.Playtime
		g.isAdmin = msg.IsAdmin
		g.sessionStart = time.Now()
		if g.localChar != nil {
			g.localChar.X, g.localChar.Y = msg.X, msg.Y
			g.localChar.TargetX, g.localChar.TargetY = msg.X, msg.Y
		}
		if msg.Body != "" || msg.Head != "" || msg.Hat != "" || msg.Shield != "" || msg.Sword != "" {
			g.cosmeticMenu.SetByFilenames(msg.Body, msg.Head, msg.Hat, msg.Shield, msg.Sword)
		}
		g.sendCosmetics()
		if g.currentMapName != "" && g.conn != nil {
			g.conn.SendJSON(map[string]string{"type": "change_map", "map": g.currentMapName})
		}
		g.chat.AddMessage("", fmt.Sprintf("Connected as %s", msg.Name), true)

	case "auth_error":
		g.chat.AddMessage("", "Auth error: "+msg.Msg, true)
		g.disconnect()
		g.state = StateMainMenu

	case "state":
		g.mu.Lock()

		// Remote players
		seen := make(map[string]bool)
		for _, p := range msg.Players {
			if p.ID == g.localID {
				continue
			}
			seen[p.ID] = true
			if ch, ok := g.otherPlayers[p.ID]; ok {
				ch.TargetX, ch.TargetY = p.X, p.Y
				ch.Dir = p.Dir
				ch.Moving = p.Moving
				ch.Gralats = p.Gralats
				ch.Playtime = p.Playtime
				if p.MaxHP > 0 {
					ch.MaxHP = p.MaxHP
					ch.HP = p.HP
				}
				ch.SetCosmetics(p.Body, p.Head, p.Hat, p.Shield, p.Sword)
				// Mount / ride sync
				ch.Mounted = p.Mounted
				if p.Mounted || p.AnimState == AnimRide {
					ch.AnimState = AnimRide
					ch.Mounted = true
				} else if ch.AnimState == AnimRide && !p.Mounted {
					ch.AnimState = AnimIdle
					ch.Mounted = false
				}
				// Sword
				if p.AnimState == AnimSword && ch.AnimState != AnimSword {
					ch.StartSword()
				}
				// Sit
				if p.AnimState == AnimSit {
					ch.AnimState = AnimSit
				} else if ch.AnimState == AnimSit && p.AnimState != AnimSit {
					ch.AnimState = AnimIdle
				}
				// Grab
				if p.AnimState == AnimGrab && ch.AnimState != AnimGrab {
					ch.AnimState = AnimGrab
				} else if ch.AnimState == AnimGrab && p.AnimState != AnimGrab {
					ch.AnimState = AnimIdle
				}
				// Dead / respawn
				if p.AnimState == AnimDead && ch.AnimState != AnimDead {
					ch.AnimState = AnimDead
					ch.hurtTimer = 0
					ch.knockTimer = 0
				} else if ch.AnimState == AnimDead && p.AnimState != AnimDead {
					ch.AnimState = AnimIdle
					ch.X, ch.Y = p.X, p.Y
					ch.TargetX, ch.TargetY = p.X, p.Y
				}
				// Push
				if p.AnimState == AnimPush && ch.AnimState != AnimPush && ch.AnimState != AnimDead {
					ch.AnimState = AnimPush
				} else if ch.AnimState == AnimPush && p.AnimState != AnimPush {
					ch.AnimState = AnimIdle
				}
				// Hurt
				if p.AnimState == AnimHurt && ch.AnimState != AnimHurt && ch.AnimState != AnimDead {
					ch.hurtTimer = 0.30
					ch.AnimState = AnimHurt
				}
				// Item anims
				itemAnims := map[string]bool{
					AnimClassicJuggle: true, AnimPompoms: true, AnimJuggle: true,
				}
				if itemAnims[p.AnimState] && !itemAnims[ch.AnimState] {
					ch.AnimState = p.AnimState
				} else if itemAnims[ch.AnimState] && !itemAnims[p.AnimState] {
					ch.AnimState = AnimIdle
				}
			} else {
				ch := NewCharacter(g.bodyImg, g.headImg, p.X, p.Y, p.Name, false, 0)
				ch.Gralats = p.Gralats
				ch.Playtime = p.Playtime
				ch.Mounted = p.Mounted
				if p.MaxHP > 0 {
					ch.MaxHP = p.MaxHP
					ch.HP = p.HP
				}
				if p.Mounted {
					ch.AnimState = AnimRide
				}
				ch.SetCosmetics(p.Body, p.Head, p.Hat, p.Shield, p.Sword)
				g.otherPlayers[p.ID] = ch
			}
		}
		for id := range g.otherPlayers {
			if !seen[id] {
				delete(g.otherPlayers, id)
			}
		}

		// NPCs
		seenNPC := make(map[string]bool)
		for _, n := range msg.NPCs {
			seenNPC[n.ID] = true
			if ch, ok := g.npcs[n.ID]; ok {
				ch.TargetX, ch.TargetY = n.X, n.Y
				ch.Dir = n.Dir
				ch.Moving = n.Moving
				ch.HP = n.HP
				ch.MaxHP = n.MaxHP
				if n.AnimState == "dead" && ch.AnimState != AnimDead {
					ch.AnimState = AnimDead
				} else if n.AnimState == "" && ch.AnimState == AnimDead {
					ch.AnimState = AnimIdle
				}
			} else {
				ch := NewCharacter(g.bodyImg, g.headImg, n.X, n.Y, n.Name, true, n.NPCType)
				ch.HP = n.HP
				ch.MaxHP = n.MaxHP
				if n.AnimState == "dead" {
					ch.AnimState = AnimDead
				}
				g.npcs[n.ID] = ch
			}
		}
		for id := range g.npcs {
			if !seenNPC[id] {
				delete(g.npcs, id)
			}
		}
		g.mu.Unlock()

		// World gralats
		g.grMu.Lock()
		g.worldGralats = msg.Gralats
		g.grMu.Unlock()

		// World items
		if msg.WorldItems != nil {
			g.mu.Lock()
			g.worldItems = msg.WorldItems
			g.mu.Unlock()
			g.adminMenu.SetWorldItems(msg.WorldItems)
		}

	case "chat":
		g.chat.AddMessage(msg.From, msg.Msg, false)
		g.mu.Lock()
		for _, p := range g.otherPlayers {
			if p.Name == msg.From {
				p.SetChatMsg(msg.Msg)
				if code := containsEmoji(msg.Msg); code != "" {
					if img := emojiImageFor(code); img != nil {
						p.SetEmoji(img)
					}
				}
				break
			}
		}
		g.mu.Unlock()

	case "system":
		g.chat.AddMessage("", msg.Msg, true)

	case "gralat_update":
		g.localGralats = msg.GralatN

	case "npc_dialog":
		g.npcDialog = msg.Msg
		g.npcGralatN = msg.GralatN
		if msg.GralatN > 0 {
			g.localGralats += msg.GralatN
		}

	case "npc_damage":
		g.mu.Lock()
		if npc, ok := g.npcs[msg.NPCID]; ok {
			npc.HP = msg.HP
			if msg.Killed {
				npc.AnimState = AnimDead
			}
		}
		g.mu.Unlock()
		if msg.Killed {
			g.chat.AddMessage("", "You defeated an enemy!", true)
		}

	case "mount_ok":
		if g.localChar != nil {
			g.localChar.Mounted = true
			g.localChar.AnimState = AnimRide
		}

	case "dismount_ok":
		if g.localChar != nil {
			g.localChar.Mounted = false
			g.localChar.AnimState = AnimIdle
		}

	case "pvp_damage":
		if g.localChar != nil && g.localChar.AnimState != AnimDead {
			newHP := msg.HP
			if newHP < 0 {
				newHP = 0
			}
			if newHP > g.localMaxHP {
				newHP = g.localMaxHP
			}
			if newHP < g.localHP {
				dmg := g.localHP - newHP
				g.localHP = newHP
				if g.localHP == 0 {
					g.localChar.AnimState = AnimDead
					g.localChar.hurtTimer = 0
					g.localChar.knockTimer = 0
					g.localChar.knockVX = 0
					g.localChar.knockVY = 0
					g.chat.AddMessage("", "You have been killed!", true)
				} else {
					g.localChar.SetHurt(msg.AtkX, msg.AtkY, dmg)
				}
			}
		}

	case "inventory_data":
		g.localInventory = msg.Inventory
		if g.inventoryMenu != nil {
			g.inventoryMenu.SetItems(g.localInventory)
		}

	case "use_item_ok":
		if msg.PlayerID != g.localID {
			g.mu.Lock()
			if ch, ok := g.otherPlayers[msg.PlayerID]; ok {
				ch.AnimState = msg.AnimSt
			}
			g.mu.Unlock()
		}

	case "buy_result":
		if msg.Success {
			g.chat.AddMessage("", msg.Msg, true)
			if msg.GralatN > 0 {
				g.localGralats = msg.GralatN
			}
		} else {
			g.chat.AddMessage("", "Error: "+msg.Msg, true)
		}
	}
}


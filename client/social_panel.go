package main

import (
	"fmt"
	"image/color"
	"strings"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// ── SocialPanel ───────────────────────────────────────────────────────────────
// Unified panel rendered below the main PanelMenu for Friends / Guilds / Quests.
// The panel appears in the region from panelBottom to the bottom of the screen.

type SocialPanel struct {
	// Data
	friends     []FriendEntry
	requests    []FriendEntry
	myGuild     *GuildInfo
	guildList   []GuildListEntry
	quests      []QuestEntry

	// State per sub-panel
	friendInput   string  // "add friend" text box
	friendFocus   bool
	guildNameIn   string
	guildTagIn    string
	guildDescIn   string
	guildFocus    int // 0=none 1=name 2=tag 3=desc 4=search
	guildSearch   string
	questScroll   int
	friendScroll  int
	guildMemberScroll int

	// Text inputs (Ebiten-style)
	bsHeld bool
	bsTimer int64

	// Pending action result
	resultMsg     string
	resultIsError bool
}

func NewSocialPanel() *SocialPanel { return &SocialPanel{} }

// ── Data setters (called from game_network.go) ───────────────────────────────

func (sp *SocialPanel) SetFriends(friends, requests []FriendEntry) {
	sp.friends = friends
	sp.requests = requests
}

func (sp *SocialPanel) SetGuild(g *GuildInfo) {
	sp.myGuild = g
}

func (sp *SocialPanel) SetGuildList(list []GuildListEntry) {
	sp.guildList = list
}

func (sp *SocialPanel) SetQuests(q []QuestEntry) {
	sp.quests = q
}

// ── Update ────────────────────────────────────────────────────────────────────

// Update processes input for the given sub-panel ("Friends", "Guilds", "Quests").
// Returns a SendAction if a message needs to be sent to the server.
type SendAction struct {
	Type    string
	Payload map[string]interface{}
}

func (sp *SocialPanel) Update(sub string, panelBot int) *SendAction {
	if sub == "" {
		return nil
	}

	switch sub {
	case "Friends":
		return sp.updateFriends(panelBot)
	case "Guilds":
		return sp.updateGuilds(panelBot)
	case "Quests":
		return sp.updateQuests(panelBot)
	}
	return nil
}

func (sp *SocialPanel) updateFriends(panelBot int) *SendAction {
	mx, my := ebiten.CursorPosition()
	clicked := inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft)

	// Text input for "Add friend"
	inputRect := sp.friendInputRect(panelBot)
	if clicked && inRect(mx, my, inputRect) {
		sp.friendFocus = true
	} else if clicked {
		sp.friendFocus = false
	}
	if sp.friendFocus {
		for _, r := range ebiten.AppendInputChars(nil) {
			if utf8.RuneCountInString(sp.friendInput) < 20 {
				sp.friendInput += string(r)
			}
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyBackspace) && len(sp.friendInput) > 0 {
			runes := []rune(sp.friendInput)
			sp.friendInput = string(runes[:len(runes)-1])
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyEnter) && sp.friendInput != "" {
			name := strings.TrimSpace(sp.friendInput)
			sp.friendInput = ""
			sp.friendFocus = false
			return &SendAction{"friend_add", map[string]interface{}{"target": name}}
		}
	}

	// Accept / Remove buttons per friend
	y := panelBot + 50
	for _, f := range sp.requests {
		btn := [4]int{socialPanelX() + 340, y - 12, 60, 18}
		if clicked && inRect(mx, my, btn) {
			return &SendAction{"friend_accept", map[string]interface{}{"from": f.Name}}
		}
		y += 22
	}
	y += 10
	for _, f := range sp.friends {
		if f.Status != "accepted" {
			continue
		}
		btn := [4]int{socialPanelX() + 340, y - 12, 60, 18}
		if clicked && inRect(mx, my, btn) {
			return &SendAction{"friend_remove", map[string]interface{}{"target": f.Name}}
		}
		y += 22
	}
	return nil
}

func (sp *SocialPanel) updateGuilds(panelBot int) *SendAction {
	mx, my := ebiten.CursorPosition()
	clicked := inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft)
	px := socialPanelX()

	if sp.myGuild != nil {
		// In a guild — show Leave button
		leaveBtn := [4]int{px + 10, panelBot + 16, 80, 20}
		if clicked && inRect(mx, my, leaveBtn) {
			return &SendAction{"guild_leave", nil}
		}
		return nil
	}

	// Not in a guild — show create form + join from list
	// Focus cycling for create form
	inputs := [][4]int{
		sp.guildNameRect(panelBot),
		sp.guildTagRect(panelBot),
		sp.guildDescRect(panelBot),
	}
	for i, r := range inputs {
		if clicked && inRect(mx, my, r) {
			sp.guildFocus = i + 1
		}
	}
	if clicked && !inRect(mx, my, inputs[0]) && !inRect(mx, my, inputs[1]) && !inRect(mx, my, inputs[2]) {
		// check other click targets before clearing focus
	}

	// Keyboard input for focused field
	if sp.guildFocus > 0 {
		var target *string
		var maxLen int
		switch sp.guildFocus {
		case 1:
			target, maxLen = &sp.guildNameIn, 24
		case 2:
			target, maxLen = &sp.guildTagIn, 5
		case 3:
			target, maxLen = &sp.guildDescIn, 60
		}
		if target != nil {
			for _, r := range ebiten.AppendInputChars(nil) {
				if utf8.RuneCountInString(*target) < maxLen {
					*target += string(r)
				}
			}
			if inpututil.IsKeyJustPressed(ebiten.KeyBackspace) && len(*target) > 0 {
				runes := []rune(*target)
				*target = string(runes[:len(runes)-1])
			}
		}
	}

	// Create button
	createBtn := [4]int{px + 10, panelBot + 110, 120, 22}
	if clicked && inRect(mx, my, createBtn) {
		if sp.guildNameIn != "" && sp.guildTagIn != "" {
			action := &SendAction{"guild_create", map[string]interface{}{
				"name": strings.TrimSpace(sp.guildNameIn),
				"tag":  strings.TrimSpace(sp.guildTagIn),
				"desc": strings.TrimSpace(sp.guildDescIn),
			}}
			sp.guildNameIn, sp.guildTagIn, sp.guildDescIn = "", "", ""
			sp.guildFocus = 0
			return action
		}
	}

	// Browse list — click to join
	listY := panelBot + 145
	for i, g := range sp.guildList {
		if i > 8 {
			break
		}
		btn := [4]int{px + 10, listY, 400, 18}
		if clicked && inRect(mx, my, btn) {
			return &SendAction{"guild_join", map[string]interface{}{"name": g.Name}}
		}
		listY += 20
	}

	// Refresh list button
	refreshBtn := [4]int{px + 420, panelBot + 145, 80, 18}
	if clicked && inRect(mx, my, refreshBtn) {
		return &SendAction{"guild_list", nil}
	}

	return nil
}

func (sp *SocialPanel) updateQuests(panelBot int) *SendAction {
	mx, my := ebiten.CursorPosition()
	clicked := inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft)
	px := socialPanelX()

	// Start quest buttons
	y := panelBot + 30
	for _, q := range sp.quests {
		if q.Progress == 0 && !q.Completed {
			btn := [4]int{px + 390, y, 70, 18}
			if clicked && inRect(mx, my, btn) {
				return &SendAction{"quest_start", map[string]interface{}{"quest_id": q.ID}}
			}
		}
		y += 52
	}
	return nil
}

// ── Draw ──────────────────────────────────────────────────────────────────────

func (sp *SocialPanel) Draw(screen *ebiten.Image, sub string, panelBot int) {
	if sub == "" {
		return
	}
	switch sub {
	case "Friends":
		sp.drawFriends(screen, panelBot)
	case "Guilds":
		sp.drawGuilds(screen, panelBot)
	case "Quests":
		sp.drawQuests(screen, panelBot)
	}
}

const spAlpha = uint8(245)

func socialPanelX() int { return 0 }
func socialPanelW() int { return screenW }

func (sp *SocialPanel) drawHeader(screen *ebiten.Image, topY int, title string) int {
	pw := socialPanelW()
	titleH := 22
	DrawRect(screen, 0, topY, pw, titleH, color.RGBA{55, 95, 175, spAlpha})
	DrawRect(screen, 0, topY, pw, 2, color.RGBA{120, 165, 230, spAlpha})
	DrawText(screen, title, pw/2-len(title)*fontW/2, topY+titleH-5, color.RGBA{255, 255, 255, spAlpha})
	return topY + titleH
}

// ── Friends draw ──────────────────────────────────────────────────────────────

func (sp *SocialPanel) friendInputRect(panelBot int) [4]int {
	return [4]int{socialPanelX() + 10, panelBot + 28, 200, 16}
}

func (sp *SocialPanel) drawFriends(screen *ebiten.Image, panelBot int) {
	pw := socialPanelW()
	// Panel background
	totalH := sp.friendsPanelHeight()
	DrawRect(screen, 0, panelBot, pw, totalH, color.RGBA{175, 198, 238, spAlpha})
	contentY := sp.drawHeader(screen, panelBot, "Friends")

	// Add friend input
	DrawText(screen, "Add friend:", 10, contentY+16, colGold)
	ir := sp.friendInputRect(panelBot)
	bdrC := colBorderMid
	if sp.friendFocus {
		bdrC = colInputFocus
	}
	DrawRect(screen, ir[0]-1, ir[1]-1, ir[2]+2, ir[3]+2, color.RGBA{bdrC.R, bdrC.G, bdrC.B, spAlpha})
	DrawRect(screen, ir[0], ir[1], ir[2], ir[3], color.RGBA{20, 20, 50, 230})
	display := sp.friendInput
	DrawText(screen, display, ir[0]+4, ir[1]+ir[3]-3, colTextWhite)

	btnX := ir[0] + ir[2] + 8
	DrawRect(screen, btnX, ir[1]-1, 50, ir[3]+2, color.RGBA{55, 95, 175, spAlpha})
	DrawText(screen, "Add", btnX+8, ir[1]+ir[3]-3, colTextWhite)

	y := contentY + 30

	// Pending requests
	if len(sp.requests) > 0 {
		DrawText(screen, "Pending requests:", 10, y+10, colGoldDim)
		y += 18
		for _, req := range sp.requests {
			dot := "●"
			DrawText(screen, dot+" "+req.Name+" (wants to be friends)", 14, y+10, color.RGBA{255, 200, 80, spAlpha})
			DrawRect(screen, socialPanelX()+340, y-2, 60, 18, color.RGBA{55, 175, 95, spAlpha})
			DrawText(screen, "Accept", socialPanelX()+345, y+10, colTextWhite)
			y += 22
		}
		y += 4
	}

	// Accepted friends
	DrawText(screen, "Friends:", 10, y+10, colGoldDim)
	y += 18
	if len(sp.friends) == 0 {
		DrawText(screen, "No friends yet. Add some!", 14, y+10, colTextDim)
	} else {
		for _, f := range sp.friends {
			if f.Status != "accepted" {
				continue
			}
			onlineClr := color.RGBA{100, 100, 120, spAlpha}
			dot := "○"
			if f.Online {
				onlineClr = color.RGBA{80, 215, 100, spAlpha}
				dot = "●"
			}
			DrawText(screen, dot+" "+f.Name, 14, y+10, onlineClr)
			DrawRect(screen, socialPanelX()+340, y-2, 60, 18, color.RGBA{175, 55, 55, spAlpha})
			DrawText(screen, "Remove", socialPanelX()+342, y+10, colTextWhite)
			y += 22
		}
	}
}

func (sp *SocialPanel) friendsPanelHeight() int {
	base := 60
	base += len(sp.requests) * 22
	accepted := 0
	for _, f := range sp.friends {
		if f.Status == "accepted" {
			accepted++
		}
	}
	if accepted == 0 {
		accepted = 1
	}
	return base + accepted*22 + 20
}

// ── Guilds draw ───────────────────────────────────────────────────────────────

func (sp *SocialPanel) guildNameRect(panelBot int) [4]int {
	return [4]int{120, panelBot + 38, 200, 16}
}
func (sp *SocialPanel) guildTagRect(panelBot int) [4]int {
	return [4]int{340, panelBot + 38, 60, 16}
}
func (sp *SocialPanel) guildDescRect(panelBot int) [4]int {
	return [4]int{120, panelBot + 62, 350, 16}
}

func (sp *SocialPanel) drawGuilds(screen *ebiten.Image, panelBot int) {
	pw := socialPanelW()
	totalH := 300
	DrawRect(screen, 0, panelBot, pw, totalH, color.RGBA{175, 198, 238, spAlpha})
	contentY := sp.drawHeader(screen, panelBot, "Guilds")

	if sp.myGuild != nil {
		// In a guild — show info
		g := sp.myGuild
		DrawText(screen, fmt.Sprintf("[%s] %s", g.Tag, g.Name), 10, contentY+16, colGold)
		DrawText(screen, "Leader: "+g.Leader, 10, contentY+30, colTextWhite)
		DrawText(screen, g.Desc, 10, contentY+44, colTextDim)

		y := contentY + 60
		DrawText(screen, "Members:", 10, y, colGoldDim)
		y += 14
		for _, m := range g.Members {
			dot := "○"
			clr := colTextDim
			if m.Online {
				dot = "●"
				clr = colTextWhite
			}
			rank := ""
			if m.Rank == "leader" {
				rank = " [Leader]"
				clr = colGold
			}
			DrawText(screen, dot+" "+m.Name+rank, 14, y, clr)
			y += 14
		}
		// Leave button
		DrawRect(screen, 10, panelBot+16, 80, 20, color.RGBA{175, 55, 55, spAlpha})
		DrawText(screen, "Leave Guild", 14, panelBot+29, colTextWhite)
		return
	}

	// Not in a guild — create form
	DrawText(screen, "Create a guild:", 10, contentY+16, colGold)
	DrawText(screen, "Name:", 10, contentY+36, colTextDim)
	sp.drawTextInput(screen, sp.guildNameRect(panelBot), sp.guildNameIn, sp.guildFocus == 1)
	DrawText(screen, "Tag:", 310, contentY+36, colTextDim)
	sp.drawTextInput(screen, sp.guildTagRect(panelBot), sp.guildTagIn, sp.guildFocus == 2)
	DrawText(screen, "Desc:", 10, contentY+60, colTextDim)
	sp.drawTextInput(screen, sp.guildDescRect(panelBot), sp.guildDescIn, sp.guildFocus == 3)

	DrawRect(screen, 10, panelBot+110, 120, 22, color.RGBA{55, 95, 175, spAlpha})
	DrawText(screen, "Create Guild", 16, panelBot+124, colTextWhite)

	// Guild list
	listY := contentY + 122
	DrawText(screen, "Join a guild:", 10, listY, colGold)
	DrawRect(screen, 420, panelBot+143, 80, 18, color.RGBA{55, 95, 175, spAlpha})
	DrawText(screen, "Refresh", 425, panelBot+155, colTextWhite)
	listY += 16

	if len(sp.guildList) == 0 {
		DrawText(screen, "No guilds yet. Create one!", 14, listY+12, colTextDim)
	}
	for i, g := range sp.guildList {
		if i > 8 {
			break
		}
		DrawRect(screen, 10, listY, 400, 18, color.RGBA{55, 75, 140, 200})
		line := fmt.Sprintf("[%s] %s — %s (%d members)", g.Tag, g.Name, g.Leader, g.Members)
		DrawText(screen, line, 14, listY+12, colTextWhite)
		listY += 20
	}
}

func (sp *SocialPanel) drawTextInput(screen *ebiten.Image, r [4]int, val string, focused bool) {
	bdr := colBorderMid
	if focused {
		bdr = colInputFocus
	}
	DrawRect(screen, r[0]-1, r[1]-1, r[2]+2, r[3]+2, color.RGBA{bdr.R, bdr.G, bdr.B, spAlpha})
	DrawRect(screen, r[0], r[1], r[2], r[3], color.RGBA{20, 20, 50, 230})
	DrawText(screen, val, r[0]+4, r[1]+r[3]-3, colTextWhite)
}

// ── Quests draw ───────────────────────────────────────────────────────────────

func (sp *SocialPanel) drawQuests(screen *ebiten.Image, panelBot int) {
	pw := socialPanelW()
	n := len(sp.quests)
	if n == 0 {
		n = 1
	}
	totalH := 30 + n*52 + 20
	if totalH < 80 {
		totalH = 80
	}
	DrawRect(screen, 0, panelBot, pw, totalH, color.RGBA{175, 198, 238, spAlpha})
	contentY := sp.drawHeader(screen, panelBot, "Quests")

	if len(sp.quests) == 0 {
		DrawText(screen, "Loading quests...", 14, contentY+20, colTextDim)
		return
	}

	y := contentY + 8
	for _, q := range sp.quests {
		// Background card
		cardClr := color.RGBA{55, 75, 140, 200}
		if q.Completed {
			cardClr = color.RGBA{35, 85, 45, 200}
		}
		DrawRect(screen, 8, y, pw-16, 46, cardClr)
		DrawRect(screen, 8, y, pw-16, 2, color.RGBA{120, 165, 230, 180})

		// Title
		titleClr := colGold
		if q.Completed {
			titleClr = colTextOK
		}
		DrawText(screen, q.Name, 14, y+14, titleClr)
		if q.Completed {
			DrawText(screen, "✓ COMPLETE", pw-100, y+14, colTextOK)
		} else if q.Progress > 0 {
			pct := fmt.Sprintf("%d/%d", q.Progress, q.Required)
			DrawText(screen, pct, pw-60, y+14, colTextDim)
		} else {
			// Start button
			DrawRect(screen, pw-80, y+4, 70, 18, color.RGBA{55, 95, 175, spAlpha})
			DrawText(screen, "Accept", pw-76, y+16, colTextWhite)
		}

		DrawText(screen, q.Objective, 14, y+28, colTextDim)

		// Progress bar
		if !q.Completed && q.Required > 0 {
			barW := 200
			barX := 14
			barY := y + 38
			DrawRect(screen, barX, barY, barW, 6, color.RGBA{30, 30, 60, 200})
			filled := barW * q.Progress / q.Required
			if filled > 0 {
				DrawRect(screen, barX, barY, filled, 6, color.RGBA{55, 175, 95, 230})
			}
			DrawText(screen, fmt.Sprintf("Reward: %d G", q.Reward), barX+barW+10, barY+6, colGoldDim)
		}

		y += 52
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func inRect(mx, my int, r [4]int) bool {
	return mx >= r[0] && mx < r[0]+r[2] && my >= r[1] && my < r[1]+r[3]
}

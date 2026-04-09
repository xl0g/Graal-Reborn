package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// GameState is the top-level screen selector.
type GameState int

const (
	StateMainMenu GameState = iota
	StateLogin
	StateRegister
	StatePlaying
)

// ──────────────────────────────────────────────────────────────
// MAIN MENU
// ──────────────────────────────────────────────────────────────

// menuItem holds text and the state it transitions to.
type menuItem struct {
	label  string
	target GameState
	isQuit bool
}

type MainMenu struct {
	sel  int
	items []menuItem
	quit  bool
}

func NewMainMenu() *MainMenu {
	return &MainMenu{
		items: []menuItem{
			{label: "PLAY",            target: StateLogin},
			{label: "CREATE ACCOUNT",  target: StateRegister},
			{label: "QUIT",            isQuit: true},
		},
	}
}

func (m *MainMenu) WantsQuit() bool { return m.quit }

// itemRect returns (x, y, w, h) for item i.
func (m *MainMenu) itemRect(i int) (int, int, int, int) {
	const (
		bx = 190
		bw = 420
		bh = 46
		by0 = 290
		gap = 22
	)
	return bx, by0 + i*(bh+gap), bw, bh
}

func (m *MainMenu) Update() GameState {
	m.quit = false
	mx, my := ebiten.CursorPosition()

	if inpututil.IsKeyJustPressed(ebiten.KeyUp) && m.sel > 0 {
		m.sel--
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyDown) && m.sel < len(m.items)-1 {
		m.sel++
	}

	for i := range m.items {
		bx, by, bw, bh := m.itemRect(i)
		if mx >= bx && mx < bx+bw && my >= by && my < by+bh {
			m.sel = i
			if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
				return m.confirm()
			}
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) {
		return m.confirm()
	}
	return StateMainMenu
}

func (m *MainMenu) confirm() GameState {
	it := m.items[m.sel]
	if it.isQuit {
		m.quit = true
		return StateMainMenu
	}
	return it.target
}

func (m *MainMenu) Draw(screen *ebiten.Image) {
	DrawStarBg(screen)

	// ── Title panel ───────────────────────────────────────
	DrawPanel(screen, 130, 52, 540, 100)
	// Title 2× centered
	title := "GO MULTIPLAYER"
	tx := 130 + (540-BigTextW(title))/2
	DrawBigText(screen, title, tx+2, 64, colGoldDim)   // shadow
	DrawBigText(screen, title, tx, 62, colGold)

	sub := "Real-time multiplayer game"
	DrawText(screen, sub, screenW/2-len(sub)*fontW/2, 130, colTextDim)

	// ── Menu items ────────────────────────────────────────
	for i, it := range m.items {
		bx, by, bw, bh := m.itemRect(i)
		selected := i == m.sel

		// Background
		bgClr := colPanelBg
		bdrClr := colBorderMid
		if selected {
			bgClr = colSelBg
			bdrClr = colBorderHL
		}
		DrawPanel(screen, bx-4, by-4, bw+8, bh+8)
		DrawRectBorder(screen, bx, by, bw, bh, bgClr, bdrClr)

		// Cursor "▶"
		cursor := "  "
		if selected {
			cursor = "> "
		}
		full := cursor + it.label

		lblClr := colTextDim
		if selected {
			lblClr = colGoldBright
		}
		tw := len(full) * fontW
		DrawText(screen, full, bx+(bw-tw)/2, by+(bh+fontH)/2-1, lblClr)
	}

	// ── Footer ────────────────────────────────────────────
	hint := "Arrows + Enter   or   click to navigate"
	DrawText(screen, hint, screenW/2-len(hint)*fontW/2, screenH-14, colTextDim)
}

// ──────────────────────────────────────────────────────────────
// LOGIN MENU
// ──────────────────────────────────────────────────────────────

// Precise layout constants so click zones match visual elements exactly.
const (
	loginPanelX = 130
	loginPanelY = 42
	loginPanelW = 540
	loginPanelH = 470

	loginFieldX = loginPanelX + 30        // 160
	loginFieldW = loginPanelW - 60        // 480

	loginUserY = 198 // box top (label baseline at loginUserY-labelGap)
	loginPassY = loginUserY + 36 + 44     // 278
	loginBoxH  = 36

	loginBtnY  = loginPassY + loginBoxH + 36 // 350
	loginBtnW  = 220
	loginBtnH  = 38
	loginBtnX1 = loginFieldX              // 160
	loginBtnX2 = loginFieldX + loginBtnW + 20 // 400
)

type LoginMenu struct {
	username *TextInput
	password *TextInput
	loginBtn *Button
	backBtn  *Button
	focused  int

	message string
	msgOK   bool
	loading bool
	Token   string
	Name    string
}

func NewLoginMenu() *LoginMenu {
	m := &LoginMenu{}
	m.username = NewTextInput(loginFieldX, loginUserY, loginFieldW, loginBoxH, "Username", false)
	m.password = NewTextInput(loginFieldX, loginPassY, loginFieldW, loginBoxH, "Password", true)
	m.loginBtn = NewButton(loginBtnX1, loginBtnY, loginBtnW, loginBtnH, "LOG IN")
	m.backBtn  = NewButton(loginBtnX2, loginBtnY, loginBtnW, loginBtnH, "BACK")
	m.username.IsFocused = true
	return m
}

func (m *LoginMenu) reset() {
	m.username.Value = ""
	m.password.Value = ""
	m.message = ""
	m.Token = ""
	m.Name = ""
	m.loading = false
	m.focused = 0
	m.username.IsFocused = true
	m.password.IsFocused = false
}

func (m *LoginMenu) setFocus(i int) {
	m.focused = i
	m.username.IsFocused = i == 0
	m.password.IsFocused = i == 1
}

func (m *LoginMenu) Update() GameState {
	if m.loading {
		return StateLogin
	}
	if m.Token != "" {
		return StatePlaying
	}

	mx, my := ebiten.CursorPosition()

	// Tab focus cycling
	if inpututil.IsKeyJustPressed(ebiten.KeyTab) {
		m.setFocus((m.focused + 1) % 2)
	}

	// Click to focus — ContainsPoint covers label+box
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		switch {
		case m.username.ContainsPoint(mx, my):
			m.setFocus(0)
		case m.password.ContainsPoint(mx, my):
			m.setFocus(1)
		}
	}

	m.username.Update()
	m.password.Update()

	if (inpututil.IsKeyJustPressed(ebiten.KeyEnter) || m.loginBtn.IsClicked()) && !m.loading {
		m.submit()
	}
	if m.backBtn.IsClicked() || inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		m.reset()
		return StateMainMenu
	}
	return StateLogin
}

func (m *LoginMenu) submit() {
	user := strings.TrimSpace(m.username.Value)
	pass := m.password.Value
	if user == "" || pass == "" {
		m.message = "Please fill in all fields"
		m.msgOK = false
		return
	}
	m.loading = true
	m.message = "Logging in..."
	m.msgOK = true
	go func() {
		token, name, err := authHTTP(getAPIURL()+"/api/login",
			map[string]string{"username": user, "password": pass})
		if err != nil {
			m.message = err.Error()
			m.msgOK = false
		} else {
			m.Token = token
			m.Name = name
		}
		m.loading = false
	}()
}

func (m *LoginMenu) Draw(screen *ebiten.Image) {
	DrawStarBg(screen)
	DrawPanel(screen, loginPanelX, loginPanelY, loginPanelW, loginPanelH)

	// Title
	title := "LOGIN"
	tx := loginPanelX + (loginPanelW-BigTextW(title))/2
	DrawBigText(screen, title, tx+2, loginPanelY+18, colGoldDim)
	DrawBigText(screen, title, tx, loginPanelY+16, colGold)

	// Divider below title
	DrawHDivider(screen, loginPanelX+12, loginPanelY+52, loginPanelW-24)

	m.username.Draw(screen)
	m.password.Draw(screen)
	m.loginBtn.Draw(screen)
	m.backBtn.Draw(screen)

	// Status message
	if m.message != "" {
		clr := colTextErr
		if m.msgOK {
			clr = colTextOK
		}
		DrawText(screen, m.message, loginFieldX, loginBtnY+loginBtnH+28, clr)
	}

	// Footer hint
	hint := "Tab  switch field    Enter  confirm    Esc  back"
	DrawText(screen, hint, screenW/2-len(hint)*fontW/2, screenH-10, colTextDim)
}

// ──────────────────────────────────────────────────────────────
// REGISTER MENU
// ──────────────────────────────────────────────────────────────

const (
	regPanelX = 120
	regPanelY = 30
	regPanelW = 560
	regPanelH = 510

	regFieldX = regPanelX + 30
	regFieldW = regPanelW - 60
	regBoxH   = 36

	regUserY  = 172
	regPassY  = regUserY + regBoxH + 46  // 254
	regEmailY = regPassY + regBoxH + 46  // 336

	regBtnY   = regEmailY + regBoxH + 36 // 408
	regBtnW   = 224
	regBtnH   = 38
	regBtnX1  = regFieldX
	regBtnX2  = regFieldX + regBtnW + 18
)

type RegisterMenu struct {
	username *TextInput
	password *TextInput
	email    *TextInput
	regBtn   *Button
	backBtn  *Button
	focused  int

	message string
	msgOK   bool
	loading bool
	Token   string
	Name    string
}

func NewRegisterMenu() *RegisterMenu {
	m := &RegisterMenu{}
	m.username = NewTextInput(regFieldX, regUserY,  regFieldW, regBoxH, "Username  (3-20 chars)", false)
	m.password = NewTextInput(regFieldX, regPassY,  regFieldW, regBoxH, "Password (6+ chars)", true)
	m.email    = NewTextInput(regFieldX, regEmailY, regFieldW, regBoxH, "Email (optional)", false)
	m.regBtn   = NewButton(regBtnX1, regBtnY, regBtnW, regBtnH, "REGISTER")
	m.backBtn  = NewButton(regBtnX2, regBtnY, regBtnW, regBtnH, "BACK")
	m.username.IsFocused = true
	return m
}

func (m *RegisterMenu) setFocus(i int) {
	m.focused = i
	m.username.IsFocused = i == 0
	m.password.IsFocused = i == 1
	m.email.IsFocused = i == 2
}

func (m *RegisterMenu) reset() {
	m.username.Value = ""
	m.password.Value = ""
	m.email.Value = ""
	m.message = ""
	m.Token = ""
	m.Name = ""
	m.loading = false
	m.focused = 0
	m.username.IsFocused = true
	m.password.IsFocused = false
	m.email.IsFocused = false
}

func (m *RegisterMenu) Update() GameState {
	if m.loading {
		return StateRegister
	}
	if m.Token != "" {
		return StatePlaying
	}

	mx, my := ebiten.CursorPosition()

	if inpututil.IsKeyJustPressed(ebiten.KeyTab) {
		m.setFocus((m.focused + 1) % 3)
	}
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		switch {
		case m.username.ContainsPoint(mx, my):
			m.setFocus(0)
		case m.password.ContainsPoint(mx, my):
			m.setFocus(1)
		case m.email.ContainsPoint(mx, my):
			m.setFocus(2)
		}
	}

	m.username.Update()
	m.password.Update()
	m.email.Update()

	if (inpututil.IsKeyJustPressed(ebiten.KeyEnter) || m.regBtn.IsClicked()) && !m.loading {
		m.submit()
	}
	if m.backBtn.IsClicked() || inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		m.reset()
		return StateMainMenu
	}
	return StateRegister
}

func (m *RegisterMenu) submit() {
	user  := strings.TrimSpace(m.username.Value)
	pass  := m.password.Value
	email := strings.TrimSpace(m.email.Value)
	if user == "" || pass == "" {
		m.message = "Username and password are required"
		m.msgOK = false
		return
	}
	m.loading = true
	m.message = "Creating account..."
	m.msgOK = true
	go func() {
		token, name, err := authHTTP(getAPIURL()+"/api/register",
			map[string]string{"username": user, "password": pass, "email": email})
		if err != nil {
			m.message = err.Error()
			m.msgOK = false
		} else {
			m.Token = token
			m.Name = name
			m.message = "Account created!"
			m.msgOK = true
		}
		m.loading = false
	}()
}

func (m *RegisterMenu) Draw(screen *ebiten.Image) {
	DrawStarBg(screen)
	DrawPanel(screen, regPanelX, regPanelY, regPanelW, regPanelH)

	title := "REGISTER"
	tx := regPanelX + (regPanelW-BigTextW(title))/2
	DrawBigText(screen, title, tx+2, regPanelY+18, colGoldDim)
	DrawBigText(screen, title, tx, regPanelY+16, colGold)
	DrawHDivider(screen, regPanelX+12, regPanelY+52, regPanelW-24)

	m.username.Draw(screen)
	m.password.Draw(screen)
	m.email.Draw(screen)
	m.regBtn.Draw(screen)
	m.backBtn.Draw(screen)

	if m.message != "" {
		clr := colTextErr
		if m.msgOK {
			clr = colTextOK
		}
		DrawText(screen, m.message, regFieldX, regBtnY+regBtnH+28, clr)
	}

	hint := "Tab  switch field    Enter  confirm    Esc  back"
	DrawText(screen, hint, screenW/2-len(hint)*fontW/2, screenH-10, colTextDim)
}

// ──────────────────────────────────────────────────────────────
// HTTP helpers
// ──────────────────────────────────────────────────────────────

type authResp struct {
	Token    string `json:"token"`
	Username string `json:"username"`
	Error    string `json:"error"`
}

func authHTTP(url string, body map[string]string) (token, name string, err error) {
	b, _ := json.Marshal(body)
	resp, httpErr := http.Post(url, "application/json", bytes.NewReader(b))
	if httpErr != nil {
		return "", "", fmt.Errorf("Cannot reach server")
	}
	defer resp.Body.Close()
	var result authResp
	json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode != http.StatusOK {
		if result.Error != "" {
			return "", "", fmt.Errorf(result.Error)
		}
		return "", "", fmt.Errorf("Server error (%d)", resp.StatusCode)
	}
	return result.Token, result.Username, nil
}

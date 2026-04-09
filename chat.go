package main

import (
	"image/color"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

const (
	chatMaxStored  = 50
	chatMaxVisible = 8
	chatMaxInput   = 150
	chatFadeStart  = 8 * time.Second
	chatFadeDur    = 3 * time.Second
)

// ChatLine is a single chat message entry.
type ChatLine struct {
	From     string
	Text     string
	IsSystem bool
	When     time.Time
}

// Chat manages in-game chat display and input.
type Chat struct {
	Messages []ChatLine
	Input    string
	IsOpen   bool

	bsHeld  bool
	bsTimer time.Time
}

// NewChat creates a Chat.
func NewChat() *Chat {
	return &Chat{}
}

// AddMessage appends a message (system or player).
func (c *Chat) AddMessage(from, text string, isSystem bool) {
	c.Messages = append(c.Messages, ChatLine{
		From:     from,
		Text:     text,
		IsSystem: isSystem,
		When:     time.Now(),
	})
	if len(c.Messages) > chatMaxStored {
		c.Messages = c.Messages[len(c.Messages)-chatMaxStored:]
	}
}

// Update handles chat input. Returns (message, true) when the user sends a message.
func (c *Chat) Update() (string, bool) {
	if !c.IsOpen {
		return "", false
	}

	// Append printable characters
	for _, ch := range ebiten.AppendInputChars(nil) {
		if len([]rune(c.Input)) < chatMaxInput {
			c.Input += string(ch)
		}
	}

	// Backspace with repeat
	if ebiten.IsKeyPressed(ebiten.KeyBackspace) {
		now := time.Now()
		if !c.bsHeld {
			deleteLastRuneStr(&c.Input)
			c.bsHeld = true
			c.bsTimer = now
		} else if now.Sub(c.bsTimer) > 80*time.Millisecond {
			deleteLastRuneStr(&c.Input)
			c.bsTimer = now
		}
	} else {
		c.bsHeld = false
	}

	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		c.IsOpen = false
		c.Input = ""
		return "", false
	}

	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) {
		msg := strings.TrimSpace(c.Input)
		c.Input = ""
		c.IsOpen = false
		if msg != "" {
			return msg, true
		}
	}

	return "", false
}

func deleteLastRuneStr(s *string) {
	r := []rune(*s)
	if len(r) > 0 {
		*s = string(r[:len(r)-1])
	}
}

// Draw renders the chat overlay onto screen.
func (c *Chat) Draw(screen *ebiten.Image) {
	now := time.Now()

	// Choose message window
	start := len(c.Messages) - chatMaxVisible
	if start < 0 {
		start = 0
	}

	// Vertical start for messages (bottom of message list)
	baseY := screenH - 65
	if c.IsOpen {
		baseY = screenH - 85
	}

	for i := start; i < len(c.Messages); i++ {
		msg := c.Messages[i]
		age := now.Sub(msg.When)

		// Calculate alpha (fade out when chat is closed)
		alpha := uint8(210)
		if !c.IsOpen && age > chatFadeStart {
			fade := float64(age-chatFadeStart) / float64(chatFadeDur)
			if fade >= 1 {
				continue
			}
			alpha = uint8(float64(210) * (1 - fade))
		}

		var line string
		var txtClr color.RGBA
		if msg.IsSystem {
			line = "* " + msg.Text
			txtClr = color.RGBA{160, 220, 160, alpha}
		} else {
			line = msg.From + ": " + msg.Text
			txtClr = color.RGBA{235, 235, 235, alpha}
		}

		lineW := len([]rune(line))*fontW + 12
		lineY := baseY + (i-start)*(fontH+4)

		DrawRect(screen, 6, lineY-fontH+1, lineW, fontH+4, color.RGBA{0, 0, 0, alpha / 3})
		DrawText(screen, line, 12, lineY, txtClr)
	}

	// Input box
	if c.IsOpen {
		iy := screenH - 38
		DrawRectBorder(screen, 5, iy, screenW-10, 28, color.RGBA{18, 18, 28, 215}, color.RGBA{80, 150, 255, 255})

		prompt := "> " + c.Input
		if (now.UnixMilli()/500)%2 == 0 {
			prompt += "_"
		}
		DrawText(screen, prompt, 10, iy+19, color.RGBA{255, 255, 255, 255})
	} else {
		DrawText(screen, "[T] Chat  [Echap] Menu", 10, screenH-6, color.RGBA{120, 120, 160, 160})
	}
}

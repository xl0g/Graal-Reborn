package main

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
)

const (
	emojiBubbleDuration = 4.0  // total seconds visible
	emojiRiseDuration   = 1.0  // seconds the bubble takes to rise
	emojiRisePixels     = 28.0 // pixels the bubble travels upward
)

var (
	emojiSmile *ebiten.Image
	emojiFrown *ebiten.Image
)

func loadEmojiImages() {
	emojiSmile, _, _ = ebitenutil.NewImageFromFile(
		"Assets/offline/levels/emoticons/emoticon_Smile.png")
	emojiFrown, _, _ = ebitenutil.NewImageFromFile(
		"Assets/offline/levels/emoticons/emoticon_Frown.png")
}

// emojiImageFor returns the emoticon image for a shortcode, or nil.
func emojiImageFor(text string) *ebiten.Image {
	switch text {
	case ":)":
		return emojiSmile
	case ":D":
		return emojiSmile
	case ":(":
		return emojiFrown
	}
	return nil
}

// containsEmoji returns the first emoji shortcode found in a message, or "".
func containsEmoji(msg string) string {
	for _, code := range []string{":)", ":D", ":("} {
		if len(msg) >= len(code) {
			for i := 0; i <= len(msg)-len(code); i++ {
				if msg[i:i+len(code)] == code {
					return code
				}
			}
		}
	}
	return ""
}

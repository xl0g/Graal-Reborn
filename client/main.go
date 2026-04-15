package main

import (
	"flag"
	"log"
	"os"

	"github.com/hajimehoshi/ebiten/v2"
)

func main() {
	flag.Parse()

	LoadConfig("config.json")

	ebiten.SetWindowSize(screenW, screenH)
	ebiten.SetWindowTitle("Go Multiplayer")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetTPS(60)

	bodyImg, headImg, tilesImg := loadAssets()
	game := NewGame(bodyImg, headImg, tilesImg)

	if err := ebiten.RunGame(game); err != nil && err != ebiten.Termination {
		log.Println("Erreur:", err)
		os.Exit(1)
	}
}

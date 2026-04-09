package main

import (
	"image"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
)

var gralatImg *ebiten.Image

func loadGralatImage() {
	img, _, err := ebitenutil.NewImageFromFile(
		"Assets/offline/levels/images/downloads/gralats.png")
	if err != nil {
		return
	}
	gralatImg = img
}

// gralatSprite returns the sub-image for the given denomination.
// Sprite sheet layout (64×64, 2×2 grid of 32×32 sprites):
//
//	┌──────┬──────┐
//	│  1   │  5   │
//	├──────┼──────┤
//	│  30  │ 100  │
//	└──────┴──────┘
func gralatSprite(value int) *ebiten.Image {
	if gralatImg == nil {
		return nil
	}
	b := gralatImg.Bounds()
	hw := b.Dx() / 2
	hh := b.Dy() / 2
	var r image.Rectangle
	switch value {
	case 5:
		r = image.Rect(hw, 0, b.Dx(), hh)
	case 30:
		r = image.Rect(0, hh, hw, b.Dy())
	case 100:
		r = image.Rect(hw, hh, b.Dx(), b.Dy())
	default: // 1
		r = image.Rect(0, 0, hw, hh)
	}
	return gralatImg.SubImage(r).(*ebiten.Image)
}

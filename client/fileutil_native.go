//go:build !js

package main

import "os"

// ReadGameFile reads a game data file from the local filesystem.
func ReadGameFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

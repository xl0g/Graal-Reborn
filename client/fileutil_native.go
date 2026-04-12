//go:build !js

package main

import (
	"os"
	"strings"
)

// ReadGameFile reads a game data file from the local filesystem.
func ReadGameFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// ListGameDir returns the filenames (not full paths) of image files in a
// local directory. Used by the cosmetic menu to populate its file lists.
func ListGameDir(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(e.Name()[max(0, len(e.Name())-4):])
		if ext == ".png" || strings.HasSuffix(strings.ToLower(e.Name()), ".gif") {
			files = append(files, e.Name())
		}
	}
	return files
}

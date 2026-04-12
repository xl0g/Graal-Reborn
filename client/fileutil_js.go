//go:build js

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"syscall/js"
)

// ReadGameFile fetches a game data file via HTTP (WASM/browser build).
// The base URL is taken from window.location.origin so it works on any host.
func ReadGameFile(path string) ([]byte, error) {
	origin := js.Global().Get("location").Get("origin").String()
	url := origin + "/" + path
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

// ListGameDir fetches the list of image files in a server-side directory by
// calling /api/assets/list?dir=<category>. Used by the cosmetic menu on WASM.
// dir may be a full path like "Assets/offline/levels/bodies"; only the last
// component ("bodies") is sent to the server.
func ListGameDir(dir string) []string {
	origin := js.Global().Get("location").Get("origin").String()
	url := origin + "/api/assets/list?dir=" + filepath.Base(dir)
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	var files []string
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil
	}
	return files
}

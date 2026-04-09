//go:build js

package main

import (
	"fmt"
	"io"
	"net/http"
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

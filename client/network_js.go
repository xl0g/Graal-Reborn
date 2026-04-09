//go:build js

package main

import (
	"fmt"
	"log"
	"strings"
	"syscall/js"
	"time"
)

func getAPIURL() string {
	origin := js.Global().Get("location").Get("origin").String()
	return origin
}

func getWSURL() string {
	origin := js.Global().Get("location").Get("origin").String()
	// Replace http/https with ws/wss
	if strings.HasPrefix(origin, "https://") {
		return "wss://" + origin[8:] + "/ws"
	}
	return "ws://" + strings.TrimPrefix(origin, "http://") + "/ws"
}

// Dial connects to a WebSocket server using the browser's WebSocket API.
func Dial(url string) (*Connection, error) {
	c := newConnection()

	ws := js.Global().Get("WebSocket").New(url)
	if ws.IsUndefined() || ws.IsNull() {
		return nil, fmt.Errorf("WebSocket non supporte par ce navigateur")
	}

	openCh := make(chan struct{}, 1)
	errCh := make(chan string, 1)

	// Keep JS function references alive
	var onOpen, onClose, onError, onMessage js.Func

	onOpen = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		select {
		case openCh <- struct{}{}:
		default:
		}
		return nil
	})

	onClose = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		c.Close()
		return nil
	})

	onError = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		select {
		case errCh <- "erreur WebSocket":
		default:
		}
		c.Close()
		return nil
	})

	onMessage = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		data := []byte(args[0].Get("data").String())
		select {
		case c.recvCh <- data:
		default:
			log.Println("[WS] Buffer plein, message ignore")
		}
		return nil
	})

	ws.Set("onopen", onOpen)
	ws.Set("onclose", onClose)
	ws.Set("onerror", onError)
	ws.Set("onmessage", onMessage)

	// Wait for the connection to open
	select {
	case <-openCh:
	case msg := <-errCh:
		onOpen.Release()
		onClose.Release()
		onError.Release()
		onMessage.Release()
		return nil, fmt.Errorf(msg)
	case <-time.After(8 * time.Second):
		onOpen.Release()
		onClose.Release()
		onError.Release()
		onMessage.Release()
		return nil, fmt.Errorf("timeout de connexion WebSocket")
	}

	// Writer goroutine
	go func() {
		defer onOpen.Release()
		defer onClose.Release()
		defer onError.Release()
		defer onMessage.Release()
		for {
			select {
			case data := <-c.sendCh:
				readyState := ws.Get("readyState").Int()
				if readyState == 1 { // OPEN
					ws.Call("send", string(data))
				}
			case <-c.done:
				ws.Call("close")
				return
			}
		}
	}()

	return c, nil
}

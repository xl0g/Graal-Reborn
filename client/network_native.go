//go:build !js

package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

var serverAddr string

func init() {
	flag.StringVar(&serverAddr, "server", "localhost:8080", "Adresse du serveur (host:port)")
}

func getAPIURL() string {
	return "http://" + serverAddr
}

func getWSURL() string {
	return "ws://" + serverAddr + "/ws"
}

// Dial connects to the WebSocket server and returns a Connection.
func Dial(url string) (*Connection, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	wsConn, _, err := dialer.Dial(url, nil)
	if err != nil {
		return nil, fmt.Errorf("connexion WebSocket echouee (%s): %w", url, err)
	}

	c := newConnection()

	// Reader goroutine
	go func() {
		defer c.Close()
		for {
			select {
			case <-c.done:
				wsConn.Close()
				return
			default:
			}
			wsConn.SetReadDeadline(time.Now().Add(90 * time.Second))
			_, data, err := wsConn.ReadMessage()
			if err != nil {
				log.Println("[WS] Lecture:", err)
				return
			}
			select {
			case c.recvCh <- data:
			case <-c.done:
				return
			}
		}
	}()

	// Writer goroutine
	go func() {
		for {
			select {
			case data := <-c.sendCh:
				wsConn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := wsConn.WriteMessage(websocket.TextMessage, data); err != nil {
					log.Println("[WS] Ecriture:", err)
					c.Close()
					return
				}
			case <-c.done:
				wsConn.WriteControl(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
					time.Now().Add(2*time.Second),
				)
				wsConn.Close()
				return
			}
		}
	}()

	return c, nil
}

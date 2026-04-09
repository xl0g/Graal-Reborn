package main

import (
	"encoding/json"
	"sync"
)

// Connection wraps a WebSocket connection using channels for thread-safety.
// Platform-specific Dial() functions are in network_native.go and network_js.go.
type Connection struct {
	recvCh    chan []byte
	sendCh    chan []byte
	done      chan struct{}
	closeOnce sync.Once
}

func newConnection() *Connection {
	return &Connection{
		recvCh: make(chan []byte, 512),
		sendCh: make(chan []byte, 512),
		done:   make(chan struct{}),
	}
}

// SendJSON marshals v and queues it for sending.
func (c *Connection) SendJSON(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	select {
	case c.sendCh <- data:
	case <-c.done:
	default:
		// drop if buffer full
	}
}

// TryReceive returns the next received message without blocking.
func (c *Connection) TryReceive() ([]byte, bool) {
	select {
	case data := <-c.recvCh:
		return data, true
	default:
		return nil, false
	}
}

// IsClosed reports whether the connection has been closed.
func (c *Connection) IsClosed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// Close terminates the connection.
func (c *Connection) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
	})
}

// SPDX-License-Identifier: GPL-3.0-only

package console

import "net/http"

// Conn - an exported WebSocket connection wrapper for other packages (e.g. the notification centre).
// Handshake and frame logic is shared with consoles: here only access to already validated functions.
type Conn struct{ ws *wsConn }

// Upgrade checks the handshake and switches the connection to WebSocket. The 101 response is written here.
func Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if err := checkUpgrade(r); err != nil {
		return nil, err
	}
	ws, err := upgrade(w, r)
	if err != nil {
		return nil, err
	}
	return &Conn{ws: ws}, nil
}

// ReadMessage reads the next client message (needed to handle closure and ping).
func (c *Conn) ReadMessage() error {
	_, _, err := c.ws.readMessage()
	return err
}

// WriteText sends a text frame. Safe to call from multiple goroutines.
func (c *Conn) WriteText(data []byte) error { return c.ws.writeFrame(opText, data) }

// Ping sends a ping frame (keeps the connection alive through proxies).
func (c *Conn) Ping() error { return c.ws.writeFrame(opPing, nil) }

// Close closes the connection.
func (c *Conn) Close() { c.ws.closeConn() }

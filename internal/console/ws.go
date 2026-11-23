// SPDX-License-Identifier: GPL-3.0-only

package console

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Minimal WebSocket implementation (RFC 6455) without external dependencies:
// server side only, no outgoing fragmentation, message size limit.

const (
	opCont   = 0x0
	opText   = 0x1
	opBinary = 0x2
	opClose  = 0x8
	opPing   = 0x9
	opPong   = 0xA
	maxMsg   = 1 << 20
	wsGUID   = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

	controlPrefix   = "netrave:"
	heartbeatEvery  = 15 * time.Second
	heartbeatExpiry = 45 * time.Second
)

type wsConn struct {
	c      net.Conn
	rw     *bufio.ReadWriter
	mu     sync.Mutex
	closed bool
}

// checkUpgrade validates the WebSocket handshake headers without writing a response.
func checkUpgrade(r *http.Request) error {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return errors.New("A WebSocket connection is expected")
	}
	if !strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") {
		return errors.New("Connection: Upgrade is expected")
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		return errors.New("Only WebSocket version 13 is supported")
	}
	raw, err := base64.StdEncoding.DecodeString(r.Header.Get("Sec-WebSocket-Key"))
	if err != nil || len(raw) != 16 {
		return errors.New("Invalid Sec-WebSocket-Key")
	}
	return nil
}

// upgrade switches the connection to WebSocket (after checkUpgrade).
func upgrade(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("The server does not support upgrading to WebSocket")
	}
	c, rw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	sum := sha1.Sum([]byte(key + wsGUID))
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + base64.StdEncoding.EncodeToString(sum[:]) + "\r\n\r\n"
	if _, err := rw.WriteString(resp); err != nil {
		c.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		c.Close()
		return nil, err
	}
	return &wsConn{c: c, rw: rw}, nil
}

// readMessage reads one message (assembling fragments) and handles ping/pong/close.
func (ws *wsConn) readMessage() (byte, []byte, error) {
	var msg []byte
	var msgOp byte
	for {
		hdr := make([]byte, 2)
		if _, err := io.ReadFull(ws.rw, hdr); err != nil {
			return 0, nil, err
		}
		fin := hdr[0]&0x80 != 0
		op := hdr[0] & 0x0f
		if hdr[1]&0x80 == 0 {
			return 0, nil, errors.New("The client must mask frames")
		}
		n := uint64(hdr[1] & 0x7f)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(ws.rw, b[:]); err != nil {
				return 0, nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(ws.rw, b[:]); err != nil {
				return 0, nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		if n > maxMsg || uint64(len(msg))+n > maxMsg {
			return 0, nil, errors.New("The message is too large")
		}
		var key [4]byte
		if _, err := io.ReadFull(ws.rw, key[:]); err != nil {
			return 0, nil, err
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(ws.rw, payload); err != nil {
			return 0, nil, err
		}
		for i := range payload {
			payload[i] ^= key[i%4]
		}
		switch op {
		case opPing:
			if err := ws.writeFrame(opPong, payload); err != nil {
				return 0, nil, err
			}
			continue
		case opPong:
			continue
		case opClose:
			return opClose, nil, io.EOF
		case opText, opBinary:
			msgOp = op
			msg = payload
		case opCont:
			msg = append(msg, payload...)
		default:
			return 0, nil, errors.New("Unknown frame type")
		}
		if fin {
			return msgOp, msg, nil
		}
	}
}

// writeFrame sends a server frame (unmasked). Writing is thread-safe.
func (ws *wsConn) writeFrame(op byte, data []byte) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.closed {
		return io.ErrClosedPipe
	}
	n := len(data)
	hdr := []byte{0x80 | op}
	switch {
	case n < 126:
		hdr = append(hdr, byte(n))
	case n <= 0xffff:
		hdr = append(hdr, 126, 0, 0)
		binary.BigEndian.PutUint16(hdr[2:], uint16(n))
	default:
		hdr = append(hdr, 127, 0, 0, 0, 0, 0, 0, 0, 0)
		binary.BigEndian.PutUint64(hdr[2:], uint64(n))
	}
	if _, err := ws.c.Write(hdr); err != nil {
		return err
	}
	_, err := ws.c.Write(data)
	return err
}

// handleControl replies to service messages from the browser and does not pass them to QEMU.
// Terminal input comes as binary frames, so text netrave:* frames are safely reserved.
func handleControl(ws *wsConn, op byte, data []byte) bool {
	if op != opText || !strings.HasPrefix(string(data), controlPrefix) {
		return false
	}
	if string(data) == controlPrefix+"ping" {
		_ = ws.writeFrame(opText, []byte(controlPrefix+"pong"))
	}
	return true
}

// heartbeat sends a native Ping and requires at least some activity from the browser.
// The browser replies with Pong automatically even if the application hangs; if there is no reply,
// readMessage hits the deadline and the console closes, after which the frontend silently reconnects.
func heartbeat(ws *wsConn, stop <-chan struct{}) {
	if ws.c.SetReadDeadline(time.Now().Add(heartbeatExpiry)) != nil {
		ws.closeConn()
		return
	}
	ticker := time.NewTicker(heartbeatEvery)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if ws.c.SetReadDeadline(time.Now().Add(heartbeatExpiry)) != nil || ws.writeFrame(opPing, nil) != nil {
				ws.closeConn()
				return
			}
		}
	}
}

// closeConn sends a close frame (1000) and closes the connection.
func (ws *wsConn) closeConn() {
	ws.mu.Lock()
	if !ws.closed {
		ws.closed = true
		_, _ = ws.c.Write([]byte{0x80 | opClose, 2, 0x03, 0xe8})
	}
	ws.mu.Unlock()
	_ = ws.c.Close()
}

// bridge connects the WebSocket to the TCP connection in both directions until one side closes.
func bridge(ws *wsConn, back net.Conn) {
	defer back.Close()
	defer ws.closeConn()
	done := make(chan struct{}, 2)

	go func() { // QEMU -> browser
		buf := make([]byte, 16*1024)
		for {
			n, err := back.Read(buf)
			if n > 0 {
				if werr := ws.writeFrame(opBinary, buf[:n]); werr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		done <- struct{}{}
	}()

	go func() { // browser -> QEMU
		for {
			op, data, err := ws.readMessage()
			if err != nil {
				break
			}
			if handleControl(ws, op, data) {
				continue
			}
			if op == opText || op == opBinary {
				if _, werr := back.Write(data); werr != nil {
					break
				}
			}
		}
		done <- struct{}{}
	}()

	<-done
}

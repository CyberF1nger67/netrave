// SPDX-License-Identifier: GPL-3.0-only

package console

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The test simulates the browser - server - QEMU serial port chain: a TCP echo server plays the guest, which
// responds to input. Output (guest -> browser), input (browser -> guest), long frames and closure are checked.

// startGuest starts a TCP server that first prints a prompt and then echoes input.
func startGuest(t *testing.T, greeting string) (addr string, received chan []byte) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	received = make(chan []byte, 64)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.Write([]byte(greeting))
		buf := make([]byte, 4096)
		for {
			n, err := c.Read(buf)
			if n > 0 {
				cp := append([]byte(nil), buf[:n]...)
				received <- cp
				c.Write(cp) // echo: the guest prints what it received
			}
			if err != nil {
				return
			}
		}
	}()
	return ln.Addr().String(), received
}

// bridgeHandler - the same path as in console.serve: WebSocket on input, TCP on output.
func bridgeHandler(guest string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := checkUpgrade(r); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		back, err := net.DialTimeout("tcp", guest, 2*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		ws, err := upgrade(w, r)
		if err != nil {
			back.Close()
			return
		}
		bridge(ws, back)
	}
}

// wsClient - a minimal RFC 6455 client for the test (masks frames like a browser).
type wsClient struct {
	c  net.Conn
	rd *bufio.Reader
}

func dialWS(t *testing.T, addr string) *wsClient {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	keyRaw := make([]byte, 16)
	rand.Read(keyRaw)
	key := base64.StdEncoding.EncodeToString(keyRaw)
	req := "GET /ws/serial/1 HTTP/1.1\r\nHost: " + addr + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + key + "\r\n\r\n"
	if _, err := c.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	rd := bufio.NewReader(c)
	resp, err := http.ReadResponse(rd, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("рукопожатие: статус %d", resp.StatusCode)
	}
	sum := sha1.Sum([]byte(key + wsGUID))
	if got := resp.Header.Get("Sec-WebSocket-Accept"); got != base64.StdEncoding.EncodeToString(sum[:]) {
		t.Fatalf("неверный Sec-WebSocket-Accept: %s", got)
	}
	return &wsClient{c: c, rd: rd}
}

// send sends a masked frame (text or binary).
func (w *wsClient) send(t *testing.T, op byte, payload []byte) {
	t.Helper()
	var hdr []byte
	hdr = append(hdr, 0x80|op)
	n := len(payload)
	switch {
	case n < 126:
		hdr = append(hdr, 0x80|byte(n))
	case n <= 0xffff:
		hdr = append(hdr, 0x80|126, 0, 0)
		binary.BigEndian.PutUint16(hdr[2:], uint16(n))
	default:
		hdr = append(hdr, 0x80|127, 0, 0, 0, 0, 0, 0, 0, 0)
		binary.BigEndian.PutUint64(hdr[2:], uint64(n))
	}
	key := []byte{0x12, 0x34, 0x56, 0x78}
	masked := make([]byte, n)
	for i := range payload {
		masked[i] = payload[i] ^ key[i%4]
	}
	if _, err := w.c.Write(append(append(hdr, key...), masked...)); err != nil {
		t.Fatal(err)
	}
}

// read reads one server frame (unmasked).
func (w *wsClient) read(t *testing.T) (byte, []byte) {
	t.Helper()
	w.c.SetReadDeadline(time.Now().Add(3 * time.Second))
	h := make([]byte, 2)
	if _, err := io.ReadFull(w.rd, h); err != nil {
		t.Fatalf("чтение кадра: %v", err)
	}
	op := h[0] & 0x0f
	n := uint64(h[1] & 0x7f)
	switch n {
	case 126:
		var b [2]byte
		io.ReadFull(w.rd, b[:])
		n = uint64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		io.ReadFull(w.rd, b[:])
		n = binary.BigEndian.Uint64(b[:])
	}
	p := make([]byte, n)
	if _, err := io.ReadFull(w.rd, p); err != nil {
		t.Fatalf("чтение данных: %v", err)
	}
	return op, p
}

// readBinary accumulates binary frames up to the desired length: guest output may arrive in several frames.
func (w *wsClient) readBinaryUntil(t *testing.T, want string) string {
	t.Helper()
	var got bytes.Buffer
	for got.Len() < len(want) {
		op, p := w.read(t)
		if op != opBinary {
			t.Fatalf("ожидался бинарный кадр, пришёл тип %#x", op)
		}
		got.Write(p)
	}
	return got.String()
}

func TestSerialBridgeOutputInputEcho(t *testing.T) {
	guest, _ := startGuest(t, "BOOT OK\r\n")
	srv := httptest.NewServer(bridgeHandler(guest))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")
	ws := dialWS(t, addr)
	defer ws.c.Close()

	// Output: the guest's prompt reaches the browser as a binary frame.
	if out := ws.readBinaryUntil(t, "BOOT OK\r\n"); out != "BOOT OK\r\n" {
		t.Fatalf("вывод: %q", out)
	}

	// Input: a text frame with a command goes to the guest, the echo returns to the browser.
	ws.send(t, opText, []byte("ls -la\r"))
	if out := ws.readBinaryUntil(t, "ls -la\r"); out != "ls -la\r" {
		t.Fatalf("эхо ввода: %q", out)
	}

	// Binary input (as from TextTerminal when pasting UTF-8) also passes through completely.
	ws.send(t, opBinary, []byte("привет\x03"))
	if out := ws.readBinaryUntil(t, "привет\x03"); out != "привет\x03" {
		t.Fatalf("UTF-8 и управляющие символы: %q", out)
	}
}

func TestSerialBridgeControlPingIsNotForwarded(t *testing.T) {
	guest, received := startGuest(t, "")
	srv := httptest.NewServer(bridgeHandler(guest))
	defer srv.Close()
	ws := dialWS(t, strings.TrimPrefix(srv.URL, "http://"))
	defer ws.c.Close()

	ws.send(t, opText, []byte(controlPrefix+"ping"))
	if op, p := ws.read(t); op != opText || string(p) != controlPrefix+"pong" {
		t.Fatalf("control pong: op=%#x payload=%q", op, p)
	}
	select {
	case got := <-received:
		t.Fatalf("control frame reached guest: %q", got)
	case <-time.After(100 * time.Millisecond):
	}

	// Ordinary text input still reaches the guest and is echoed normally.
	ws.send(t, opText, []byte("hello\r"))
	if out := ws.readBinaryUntil(t, "hello\r"); out != "hello\r" {
		t.Fatalf("echo after control: %q", out)
	}
}

func TestSerialBridgeLongFrames(t *testing.T) {
	guest, _ := startGuest(t, "")
	srv := httptest.NewServer(bridgeHandler(guest))
	defer srv.Close()
	ws := dialWS(t, strings.TrimPrefix(srv.URL, "http://"))
	defer ws.c.Close()

	// Length 126..65535 and greater than 65535 - testing 16- and 64-bit frame headers.
	for _, n := range []int{125, 126, 300, 70000} {
		payload := bytes.Repeat([]byte{'a' + byte(n%26)}, n)
		ws.send(t, opBinary, payload)
		if out := ws.readBinaryUntil(t, string(payload)); out != string(payload) {
			t.Fatalf("длина %d: эхо не совпало (получено %d байт)", n, len(out))
		}
	}
}

func TestSerialBridgePingCloseAndGuestGone(t *testing.T) {
	guest, _ := startGuest(t, "")
	srv := httptest.NewServer(bridgeHandler(guest))
	defer srv.Close()
	ws := dialWS(t, strings.TrimPrefix(srv.URL, "http://"))

	// Ping -> Pong with the same payload (the browser keeps the connection alive).
	ws.send(t, opPing, []byte("p1"))
	if op, p := ws.read(t); op != opPong || string(p) != "p1" {
		t.Fatalf("pong: op=%#x payload=%q", op, p)
	}

	// Closure from the browser: the server responds with a close frame and terminates the connection.
	ws.send(t, opClose, []byte{0x03, 0xe8})
	op, _ := ws.read(t)
	if op != opClose {
		t.Fatalf("ожидался close-кадр, пришёл %#x", op)
	}
}

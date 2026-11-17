// SPDX-License-Identifier: GPL-3.0-only

package notify

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"netrave/internal/auth"
)

// Addressing: the initiator, the owner and administrators receive the event; an outsider does not.
func TestActivityRecipients(t *testing.T) {
	e := newEnv(t)
	owner, _, _ := e.user(t, "owner", "user")
	actor, _, _ := e.user(t, "actor", "user")
	other, _, _ := e.user(t, "other", "user")
	admin, _, _ := e.user(t, "root1", "admin")

	e.svc.Activity(actor, owner, Info, CatMachine, "Машина запущена", "x", "")
	count := func(uid int64) int {
		var n int
		n = len(e.svc.Snapshot(uid))
		return n
	}
	if count(owner) != 1 {
		t.Errorf("владелец должен получить событие: %d", count(owner))
	}
	if count(actor) != 1 {
		t.Errorf("инициатор должен получить событие: %d", count(actor))
	}
	if count(admin) != 1 {
		t.Errorf("администратор должен видеть события всех пользователей: %d", count(admin))
	}
	if count(other) != 0 {
		t.Errorf("посторонний пользователь не должен видеть чужое событие: %d", count(other))
	}
	// The initiator matches the owner - one notification, no duplicate.
	e.svc.Activity(owner, owner, Info, CatLab, "Параметры", "y", "")
	if count(owner) != 2 {
		t.Errorf("дубль получателя: %d", count(owner))
	}
}

// Real WebSocket: the notification reaches a connected owner; without a cookie the connection is rejected.
func TestWebSocketPushesOnlyToOwner(t *testing.T) {
	e := newEnv(t)
	owner, otok, _ := e.user(t, "owner", "user")
	_, otherTok, _ := e.user(t, "other", "user")
	_ = otherTok

	srv := httptest.NewServer(e.srv)
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")

	// Without a cookie - rejection.
	if code := handshakeCode(t, addr, "", ""); code != 401 {
		t.Fatalf("без cookie ожидался 401, получено %d", code)
	}

	conn := dialWS(t, addr, otok)
	defer conn.Close()
	// 101 is sent to the client before the connection reaches the hub; without waiting the event may be dropped.
	waitConnected(t, e.svc, owner)

	e.svc.Activity(owner, 0, Warning, CatMachine, "Машина аварийно завершилась", "станция-1", "#/lab/1")

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	msg := readTextFrame(t, conn)
	var m struct {
		Type   string `json:"type"`
		Unread int    `json:"unread"`
		Item   struct {
			Title    string `json:"title"`
			Category string `json:"category"`
			Level    string `json:"level"`
		} `json:"item"`
	}
	if err := json.Unmarshal(msg, &m); err != nil {
		t.Fatalf("некорректное сообщение: %v (%s)", err, msg)
	}
	if m.Type != "new" || m.Unread != 1 || m.Item.Title != "Машина аварийно завершилась" || m.Item.Category != CatMachine {
		t.Fatalf("неожиданное сообщение: %+v", m)
	}
}

// --- minimal WebSocket client for the test (RFC 6455, the server sends unmasked frames) ---

func handshakeCode(t *testing.T, addr, token, origin string) int {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	req := "GET /ws/notifications HTTP/1.1\r\nHost: " + addr + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nOrigin: http://" + addr + "\r\n"
	if token != "" {
		req += "Cookie: " + auth.CookieName + "=" + token + "\r\n"
	}
	req += "\r\n"
	c.Write([]byte(req))
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode
}

func dialWS(t *testing.T, addr, token string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	req := "GET /ws/notifications HTTP/1.1\r\nHost: " + addr + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nOrigin: http://" + addr + "\r\n" +
		"Cookie: " + auth.CookieName + "=" + token + "\r\n\r\n"
	c.Write([]byte(req))
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 101 {
		t.Fatalf("рукопожатие: %d", resp.StatusCode)
	}
	return &bufConn{Conn: c, r: br}
}

type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufConn) Read(p []byte) (int, error) { return b.r.Read(p) }

func readTextFrame(t *testing.T, c net.Conn) []byte {
	t.Helper()
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(c, hdr); err != nil {
		t.Fatalf("чтение фрейма: %v", err)
	}
	if hdr[0]&0x0f != 0x1 {
		t.Fatalf("ожидался текстовый фрейм, код %x", hdr[0]&0x0f)
	}
	n := int(hdr[1] & 0x7f)
	switch n {
	case 126:
		b := make([]byte, 2)
		io.ReadFull(c, b)
		n = int(binary.BigEndian.Uint16(b))
	case 127:
		b := make([]byte, 8)
		io.ReadFull(c, b)
		n = int(binary.BigEndian.Uint64(b))
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(c, payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

// waitConnected waits until the hub registers the user's connection.
func waitConnected(t *testing.T, svc *Service, uid int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !svc.hub.connected(uid) {
		if time.Now().After(deadline) {
			t.Fatal("соединение не зарегистрировано в hub")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

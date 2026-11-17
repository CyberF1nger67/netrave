// SPDX-License-Identifier: GPL-3.0-only

package notify

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"netrave/internal/labtest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"netrave/internal/auth"
	"netrave/internal/database"
)

type env struct {
	db  *database.DB
	as  *auth.Service
	svc *Service
	srv http.Handler
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "n.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	as := auth.New(db)
	svc := New(db)
	mux := http.NewServeMux()
	svc.Register(mux, as)
	return &env{db: db, as: as, svc: svc, srv: mux}
}

func (e *env) user(t *testing.T, name, role string) (int64, string, string) {
	t.Helper()
	res, err := e.db.Exec(`INSERT INTO users(username,password_hash,role) VALUES(?,?,?)`, name, "x", role)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	tok, csrf, _, err := e.as.CreateSession(id, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	return id, tok, csrf
}

func (e *env) call(t *testing.T, tok, csrf, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", csrf)
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	return rec
}

type listResp struct {
	Items  []Item `json:"items"`
	Unread int    `json:"unread"`
}

func TestSendListReadDelete(t *testing.T) {
	e := newEnv(t)
	uid, tok, csrf := e.user(t, "alice", "user")
	e.svc.Send(uid, "bogus-level", "machine", "Заголовок", "тело", "#/lab/1")

	var lr listResp
	rec := e.call(t, tok, csrf, http.MethodGet, "/api/notifications", nil)
	if rec.Code != 200 {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	json.Unmarshal(rec.Body.Bytes(), &lr)
	if len(lr.Items) != 1 || lr.Unread != 1 {
		t.Fatalf("ожидалось 1 уведомление, непрочитанных 1; получено %+v", lr)
	}
	if lr.Items[0].Level != Info {
		t.Errorf("неизвестный уровень должен стать info, получено %q", lr.Items[0].Level)
	}
	id := lr.Items[0].ID

	if rec := e.call(t, tok, csrf, http.MethodPut, "/api/notifications/"+strconv.FormatInt(id, 10), map[string]bool{"read": true}); rec.Code != 200 {
		t.Fatalf("mark read: %d %s", rec.Code, rec.Body)
	}
	json.Unmarshal(e.call(t, tok, csrf, http.MethodGet, "/api/notifications", nil).Body.Bytes(), &lr)
	if lr.Unread != 0 || !lr.Items[0].Read {
		t.Fatalf("после пометки ожидалось 0 непрочитанных: %+v", lr)
	}

	e.svc.Send(uid, Warning, "machine", "Второе", "", "")
	if rec := e.call(t, tok, csrf, http.MethodPost, "/api/notifications/read-all", nil); rec.Code != 200 {
		t.Fatalf("read-all: %d", rec.Code)
	}
	json.Unmarshal(e.call(t, tok, csrf, http.MethodGet, "/api/notifications?unread=1", nil).Body.Bytes(), &lr)
	if len(lr.Items) != 0 {
		t.Fatalf("unread=1 после read-all должен быть пуст: %+v", lr)
	}

	if rec := e.call(t, tok, csrf, http.MethodDelete, "/api/notifications/"+strconv.FormatInt(id, 10), nil); rec.Code != 200 {
		t.Fatalf("delete: %d", rec.Code)
	}
	if rec := e.call(t, tok, csrf, http.MethodDelete, "/api/notifications/"+strconv.FormatInt(id, 10), nil); rec.Code != 404 {
		t.Fatalf("повторное удаление должно давать 404, получено %d", rec.Code)
	}
	if rec := e.call(t, tok, csrf, http.MethodPost, "/api/notifications/clear-read", nil); rec.Code != 200 {
		t.Fatalf("clear-read: %d", rec.Code)
	}
	json.Unmarshal(e.call(t, tok, csrf, http.MethodGet, "/api/notifications", nil).Body.Bytes(), &lr)
	if len(lr.Items) != 0 {
		t.Fatalf("после clear-read прочитанных быть не должно: %+v", lr)
	}
}

func TestNotificationsIsolatedBetweenUsers(t *testing.T) {
	e := newEnv(t)
	aid, _, _ := e.user(t, "alice", "user")
	_, btok, bcsrf := e.user(t, "bob", "user")
	e.svc.Send(aid, Info, "system", "Только для Alice", "", "")

	var lr listResp
	json.Unmarshal(e.call(t, btok, bcsrf, http.MethodGet, "/api/notifications", nil).Body.Bytes(), &lr)
	if len(lr.Items) != 0 {
		t.Fatalf("Bob видит чужие уведомления: %+v", lr.Items)
	}
	var aidItem int64
	aidItem = e.svc.Snapshot(aid)[0].ID
	if rec := e.call(t, btok, bcsrf, http.MethodDelete, "/api/notifications/"+strconv.FormatInt(aidItem, 10), nil); rec.Code != 404 {
		t.Fatalf("Bob удалил чужое уведомление (код %d)", rec.Code)
	}
}

func TestHistoryIsCapped(t *testing.T) {
	e := newEnv(t)
	uid, _, _ := e.user(t, "carol", "user")
	for i := 0; i < KeepPerUser+25; i++ {
		e.svc.Send(uid, Info, "system", "n", strconv.Itoa(i), "") // different text: not a duplicate
	}
	var n int
	n = len(e.svc.Snapshot(uid))
	if n != KeepPerUser {
		t.Fatalf("история не ограничена: %d записей, ожидалось %d", n, KeepPerUser)
	}
}

func TestAddressing(t *testing.T) {
	e := newEnv(t)
	adminID, _, _ := e.user(t, "root1", "admin")
	userID, _, _ := e.user(t, "dave", "user")
	disabledID, _, _ := e.user(t, "old", "user")
	e.db.Exec(`UPDATE users SET disabled=1 WHERE id=?`, disabledID)

	e.svc.SendAdmins(Warning, "system", "t", "", "")
	e.svc.SendActive(0, Info, "template", "t", "", "")
	e.svc.SendActive(userID, Info, "template", "skip-author", "", "")

	count := func(uid int64) int {
		var n int
		n = len(e.svc.Snapshot(uid))
		return n
	}
	// Administrator: warning (SendAdmins), broadcast, and a message "except the author" (the author is dave).
	if count(adminID) != 3 {
		t.Errorf("администратор: ожидалось 3, получено %d", count(adminID))
	}
	if count(userID) != 1 { // broadcast only, without the excluded author
		t.Errorf("пользователь: ожидалось 1, получено %d", count(userID))
	}
	if count(disabledID) != 0 {
		t.Errorf("отключённый пользователь не должен получать рассылку")
	}

	// Instance owner: lab, instance and a notification to them.
	labID := labtest.Lab(t, e.db, userID, "L", "none")
	res, err := e.db.Exec(`INSERT INTO lab_instances(lab_id,user_id) VALUES(?,?)`, labID, userID)
	if err != nil {
		t.Fatal(err)
	}
	instID, _ := res.LastInsertId()
	before := count(userID)
	e.svc.SendInstance(instID, Error, "machine", "x", "", "")
	if count(userID) != before+1 {
		t.Errorf("SendInstance не доставил владельцу экземпляра")
	}
}

func TestNilServiceIsSafe(t *testing.T) {
	var s *Service
	s.Send(1, Info, "x", "y", "", "")
	s.SendAdmins(Info, "x", "y", "", "")
	s.SendActive(0, Info, "x", "y", "", "")
	s.SendInstance(1, Info, "x", "y", "", "")
}

func TestDuplicateUnreadIsNotRepeated(t *testing.T) {
	e := newEnv(t)
	uid, _, _ := e.user(t, "erin", "user")
	for i := 0; i < 3; i++ {
		e.svc.Send(uid, Warning, "system", "Проверка окружения не пройдена", "qemu: не найден", "#/dashboard")
	}
	var n int
	n = len(e.svc.Snapshot(uid))
	if n != 1 {
		t.Fatalf("одинаковое непрочитанное уведомление продублировано: %d", n)
	}
	// After reading, the same notification is created again: it is a new event.
	e.svc.mem.readAll(uid, time.Now())
	e.svc.Send(uid, Warning, "system", "Проверка окружения не пройдена", "qemu: не найден", "#/dashboard")
	n = len(e.svc.Snapshot(uid))
	if n != 2 {
		t.Fatalf("после прочтения новое событие должно создаться: %d", n)
	}
	// Different text - a separate notification.
	e.svc.Send(uid, Warning, "system", "Проверка окружения не пройдена", "dnsmasq: не найден", "#/dashboard")
	n = len(e.svc.Snapshot(uid))
	if n != 3 {
		t.Fatalf("разный текст должен давать отдельное уведомление: %d", n)
	}
}

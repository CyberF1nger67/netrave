// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"net/http"
	"path/filepath"
	"testing"

	"netrave/internal/auth"
	"netrave/internal/database"
	"netrave/internal/notify"
)

// An administrator's message reaches only the chosen active users, or every active user, and it is
// stored as a notification of the messages category. Regular users cannot send messages.
func TestAdminMessagesReachChosenUsers(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "messages.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	as := auth.New(db)
	svc := notify.New(db)
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as, Notify: svc}).Register(mux)

	_, adminTok, adminCSRF := addTestUser(t, db, as, "root", "admin")
	targetID, _, _ := addTestUser(t, db, as, "target", "user")
	otherID, _, _ := addTestUser(t, db, as, "other", "user")
	disabledID, _, _ := addTestUser(t, db, as, "gone", "user")
	if _, err := db.Exec(`UPDATE users SET disabled=1 WHERE id=?`, disabledID); err != nil {
		t.Fatal(err)
	}

	send := func(body map[string]any) *http.Response {
		t.Helper()
		return apiCall(t, mux, adminTok, adminCSRF, http.MethodPost, "/api/messages", body).Result()
	}

	if rec := send(map[string]any{"text": "  ", "user_ids": []int64{targetID}}); rec.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty text: HTTP %d", rec.StatusCode)
	}
	if rec := send(map[string]any{"text": "hi"}); rec.StatusCode != http.StatusBadRequest {
		t.Fatalf("no recipients: HTTP %d", rec.StatusCode)
	}
	if rec := send(map[string]any{"text": "hi", "level": "error", "user_ids": []int64{targetID}}); rec.StatusCode != http.StatusBadRequest {
		t.Fatalf("unsupported level: HTTP %d", rec.StatusCode)
	}
	if rec := send(map[string]any{"text": "hi", "user_ids": []int64{disabledID}}); rec.StatusCode != http.StatusBadRequest {
		t.Fatalf("only disabled recipients: HTTP %d", rec.StatusCode)
	}

	if rec := send(map[string]any{"text": "Maintenance tonight", "level": "warning", "user_ids": []int64{targetID, targetID}}); rec.StatusCode != http.StatusOK {
		t.Fatalf("send to one user: HTTP %d", rec.StatusCode)
	}
	items := svc.Snapshot(targetID)
	if len(items) != 1 || items[0].Category != notify.CatMessage || items[0].Level != notify.Warning {
		t.Fatalf("target notifications: %+v", items)
	}
	if items[0].Params["text"] != "Maintenance tonight" || items[0].Params["sender"] != "root" {
		t.Fatalf("message parameters: %+v", items[0].Params)
	}
	if n := len(svc.Snapshot(otherID)); n != 0 {
		t.Fatalf("unrelated user received %d notifications", n)
	}

	if rec := send(map[string]any{"text": "For everyone", "all": true}); rec.StatusCode != http.StatusOK {
		t.Fatalf("send to all: HTTP %d", rec.StatusCode)
	}
	if n := len(svc.Snapshot(otherID)); n != 1 {
		t.Fatalf("active user got %d notifications, want 1", n)
	}
	if n := len(svc.Snapshot(disabledID)); n != 0 {
		t.Fatalf("disabled user got %d notifications", n)
	}

	// A regular user is refused.
	_, userTok, userCSRF := addTestUser(t, db, as, "plain", "user")
	rec := apiCall(t, mux, userTok, userCSRF, http.MethodPost, "/api/messages", map[string]any{"text": "hi", "all": true})
	if rec.Code == http.StatusOK {
		t.Fatal("regular user sent a message")
	}
}

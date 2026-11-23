// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"netrave/internal/auth"
	"netrave/internal/database"
	"netrave/internal/notify"
)

// A user's global permissions are granted by roles: the user list returns roles and their permissions.
func TestUserRolePermsInList(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	as := auth.New(db)
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as}).Register(mux)

	res, _ := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('admin','x','admin')`)
	adminID, _ := res.LastInsertId()
	res, _ = db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('u1','x','user')`)
	uid, _ := res.LastInsertId()
	res, _ = db.Exec(`INSERT INTO roles(name) VALUES('Observer')`)
	roleID, _ := res.LastInsertId()
	for _, p := range []string{"images", "logs"} {
		if _, err := db.Exec(`INSERT INTO role_permissions(role_id,permission) VALUES(?,?)`, roleID, p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO user_roles(user_id,role_id) VALUES(?,?)`, uid, roleID); err != nil {
		t.Fatal(err)
	}
	tok, _, _, err := as.CreateSession(adminID, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("user list: HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var list []struct {
		ID        int64    `json:"id"`
		RolePerms []string `json:"role_perms"`
	}
	json.Unmarshal(rec.Body.Bytes(), &list)
	for _, u := range list {
		if u.ID == uid {
			if len(u.RolePerms) != 2 || u.RolePerms[0] != "images" || u.RolePerms[1] != "logs" {
				t.Fatalf("role permissions in list: %v", u.RolePerms)
			}
			return
		}
	}
	t.Fatal("user not found in list")
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

// Changing a user account notifies only them; the initiating administrator
// and other administrators do not receive a message about "their own" changed permissions.
func TestUserUpdateNotificationTargetsChangedAccount(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "un.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	as := auth.New(db)
	svc := notify.New(db)
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as, Notify: svc}).Register(mux)

	res, _ := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('boss','x','admin')`)
	actorID, _ := res.LastInsertId()
	res, _ = db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('target','x','user')`)
	targetID, _ := res.LastInsertId()
	res, _ = db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('other','x','admin')`)
	otherID, _ := res.LastInsertId()
	tok, csrf, _, err := as.CreateSession(actorID, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	call := func(path string, body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(b))
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
		req.Header.Set("X-CSRF-Token", csrf)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	count := func(uid int64) int {
		var n int
		n = len(svc.Snapshot(uid))
		return n
	}

	if rec := call("/api/users/"+itoa(targetID), map[string]any{"disabled": true}); rec.Code != http.StatusOK {
		t.Fatalf("user update: HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if got := count(targetID); got != 1 {
		t.Fatalf("target user should receive one notification, got %d", got)
	}
	if got := count(actorID); got != 0 {
		t.Fatalf("initiator should not receive a notification about another account, got %d", got)
	}
	if got := count(otherID); got != 0 {
		t.Fatalf("another administrator should not receive a notification, got %d", got)
	}
	var title, body string
	title, body = lastNote(svc, targetID)
	if title != "Your account changed" || lastParams(svc, targetID)["user"] != "boss" {
		t.Fatalf("incorrect notification: %q / %q", title, body)
	}

	// The administrator's own permissions: the notification refers to their account.
	if rec := call("/api/users/"+itoa(actorID), map[string]any{"password": "new-password-1", "confirm": "new-password-1"}); rec.Code != http.StatusOK {
		t.Fatalf("own password change: HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if got := count(actorID); got != 1 {
		t.Fatalf("when changing own permissions the notification should go to self, got %d", got)
	}
	title, body = lastNote(svc, actorID)
	if title != "You changed your account" || !strings.Contains(lastParams(svc, actorID)["by_key"], "your own account") {
		t.Fatalf("incorrect self-notification: %q / %q", title, body)
	}
}

// lastNote returns the title and text of the user's latest notification (from the service's memory).
func lastNote(svc *notify.Service, uid int64) (string, string) {
	items := svc.Snapshot(uid)
	if len(items) == 0 {
		return "", ""
	}
	last := items[len(items)-1]
	return last.Title, last.Body
}

// lastParams returns the parameters of the newest notification of a user.
func lastParams(svc *notify.Service, uid int64) map[string]string {
	items := svc.Snapshot(uid)
	if len(items) == 0 {
		return nil
	}
	return items[len(items)-1].Params
}

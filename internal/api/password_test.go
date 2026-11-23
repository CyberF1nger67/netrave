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

	"netrave/internal/access"
	"netrave/internal/auth"
	"netrave/internal/database"
	"netrave/internal/names"
)

// Changing the password via "Account": the server accepts the `new` field and passwords
// of Latin letters, digits and symbols (no spaces) with eight or more characters.
func TestChangeOwnPassword(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "password.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	as := auth.New(db)
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as}).Register(mux)

	const oldPassword = "old-secret-123"
	hash, err := auth.HashPassword(oldPassword)
	if err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('alice',?,'user')`, hash)
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := res.LastInsertId()
	tok, csrf, _, err := as.CreateSession(uid, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	change := func(current, next string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(map[string]string{"current": current, "new": next, "confirm": next})
		req := httptest.NewRequest(http.MethodPut, "/api/me/password", bytes.NewReader(b))
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
		req.Header.Set("X-CSRF-Token", csrf)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	if rec := change(oldPassword, "short"); rec.Code != http.StatusBadRequest {
		t.Fatalf("short password accepted: HTTP %d: %s", rec.Code, rec.Body.String())
	}

	const newPassword = "novy-nadezhny-parol-2026"
	if rec := change(oldPassword, newPassword); rec.Code != http.StatusOK {
		t.Fatalf("password change: HTTP %d: %s", rec.Code, rec.Body.String())
	}

	_, stored, err := as.FindByUsername("alice")
	if err != nil {
		t.Fatal(err)
	}
	if !auth.VerifyPassword(newPassword, stored) {
		t.Fatal("new password was not saved")
	}
	if auth.VerifyPassword(oldPassword, stored) {
		t.Fatal("old password still works")
	}
}

func TestPasswordRulesLengthAndCharacters(t *testing.T) {
	cases := []struct {
		name string
		pw   string
		want bool
	}{
		{"7 chars", "1234567", false},
		{"8 chars", "12345678", true},
		{"256 chars", strings.Repeat("a", 256), true},
		{"257 chars", strings.Repeat("a", 257), false},
		{"cyrillic", strings.Repeat("я", 8), false},
		{"space inside", "pass word1", false},
		{"symbol", "pass!word1", true},
		{"tab inside", "pass\tword1", false},
	}
	for _, tc := range cases {
		if got := names.ValidPassword(tc.pw); got != tc.want {
			t.Errorf("%s: ValidPassword = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func addTestUser(t *testing.T, db *database.DB, as *auth.Service, name, role string) (int64, string, string) {
	t.Helper()
	hash, err := auth.HashPassword("old-secret-123")
	if err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES(?,?,?)`, name, hash, role)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	tok, csrf, _, err := as.CreateSession(id, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	return id, tok, csrf
}

func assignTestRole(t *testing.T, db *database.DB, userID int64, name string) {
	t.Helper()
	res, err := db.Exec(`INSERT INTO roles(name,description) VALUES(?, 'test role')`, name)
	if err != nil {
		t.Fatal(err)
	}
	roleID, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO user_roles(user_id,role_id) VALUES(?,?)`, userID, roleID); err != nil {
		t.Fatal(err)
	}
	access.InvalidateAllPermissions(db)
}

func grantTestRolePerm(t *testing.T, db *database.DB, role, perm string) {
	t.Helper()
	if _, err := db.Exec(`INSERT OR IGNORE INTO permissions(key,title,group_key,group_title,sort)
		VALUES(?,?,'users','Users',0)`, perm, perm); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO role_permissions(role_id,permission)
		SELECT id, ? FROM roles WHERE name=?`, perm, role); err != nil {
		t.Fatal(err)
	}
	access.InvalidateAllPermissions(db)
}

func changeTestPassword(mux http.Handler, tok, csrf, current, next string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(map[string]string{"current": current, "new": next, "confirm": next})
	req := httptest.NewRequest(http.MethodPut, "/api/me/password", bytes.NewReader(b))
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	req.Header.Set("X-CSRF-Token", csrf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// A role can forbid changing one's own password: without users.password the request
// is rejected, with the permission it succeeds. Users without roles and admins
// keep the previous behaviour.
func TestChangePasswordRespectsRolePermission(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "role-password.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	as := auth.New(db)
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as}).Register(mux)

	aliceID, aliceTok, aliceCSRF := addTestUser(t, db, as, "alice", "user")
	assignTestRole(t, db, aliceID, "Students")

	if rec := changeTestPassword(mux, aliceTok, aliceCSRF, "old-secret-123", "new-secret-123"); rec.Code != http.StatusForbidden {
		t.Fatalf("role without users.password: HTTP %d: %s", rec.Code, rec.Body.String())
	}

	grantTestRolePerm(t, db, "Students", "users.password")
	if rec := changeTestPassword(mux, aliceTok, aliceCSRF, "old-secret-123", "new-secret-123"); rec.Code != http.StatusOK {
		t.Fatalf("role with users.password: HTTP %d: %s", rec.Code, rec.Body.String())
	}

	_, bobTok, bobCSRF := addTestUser(t, db, as, "bob", "user")
	if rec := changeTestPassword(mux, bobTok, bobCSRF, "old-secret-123", "new-secret-123"); rec.Code != http.StatusOK {
		t.Fatalf("user without roles: HTTP %d: %s", rec.Code, rec.Body.String())
	}

	_, adminTok, adminCSRF := addTestUser(t, db, as, "root", "admin")
	if rec := changeTestPassword(mux, adminTok, adminCSRF, "old-secret-123", "new-secret-123"); rec.Code != http.StatusOK {
		t.Fatalf("administrator: HTTP %d: %s", rec.Code, rec.Body.String())
	}
}

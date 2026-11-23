// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"netrave/internal/auth"
	"netrave/internal/config"
	"netrave/internal/database"
	"netrave/internal/images"
	"netrave/internal/labs"
	"netrave/internal/notify"
)

func apiCall(t *testing.T, mux http.Handler, tok, csrf, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", csrf)
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func userRole(t *testing.T, db *database.DB, id int64) string {
	t.Helper()
	var role string
	if err := db.QueryRow(`SELECT role FROM users WHERE id=?`, id).Scan(&role); err != nil {
		t.Fatal(err)
	}
	return role
}

// An administrator can assign and revoke the administrator role for other
// users. Protection against losing the last administrator and against demoting
// oneself remains. Passwords are accepted only with matching
// confirmation.
func TestAdminRolesCanBeAssignedAndChanged(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "roles.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	as := auth.New(db)
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as, Notify: notify.New(db)}).Register(mux)

	adminID, adminTok, adminCSRF := addTestUser(t, db, as, "root", "admin")
	targetID, _, _ := addTestUser(t, db, as, "target", "user")

	create := func(body map[string]any) *httptest.ResponseRecorder {
		return apiCall(t, mux, adminTok, adminCSRF, http.MethodPost, "/api/users", body)
	}

	if rec := create(map[string]any{
		"username": "newadmin", "password": "secret-pass-1", "confirm": "secret-pass-1", "role": "admin",
	}); rec.Code != http.StatusCreated {
		t.Fatalf("creating administrator: HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM users WHERE username='newadmin'`).Scan(&count)
	if count != 1 {
		t.Fatal("administrator was not created")
	}

	if rec := create(map[string]any{
		"username": "mismatch", "password": "secret-pass-1", "confirm": "secret-pass-2", "role": "user",
	}); rec.Code != http.StatusBadRequest {
		t.Fatalf("mismatched confirmation on create: HTTP %d: %s", rec.Code, rec.Body.String())
	}
	db.QueryRow(`SELECT COUNT(*) FROM users WHERE username='mismatch'`).Scan(&count)
	if count != 0 {
		t.Fatal("user created with mismatched confirmation")
	}

	if rec := create(map[string]any{
		"username": "worker", "password": "secret-pass-1", "confirm": "secret-pass-1", "role": "user",
	}); rec.Code != http.StatusCreated {
		t.Fatalf("regular user: HTTP %d: %s", rec.Code, rec.Body.String())
	}

	if rec := apiCall(t, mux, adminTok, adminCSRF, http.MethodPut, "/api/users/"+itoa(targetID), map[string]any{"role": "admin"}); rec.Code != http.StatusOK {
		t.Fatalf("promotion to administrator: HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if got := userRole(t, db, targetID); got != "admin" {
		t.Fatalf("role did not change: %q", got)
	}

	otherAdminID, _, _ := addTestUser(t, db, as, "second-admin", "admin")
	if rec := apiCall(t, mux, adminTok, adminCSRF, http.MethodPut, "/api/users/"+itoa(otherAdminID), map[string]any{"role": "user"}); rec.Code != http.StatusOK {
		t.Fatalf("demotion of administrator: HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if got := userRole(t, db, otherAdminID); got != "user" {
		t.Fatalf("administrator role did not change: %q", got)
	}

	// You cannot demote yourself, even if there are other administrators in the system.
	if rec := apiCall(t, mux, adminTok, adminCSRF, http.MethodPut, "/api/users/"+itoa(adminID), map[string]any{"role": "user"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("self-demotion: HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if got := userRole(t, db, adminID); got != "admin" {
		t.Fatalf("own role changed to %q", got)
	}

	if rec := apiCall(t, mux, adminTok, adminCSRF, http.MethodPut, "/api/users/"+itoa(targetID), map[string]any{
		"password": "reset-pass-123", "confirm": "different-pass-123",
	}); rec.Code != http.StatusBadRequest {
		t.Fatalf("mismatched confirmation on reset: HTTP %d: %s", rec.Code, rec.Body.String())
	}
	_, stored, err := as.FindByUsername("target")
	if err != nil {
		t.Fatal(err)
	}
	if auth.VerifyPassword("reset-pass-123", stored) {
		t.Fatal("password changed despite mismatched confirmation")
	}

	if rec := apiCall(t, mux, adminTok, adminCSRF, http.MethodPut, "/api/users/"+itoa(targetID), map[string]any{
		"password": "reset-pass-123", "confirm": "reset-pass-123",
	}); rec.Code != http.StatusOK {
		t.Fatalf("password reset: HTTP %d: %s", rec.Code, rec.Body.String())
	}
	_, stored, err = as.FindByUsername("target")
	if err != nil {
		t.Fatal(err)
	}
	if !auth.VerifyPassword("reset-pass-123", stored) {
		t.Fatal("new administrator password was not saved")
	}
	if stored == "reset-pass-123" {
		t.Fatal("password stored in plain text")
	}
}

func TestAdminHasImagesAndTemplatesAccess(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "access.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	as := auth.New(db)
	svc := notify.New(db)
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as, Notify: svc}).Register(mux)
	(&images.Handler{DB: db, Auth: as, Cfg: &config.Config{DisksDir: t.TempDir()}, Notify: svc}).Register(mux)
	(&labs.Handler{DB: db, Auth: as, Notify: svc}).Register(mux)

	_, adminTok, adminCSRF := addTestUser(t, db, as, "root", "admin")
	_, userTok, userCSRF := addTestUser(t, db, as, "plain", "user")

	if rec := apiCall(t, mux, userTok, userCSRF, http.MethodPost, "/api/images/scan", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("image scan without permission: HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if rec := apiCall(t, mux, adminTok, adminCSRF, http.MethodPost, "/api/images/scan", nil); rec.Code != http.StatusOK {
		t.Fatalf("administrator and images: HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if rec := apiCall(t, mux, adminTok, adminCSRF, http.MethodGet, "/api/images", nil); rec.Code != http.StatusOK {
		t.Fatalf("image view by administrator: HTTP %d: %s", rec.Code, rec.Body.String())
	}

	template := map[string]any{
		"name": "Admin_template", "vendor": "Test", "type": "linux",
		"arch": "x86_64", "machine": "pc", "cpu": 1, "ram_mb": 128, "nics": 1,
		"nic_model": "e1000", "disk_bus": "virtio", "bios": "seabios", "console": "serial",
	}
	if rec := apiCall(t, mux, userTok, userCSRF, http.MethodPost, "/api/templates", template); rec.Code != http.StatusForbidden {
		t.Fatalf("template creation without permission: HTTP %d: %s", rec.Code, rec.Body.String())
	}
	template["name"] = "Admin_template"
	if rec := apiCall(t, mux, adminTok, adminCSRF, http.MethodPost, "/api/templates", template); rec.Code != http.StatusCreated {
		t.Fatalf("administrator and templates: HTTP %d: %s", rec.Code, rec.Body.String())
	}
}

// Changing one's own password rejects a mismatched confirmation and verifies
// the current password on the server.
func TestChangeOwnPasswordRequiresMatchingConfirmation(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "change.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	as := auth.New(db)
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as, Notify: notify.New(db)}).Register(mux)

	_, tok, csrf := addTestUser(t, db, as, "alice", "user")
	change := func(current, next, confirm string) *httptest.ResponseRecorder {
		return apiCall(t, mux, tok, csrf, http.MethodPut, "/api/me/password", map[string]string{
			"current": current, "new": next, "confirm": confirm,
		})
	}

	if rec := change("old-secret-123", "new-secret-123", "other-secret-123"); rec.Code != http.StatusBadRequest {
		t.Fatalf("mismatched confirmation: HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if rec := change("wrong-password", "new-secret-123", "new-secret-123"); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong current password: HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if rec := change("old-secret-123", "new-secret-123", "new-secret-123"); rec.Code != http.StatusOK {
		t.Fatalf("matching confirmation: HTTP %d: %s", rec.Code, rec.Body.String())
	}
	_, stored, err := as.FindByUsername("alice")
	if err != nil {
		t.Fatal(err)
	}
	if !auth.VerifyPassword("new-secret-123", stored) {
		t.Fatal("new password was not saved")
	}
}

// When creating a user, the administrator immediately assigns a role from the "Roles" section.
func TestCreateUserWithCustomRole(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	as := auth.New(db)
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as, Notify: notify.New(db)}).Register(mux)
	_, adminTok, adminCSRF := addTestUser(t, db, as, "root", "admin")
	userID, userTok, userCSRF := addTestUser(t, db, as, "alice", "user")

	res, err := db.Exec(`INSERT INTO roles(name,description) VALUES('Operator','')`)
	if err != nil {
		t.Fatal(err)
	}
	roleID, _ := res.LastInsertId()

	rec := apiCall(t, mux, adminTok, adminCSRF, http.MethodPost, "/api/users", map[string]any{
		"username": "bob", "password": "secret-pass-1", "confirm": "secret-pass-1", "role": "user", "role_id": roleID,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating user with role: HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM user_roles ur JOIN users u ON u.id=ur.user_id WHERE u.username='bob' AND ur.role_id=?`, roleID).Scan(&n)
	if n != 1 {
		t.Fatal("role not assigned on user creation")
	}

	// An unknown role is rejected.
	if rec := apiCall(t, mux, adminTok, adminCSRF, http.MethodPost, "/api/users", map[string]any{
		"username": "carol", "password": "secret-pass-1", "role": "user", "role_id": 9999,
	}); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown role: expected 400, got %d", rec.Code)
	}

	// A regular user with management permission cannot grant a role.
	if rec := apiCall(t, mux, userTok, userCSRF, http.MethodPost, "/api/users", map[string]any{
		"username": "dave", "password": "secret-pass-1", "role": "user", "role_id": roleID,
	}); rec.Code != http.StatusForbidden {
		t.Fatalf("granting role by non-administrator: expected 403, got %d", rec.Code)
	}
	_ = userID
}

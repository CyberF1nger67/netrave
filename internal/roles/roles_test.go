// SPDX-License-Identifier: GPL-3.0-only

package roles

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"netrave/internal/access"
	"netrave/internal/api"
	"netrave/internal/auth"
	"netrave/internal/config"
	"netrave/internal/database"
	"netrave/internal/labfile"
	"netrave/internal/labtest"
	"netrave/internal/permcatalog"
)

type env struct {
	db  *database.DB
	as  *auth.Service
	srv http.Handler
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "roles.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := &config.Config{ConfigPath: filepath.Join(t.TempDir(), "netrave.json")}
	as := auth.New(db)
	as.UseConfig(cfg)
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as, Cfg: cfg}).Register(mux)
	(&api.Handler{DB: db, Auth: as, Cfg: cfg}).Register(mux)
	return &env{db: db, as: as, srv: mux}
}

type who struct {
	id        int64
	tok, csrf string
}

func (e *env) user(t *testing.T, name, role string) who {
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
	return who{id, tok, csrf}
}

func (e *env) call(u who, method, path string, body any) *httptest.ResponseRecorder {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: u.tok})
	if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", u.csrf)
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	return rec
}

func (e *env) createRole(t *testing.T, admin who, name string, perms []string) Role {
	t.Helper()
	rec := e.call(admin, http.MethodPost, "/api/roles", map[string]any{
		"name": name, "description": "test role", "permissions": perms,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating role: %d %s", rec.Code, rec.Body)
	}
	var role Role
	if err := json.Unmarshal(rec.Body.Bytes(), &role); err != nil {
		t.Fatal(err)
	}
	return role
}

func (e *env) addMembers(t *testing.T, admin who, role int64, ids ...int64) {
	t.Helper()
	rec := e.call(admin, http.MethodPost, "/api/roles/"+strconv.FormatInt(role, 10)+"/members",
		map[string]any{"user_ids": ids})
	if rec.Code != http.StatusOK {
		t.Fatalf("adding members: %d %s", rec.Code, rec.Body)
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func TestRoleCRUDPermissionsAndMembers(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	alice := e.user(t, "alice", "user")
	bob := e.user(t, "bob", "user")

	role := e.createRole(t, admin, "Teacher", []string{permcatalog.LabView, permcatalog.LabEdit})
	if role.MemberCount != 0 || len(role.Permissions) != 2 {
		t.Fatalf("role created incorrectly: %+v", role)
	}
	e.addMembers(t, admin, role.ID, alice.id, bob.id)

	var list []Role
	rec := e.call(admin, http.MethodGet, "/api/roles", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("role list: %d %s", rec.Code, rec.Body)
	}
	json.Unmarshal(rec.Body.Bytes(), &list)
	// The first entry is the built-in administrator role, shown for display only.
	if len(list) != 2 || !list[0].System || list[0].ID != AdminRoleID {
		t.Fatalf("built-in administrator role must be first: %+v", list)
	}
	if list[1].MemberCount != 2 {
		t.Fatalf("members not counted: %+v", list)
	}

	rec = e.call(admin, http.MethodPut, "/api/roles/"+strconv.FormatInt(role.ID, 10), map[string]any{
		"name": "Teacher-2", "description": "updated",
		"permissions": []string{permcatalog.LabView, permcatalog.LabStartStop},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("updating role: %d %s", rec.Code, rec.Body)
	}
	var updated Role
	json.Unmarshal(rec.Body.Bytes(), &updated)
	if updated.Name != "Teacher-2" || !contains(updated.Permissions, permcatalog.LabStartStop) {
		t.Fatalf("role updated incorrectly: %+v", updated)
	}

	rec = e.call(admin, http.MethodDelete, "/api/roles/"+strconv.FormatInt(role.ID, 10)+"/members/"+strconv.FormatInt(alice.id, 10), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("removing member: %d %s", rec.Code, rec.Body)
	}
	var members []Member
	rec = e.call(admin, http.MethodGet, "/api/roles/"+strconv.FormatInt(role.ID, 10)+"/members", nil)
	json.Unmarshal(rec.Body.Bytes(), &members)
	if len(members) != 1 || members[0].ID != bob.id {
		t.Fatalf("members after removal: %+v", members)
	}
	if rec = e.call(admin, http.MethodDelete, "/api/roles/"+strconv.FormatInt(role.ID, 10), nil); rec.Code != http.StatusOK {
		t.Fatalf("deleting role: %d %s", rec.Code, rec.Body)
	}
	var count int
	e.db.QueryRow(`SELECT COUNT(*) FROM user_roles WHERE role_id=?`, role.ID).Scan(&count)
	if count != 0 {
		t.Fatal("links of deleted role remained")
	}
}

// The built-in administrator role is read-only: it cannot be changed or deleted.
func TestAdminRoleIsReadOnly(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	path := "/api/roles/" + strconv.FormatInt(AdminRoleID, 10)
	for _, c := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodDelete, path, nil},
		{http.MethodPut, path + "/permissions", map[string]any{"permissions": []string{}}},
	} {
		if rec := e.call(admin, c.method, c.path, c.body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s %s: expected 400, got %d", c.method, c.path, rec.Code)
		}
	}
	// The description of the built-in role can be changed.
	if rec := e.call(admin, http.MethodPut, path, map[string]any{"description": "changed"}); rec.Code != http.StatusOK {
		t.Fatalf("changing the administrator role description: %d %s", rec.Code, rec.Body)
	}
}

// A role gains access to a lab only through an entry in the lab file; deleting the role
// removes that entry from every lab.
func TestRoleLabAccessAndDeletionCleanup(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	owner := e.user(t, "owner", "user")
	alice := e.user(t, "alice", "user")
	lab := labtest.Lab(t, e.db, owner.id, "lab", "none")

	role := e.createRole(t, admin, "Operators", []string{permcatalog.LabView})
	e.addMembers(t, admin, role.ID, alice.id)
	if _, err := e.db.Labs.Update(lab, func(l *labfile.Lab) error {
		l.Permissions = append(l.Permissions, labfile.Permission{RoleID: role.ID, Perm: access.LabView, Value: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	perms, err := access.Perms(e.db, lab, alice.id, false)
	if err != nil || !perms[access.LabView] {
		t.Fatalf("role entry must open the lab: %v %v", perms, err)
	}

	if rec := e.call(admin, http.MethodDelete, "/api/roles/"+strconv.FormatInt(role.ID, 10), nil); rec.Code != http.StatusOK {
		t.Fatalf("deleting role: %d %s", rec.Code, rec.Body)
	}
	fl, _ := e.db.Labs.Lab(lab)
	for _, p := range fl.Permissions {
		if p.RoleID == role.ID {
			t.Fatal("permissions of the deleted role remained in the lab file")
		}
	}
}

func TestRolePermissionsPersist(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	role := e.createRole(t, admin, "Persisted", []string{permcatalog.LabView, permcatalog.FolderAccess})
	var got Role
	rec := e.call(admin, http.MethodGet, "/api/roles/"+strconv.FormatInt(role.ID, 10), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("reading role: %d %s", rec.Code, rec.Body)
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	if !contains(got.Permissions, permcatalog.LabView) || !contains(got.Permissions, permcatalog.FolderAccess) {
		t.Fatalf("permissions lost: %+v", got.Permissions)
	}
}

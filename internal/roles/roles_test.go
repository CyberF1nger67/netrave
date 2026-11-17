// SPDX-License-Identifier: GPL-3.0-only

package roles

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"netrave/internal/labtest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"netrave/internal/access"
	"netrave/internal/api"
	"netrave/internal/auth"
	"netrave/internal/config"
	"netrave/internal/database"
	"netrave/internal/permcatalog"
)

type env struct {
	db      *database.DB
	as      *auth.Service
	srv     http.Handler
	path    string
	folders string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "roles.db")
	folders := filepath.Join(dir, "folders.yaml")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.AttachFolders(folders); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{ConfigPath: filepath.Join(t.TempDir(), "netrave.json")}
	as := auth.New(db)
	as.UseConfig(cfg)
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as, Cfg: cfg}).Register(mux)
	(&api.Handler{DB: db, Auth: as, Cfg: cfg}).Register(mux)
	return &env{db: db, as: as, srv: mux, path: path, folders: folders}
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

func (e *env) createRole(t *testing.T, admin who, name string, perms []string, folders []int64) Role {
	t.Helper()
	rec := e.call(admin, http.MethodPost, "/api/roles", map[string]any{
		"name": name, "description": "test role", "permissions": perms, "folder_ids": folders,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("создание роли: %d %s", rec.Code, rec.Body)
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
		t.Fatalf("добавление участников: %d %s", rec.Code, rec.Body)
	}
}

func (e *env) folder(t *testing.T, name string) int64 {
	t.Helper()
	f, err := e.db.Folders.Create(nil, name, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	return f.ID
}

func (e *env) lab(t *testing.T, owner int64, folder int64) int64 {
	t.Helper()
	id := labtest.Lab(t, e.db, owner, "lab", "none")
	if folder != 0 {
		if err := e.db.Folders.Place(id, folder); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func TestRoleCRUDPermissionsFoldersAndMembers(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	alice := e.user(t, "alice", "user")
	bob := e.user(t, "bob", "user")
	folder := e.folder(t, "Course")

	role := e.createRole(t, admin, "Teacher", []string{permcatalog.LabView, permcatalog.LabEdit}, []int64{folder})
	if role.MemberCount != 0 || len(role.Permissions) != 2 || len(role.FolderIDs) != 1 {
		t.Fatalf("роль создана неверно: %+v", role)
	}
	e.addMembers(t, admin, role.ID, alice.id, bob.id)

	var list []Role
	rec := e.call(admin, http.MethodGet, "/api/roles", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("список ролей: %d %s", rec.Code, rec.Body)
	}
	json.Unmarshal(rec.Body.Bytes(), &list)
	// The first entry is the built-in administrator role, shown for display only.
	if len(list) != 2 || !list[0].System || list[0].ID != AdminRoleID {
		t.Fatalf("встроенная роль администратора должна быть первой: %+v", list)
	}
	if list[1].MemberCount != 2 {
		t.Fatalf("участники не учтены: %+v", list)
	}

	// Changing the name, permissions and folders.
	rec = e.call(admin, http.MethodPut, "/api/roles/"+strconv.FormatInt(role.ID, 10), map[string]any{
		"name": "Teacher-2", "description": "updated",
		"permissions": []string{permcatalog.LabView, permcatalog.LabStartStop},
		"folder_ids":  []int64{},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("изменение роли: %d %s", rec.Code, rec.Body)
	}
	var updated Role
	json.Unmarshal(rec.Body.Bytes(), &updated)
	if updated.Name != "Teacher-2" || len(updated.FolderIDs) != 0 || !contains(updated.Permissions, permcatalog.LabStartStop) {
		t.Fatalf("роль изменена неверно: %+v", updated)
	}

	// Deleting a member and a role.
	rec = e.call(admin, http.MethodDelete, "/api/roles/"+strconv.FormatInt(role.ID, 10)+"/members/"+strconv.FormatInt(alice.id, 10), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("удаление участника: %d %s", rec.Code, rec.Body)
	}
	var members []Member
	rec = e.call(admin, http.MethodGet, "/api/roles/"+strconv.FormatInt(role.ID, 10)+"/members", nil)
	json.Unmarshal(rec.Body.Bytes(), &members)
	if len(members) != 1 || members[0].ID != bob.id {
		t.Fatalf("участники после удаления: %+v", members)
	}
	if rec = e.call(admin, http.MethodDelete, "/api/roles/"+strconv.FormatInt(role.ID, 10), nil); rec.Code != http.StatusOK {
		t.Fatalf("удаление роли: %d %s", rec.Code, rec.Body)
	}
	var count int
	e.db.QueryRow(`SELECT COUNT(*) FROM user_roles WHERE role_id=?`, role.ID).Scan(&count)
	if count != 0 {
		t.Fatal("связи удалённой роли остались")
	}
}

// The built-in administrator role is read-only: it cannot be changed or deleted.
func TestAdminRoleIsReadOnly(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	e.user(t, "alice", "user")
	path := "/api/roles/" + strconv.FormatInt(AdminRoleID, 10)

	rec := e.call(admin, http.MethodGet, path, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("чтение роли администратора: %d %s", rec.Code, rec.Body)
	}
	var role Role
	json.Unmarshal(rec.Body.Bytes(), &role)
	if !role.System || len(role.Permissions) != len(permcatalog.Catalog) || role.MemberCount != 1 {
		t.Fatalf("встроенная роль описана неверно: %+v", role)
	}

	// The built-in role's description changes, nothing else.
	rec = e.call(admin, http.MethodPut, path, map[string]any{"description": "Operators of the whole system"})
	if rec.Code != http.StatusOK {
		t.Fatalf("изменение описания роли администратора: %d %s", rec.Code, rec.Body)
	}
	json.Unmarshal(rec.Body.Bytes(), &role)
	if role.Description != "Operators of the whole system" || !role.System {
		t.Fatalf("описание не сохранено: %+v", role)
	}

	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPut, path + "/permissions", map[string]any{"permissions": []string{}}},
		{http.MethodPut, path + "/folders", map[string]any{"folder_ids": []int64{}}},
		{http.MethodDelete, path, nil},
	} {
		if rec := e.call(admin, tc.method, tc.path, tc.body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s %s: ожидался 400, получен %d %s", tc.method, tc.path, rec.Code, rec.Body)
		}
	}

	// The built-in role's name is taken.
	if rec := e.call(admin, http.MethodPost, "/api/roles", map[string]any{"name": "administrator"}); rec.Code != http.StatusConflict {
		t.Fatalf("создание роли с названием администратора: %d %s", rec.Code, rec.Body)
	}
}

// The simultaneous lab limit is stored in the role and returned in the list.
func TestRoleRunningLimit(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")

	rec := e.call(admin, http.MethodPost, "/api/roles", map[string]any{"name": "Limited", "max_running_labs": 2})
	if rec.Code != http.StatusCreated {
		t.Fatalf("создание роли: %d %s", rec.Code, rec.Body)
	}
	var role Role
	json.Unmarshal(rec.Body.Bytes(), &role)
	if role.MaxRunningLabs != 2 {
		t.Fatalf("предел не сохранён при создании: %+v", role)
	}

	rec = e.call(admin, http.MethodPut, "/api/roles/"+strconv.FormatInt(role.ID, 10), map[string]any{"max_running_labs": 5})
	if rec.Code != http.StatusOK {
		t.Fatalf("изменение роли: %d %s", rec.Code, rec.Body)
	}
	json.Unmarshal(rec.Body.Bytes(), &role)
	if role.MaxRunningLabs != 5 {
		t.Fatalf("предел не изменился: %+v", role)
	}

	// A negative value is rejected.
	if rec := e.call(admin, http.MethodPut, "/api/roles/"+strconv.FormatInt(role.ID, 10), map[string]any{"max_running_labs": -1}); rec.Code != http.StatusBadRequest {
		t.Fatalf("отрицательный предел: ожидался 400, получен %d", rec.Code)
	}
}

func TestRoleGrantsConsoleClipboard(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	owner := e.user(t, "owner", "user")
	alice := e.user(t, "alice", "user")

	folder := e.folder(t, "Clipboard")
	lab := e.lab(t, owner.id, folder)
	role := e.createRole(t, admin, "Operators", []string{permcatalog.LabView, permcatalog.MachClipboard}, []int64{folder})
	e.addMembers(t, admin, role.ID, alice.id)

	perms, err := access.Perms(e.db, lab, alice.id, false)
	if err != nil {
		t.Fatal(err)
	}
	if !perms[access.ConsoleClipboard] {
		t.Fatalf("роль не выдала console.clipboard: %v", perms)
	}
}

func TestOnlyAdminManagesRoles(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	alice := e.user(t, "alice", "user")
	role := e.createRole(t, admin, "Viewer", []string{permcatalog.LabView}, nil)
	e.addMembers(t, admin, role.ID, alice.id)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/roles"},
		{http.MethodPost, "/api/roles"},
		{http.MethodGet, "/api/roles/" + strconv.FormatInt(role.ID, 10)},
		{http.MethodPut, "/api/roles/" + strconv.FormatInt(role.ID, 10)},
		{http.MethodDelete, "/api/roles/" + strconv.FormatInt(role.ID, 10)},
		{http.MethodGet, "/api/roles/" + strconv.FormatInt(role.ID, 10) + "/members"},
		{http.MethodPost, "/api/roles/" + strconv.FormatInt(role.ID, 10) + "/members"},
		{http.MethodDelete, "/api/roles/" + strconv.FormatInt(role.ID, 10) + "/members/" + strconv.FormatInt(alice.id, 10)},
	} {
		rec := e.call(alice, tc.method, tc.path, map[string]any{"name": "Hack", "user_ids": []int64{alice.id}})
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: обычный пользователь получил %d", tc.method, tc.path, rec.Code)
		}
	}
}

func TestRoleUnionAndRevoke(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	owner := e.user(t, "owner", "user")
	alice := e.user(t, "alice", "user")

	folderA := e.folder(t, "A")
	folderB := e.folder(t, "B")
	labA := e.lab(t, owner.id, folderA)
	labB := e.lab(t, owner.id, folderB)

	roleA := e.createRole(t, admin, "A", []string{permcatalog.LabView, permcatalog.LabStartStop}, []int64{folderA})
	roleB := e.createRole(t, admin, "B", []string{permcatalog.LabView, permcatalog.LabCopy, permcatalog.LabDelete}, []int64{folderB})
	e.addMembers(t, admin, roleA.ID, alice.id)
	e.addMembers(t, admin, roleB.ID, alice.id)

	u, _, err := e.as.FindByUsername("alice")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{permcatalog.LabView, permcatalog.LabStartStop, permcatalog.LabCopy, permcatalog.LabDelete} {
		if !u.HasRolePerm(key) {
			t.Errorf("нет объединённого права %s: %v", key, u.RolePerms)
		}
	}
	permsA, err := access.Perms(e.db, labA, alice.id, false)
	if err != nil || !permsA[access.LabView] || !permsA[access.MachStart] {
		t.Fatalf("права на лабораторию A: %v %v", permsA, err)
	}
	permsB, err := access.Perms(e.db, labB, alice.id, false)
	if err != nil || !permsB[access.LabView] || !permsB[access.LabDelete] {
		t.Fatalf("права на лабораторию B: %v %v", permsB, err)
	}

	// Removing from role A revokes only its permissions and folder.
	rec := e.call(admin, http.MethodDelete, "/api/roles/"+strconv.FormatInt(roleA.ID, 10)+"/members/"+strconv.FormatInt(alice.id, 10), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("удаление из роли A: %d %s", rec.Code, rec.Body)
	}
	u, _, _ = e.as.FindByUsername("alice")
	if u.HasRolePerm(permcatalog.LabStartStop) {
		t.Error("право роли A сохранилось после удаления")
	}
	if !u.HasRolePerm(permcatalog.LabCopy) || !u.HasRolePerm(permcatalog.LabDelete) {
		t.Error("права роли B потерялись")
	}
	if _, err := access.Perms(e.db, labA, alice.id, false); err != sql.ErrNoRows {
		t.Errorf("папка роли A осталась доступна: %v", err)
	}
	if perms, err := access.Perms(e.db, labB, alice.id, false); err != nil || !perms[access.LabView] {
		t.Errorf("папка роли B потерялась: %v %v", perms, err)
	}
}

func TestRoleDoesNotGrantAdministrator(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	alice := e.user(t, "alice", "user")

	all := make([]string, 0, len(permcatalog.Catalog))
	for _, p := range permcatalog.Catalog {
		all = append(all, p.Key)
	}
	// A role with the maximum permission set, named differently, does not make one an administrator.
	role := e.createRole(t, admin, "Supervisor", all, nil)
	e.addMembers(t, admin, role.ID, alice.id)

	u, _, err := e.as.FindByUsername("alice")
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != "user" {
		t.Fatalf("роль изменила users.role: %q", u.Role)
	}
	// Even with admin.access a user cannot manage roles or appoint administrators.
	if rec := e.call(alice, http.MethodGet, "/api/roles", nil); rec.Code != http.StatusForbidden {
		t.Errorf("управление ролями через роль: %d", rec.Code)
	}
	rec := e.call(alice, http.MethodPut, "/api/users/"+strconv.FormatInt(alice.id, 10), map[string]any{"role": "admin"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("повышение до администратора через API: %d %s", rec.Code, rec.Body)
	}
	var roleName string
	e.db.QueryRow(`SELECT role FROM users WHERE id=?`, alice.id).Scan(&roleName)
	if roleName != "user" {
		t.Fatalf("пользователь стал администратором: %s", roleName)
	}
}

func TestRolesSurviveRestart(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	alice := e.user(t, "alice", "user")
	folder := e.folder(t, "Persist")
	role := e.createRole(t, admin, "Persisted", []string{permcatalog.LabView, permcatalog.FolderAccess}, []int64{folder})
	e.addMembers(t, admin, role.ID, alice.id)

	e.db.Close()
	db, err := database.Open(e.path)
	if err != nil {
		t.Fatalf("повторное открытие: %v", err)
	}
	defer db.Close()
	if err := db.AttachFolders(e.folders); err != nil {
		t.Fatal(err)
	}
	as := auth.New(db)
	u, _, err := as.FindByUsername("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Roles) != 1 || u.Roles[0].Name != "Persisted" {
		t.Fatalf("роли после перезапуска: %+v", u.Roles)
	}
	if !u.HasRolePerm(permcatalog.LabView) || !u.HasRolePerm(permcatalog.FolderAccess) {
		t.Fatalf("права после перезапуска: %v", u.RolePerms)
	}
	if ids := db.Folders.RoleFolderIDs(role.ID); len(ids) != 1 || ids[0] != folder {
		t.Fatalf("папки роли после перезапуска: %v", ids)
	}
	var members int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_roles WHERE role_id=? AND user_id=?`, role.ID, alice.id).Scan(&members); err != nil || members != 1 {
		t.Fatalf("участники после перезапуска: %d %v", members, err)
	}
}

// The built-in administrator role can be assigned to and removed from a member - this changes users.role.
func TestAdminRoleMembershipChangesAccountRole(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	alice := e.user(t, "alice", "user")
	base := "/api/roles/" + strconv.FormatInt(AdminRoleID, 10) + "/members"

	roleOf := func(id int64) string {
		t.Helper()
		var role string
		if err := e.db.QueryRow(`SELECT role FROM users WHERE id=?`, id).Scan(&role); err != nil {
			t.Fatal(err)
		}
		return role
	}

	if rec := e.call(admin, http.MethodPost, base, map[string]any{"user_ids": []int64{alice.id}}); rec.Code != http.StatusOK {
		t.Fatalf("назначение роли администратора: %d %s", rec.Code, rec.Body)
	}
	if roleOf(alice.id) != "admin" {
		t.Fatal("пользователь не стал администратором")
	}
	// The built-in role shows administrators as members.
	rec := e.call(admin, http.MethodGet, "/api/roles/"+strconv.FormatInt(AdminRoleID, 10), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("чтение роли: %d", rec.Code)
	}
	var role Role
	json.Unmarshal(rec.Body.Bytes(), &role)
	if role.MemberCount != 2 {
		t.Fatalf("участников администраторов: %d, ожидалось 2", role.MemberCount)
	}

	if rec := e.call(admin, http.MethodDelete, base+"/"+strconv.FormatInt(alice.id, 10), nil); rec.Code != http.StatusOK {
		t.Fatalf("снятие роли администратора: %d %s", rec.Code, rec.Body)
	}
	if roleOf(alice.id) != "user" {
		t.Fatal("пользователь не перестал быть администратором")
	}
}

// The last active administrator cannot be demoted.
func TestCannotRemoveLastAdmin(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	base := "/api/roles/" + strconv.FormatInt(AdminRoleID, 10) + "/members/" + strconv.FormatInt(admin.id, 10)
	if rec := e.call(admin, http.MethodDelete, base, nil); rec.Code != http.StatusConflict {
		t.Fatalf("понижение последнего администратора: ожидался 409, получен %d %s", rec.Code, rec.Body)
	}
}

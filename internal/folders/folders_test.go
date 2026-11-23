// SPDX-License-Identifier: GPL-3.0-only

package folders

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"

	"netrave/internal/access"
	"netrave/internal/auth"
	"netrave/internal/database"
	"netrave/internal/labtest"
)

type env struct {
	db  *database.DB
	as  *auth.Service
	srv http.Handler
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "f.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	as := auth.New(db)
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as}).Register(mux)
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

func (e *env) lab(t *testing.T, owner int64, name, level string) string {
	t.Helper()
	return labtest.Lab(t, e.db, owner, name, level)
}

func (e *env) folder(t *testing.T, u who, name, parent string) string {
	t.Helper()
	rec := e.call(u, http.MethodPost, "/api/lab-folders", map[string]any{"name": name, "parent": parent})
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating folder %q: %d %s", name, rec.Code, rec.Body)
	}
	var f Folder
	if err := json.Unmarshal(rec.Body.Bytes(), &f); err != nil {
		t.Fatal(err)
	}
	return f.Path
}

func (e *env) place(t *testing.T, u who, labName, folder string) {
	t.Helper()
	var body any
	if folder == "" {
		body = map[string]any{"folder": nil}
	} else {
		body = map[string]any{"folder": folder}
	}
	rec := e.call(u, http.MethodPut, "/api/labs/"+url.PathEscape(labName)+"/folder", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("moving lab: %d %s", rec.Code, rec.Body)
	}
}

func (e *env) list(t *testing.T, u who) []Folder {
	rec := e.call(u, http.MethodGet, "/api/lab-folders", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("folder list: %d %s", rec.Code, rec.Body)
	}
	var out []Folder
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOnlyAdminManagesFolders(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	alice := e.user(t, "alice", "user")
	f := e.folder(t, admin, "Net", "")

	if rec := e.call(alice, http.MethodPost, "/api/lab-folders", map[string]any{"name": "mine"}); rec.Code != http.StatusForbidden {
		t.Errorf("user created folder: %d", rec.Code)
	}
	if rec := e.call(alice, http.MethodPut, "/api/lab-folders", map[string]any{"path": f, "name": "hack"}); rec.Code != http.StatusForbidden {
		t.Errorf("user renamed folder: %d", rec.Code)
	}
	if rec := e.call(alice, http.MethodDelete, "/api/lab-folders?path="+url.QueryEscape(f), nil); rec.Code != http.StatusForbidden {
		t.Errorf("user deleted folder: %d", rec.Code)
	}
	lab := e.lab(t, alice.id, "alice-lab", "view")
	if rec := e.call(alice, http.MethodPut, "/api/labs/"+lab+"/folder", map[string]any{"folder": f}); rec.Code != http.StatusForbidden {
		t.Errorf("user moved lab: %d", rec.Code)
	}
	// Reading the tree is available to all authenticated users.
	if rec := e.call(alice, http.MethodGet, "/api/lab-folders", nil); rec.Code != http.StatusOK {
		t.Errorf("folder list: %d", rec.Code)
	}
}

func TestNestedFoldersCycleAndDepth(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	a := e.folder(t, admin, "A", "")
	b := e.folder(t, admin, "B", a)
	c := e.folder(t, admin, "C", b)

	if rec := e.call(admin, http.MethodPut, "/api/lab-folders", map[string]any{"path": a, "parent": a}); rec.Code != http.StatusBadRequest {
		t.Errorf("move into itself: expected 400, got %d", rec.Code)
	}
	if rec := e.call(admin, http.MethodPut, "/api/lab-folders", map[string]any{"path": a, "parent": c}); rec.Code != http.StatusBadRequest {
		t.Errorf("cycle: expected 400, got %d", rec.Code)
	}
	if rec := e.call(admin, http.MethodPut, "/api/lab-folders", map[string]any{"path": b, "parent": nil}); rec.Code != http.StatusOK {
		t.Fatalf("move to root: %d %s", rec.Code, rec.Body)
	}
	list := e.list(t, admin)
	if len(list) != 3 {
		t.Fatalf("expected 3 folders, got %d", len(list))
	}
	// Nesting limit.
	parent := ""
	for i := 1; i <= maxDepth; i++ {
		parent = e.folder(t, admin, "L"+strconv.Itoa(i), parent)
	}
	if rec := e.call(admin, http.MethodPost, "/api/lab-folders", map[string]any{"name": "too-deep", "parent": parent}); rec.Code != http.StatusBadRequest {
		t.Errorf("depth beyond %d must be rejected: %d", maxDepth, rec.Code)
	}
}

func TestFolderVisibleOnlyWithAccessibleLab(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	alice := e.user(t, "alice", "user")
	bob := e.user(t, "bob", "user")
	shared := e.folder(t, admin, "Shared", "")
	foreign := e.folder(t, admin, "Foreign", "")
	lab := e.lab(t, bob.id, "bob-lab", "none")
	e.place(t, admin, lab, shared)

	// The lab is not public, so alice does not see it or its folder.
	if _, err := access.Perms(e.db, lab, alice.id, false); err != nil {
		t.Fatal(err)
	}
	if got := e.list(t, alice); len(got) != 0 {
		t.Fatalf("alice sees folders without an accessible lab: %+v", got)
	}
	// A direct permission opens the lab and, through it, the folder.
	labtest.Perm(t, e.db, lab, alice.id, access.LabView, true)
	got := e.list(t, alice)
	if len(got) != 1 || got[0].Path != shared || got[0].LabCount != 1 {
		t.Fatalf("folder not derived from the accessible lab: %+v", got)
	}
	if got[0].Path == foreign {
		t.Fatal("foreign folder is visible")
	}
	// The administrator sees every folder, including empty ones.
	if list := e.list(t, admin); len(list) != 2 {
		t.Fatalf("admin folder list: %+v", list)
	}
}

func TestSubtreeMakesAncestorsVisible(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	viewer := e.user(t, "viewer", "user")
	parent := e.folder(t, admin, "Course", "")
	child := e.folder(t, admin, "Labs", parent)
	lab := e.lab(t, admin.id, "public-lab", "view")
	e.place(t, admin, lab, child)

	list := e.list(t, viewer)
	got := map[string]int{}
	for _, f := range list {
		got[f.Path] = f.LabCount
	}
	if _, ok := got[parent]; !ok {
		t.Errorf("ancestor of a visible folder is missing: %+v", got)
	}
	if got[child] != 1 {
		t.Errorf("lab count in the child folder: %d", got[child])
	}
}

func TestDeleteFolderKeepsLabs(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	owner := e.user(t, "owner", "user")
	lab := e.lab(t, owner.id, "owner-lab", "none")

	f := e.folder(t, admin, "Temp", "")
	e.place(t, admin, lab, f)

	if rec := e.call(admin, http.MethodDelete, "/api/lab-folders?path="+url.QueryEscape(f), nil); rec.Code != http.StatusOK {
		t.Fatalf("deleting folder: %d %s", rec.Code, rec.Body)
	}
	if _, ok := e.db.Labs.Lab(lab); !ok {
		t.Fatal("deleting folder removed lab")
	}
	if folder, _ := e.db.Labs.Folder(lab); folder != "" {
		t.Errorf("after deleting folder the lab must return to the root: %q", folder)
	}

	// Deletion with moving to another folder, including nested subfolders.
	f2 := e.folder(t, admin, "Old", "")
	sub := e.folder(t, admin, "Nested", f2)
	target := e.folder(t, admin, "New", "")
	e.place(t, admin, lab, sub)
	rec := e.call(admin, http.MethodDelete, "/api/lab-folders?path="+url.QueryEscape(f2)+"&move_to="+url.QueryEscape(target), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("deleting with move: %d %s", rec.Code, rec.Body)
	}
	if folder, _ := e.db.Labs.Folder(lab); folder != target {
		t.Fatalf("lab not moved to the specified folder: %q", folder)
	}
	if paths := e.list(t, admin); len(paths) != 1 || paths[0].Path != target {
		t.Fatalf("nested folders were not removed: %+v", paths)
	}
}

func TestRenameAndMoveFolder(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	a := e.folder(t, admin, "A", "")
	b := e.folder(t, admin, "B", "")
	lab := e.lab(t, admin.id, "lab", "none")
	e.place(t, admin, lab, a)

	rec := e.call(admin, http.MethodPut, "/api/lab-folders", map[string]any{"path": a, "name": "Renamed"})
	if rec.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body)
	}
	if folder, _ := e.db.Labs.Folder(lab); folder != "Renamed" {
		t.Fatalf("renaming the folder lost the lab placement: %q", folder)
	}
	rec = e.call(admin, http.MethodPut, "/api/lab-folders", map[string]any{"path": "Renamed", "parent": b})
	if rec.Code != http.StatusOK {
		t.Fatalf("move: %d %s", rec.Code, rec.Body)
	}
	if folder, _ := e.db.Labs.Folder(lab); folder != filepath.ToSlash(filepath.Join(b, "Renamed")) {
		t.Fatalf("moving the folder lost the lab placement: %q", folder)
	}
}

func TestFolderStructurePersists(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	f := e.folder(t, admin, "Perm", "")
	lab := e.lab(t, admin.id, "persist-lab", "none")
	e.place(t, admin, lab, f)

	// The tree is the file system, so a new database/store over the same directory sees it.
	if folder, ok := e.db.Labs.Folder(lab); !ok || folder != "Perm" {
		t.Fatalf("placement not saved: %q %v", folder, ok)
	}
}

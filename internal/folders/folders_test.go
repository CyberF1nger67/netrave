// SPDX-License-Identifier: GPL-3.0-only

package folders

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"netrave/internal/access"
	"netrave/internal/auth"
	"netrave/internal/database"
	"netrave/internal/labtest"
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
	path := filepath.Join(dir, "f.db")
	folders := filepath.Join(dir, "folders.yaml")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.AttachFolders(folders); err != nil {
		t.Fatal(err)
	}
	as := auth.New(db)
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as}).Register(mux)
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

func (e *env) lab(t *testing.T, owner int64, level string) int64 {
	t.Helper()
	return labtest.Lab(t, e.db, owner, "lab", level)
}

func (e *env) folder(t *testing.T, u who, name string, parent *int64) int64 {
	t.Helper()
	rec := e.call(u, http.MethodPost, "/api/lab-folders", map[string]any{"name": name, "parent_id": parent})
	if rec.Code != http.StatusCreated {
		t.Fatalf("создание папки %q: %d %s", name, rec.Code, rec.Body)
	}
	var f Folder
	json.Unmarshal(rec.Body.Bytes(), &f)
	return f.ID
}

func (e *env) assign(t *testing.T, admin who, uid int64, ids ...int64) {
	t.Helper()
	rec := e.call(admin, http.MethodPut, "/api/users/"+strconv.FormatInt(uid, 10)+"/folders",
		map[string]any{"folder_ids": ids})
	if rec.Code != http.StatusOK {
		t.Fatalf("назначение папок: %d %s", rec.Code, rec.Body)
	}
}

func (e *env) place(t *testing.T, admin who, lab int64, folder *int64) {
	t.Helper()
	rec := e.call(admin, http.MethodPut, "/api/labs/"+strconv.FormatInt(lab, 10)+"/folder",
		map[string]any{"folder_id": folder})
	if rec.Code != http.StatusOK {
		t.Fatalf("перемещение лаборатории: %d %s", rec.Code, rec.Body)
	}
}

func i64(v int64) *int64 { return &v }

func TestOnlyAdminManagesFolders(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	alice := e.user(t, "alice", "user")
	f := e.folder(t, admin, "Net", nil)

	if rec := e.call(alice, http.MethodPost, "/api/lab-folders", map[string]any{"name": "mine"}); rec.Code != http.StatusForbidden {
		t.Errorf("пользователь создал папку: %d", rec.Code)
	}
	if rec := e.call(alice, http.MethodPut, "/api/lab-folders/"+id(f), map[string]any{"name": "hack"}); rec.Code != http.StatusForbidden {
		t.Errorf("пользователь переименовал папку: %d", rec.Code)
	}
	if rec := e.call(alice, http.MethodDelete, "/api/lab-folders/"+id(f), nil); rec.Code != http.StatusForbidden {
		t.Errorf("пользователь удалил папку: %d", rec.Code)
	}
	lab := e.lab(t, alice.id, "view")
	if rec := e.call(alice, http.MethodPut, "/api/labs/"+id(lab)+"/folder", map[string]any{"folder_id": f}); rec.Code != http.StatusForbidden {
		t.Errorf("пользователь переместил лабораторию: %d", rec.Code)
	}
	if rec := e.call(alice, http.MethodGet, "/api/users/"+id(alice.id)+"/folders", nil); rec.Code != http.StatusForbidden {
		t.Errorf("пользователь прочитал назначения: %d", rec.Code)
	}
	if rec := e.call(alice, http.MethodPut, "/api/users/"+id(alice.id)+"/folders", map[string]any{"folder_ids": []int64{f}}); rec.Code != http.StatusForbidden {
		t.Errorf("пользователь назначил себе папку: %d", rec.Code)
	}
	// Reading the tree is available to all authenticated users.
	if rec := e.call(alice, http.MethodGet, "/api/lab-folders", nil); rec.Code != http.StatusOK {
		t.Errorf("список папок: %d", rec.Code)
	}
}

func TestNestedFoldersCycleAndDepth(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	a := e.folder(t, admin, "A", nil)
	b := e.folder(t, admin, "B", i64(a))
	c := e.folder(t, admin, "C", i64(b))

	if rec := e.call(admin, http.MethodPut, "/api/lab-folders/"+id(a), map[string]any{"parent_id": a}); rec.Code != http.StatusBadRequest {
		t.Errorf("перенос в себя: ожидался 400, получено %d", rec.Code)
	}
	if rec := e.call(admin, http.MethodPut, "/api/lab-folders/"+id(a), map[string]any{"parent_id": c}); rec.Code != http.StatusBadRequest {
		t.Errorf("цикл: ожидался 400, получено %d", rec.Code)
	}
	if rec := e.call(admin, http.MethodPut, "/api/lab-folders/"+id(b), map[string]any{"parent_id": nil}); rec.Code != http.StatusOK {
		t.Fatalf("перенос в корень: %d %s", rec.Code, rec.Body)
	}
	if bf, ok := e.db.Folders.Folder(b); !ok || bf.ParentID != 0 {
		t.Errorf("после переноса в корень parent_id должен быть NULL")
	}
	var list []Folder
	rec := e.call(admin, http.MethodGet, "/api/lab-folders", nil)
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 3 {
		t.Fatalf("ожидалось 3 папки, получено %d", len(list))
	}
	// Nesting limit.
	var pid *int64
	for i := 1; i <= maxDepth; i++ {
		pid = i64(e.folder(t, admin, "L"+strconv.Itoa(i)+"-"+strconv.FormatInt(int64(i), 10), pid))
	}
	if rec := e.call(admin, http.MethodPost, "/api/lab-folders", map[string]any{"name": "too-deep", "parent_id": pid}); rec.Code != http.StatusBadRequest {
		t.Errorf("глубина сверх %d должна отклоняться: %d", maxDepth, rec.Code)
	}
}

func TestAssignmentGrantsAndRevokesFolderAccess(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	owner := e.user(t, "owner", "user")
	viewer := e.user(t, "viewer", "user")
	lab := e.lab(t, owner.id, "view")

	f := e.folder(t, admin, "Shared", nil)
	e.place(t, admin, lab, i64(f))

	// A lab in an unassigned folder does not exist for a viewer.
	if _, err := access.Perms(e.db, lab, viewer.id, false); err != sql.ErrNoRows {
		t.Fatalf("до назначения ожидался sql.ErrNoRows, получено %v", err)
	}
	e.assign(t, admin, viewer.id, f)
	perms, err := access.Perms(e.db, lab, viewer.id, false)
	if err != nil || !perms[access.LabView] {
		t.Fatalf("после назначения нужен просмотр: %v %v", perms, err)
	}
	// The administrator sees the lab regardless of assignments.
	if perms, err := access.Perms(e.db, lab, admin.id, true); err != nil || !perms[access.LabView] {
		t.Fatalf("администратор должен видеть лабораторию: %v %v", perms, err)
	}
	// Revoking access hides the lab.
	e.assign(t, admin, viewer.id)
	if _, err := access.Perms(e.db, lab, viewer.id, false); err != sql.ErrNoRows {
		t.Fatalf("после отзыва ожидался sql.ErrNoRows, получено %v", err)
	}
}

func TestAssignmentOfParentCoversSubfolders(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	owner := e.user(t, "owner", "user")
	viewer := e.user(t, "viewer", "user")
	lab := e.lab(t, owner.id, "view")

	parent := e.folder(t, admin, "Course", nil)
	child := e.folder(t, admin, "Labs", i64(parent))
	e.place(t, admin, lab, i64(child))
	e.assign(t, admin, viewer.id, parent)

	perms, err := access.Perms(e.db, lab, viewer.id, false)
	if err != nil || !perms[access.LabView] {
		t.Fatalf("назначение родительской папки должно открывать подпапки: %v %v", perms, err)
	}

	// The viewer's folder list: the assigned folder and its subfolders, no others.
	other := e.folder(t, admin, "Foreign", nil)
	var list []Folder
	rec := e.call(viewer, http.MethodGet, "/api/lab-folders", nil)
	json.Unmarshal(rec.Body.Bytes(), &list)
	got := map[int64]bool{}
	for _, f := range list {
		got[f.ID] = true
	}
	if !got[parent] || !got[child] {
		t.Errorf("зритель не видит назначенную папку и подпапку: %+v", got)
	}
	if got[other] {
		t.Errorf("зритель видит чужую папку")
	}
	for _, f := range list {
		if f.ID == child && f.LabCount != 1 {
			t.Errorf("счётчик лабораторий в папке: %d", f.LabCount)
		}
	}
}

func TestDeleteFolderKeepsLabs(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	owner := e.user(t, "owner", "user")
	lab := e.lab(t, owner.id, "none")

	f := e.folder(t, admin, "Temp", nil)
	e.place(t, admin, lab, i64(f))

	if rec := e.call(admin, http.MethodDelete, "/api/lab-folders/"+id(f), nil); rec.Code != http.StatusOK {
		t.Fatalf("удаление папки: %d %s", rec.Code, rec.Body)
	}
	if _, ok := e.db.Labs.Lab(lab); !ok {
		t.Fatal("удаление папки удалило лабораторию")
	}
	if _, ok := e.db.Folders.Placement(lab); ok {
		t.Errorf("после удаления папки лаборатория должна вернуться в «Без папки»")
	}

	// Deletion with moving to another folder.
	f2 := e.folder(t, admin, "Old", nil)
	target := e.folder(t, admin, "New", nil)
	e.place(t, admin, lab, i64(f2))
	rec := e.call(admin, http.MethodDelete, "/api/lab-folders/"+id(f2)+"?move_to="+id(target), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("удаление с переносом: %d %s", rec.Code, rec.Body)
	}
	if folder, ok := e.db.Folders.Placement(lab); !ok || folder != target {
		t.Fatalf("лаборатория не перенесена в указанную папку: %d %v", folder, ok)
	}
}

func TestFolderStructureAndAssignmentsPersist(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	viewer := e.user(t, "viewer", "user")
	f := e.folder(t, admin, "Perm", nil)
	e.assign(t, admin, viewer.id, f)
	lab := e.lab(t, admin.id, "none")
	e.place(t, admin, lab, i64(f))
	e.db.Close()

	// Reopening the same database: structure and assignments are in place.
	db, err := database.Open(e.path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.AttachFolders(e.folders); err != nil {
		t.Fatal(err)
	}
	if got, ok := db.Folders.Folder(f); !ok || got.Name != "Perm" {
		t.Fatalf("папка не сохранилась: %+v %v", got, ok)
	}
	if ids := db.Folders.UserFolderIDs(viewer.id); len(ids) != 1 || ids[0] != f {
		t.Errorf("назначение папки не сохранилось: %v", ids)
	}
	if p, ok := db.Folders.Placement(lab); !ok || p != f {
		t.Errorf("расположение лаборатории не сохранилось: %d %v", p, ok)
	}
}

func TestUserFoldersEndpoint(t *testing.T) {
	e := newEnv(t)
	admin := e.user(t, "root", "admin")
	viewer := e.user(t, "viewer", "user")
	a := e.folder(t, admin, "A", nil)
	b := e.folder(t, admin, "B", nil)
	e.assign(t, admin, viewer.id, a, b)

	var got struct {
		FolderIDs []int64 `json:"folder_ids"`
	}
	rec := e.call(admin, http.MethodGet, "/api/users/"+id(viewer.id)+"/folders", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("чтение назначений: %d %s", rec.Code, rec.Body)
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.FolderIDs) != 2 {
		t.Fatalf("ожидалось 2 папки, получено %v", got.FolderIDs)
	}
	// An unknown folder is rejected, previous assignments are unchanged.
	rec = e.call(admin, http.MethodPut, "/api/users/"+id(viewer.id)+"/folders", map[string]any{"folder_ids": []int64{a, 99999}})
	if rec.Code != http.StatusNotFound {
		t.Errorf("неизвестная папка: ожидался 404, получено %d", rec.Code)
	}
	if n := len(e.db.Folders.UserFolderIDs(viewer.id)); n != 2 {
		t.Errorf("отклонённый запрос изменил назначения: %d", n)
	}
}

func id(v int64) string { return strconv.FormatInt(v, 10) }

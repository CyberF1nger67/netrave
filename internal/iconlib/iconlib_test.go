// SPDX-License-Identifier: GPL-3.0-only

package iconlib

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"netrave/internal/auth"
	"netrave/internal/database"
	"netrave/internal/icons"
)

func pngBytes(t *testing.T, side int, c color.Color) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// env - database, sessions and icons directory in one directory: this checks persistence after a restart.
type env struct {
	db    *database.DB
	as    *auth.Service
	icons *icons.Store
	srv   http.Handler
}

func open(t *testing.T, dir string) *env {
	t.Helper()
	db, err := database.Open(filepath.Join(dir, "netrave.db"))
	if err != nil {
		t.Fatal(err)
	}
	as := auth.New(db)
	st := icons.New(filepath.Join(dir, "icons"))
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as, Icons: st}).Register(mux)
	return &env{db: db, as: as, icons: st, srv: mux}
}

type who struct {
	id        int64
	tok, csrf string
}

func (e *env) user(t *testing.T, name string) who {
	t.Helper()
	return e.account(t, name, "user")
}

func (e *env) admin(t *testing.T, name string) who {
	t.Helper()
	return e.account(t, name, "admin")
}

func (e *env) account(t *testing.T, name, role string) who {
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

func (e *env) call(u who, method, path, ctype string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: u.tok})
	if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", u.csrf)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	return rec
}

func (e *env) list(t *testing.T, u who) []Item {
	t.Helper()
	rec := e.call(u, http.MethodGet, "/api/icon-library", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: code %d, %s", rec.Code, rec.Body.String())
	}
	var items []Item
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	return items
}

func (e *env) upload(t *testing.T, u who, query string, data []byte) uploadResult {
	t.Helper()
	return e.uploadAs(t, u, query, "image/png", data)
}

func (e *env) uploadAs(t *testing.T, u who, query, ctype string, data []byte) uploadResult {
	t.Helper()
	rec := e.call(u, http.MethodPost, "/api/icon-library"+query, ctype, data)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("upload: code %d, %s", rec.Code, rec.Body.String())
	}
	var r uploadResult
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestUploadDedupesSameImage(t *testing.T) {
	e := open(t, t.TempDir())
	defer e.db.Close()
	ann := e.user(t, "ann")
	png1 := pngBytes(t, 16, color.NRGBA{R: 200, A: 255})

	first := e.upload(t, ann, "?name=Router", png1)
	if !first.Created || first.Name != "Router" {
		t.Fatalf("first upload: created=%v name=%q", first.Created, first.Name)
	}
	again := e.upload(t, ann, "?name=Other", png1)
	if again.Created {
		t.Fatal("re-upload of same picture created duplicate")
	}
	if again.ID != first.ID || again.Name != "Router" {
		t.Fatalf("repeat must return same record with old name: id %d/%d name %q", again.ID, first.ID, again.Name)
	}
	if items := e.list(t, ann); len(items) != 1 {
		t.Fatalf("%d records in library, expected 1", len(items))
	}
}

func TestLibrarySurvivesRestartAndRelogin(t *testing.T) {
	dir := t.TempDir()
	e := open(t, dir)
	ann := e.user(t, "ann")
	up := e.upload(t, ann, "?name=Switch", pngBytes(t, 32, color.NRGBA{G: 180, A: 255}))
	e.db.Close()

	// New process: the same database and the same icons directory. The session is in the database, so sign-in persists.
	e2 := open(t, dir)
	defer e2.db.Close()
	items := e2.list(t, ann)
	if len(items) != 1 || items[0].Hash != up.Hash || items[0].Name != "Switch" {
		t.Fatalf("library after restart: %+v", items)
	}
	rec := e2.call(ann, http.MethodGet, items[0].URL, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("picture after restart: code %d", rec.Code)
	}
	if _, err := png.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil {
		t.Fatalf("picture after restart is not PNG: %v", err)
	}
}

func TestRenameAndIsolation(t *testing.T) {
	e := open(t, t.TempDir())
	defer e.db.Close()
	ann := e.user(t, "ann")
	bob := e.user(t, "bob")
	it := e.upload(t, ann, "", pngBytes(t, 20, color.NRGBA{B: 255, A: 255}))

	rename := []byte(`{"name":"Switch"}`)
	path := "/api/icon-library/" + strconv.FormatInt(it.ID, 10)
	if rec := e.call(ann, http.MethodPut, path, "application/json", rename); rec.Code != http.StatusOK {
		t.Fatalf("rename by owner: code %d, %s", rec.Code, rec.Body.String())
	}
	if items := e.list(t, ann); items[0].Name != "Switch" {
		t.Fatalf("name did not change: %q", items[0].Name)
	}

	// Another user's entry is indistinguishable from a nonexistent one.
	if rec := e.call(bob, http.MethodPut, path, "application/json", rename); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign rename: code %d, expected 404", rec.Code)
	}
	if rec := e.call(bob, http.MethodGet, path+"/image", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign picture: code %d, expected 404", rec.Code)
	}
	if items := e.list(t, bob); len(items) != 0 {
		t.Fatalf("another user's library is not empty: %+v", items)
	}
}

func TestAdminsShareIconLibrary(t *testing.T) {
	e := open(t, t.TempDir())
	defer e.db.Close()
	ann := e.admin(t, "ann")
	bob := e.admin(t, "bob")
	carol := e.user(t, "carol")
	it := e.upload(t, ann, "?name=Router", pngBytes(t, 18, color.NRGBA{R: 30, G: 120, A: 255}))

	// The second administrator sees the first one's icon and can open its picture.
	items := e.list(t, bob)
	if len(items) != 1 || items[0].ID != it.ID || items[0].Name != "Router" {
		t.Fatalf("shared admin library: %+v", items)
	}
	if rec := e.call(bob, http.MethodGet, it.URL, "", nil); rec.Code != http.StatusOK {
		t.Fatalf("picture for another admin: code %d", rec.Code)
	}

	// A regular user does not see administrator icons.
	if items := e.list(t, carol); len(items) != 0 {
		t.Fatalf("admin icons visible to user: %+v", items)
	}
	if rec := e.call(carol, http.MethodGet, it.URL, "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("user opened admin picture: code %d", rec.Code)
	}

	// Uploading the same picture again by another administrator does not create a duplicate.
	again := e.upload(t, bob, "?name=Other", pngBytes(t, 18, color.NRGBA{R: 30, G: 120, A: 255}))
	if again.Created || again.ID != it.ID {
		t.Fatalf("duplicate in shared library: created=%v id=%d, expected id=%d", again.Created, again.ID, it.ID)
	}
}

func TestDeleteKeepsFileStillInUse(t *testing.T) {
	e := open(t, t.TempDir())
	defer e.db.Close()
	ann := e.user(t, "ann")
	bob := e.user(t, "bob")
	data := pngBytes(t, 24, color.NRGBA{R: 10, G: 10, B: 10, A: 255})
	a := e.upload(t, ann, "", data)
	b := e.upload(t, bob, "", data)
	if a.Hash != b.Hash {
		t.Fatal("same picture must have one hash across users")
	}

	// Ann deletes her entry, but the file is still needed by Bob's library.
	path := "/api/icon-library/" + strconv.FormatInt(a.ID, 10)
	if rec := e.call(ann, http.MethodDelete, path, "", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: code %d, %s", rec.Code, rec.Body.String())
	}
	if !e.icons.Exists(a.Hash) {
		t.Fatal("file deleted although another user's library uses it")
	}

	// The last entry is deleted - the file is no longer needed by anyone.
	path = "/api/icon-library/" + strconv.FormatInt(b.ID, 10)
	if rec := e.call(bob, http.MethodDelete, path, "", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("deleting last record: code %d", rec.Code)
	}
	if e.icons.Exists(a.Hash) {
		t.Fatal("file remained on disk after last reference was deleted")
	}
}

func TestRejectsUnsupportedTypeAndBrokenImage(t *testing.T) {
	e := open(t, t.TempDir())
	defer e.db.Close()
	ann := e.user(t, "ann")
	if rec := e.call(ann, http.MethodPost, "/api/icon-library", "text/plain", []byte("x")); rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("unsupported Content-Type: code %d", rec.Code)
	}
	if rec := e.call(ann, http.MethodPost, "/api/icon-library", "image/png", []byte("not a png")); rec.Code != http.StatusBadRequest {
		t.Fatalf("corrupted PNG: code %d", rec.Code)
	}
	if rec := e.call(ann, http.MethodPost, "/api/icon-library", "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32"><script>alert(1)</script></svg>`)); rec.Code != http.StatusBadRequest {
		t.Fatalf("SVG with script: code %d", rec.Code)
	}
}

func TestUploadsSVGAndServesItAsVector(t *testing.T) {
	e := open(t, t.TempDir())
	defer e.db.Close()
	ann := e.user(t, "ann")
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" width="64" height="64"><rect width="64" height="64" fill="#123456"/><circle cx="32" cy="32" r="12" fill="url(#g)"/><defs><linearGradient id="g"><stop offset="0" stop-color="#fff"/></linearGradient></defs></svg>`)
	up := e.uploadAs(t, ann, "?name=Vector", "image/svg+xml", svg)
	if up.Width != 64 || up.Height != 64 {
		t.Fatalf("SVG size: %d×%d", up.Width, up.Height)
	}
	rec := e.call(ann, http.MethodGet, up.URL, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("SVG picture: code %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Fatalf("MIME SVG: %q", ct)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("<svg")) {
		t.Fatalf("response is not SVG: %q", rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("<script")) {
		t.Fatalf("script got into saved SVG")
	}
}

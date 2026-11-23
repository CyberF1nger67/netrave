// SPDX-License-Identifier: GPL-3.0-only

package images

import (
	"net/http"
	"net/http/httptest"
	"netrave/internal/catalog"
	"netrave/internal/labfile"
	"netrave/internal/labtest"
	"path/filepath"
	"strings"
	"testing"

	"netrave/internal/auth"
	"netrave/internal/config"
	"netrave/internal/database"
	"netrave/internal/qemu"
)

// Image files cannot be replaced while a running instance machine uses them.
// Machine state is stored in instance_devices (not in the devices project).
func TestUploadIsBlockedWhileImageRunsInInstance(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "img.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	as := auth.New(db)
	cfg := &config.Config{DisksDir: t.TempDir(), LabsDir: t.TempDir(), RunDir: t.TempDir()}
	m := qemu.New(db, cfg, nil)
	h := &Handler{DB: db, Auth: as, Cfg: cfg, M: m}
	mux := http.NewServeMux()
	h.Register(mux)

	res, _ := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('admin','x','admin')`)
	admin, _ := res.LastInsertId()
	_, err = db.Catalog.AddImage(catalog.Image{Name: "disk1", Dirname: "disk1"})
	if err != nil {
		t.Fatal(err)
	}
	lab := labtest.Lab(t, db, admin, "L", "")
	dev := labtest.Device(t, db, lab, labfile.Device{Name: "pc"})
	res, _ = db.Exec(`INSERT INTO lab_instances(lab_id,user_id) VALUES(?,?)`, lab, admin)
	inst, _ := res.LastInsertId()
	dres, err := db.Exec(`INSERT INTO instance_devices(instance_id,device_id,image_id) VALUES(?,?,?)`, inst, dev, "disk1")
	if err != nil {
		t.Fatal(err)
	}
	rid, _ := dres.LastInsertId()
	m.SetRuntime(rid, qemu.Runtime{State: "running"})

	tok, csrf, _, err := as.CreateSession(admin, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	upload := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/images/upload?dir=disk1&file=a.qcow2", strings.NewReader("x"))
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
		req.Header.Set("X-CSRF-Token", csrf)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	if rec := upload(); rec.Code != http.StatusConflict {
		t.Fatalf("running machine holds image: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}

	m.SetRuntime(rid, qemu.Runtime{State: "stopped"})
	if rec := upload(); rec.Code == http.StatusConflict {
		t.Fatalf("stopped machine must not block image: %s", rec.Body.String())
	}
}

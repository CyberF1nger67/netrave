// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"netrave/internal/catalog"
	"path/filepath"
	"strconv"
	"testing"

	"netrave/internal/auth"
	"netrave/internal/database"
)

// A template stores a disk and an icon, and an unknown icon is discarded on save.
func TestTemplateCarriesDiskAndIcon(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	as := auth.New(db)
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as}).Register(mux)

	res, _ := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('root','x','admin')`)
	admin, _ := res.LastInsertId()
	tok, csrf, _, err := as.CreateSession(admin, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	call := func(body map[string]any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/templates", bytes.NewReader(raw))
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
		req.Header.Set("X-CSRF-Token", csrf)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	base := map[string]any{
		"vendor": "Cisco", "type": "router", "arch": "x86_64", "machine": "pc", "cpu": 1,
		"ram_mb": 512, "nics": 2, "nic_model": "virtio-net-pci", "disk_bus": "virtio", "bios": "seabios",
		"console": "serial", "extra_args": "", "category": "cisco",
	}

	image, _ := db.Catalog.AddImage(catalog.Image{Name: "ubuntu", Dirname: "ubuntu", Kind: "image", LayoutJSON: "{}"})

	withAssets := map[string]any{"name": "Router", "image_id": image, "icon": "router"}
	for k, v := range base {
		withAssets[k] = v
	}
	rec := call(withAssets)
	if rec.Code != http.StatusCreated {
		t.Fatalf("создание шаблона: %d %s", rec.Code, rec.Body.String())
	}
	var tpl Template
	json.Unmarshal(rec.Body.Bytes(), &tpl)
	if tpl.ImageID != image || tpl.Icon != "router" {
		t.Fatalf("диск или иконка шаблона не сохранены: %+v", tpl)
	}

	bad := map[string]any{"name": "Bad", "icon": "unknown"}
	for k, v := range base {
		bad[k] = v
	}
	if rec := call(bad); rec.Code != http.StatusCreated {
		t.Fatalf("второй шаблон: %d %s", rec.Code, rec.Body.String())
	}
	n := 0
	for _, t := range db.Catalog.Templates() {
		if t.Icon == "unknown" {
			n++
		}
	}
	if n != 0 {
		t.Fatal("неизвестная иконка не должна сохраняться")
	}
}

// A template duplicate gets all parameters, the disk and icon of the source, and its own name.
func TestDuplicateTemplate(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	as := auth.New(db)
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as}).Register(mux)
	res, _ := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('root','x','admin')`)
	admin, _ := res.LastInsertId()
	tok, csrf, _, err := as.CreateSession(admin, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path string, body any) *httptest.ResponseRecorder {
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
		req.Header.Set("X-CSRF-Token", csrf)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	image, _ := db.Catalog.AddImage(catalog.Image{Name: "ubuntu", Dirname: "ubuntu", Kind: "image", LayoutJSON: "{}"})

	rec := call(http.MethodPost, "/api/templates", map[string]any{
		"name": "Router", "vendor": "Cisco", "type": "router", "arch": "x86_64", "machine": "pc", "cpu": 4,
		"ram_mb": 2048, "nics": 3, "nic_model": "virtio-net-pci", "disk_bus": "virtio", "bios": "uefi",
		"console": "vnc", "extra_args": "", "image_id": image, "icon": "router", "category": "cisco",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("создание: %d %s", rec.Code, rec.Body.String())
	}
	var src Template
	json.Unmarshal(rec.Body.Bytes(), &src)

	rec = call(http.MethodPost, "/api/templates/"+strconv.FormatInt(src.ID, 10)+"/duplicate", nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("дубликат: %d %s", rec.Code, rec.Body.String())
	}
	var copy1 Template
	json.Unmarshal(rec.Body.Bytes(), &copy1)
	if copy1.ID == src.ID || copy1.Name != "Router-copy" {
		t.Fatalf("имя копии: %+v", copy1)
	}
	if copy1.ImageID != image || copy1.Icon != "router" || copy1.CPU != 4 || copy1.NICs != 3 || copy1.BIOS != "uefi" || copy1.Console != "vnc" {
		t.Fatalf("параметры копии отличаются: %+v", copy1)
	}

	// The second duplicate gets the next number.
	rec = call(http.MethodPost, "/api/templates/"+strconv.FormatInt(src.ID, 10)+"/duplicate", nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("второй дубликат: %d", rec.Code)
	}
	var copy2 Template
	json.Unmarshal(rec.Body.Bytes(), &copy2)
	if copy2.Name != "Router-copy-2" {
		t.Fatalf("имя второго дубликата: %s", copy2.Name)
	}

	// Unknown template - 404.
	if rec := call(http.MethodPost, "/api/templates/9999/duplicate", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("неизвестный шаблон: %d", rec.Code)
	}
}

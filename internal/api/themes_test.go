// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"netrave/internal/access"
	"netrave/internal/auth"
	"netrave/internal/config"
	"netrave/internal/database"
)

// Built-in themes are served by the server from YAML files so the frontend does not need to keep a copy.
func TestThemePresetsEndpoint(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "themes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	as := auth.New(db)
	mux := http.NewServeMux()
	cfg := &config.Config{ThemesDir: filepath.Join(t.TempDir(), "themes")}
	(&Handler{DB: db, Auth: as, Cfg: cfg}).Register(mux)

	res, _ := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('u1','x','user')`)
	uid, _ := res.LastInsertId()
	tok, _, _, err := as.CreateSession(uid, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/me/theme/presets", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("каталог тем: HTTP %d, %s", rec.Code, rec.Body.String())
	}
	var catalog struct {
		Standard []struct {
			ID string `json:"id"`
		} `json:"standard"`
		Presets []struct {
			ID     string            `json:"id"`
			Colors map[string]string `json:"colors"`
		} `json:"presets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Standard) != 2 || len(catalog.Presets) != 16 {
		t.Fatalf("тем: стандартных %d, пресетов %d", len(catalog.Standard), len(catalog.Presets))
	}
	if len(catalog.Presets[0].Colors) == 0 {
		t.Fatal("у пресета нет палитры")
	}
}

// Creating a custom theme requires the themes.create permission; selecting a ready-made theme is available to everyone.
func TestThemeCreationNeedsPermission(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "themes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	as := auth.New(db)
	mux := http.NewServeMux()
	cfg := &config.Config{ThemesDir: filepath.Join(t.TempDir(), "themes")}
	(&Handler{DB: db, Auth: as, Cfg: cfg}).Register(mux)

	res, _ := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('u1','x','user')`)
	uid, _ := res.LastInsertId()
	tok, csrf, _, err := as.CreateSession(uid, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	put := func(body string) int {
		req := httptest.NewRequest(http.MethodPut, "/api/me/theme", bytes.NewBufferString(body))
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
		req.Header.Set("X-CSRF-Token", csrf)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	custom := `{"active":"preset:dark","custom":[{"id":"mine","name":"Mine","base":"dark","colors":{"accent":"#112233"}}]}`
	if code := put(custom); code != http.StatusForbidden {
		t.Fatalf("создание темы без права: HTTP %d, ожидался 403", code)
	}
	if code := put(`{"active":"preset:dark","custom":[]}`); code != http.StatusOK {
		t.Fatalf("выбор готовой темы без права: HTTP %d", code)
	}

	// With the permission creation is allowed.
	res, _ = db.Exec(`INSERT INTO roles(name) VALUES('Дизайнер')`)
	roleID, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO role_permissions(role_id,permission) VALUES(?,?)`, roleID, "themes.create"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO user_roles(user_id,role_id) VALUES(?,?)`, uid, roleID); err != nil {
		t.Fatal(err)
	}
	access.InvalidateAllPermissions(db)
	tok, csrf, _, err = as.CreateSession(uid, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if code := put(custom); code != http.StatusOK {
		t.Fatalf("создание темы с правом: HTTP %d", code)
	}

	// The theme is stored as <themes_dir>/<id>.yaml and read back.
	if _, err := os.Stat(filepath.Join(cfg.ThemesDir, strconv.FormatInt(uid, 10)+".yaml")); err != nil {
		t.Fatalf("файл темы не создан: %v", err)
	}
	if code := put(`{"active":"preset:dark","custom":[]}`); code != http.StatusOK {
		t.Fatalf("сохранение темы: HTTP %d", code)
	}
}

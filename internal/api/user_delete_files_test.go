// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"netrave/internal/auth"
	"netrave/internal/config"
	"netrave/internal/database"
	"netrave/internal/icons"
)

// Deleting a user removes their theme and icon library files; other users' files remain.
func TestDeleteUserRemovesThemeAndIconLibraryFiles(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "del.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	as := auth.New(db)
	themesDir := filepath.Join(t.TempDir(), "themes")
	iconsDir := filepath.Join(t.TempDir(), "icons")
	cfg := &config.Config{ThemesDir: themesDir, IconsDir: iconsDir}
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as, Cfg: cfg}).Register(mux)

	res, _ := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('admin','x','admin')`)
	admin, _ := res.LastInsertId()
	res, _ = db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('gone','x','user')`)
	gone, _ := res.LastInsertId()
	res, _ = db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('keep','x','user')`)
	keep, _ := res.LastInsertId()

	libDir := icons.New(iconsDir).LibraryDir()
	for _, dir := range []string{themesDir, libDir} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		for _, uid := range []int64{gone, keep} {
			name := filepath.Join(dir, itoa(uid)+".yaml")
			if err := os.WriteFile(name, []byte("x: 1\n"), 0o640); err != nil {
				t.Fatal(err)
			}
		}
	}

	tok, csrf, _, err := as.CreateSession(admin, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	rec := apiCall(t, mux, tok, csrf, http.MethodDelete, "/api/users/"+itoa(gone), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("deleting user: HTTP %d %s", rec.Code, rec.Body.String())
	}

	for _, dir := range []string{themesDir, libDir} {
		if _, err := os.Stat(filepath.Join(dir, itoa(gone)+".yaml")); !os.IsNotExist(err) {
			t.Errorf("file of deleted user remained in %s: %v", dir, err)
		}
		if _, err := os.Stat(filepath.Join(dir, itoa(keep)+".yaml")); err != nil {
			t.Errorf("file of another user disappeared in %s: %v", dir, err)
		}
	}
}

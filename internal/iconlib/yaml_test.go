// SPDX-License-Identifier: GPL-3.0-only

package iconlib

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"netrave/internal/database"
	"netrave/internal/icons"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	img.Set(1, 1, color.RGBA{200, 10, 10, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The library is stored in the owner's YAML file: adding, re-adding without duplication, renaming,
// the picture path in the file and deletion.
func TestLibraryLivesInYAML(t *testing.T) {
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "lib.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	res, _ := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('u','x','user')`)
	uid, _ := res.LastInsertId()
	st := icons.New(filepath.Join(dir, "icons"))

	clean, info, err := icons.Clean(testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save(clean, info.Hash); err != nil {
		t.Fatal(err)
	}

	first, created, err := Remember(db, st, uid, info, "Роутер")
	if err != nil || !created {
		t.Fatalf("первая запись: created=%v err=%v", created, err)
	}
	again, created, err := Remember(db, st, uid, info, "Другое")
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("повтор должен вернуть ту же запись: %+v created=%v err=%v", again, created, err)
	}

	raw, err := os.ReadFile(filepath.Join(st.LibraryDir(), strconv.FormatInt(uid, 10)+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "file: "+info.Hash+".png") {
		t.Fatalf("путь к картинке должен быть в YAML:\n%s", raw)
	}

	renamed, err := Rename(db, st, uid, first.ID, "Коммутатор")
	if err != nil || renamed.Name != "Коммутатор" {
		t.Fatalf("переименование: %+v %v", renamed, err)
	}
	img, err := Image(db, st, uid, first.ID)
	if err != nil || len(img) == 0 {
		t.Fatalf("картинка не читается: %v", err)
	}

	hash, err := Delete(db, st, uid, first.ID)
	if err != nil || hash != info.Hash {
		t.Fatalf("удаление: %q %v", hash, err)
	}
	items, err := List(db, st, uid)
	if err != nil || len(items) != 0 {
		t.Fatalf("после удаления список пуст: %+v %v", items, err)
	}
	refs, _ := st.ReferencedByLibrary()
	if refs[info.Hash] {
		t.Fatal("удалённая иконка не должна числиться в библиотеках")
	}
}

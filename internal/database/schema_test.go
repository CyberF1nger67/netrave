// SPDX-License-Identifier: GPL-3.0-only

package database

import (
	"netrave/internal/catalog"
	"path/filepath"
	"strings"
	"testing"

	"netrave/internal/labfile"
)

func openTemp(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSchemaCreatesFreshDatabaseAtCurrentVersion(t *testing.T) {
	db := openTemp(t)
	var ver int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&ver); err != nil || ver != schemaVersion {
		t.Fatalf("версия схемы: %d, %v", ver, err)
	}
	// Installation creates no templates: the catalog is filled only by the user or by import.
	if n := len(db.Catalog.Templates()); n != 0 {
		t.Fatalf("шаблонов в новой установке: %d, ожидалось 0", n)
	}
}

func TestSchemaRejectsOtherVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version = 27"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "создайте базу заново") {
		t.Fatalf("старая версия должна отклоняться, получено: %v", err)
	}
}

func TestDeletedTemplateImageBecomesNull(t *testing.T) {
	db := openTemp(t)
	img, err := db.Catalog.AddImage(catalog.Image{Name: "disk", Dirname: "disk", Kind: "image"})
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := db.Catalog.AddTemplate(catalog.Template{Name: "t", Arch: "x86_64", BIOS: "seabios", Console: "serial", ImageID: img})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Catalog.DeleteImage(img); err != nil {
		t.Fatal(err)
	}
	got, _ := db.Catalog.Template(tpl)
	if got.ImageID != 0 {
		t.Fatalf("после удаления образа ссылка шаблона должна сброситься, осталась %d", got.ImageID)
	}
}

func TestTemplateRejectsUnknownImage(t *testing.T) {
	db := openTemp(t)
	if _, err := db.Catalog.AddTemplate(catalog.Template{Name: "t0", Arch: "x86_64", BIOS: "seabios", Console: "serial", ImageID: 999}); err == nil {
		t.Fatal("ссылка на несуществующий образ должна отклоняться")
	}
}

func TestSnapshotImageNeedsParent(t *testing.T) {
	db := openTemp(t)
	if _, err := db.Catalog.AddImage(catalog.Image{Name: "s", Dirname: "s1", Kind: "snapshot"}); err == nil {
		t.Fatal("слой без родителя должен отклоняться")
	}
	base, err := db.Catalog.AddImage(catalog.Image{Name: "base", Dirname: "base", Kind: "image"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Catalog.AddImage(catalog.Image{Name: "s", Dirname: "s2", Kind: "snapshot", ParentID: base}); err != nil {
		t.Fatalf("корректный слой: %v", err)
	}
	if _, err := db.Catalog.AddImage(catalog.Image{Name: "x", Dirname: "x1", Kind: "image", ParentID: base}); err == nil {
		t.Fatal("образ с родителем, но не слой, должен отклоняться")
	}
}

func TestInstanceDevicesRejectUnknownBIOS(t *testing.T) {
	db := openTemp(t)
	if _, err := db.Exec(`INSERT INTO users(username,password_hash) VALUES('u','x')`); err != nil {
		t.Fatal(err)
	}
	saved, err := db.Labs.Save(labfile.Lab{OwnerID: 1, Name: "L", PublicLevel: "none", Devices: []labfile.Device{{Name: "d1"}}})
	if err != nil {
		t.Fatal(err)
	}
	dev1 := saved.Devices[0].ID
	res, _ := db.Exec(`INSERT INTO lab_instances(lab_id,user_id) VALUES(?,1)`, saved.ID)
	inst, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO instance_devices(instance_id,device_id,bios) VALUES(?,?,'coreboot')`, inst, dev1); err == nil {
		t.Fatal("неизвестный BIOS должен отклоняться")
	}
}

// SPDX-License-Identifier: GPL-3.0-only

package qemu

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"netrave/internal/catalog"
	"netrave/internal/config"
	"netrave/internal/database"
	"netrave/internal/labfile"
	"netrave/internal/labtest"
)

// Rewriting the original image does not require deleting other machines: their working disks
// switch to the saved copy of the previous image and keep their data.
func TestOverwriteOriginalKeepsDependentMachines(t *testing.T) {
	qemuImg, err := exec.LookPath("qemu-img")
	if err != nil {
		t.Skip("qemu-img не найден")
	}
	qemuIO, err := exec.LookPath("qemu-io")
	if err != nil {
		t.Skip("qemu-io не найден")
	}

	root := t.TempDir()
	db, err := database.Open(filepath.Join(root, "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := &config.Config{
		DataDir:   root,
		ImagesDir: filepath.Join(root, "images"),
		LabsDir:   filepath.Join(root, "labs"),
		RunDir:    filepath.Join(root, "run"),
		QEMUPath:  "qemu-system-x86_64",
	}
	for _, dir := range []string{cfg.ImagesDir, cfg.LabsDir, cfg.RunDir} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}

	res, err := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('owner','x','user')`)
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := res.LastInsertId()
	labID := labtest.Lab(t, db, uid, "lab", "")
	imgID, err := db.Catalog.AddImage(catalog.Image{Name: "base", Dirname: "base", Kind: "image",
		LayoutJSON: `{"disks":[{"name":"base.qcow2","format":"qcow2"}],"cdroms":[]}`})
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(cfg.ImagesDir, "base", "base.qcow2")
	if err := os.MkdirAll(filepath.Dir(base), 0o750); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(qemuImg, "create", "-f", "qcow2", base, "8M").CombinedOutput(); err != nil {
		t.Fatalf("qemu-img create: %v: %s", err, out)
	}

	res, err = db.Exec(`INSERT INTO lab_instances(lab_id,user_id) VALUES(?,?)`, labID, uid)
	if err != nil {
		t.Fatal(err)
	}
	instID, _ := res.LastInsertId()

	// Two machines use the same image; each has its own working layer.
	var rids []int64
	for i, name := range []string{"a", "b"} {
		devID := labtest.Device(t, db, labID, labfile.Device{Name: name, ImageID: imgID, CPU: 1, RAMMB: 512, NICs: 1})
		res, err = db.Exec(`INSERT INTO instance_devices(instance_id,device_id,image_id,cpu,ram_mb,nics)
			VALUES(?,?,?,1,512,1)`, instID, devID, imgID)
		if err != nil {
			t.Fatal(err)
		}
		rid, _ := res.LastInsertId()
		rids = append(rids, rid)
		overlay := DevicePaths(cfg.LabsDir, cfg.RunDir, labID, rid).Dir
		if err := os.MkdirAll(overlay, 0o750); err != nil {
			t.Fatal(err)
		}
		disk := filepath.Join(overlay, "disk0.qcow2")
		if out, err := exec.Command(qemuImg, "create", "-f", "qcow2", "-F", "qcow2", "-b", base, disk).CombinedOutput(); err != nil {
			t.Fatalf("qemu-img create overlay: %v: %s", err, out)
		}
		pattern := "0xaa"
		if i == 1 {
			pattern = "0xbb"
		}
		if out, err := exec.Command(qemuIO, "-f", "qcow2", "-c", "write -P "+pattern+" 0 64k", disk).CombinedOutput(); err != nil {
			t.Fatalf("qemu-io write: %v: %s", err, out)
		}
	}

	m := New(db, cfg, nil)
	if _, err := m.Commit(rids[0], CommitOriginal, "", false); err != nil {
		t.Fatalf("перезапись исходного образа: %v", err)
	}

	// The current machine's data made it into the new base image.
	if got := rawFirstByte(t, qemuImg, base); got != 0xaa {
		t.Fatalf("новый базовый образ не содержит изменений машины: %#x", got)
	}

	// The second machine stayed in the image, its layer is intact and no longer points to the new file.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM instance_devices WHERE image_id=?`, imgID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("зависимые машины потеряны: %d", count)
	}
	overlay := filepath.Join(DevicePaths(cfg.LabsDir, cfg.RunDir, labID, rids[1]).Dir, "disk0.qcow2")
	out, err := exec.Command(qemuImg, "info", "--output=json", overlay).Output()
	if err != nil {
		t.Fatalf("qemu-img info: %v", err)
	}
	if !strings.Contains(string(out), ".orig-") {
		t.Fatalf("рабочий диск второй машины не переключён на сохранённую копию: %s", out)
	}
	if got := rawFirstByte(t, qemuImg, overlay); got != 0xbb {
		t.Fatalf("данные второй машины изменились: %#x", got)
	}
}

// Rewriting the image does not stop a running dependent machine: its disk keeps
// pointing to the previous image until restart, and the new image applies only after Wipe.
func TestOverwriteOriginalDefersRunningDependent(t *testing.T) {
	qemuImg, err := exec.LookPath("qemu-img")
	if err != nil {
		t.Skip("qemu-img не найден")
	}
	qemuIO, err := exec.LookPath("qemu-io")
	if err != nil {
		t.Skip("qemu-io не найден")
	}

	root := t.TempDir()
	db, err := database.Open(filepath.Join(root, "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := &config.Config{
		DataDir:   root,
		ImagesDir: filepath.Join(root, "images"),
		LabsDir:   filepath.Join(root, "labs"),
		RunDir:    filepath.Join(root, "run"),
		QEMUPath:  "qemu-system-x86_64",
	}
	for _, dir := range []string{cfg.ImagesDir, cfg.LabsDir, cfg.RunDir} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}

	res, err := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('owner','x','user')`)
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := res.LastInsertId()
	labID := labtest.Lab(t, db, uid, "lab", "")
	imgID, err := db.Catalog.AddImage(catalog.Image{Name: "base", Dirname: "base", Kind: "image",
		LayoutJSON: `{"disks":[{"name":"base.qcow2","format":"qcow2"}],"cdroms":[]}`})
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(cfg.ImagesDir, "base", "base.qcow2")
	if err := os.MkdirAll(filepath.Dir(base), 0o750); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(qemuImg, "create", "-f", "qcow2", base, "8M").CombinedOutput(); err != nil {
		t.Fatalf("qemu-img create: %v: %s", err, out)
	}

	res, err = db.Exec(`INSERT INTO lab_instances(lab_id,user_id) VALUES(?,?)`, labID, uid)
	if err != nil {
		t.Fatal(err)
	}
	instID, _ := res.LastInsertId()

	var rids []int64
	for i, name := range []string{"a", "b"} {
		devID := labtest.Device(t, db, labID, labfile.Device{Name: name, ImageID: imgID, CPU: 1, RAMMB: 512, NICs: 1})
		res, err = db.Exec(`INSERT INTO instance_devices(instance_id,device_id,image_id,cpu,ram_mb,nics)
			VALUES(?,?,?,1,512,1)`, instID, devID, imgID)
		if err != nil {
			t.Fatal(err)
		}
		rid, _ := res.LastInsertId()
		rids = append(rids, rid)
		dir := DevicePaths(cfg.LabsDir, cfg.RunDir, labID, rid).Dir
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		overlay := filepath.Join(dir, "disk0.qcow2")
		if out, err := exec.Command(qemuImg, "create", "-f", "qcow2", "-F", "qcow2", "-b", base, overlay).CombinedOutput(); err != nil {
			t.Fatalf("qemu-img create overlay: %v: %s", err, out)
		}
		pattern := "0xaa"
		if i == 1 {
			pattern = "0xbb"
		}
		if out, err := exec.Command(qemuIO, "-f", "qcow2", "-c", "write -P "+pattern+" 0 64k", overlay).CombinedOutput(); err != nil {
			t.Fatalf("qemu-io write: %v: %s", err, out)
		}
	}

	m := New(db, cfg, nil)
	m.SetRuntime(rids[1], Runtime{State: "running"}) // the second machine is running, it must not be stopped
	if _, err := m.Commit(rids[0], CommitOriginal, "", false); err != nil {
		t.Fatalf("перезапись исходного образа: %v", err)
	}

	// The running machine stayed in the same state and was not stopped.
	if state := m.State(rids[1]); state != "running" {
		t.Fatalf("зависимую машину остановили: состояние %q", state)
	}
	dependent := filepath.Join(DevicePaths(cfg.LabsDir, cfg.RunDir, labID, rids[1]).Dir, "disk0.qcow2")
	out, err := exec.Command(qemuImg, "info", "--output=json", dependent).Output()
	if err != nil {
		t.Fatalf("qemu-img info: %v", err)
	}
	if strings.Contains(string(out), ".orig-") {
		t.Fatalf("работающий диск переключили на ходу: %s", out)
	}
	marker := rebaseMarker(dependent)
	saved, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("нет отложенного переключения: %v", err)
	}
	if !strings.Contains(string(saved), ".orig-") || !fileExists(string(saved)) {
		t.Fatalf("маркер не указывает на сохранённый прошлый образ: %q", saved)
	}
	// The running machine's data is still visible through its chain.
	if got := rawFirstByte(t, qemuImg, dependent); got != 0xbb {
		t.Fatalf("данные работающей машины изменились: %#x", got)
	}

	// Before the next start the disk returns to the previous image and the data is intact.
	spec, _, err := m.loadSpec(rids[1])
	if err != nil {
		t.Fatal(err)
	}
	if err := m.applyPendingRebases(spec); err != nil {
		t.Fatalf("применение отложенного переключения: %v", err)
	}
	if fileExists(marker) {
		t.Fatal("маркер не удалён после переключения")
	}
	if got := rawFirstByte(t, qemuImg, dependent); got != 0xbb {
		t.Fatalf("данные после переключения потеряны: %#x", got)
	}

	// Wiping the disk returns the machine to the new image.
	if err := m.Wipe(rids[1]); err != nil {
		t.Fatalf("сброс диска: %v", err)
	}
	spec, _, err = m.loadSpec(rids[1])
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ensureDisks(spec, m.paths(spec)); err != nil {
		t.Fatalf("создание диска после сброса: %v", err)
	}
	if got := rawFirstByte(t, qemuImg, dependent); got != 0xaa {
		t.Fatalf("после сброса машина не получила новый образ: %#x", got)
	}
}

// After the original is rewritten, another machine's snapshot layer still sees the previous
// image content: its backing file switches to the hidden copy of the previous generation,
// and changes made after the snapshot stay in the machine's working disk.
func TestOverwriteOriginalRebasesSnapshotLayer(t *testing.T) {
	qemuImg, err := exec.LookPath("qemu-img")
	if err != nil {
		t.Skip("qemu-img не найден")
	}
	qemuIO, err := exec.LookPath("qemu-io")
	if err != nil {
		t.Skip("qemu-io не найден")
	}

	root := t.TempDir()
	db, err := database.Open(filepath.Join(root, "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := &config.Config{
		DataDir:   root,
		ImagesDir: filepath.Join(root, "images"),
		LabsDir:   filepath.Join(root, "labs"),
		RunDir:    filepath.Join(root, "run"),
		QEMUPath:  "qemu-system-x86_64",
	}
	for _, dir := range []string{cfg.ImagesDir, cfg.LabsDir, cfg.RunDir} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}

	res, err := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('owner','x','user')`)
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := res.LastInsertId()
	labID := labtest.Lab(t, db, uid, "lab", "")
	imgID, err := db.Catalog.AddImage(catalog.Image{Name: "base", Dirname: "base", Kind: "image",
		LayoutJSON: `{"disks":[{"name":"base.qcow2","format":"qcow2"}],"cdroms":[]}`})
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(cfg.ImagesDir, "base", "base.qcow2")
	if err := os.MkdirAll(filepath.Dir(base), 0o750); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(qemuImg, "create", "-f", "qcow2", base, "8M").CombinedOutput(); err != nil {
		t.Fatalf("qemu-img create: %v: %s", err, out)
	}

	res, err = db.Exec(`INSERT INTO lab_instances(lab_id,user_id) VALUES(?,?)`, labID, uid)
	if err != nil {
		t.Fatal(err)
	}
	instID, _ := res.LastInsertId()
	var rids []int64
	for _, name := range []string{"a", "b"} {
		devID := labtest.Device(t, db, labID, labfile.Device{Name: name, ImageID: imgID, CPU: 1, RAMMB: 512, NICs: 1})
		res, err = db.Exec(`INSERT INTO instance_devices(instance_id,device_id,image_id,cpu,ram_mb,nics)
			VALUES(?,?,?,1,512,1)`, instID, devID, imgID)
		if err != nil {
			t.Fatal(err)
		}
		rid, _ := res.LastInsertId()
		rids = append(rids, rid)
		dir := DevicePaths(cfg.LabsDir, cfg.RunDir, labID, rid).Dir
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		overlay := filepath.Join(dir, "disk0.qcow2")
		if out, err := exec.Command(qemuImg, "create", "-f", "qcow2", "-F", "qcow2", "-b", base, overlay).CombinedOutput(); err != nil {
			t.Fatalf("qemu-img create overlay: %v: %s", err, out)
		}
		pattern := "0xaa"
		if name == "b" {
			pattern = "0x11"
		}
		if out, err := exec.Command(qemuIO, "-f", "qcow2", "-c", "write -P "+pattern+" 0 64k", overlay).CombinedOutput(); err != nil {
			t.Fatalf("qemu-io write: %v: %s", err, out)
		}
	}

	m := New(db, cfg, nil)
	// Machine b saves a snapshot; data up to it goes to a layer and a new disk goes on top.
	snapID, err := m.Commit(rids[1], CommitSnapshot, "snap", false)
	if err != nil {
		t.Fatalf("снапшот: %v", err)
	}
	overlayB := filepath.Join(DevicePaths(cfg.LabsDir, cfg.RunDir, labID, rids[1]).Dir, "disk0.qcow2")
	if out, err := exec.Command(qemuIO, "-f", "qcow2", "-c", "write -P 0xbb 0 64k", overlayB).CombinedOutput(); err != nil {
		t.Fatalf("qemu-io write после снапшота: %v: %s", err, out)
	}

	if _, err := m.Commit(rids[0], CommitOriginal, "", false); err != nil {
		t.Fatalf("перезапись исходного образа: %v", err)
	}

	// The new base image contains machine a's changes.
	if got := rawFirstByte(t, qemuImg, base); got != 0xaa {
		t.Fatalf("новый базовый образ не содержит изменений машины: %#x", got)
	}
	snap, ok := db.Catalog.Image(snapID)
	if !ok {
		t.Fatal("слой снапшота потерян")
	}
	layer := filepath.Join(cfg.ImagesDir, snap.Dirname, "base.qcow2")
	out, err := exec.Command(qemuImg, "info", "--output=json", layer).Output()
	if err != nil {
		t.Fatalf("qemu-img info слоя: %v", err)
	}
	if !strings.Contains(string(out), ".orig-") {
		t.Fatalf("слой снапшота не переключён на прежний образ: %s", out)
	}
	if got := rawFirstByte(t, qemuImg, layer); got != 0x11 {
		t.Fatalf("содержимое снапшота изменилось: %#x", got)
	}
	if got := rawFirstByte(t, qemuImg, overlayB); got != 0xbb {
		t.Fatalf("изменения машины после снапшота потеряны: %#x", got)
	}
}

// The snapshot layer of a running machine is not changed on the fly when the original is rewritten:
// it stays on the previous image via a marker and switches before the next start.
func TestOverwriteOriginalDefersSnapshotLayerInUse(t *testing.T) {
	qemuImg, err := exec.LookPath("qemu-img")
	if err != nil {
		t.Skip("qemu-img не найден")
	}
	qemuIO, err := exec.LookPath("qemu-io")
	if err != nil {
		t.Skip("qemu-io не найден")
	}

	root := t.TempDir()
	db, err := database.Open(filepath.Join(root, "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := &config.Config{
		DataDir:   root,
		ImagesDir: filepath.Join(root, "images"),
		LabsDir:   filepath.Join(root, "labs"),
		RunDir:    filepath.Join(root, "run"),
		QEMUPath:  "qemu-system-x86_64",
	}
	for _, dir := range []string{cfg.ImagesDir, cfg.LabsDir, cfg.RunDir} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}

	res, err := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('owner','x','user')`)
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := res.LastInsertId()
	labID := labtest.Lab(t, db, uid, "lab", "")
	imgID, err := db.Catalog.AddImage(catalog.Image{Name: "base", Dirname: "base", Kind: "image",
		LayoutJSON: `{"disks":[{"name":"base.qcow2","format":"qcow2"}],"cdroms":[]}`})
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(cfg.ImagesDir, "base", "base.qcow2")
	if err := os.MkdirAll(filepath.Dir(base), 0o750); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(qemuImg, "create", "-f", "qcow2", base, "8M").CombinedOutput(); err != nil {
		t.Fatalf("qemu-img create: %v: %s", err, out)
	}

	res, err = db.Exec(`INSERT INTO lab_instances(lab_id,user_id) VALUES(?,?)`, labID, uid)
	if err != nil {
		t.Fatal(err)
	}
	instID, _ := res.LastInsertId()
	var rids []int64
	for _, name := range []string{"a", "b"} {
		devID := labtest.Device(t, db, labID, labfile.Device{Name: name, ImageID: imgID, CPU: 1, RAMMB: 512, NICs: 1})
		res, err = db.Exec(`INSERT INTO instance_devices(instance_id,device_id,image_id,cpu,ram_mb,nics)
			VALUES(?,?,?,1,512,1)`, instID, devID, imgID)
		if err != nil {
			t.Fatal(err)
		}
		rid, _ := res.LastInsertId()
		rids = append(rids, rid)
		dir := DevicePaths(cfg.LabsDir, cfg.RunDir, labID, rid).Dir
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		overlay := filepath.Join(dir, "disk0.qcow2")
		if out, err := exec.Command(qemuImg, "create", "-f", "qcow2", "-F", "qcow2", "-b", base, overlay).CombinedOutput(); err != nil {
			t.Fatalf("qemu-img create overlay: %v: %s", err, out)
		}
		pattern := "0xaa"
		if name == "b" {
			pattern = "0x11"
		}
		if out, err := exec.Command(qemuIO, "-f", "qcow2", "-c", "write -P "+pattern+" 0 64k", overlay).CombinedOutput(); err != nil {
			t.Fatalf("qemu-io write: %v: %s", err, out)
		}
	}

	m := New(db, cfg, nil)
	snapID, err := m.Commit(rids[1], CommitSnapshot, "snap", false)
	if err != nil {
		t.Fatalf("снапшот: %v", err)
	}
	overlayB := filepath.Join(DevicePaths(cfg.LabsDir, cfg.RunDir, labID, rids[1]).Dir, "disk0.qcow2")
	if out, err := exec.Command(qemuIO, "-f", "qcow2", "-c", "write -P 0xbb 0 64k", overlayB).CombinedOutput(); err != nil {
		t.Fatalf("qemu-io write после снапшота: %v: %s", err, out)
	}
	// Machine b is running; rewriting the image must not touch its layer on the fly.
	m.SetRuntime(rids[1], Runtime{State: "running"})

	if _, err := m.Commit(rids[0], CommitOriginal, "", false); err != nil {
		t.Fatalf("перезапись исходного образа: %v", err)
	}

	snap, ok := db.Catalog.Image(snapID)
	if !ok {
		t.Fatal("слой снапшота потерян")
	}
	layer := filepath.Join(cfg.ImagesDir, snap.Dirname, "base.qcow2")
	marker := rebaseMarker(layer)
	saved, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("нет отложенного переключения слоя: %v", err)
	}
	if !strings.Contains(string(saved), ".orig-") || !fileExists(string(saved)) {
		t.Fatalf("маркер слоя не указывает на прошлый образ: %q", saved)
	}
	out, err := exec.Command(qemuImg, "info", "--output=json", layer).Output()
	if err != nil {
		t.Fatalf("qemu-img info слоя: %v", err)
	}
	if strings.Contains(string(out), ".orig-") {
		t.Fatalf("слой работающей машины переключили на ходу: %s", out)
	}

	// Before the next start the layer returns to the previous image.
	spec, _, err := m.loadSpec(rids[1])
	if err != nil {
		t.Fatal(err)
	}
	if err := m.applyPendingRebases(spec); err != nil {
		t.Fatalf("применение отложенного переключения слоя: %v", err)
	}
	if fileExists(marker) {
		t.Fatal("маркер слоя не удалён после переключения")
	}
	out, err = exec.Command(qemuImg, "info", "--output=json", layer).Output()
	if err != nil {
		t.Fatalf("qemu-img info слоя: %v", err)
	}
	if !strings.Contains(string(out), ".orig-") {
		t.Fatalf("слой не переключён на прежний образ: %s", out)
	}
	if got := rawFirstByte(t, qemuImg, layer); got != 0x11 {
		t.Fatalf("содержимое снапшота изменилось: %#x", got)
	}
	if got := rawFirstByte(t, qemuImg, overlayB); got != 0xbb {
		t.Fatalf("изменения машины после снапшота потеряны: %#x", got)
	}
}

// rawFirstByte converts an image to a temporary raw file and returns the first byte.
func rawFirstByte(t *testing.T, qemuImg, image string) byte {
	t.Helper()
	raw := filepath.Join(t.TempDir(), "disk.raw")
	if out, err := exec.Command(qemuImg, "convert", "-O", "raw", image, raw).CombinedOutput(); err != nil {
		t.Fatalf("qemu-img convert: %v: %s", err, out)
	}
	b, err := os.ReadFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Fatal("пустой raw-образ")
	}
	return b[0]
}

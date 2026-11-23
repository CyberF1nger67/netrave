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
		t.Skip("qemu-img not found")
	}
	qemuIO, err := exec.LookPath("qemu-io")
	if err != nil {
		t.Skip("qemu-io not found")
	}

	root := t.TempDir()
	db, err := database.Open(filepath.Join(root, "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := &config.Config{
		DataDir:      root,
		DisksDir:     filepath.Join(root, "images"),
		LabsDir:      filepath.Join(root, "labs"),
		InstancesDir: filepath.Join(root, "instances"),
		LogsDir:      filepath.Join(root, "logs"),
		RunDir:       filepath.Join(root, "run"),
		QEMUPath:     "qemu-system-x86_64",
	}
	for _, dir := range []string{cfg.DisksDir, cfg.LabsDir, cfg.InstancesDir, cfg.LogsDir, cfg.RunDir} {
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
	_, err = db.Catalog.AddImage(catalog.Image{Name: "base", Dirname: "base",
		LayoutJSON: `{"disks":[{"name":"base.qcow2","format":"qcow2"}],"cdroms":[]}`})
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(cfg.DisksDir, "base", "base.qcow2")
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
		devID := labtest.Device(t, db, labID, labfile.Device{Name: name, ImageID: "base", CPU: 1, RAMMB: 512, NICs: 1})
		res, err = db.Exec(`INSERT INTO instance_devices(instance_id,device_id,image_id,cpu,ram_mb,nics)
			VALUES(?,?,?,1,512,1)`, instID, devID, "base")
		if err != nil {
			t.Fatal(err)
		}
		rid, _ := res.LastInsertId()
		rids = append(rids, rid)
		overlay := mustWorkDir(t, db, cfg.InstancesDir, rid)
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
		t.Fatalf("overwriting source image: %v", err)
	}

	// The current machine's data made it into the new base image.
	if got := rawFirstByte(t, qemuImg, base); got != 0xaa {
		t.Fatalf("new base image does not contain the machine's changes: %#x", got)
	}

	// The second machine stayed in the image, its layer is intact and no longer points to the new file.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM instance_devices WHERE image_id=?`, "base").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("dependent machines lost: %d", count)
	}
	overlay := filepath.Join(mustWorkDir(t, db, cfg.InstancesDir, rids[1]), "disk0.qcow2")
	out, err := exec.Command(qemuImg, "info", "--output=json", overlay).Output()
	if err != nil {
		t.Fatalf("qemu-img info: %v", err)
	}
	if !strings.Contains(string(out), ".orig-") {
		t.Fatalf("second machine's working disk was not switched to the saved copy: %s", out)
	}
	if got := rawFirstByte(t, qemuImg, overlay); got != 0xbb {
		t.Fatalf("second machine's data changed: %#x", got)
	}
}

// Rewriting the image does not stop a running dependent machine: its disk keeps
// pointing to the previous image until restart, and the new image applies only after Wipe.
func TestOverwriteOriginalDefersRunningDependent(t *testing.T) {
	qemuImg, err := exec.LookPath("qemu-img")
	if err != nil {
		t.Skip("qemu-img not found")
	}
	qemuIO, err := exec.LookPath("qemu-io")
	if err != nil {
		t.Skip("qemu-io not found")
	}

	root := t.TempDir()
	db, err := database.Open(filepath.Join(root, "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := &config.Config{
		DataDir:      root,
		DisksDir:     filepath.Join(root, "images"),
		LabsDir:      filepath.Join(root, "labs"),
		InstancesDir: filepath.Join(root, "instances"),
		LogsDir:      filepath.Join(root, "logs"),
		RunDir:       filepath.Join(root, "run"),
		QEMUPath:     "qemu-system-x86_64",
	}
	for _, dir := range []string{cfg.DisksDir, cfg.LabsDir, cfg.InstancesDir, cfg.LogsDir, cfg.RunDir} {
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
	_, err = db.Catalog.AddImage(catalog.Image{Name: "base", Dirname: "base",
		LayoutJSON: `{"disks":[{"name":"base.qcow2","format":"qcow2"}],"cdroms":[]}`})
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(cfg.DisksDir, "base", "base.qcow2")
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
		devID := labtest.Device(t, db, labID, labfile.Device{Name: name, ImageID: "base", CPU: 1, RAMMB: 512, NICs: 1})
		res, err = db.Exec(`INSERT INTO instance_devices(instance_id,device_id,image_id,cpu,ram_mb,nics)
			VALUES(?,?,?,1,512,1)`, instID, devID, "base")
		if err != nil {
			t.Fatal(err)
		}
		rid, _ := res.LastInsertId()
		rids = append(rids, rid)
		dir := mustWorkDir(t, db, cfg.InstancesDir, rid)
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
		t.Fatalf("overwriting source image: %v", err)
	}

	// The running machine stayed in the same state and was not stopped.
	if state := m.State(rids[1]); state != "running" {
		t.Fatalf("dependent machine was stopped: state %q", state)
	}
	dependent := filepath.Join(mustWorkDir(t, db, cfg.InstancesDir, rids[1]), "disk0.qcow2")
	out, err := exec.Command(qemuImg, "info", "--output=json", dependent).Output()
	if err != nil {
		t.Fatalf("qemu-img info: %v", err)
	}
	if strings.Contains(string(out), ".orig-") {
		t.Fatalf("running disk was switched on the fly: %s", out)
	}
	marker := rebaseMarker(dependent)
	saved, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("no deferred switch: %v", err)
	}
	if !strings.Contains(string(saved), ".orig-") || !fileExists(string(saved)) {
		t.Fatalf("marker does not point to the saved previous image: %q", saved)
	}
	// The running machine's data is still visible through its chain.
	if got := rawFirstByte(t, qemuImg, dependent); got != 0xbb {
		t.Fatalf("running machine's data changed: %#x", got)
	}

	// Before the next start the disk returns to the previous image and the data is intact.
	spec, _, err := m.loadSpec(rids[1])
	if err != nil {
		t.Fatal(err)
	}
	if err := m.applyPendingRebases(spec); err != nil {
		t.Fatalf("applying deferred switch: %v", err)
	}
	if fileExists(marker) {
		t.Fatal("marker was not removed after switch")
	}
	if got := rawFirstByte(t, qemuImg, dependent); got != 0xbb {
		t.Fatalf("data lost after switch: %#x", got)
	}

	// Wiping the disk returns the machine to the new image.
	if err := m.Wipe(rids[1]); err != nil {
		t.Fatalf("disk reset: %v", err)
	}
	spec, _, err = m.loadSpec(rids[1])
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ensureDisks(spec, m.paths(spec)); err != nil {
		t.Fatalf("creating disk after reset: %v", err)
	}
	if got := rawFirstByte(t, qemuImg, dependent); got != 0xaa {
		t.Fatalf("after reset the machine did not get the new image: %#x", got)
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
		t.Fatal("empty raw image")
	}
	return b[0]
}

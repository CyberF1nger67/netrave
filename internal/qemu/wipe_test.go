// SPDX-License-Identifier: GPL-3.0-only

package qemu

import (
	"errors"
	"netrave/internal/catalog"
	"os"
	"path/filepath"
	"testing"

	"netrave/internal/config"
	"netrave/internal/database"
	"netrave/internal/labfile"
	"netrave/internal/labtest"
)

type wipeEnv struct {
	db  *database.DB
	m   *Manager
	cfg *config.Config
	rid int64
	img int64
}

// newWipeEnv creates a machine with an administrator image, a working overlay disk,
// UEFI Vars and a snapshot record - that is, everything Wipe must remove.
func newWipeEnv(t *testing.T) *wipeEnv {
	t.Helper()
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
	devID := labtest.Device(t, db, labID, labfile.Device{Name: "pc1", ImageID: imgID, CPU: 2, RAMMB: 2048, NICs: 1})
	res, err = db.Exec(`INSERT INTO lab_instances(lab_id,user_id) VALUES(?,?)`, labID, uid)
	if err != nil {
		t.Fatal(err)
	}
	instID, _ := res.LastInsertId()
	res, err = db.Exec(`INSERT INTO instance_devices(instance_id,device_id,image_id,cpu,ram_mb,nics,layer_image_id)
		VALUES(?,?,?,2,2048,1,?)`, instID, devID, imgID, imgID)
	if err != nil {
		t.Fatal(err)
	}
	rid, _ := res.LastInsertId()
	if _, err := db.Catalog.AddSnapshot(rid, "before", 0); err != nil {
		t.Fatal(err)
	}

	m := New(db, cfg, nil)
	e := &wipeEnv{db: db, m: m, cfg: cfg, rid: rid, img: imgID}

	base := filepath.Join(cfg.ImagesDir, "base", "base.qcow2")
	if err := os.MkdirAll(filepath.Dir(base), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base, []byte("ADMIN-BASE-IMAGE"), 0o640); err != nil {
		t.Fatal(err)
	}
	e.writeUserFiles(t)
	return e
}

// writeUserFiles simulates user changes: overlay disk, Vars and a snapshot.
func (e *wipeEnv) writeUserFiles(t *testing.T) {
	t.Helper()
	s, _, err := e.m.loadSpec(e.rid)
	if err != nil {
		t.Fatal(err)
	}
	p := e.m.paths(s)
	if err := os.MkdirAll(p.Dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Dir, "disk0.qcow2"), []byte("USER-DATA"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Vars, []byte("UEFI-VARS"), 0o640); err != nil {
		t.Fatal(err)
	}
	// A repeated call (the test itself calls the helper after shared setup) must not fail on an already created snapshot.
	if _, err := e.db.Catalog.AddSnapshot(e.rid, "user-snapshot", 0); err != nil && !errors.Is(err, catalog.ErrExists) {
		t.Fatal(err)
	}
}

func (e *wipeEnv) state(t *testing.T) (state string, pid, vnc, serial, layer int64) {
	t.Helper()
	if err := e.db.QueryRow(`SELECT COALESCE(layer_image_id,0) FROM instance_devices WHERE id=?`, e.rid).
		Scan(&layer); err != nil {
		t.Fatal(err)
	}
	rt := e.m.Runtime(e.rid)
	return rt.State, int64(rt.PID), int64(rt.VNCPort), int64(rt.SerialPort), layer
}

func (e *wipeEnv) assertReset(t *testing.T) {
	t.Helper()
	state, pid, vnc, serial, layer := e.state(t)
	if state != "stopped" || pid != 0 || vnc != 0 || serial != 0 || layer != 0 {
		t.Fatalf("после Wipe: state=%q pid=%d vnc=%d serial=%d layer=%d", state, pid, vnc, serial, layer)
	}
	s, _, err := e.m.loadSpec(e.rid)
	if err != nil {
		t.Fatal(err)
	}
	p := e.m.paths(s)
	if _, err := os.Stat(filepath.Join(p.Dir, "disk0.qcow2")); !os.IsNotExist(err) {
		t.Fatalf("рабочий диск не удалён: %v", err)
	}
	if _, err := os.Stat(p.Vars); !os.IsNotExist(err) {
		t.Fatalf("UEFI Vars не удалены: %v", err)
	}
	snaps := len(e.db.Catalog.SnapshotsOf(e.rid))
	if snaps != 0 {
		t.Fatalf("снимки не удалены: %d", snaps)
	}
	base, err := os.ReadFile(filepath.Join(e.cfg.ImagesDir, "base", "base.qcow2"))
	if err != nil || string(base) != "ADMIN-BASE-IMAGE" {
		t.Fatalf("эталонный образ администратора изменён: %q, %v", base, err)
	}
	var imageID, cpu, ram, nics int64
	if err := e.db.QueryRow(`SELECT COALESCE(image_id,0),cpu,ram_mb,nics FROM instance_devices WHERE id=?`, e.rid).
		Scan(&imageID, &cpu, &ram, &nics); err != nil {
		t.Fatal(err)
	}
	if imageID != e.img || cpu != 2 || ram != 2048 || nics != 1 {
		t.Fatalf("конфигурация машины изменилась: image=%d cpu=%d ram=%d nics=%d", imageID, cpu, ram, nics)
	}
}

func TestWipeStoppedMachineRestoresAdminImage(t *testing.T) {
	e := newWipeEnv(t)
	if err := e.m.Wipe(e.rid); err != nil {
		t.Fatalf("Wipe остановленной машины: %v", err)
	}
	e.assertReset(t)
}

// Wiping a powered-on machine stops QEMU itself and only then deletes the disks.
func TestWipeRunningMachineStopsAndResets(t *testing.T) {
	e := newWipeEnv(t)
	e.writeUserFiles(t)
	e.m.SetRuntime(e.rid, Runtime{State: "running", PID: 12345, VNCPort: 5901, SerialPort: 30123})
	s, _, err := e.m.loadSpec(e.rid)
	if err != nil {
		t.Fatal(err)
	}
	v := &vm{spec: s, pid: 12345, done: make(chan struct{})}
	close(v.done) // the process has already exited; Wipe must wait for v.done before deleting files
	e.m.mu.Lock()
	e.m.vms[e.rid] = v
	e.m.mu.Unlock()

	if err := e.m.Wipe(e.rid); err != nil {
		t.Fatalf("Wipe запущенной машины: %v", err)
	}
	e.assertReset(t)
}

// During startup or shutdown, Wipe must not touch the disk.
func TestWipeBusyWhileStarting(t *testing.T) {
	e := newWipeEnv(t)
	e.m.SetRuntime(e.rid, Runtime{State: "starting"})
	if err := e.m.Wipe(e.rid); !errors.Is(err, ErrBusy) {
		t.Fatalf("ожидалась ErrBusy, получено %v", err)
	}
	s, _, err := e.m.loadSpec(e.rid)
	if err != nil {
		t.Fatal(err)
	}
	p := e.m.paths(s)
	if _, err := os.Stat(filepath.Join(p.Dir, "disk0.qcow2")); err != nil {
		t.Fatalf("файлы не должны затрагиваться при занятой машине: %v", err)
	}
	snaps := len(e.db.Catalog.SnapshotsOf(e.rid))
	if snaps == 0 {
		t.Fatal("снимки не должны удаляться при занятой машине")
	}
}

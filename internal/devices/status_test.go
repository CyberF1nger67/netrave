// SPDX-License-Identifier: GPL-3.0-only

package devices

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"netrave/internal/auth"
	"netrave/internal/config"
	"netrave/internal/database"
	"netrave/internal/labfile"
	"netrave/internal/labtest"
	"netrave/internal/qemu"
)

// Machine status: only machines of the current lab's own instance, zeros for stopped ones,
// real process memory and disk space used by each machine.
func TestStatusOnlyOwnInstanceWithRealNumbers(t *testing.T) {
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "d.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := &config.Config{LabsDir: filepath.Join(dir, "labs"), InstancesDir: filepath.Join(dir, "instances"), LogsDir: filepath.Join(dir, "logs"), RunDir: filepath.Join(dir, "run")}
	as := auth.New(db)
	h := &Handler{DB: db, Auth: as, M: qemu.New(db, cfg, nil)}
	mux := http.NewServeMux()
	h.Register(mux)

	res, _ := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('admin','x','admin')`)
	adminID, _ := res.LastInsertId()
	res, _ = db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('u1','x','user')`)
	uid, _ := res.LastInsertId()
	lab := labtest.Lab(t, db, adminID, "lab", "none")

	// Two machines in the lab and two instances: the user's and the administrator's.
	devStopped := labtest.Device(t, db, lab, labfile.Device{Name: "pc1"})
	devRunning := labtest.Device(t, db, lab, labfile.Device{Name: "pc2"})

	res, _ = db.Exec(`INSERT INTO lab_instances(lab_id,user_id) VALUES(?,?)`, lab, uid)
	instU, _ := res.LastInsertId()
	res, _ = db.Exec(`INSERT INTO lab_instances(lab_id,user_id) VALUES(?,?)`, lab, adminID)
	instA, _ := res.LastInsertId()

	res, _ = db.Exec(`INSERT INTO instance_devices(instance_id,device_id) VALUES(?,?)`, instU, devStopped)
	ridStopped, _ := res.LastInsertId()
	// pc2 belongs to the user: the process actually exists (the current test process).
	res, _ = db.Exec(`INSERT INTO instance_devices(instance_id,device_id) VALUES(?,?)`,
		instU, devRunning)
	ridRunning, _ := res.LastInsertId()
	h.M.SetRuntime(ridRunning, qemu.Runtime{State: "running", PID: os.Getpid()})
	// The administrator's machine in the same lab project: it must not appear in the user's response.
	res, _ = db.Exec(`INSERT INTO instance_devices(instance_id,device_id) VALUES(?,?)`,
		instA, devStopped)
	ridAdmin, _ := res.LastInsertId()
	h.M.SetRuntime(ridAdmin, qemu.Runtime{State: "running", PID: os.Getpid()})

	labtest.Perm(t, db, lab, uid, "lab.view", true)
	tok, _, _, err := as.CreateSession(uid, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	// On the user's disk, a stopped machine has a 2 MiB overlay (fully written, not sparse).
	wd, err := qemu.WorkDir(db, cfg.InstancesDir, ridStopped)
	if err != nil {
		t.Fatal(err)
	}
	diskDir := wd.Dir
	if err := os.MkdirAll(diskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(diskDir, "disk0.qcow2"), make([]byte, 2<<20), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/labs/"+lab+"/status", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Machines []machineStatus `json:"machines"`
		Totals   statusTotals    `json:"totals"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	if len(resp.Machines) != 2 {
		t.Fatalf("expected 2 user instance machines (excluding administrator machine), got %d", len(resp.Machines))
	}
	byRID := map[int64]machineStatus{}
	for _, m := range resp.Machines {
		byRID[m.RID] = m
	}

	stopped, ok := byRID[ridStopped]
	if !ok {
		t.Fatalf("stopped machine not found in response: %+v", resp.Machines)
	}
	if stopped.Running || stopped.CPUPct != 0 || stopped.RAMBytes != 0 || stopped.PID != 0 {
		t.Fatalf("stopped machine must have zero load: %+v", stopped)
	}
	if stopped.DiskBytes < 2<<20 {
		t.Fatalf("machine disk usage is less than the 2 MiB written: %d", stopped.DiskBytes)
	}

	running, ok := byRID[ridRunning]
	if !ok {
		t.Fatalf("running machine not found in response")
	}
	if !running.Running || running.PID != os.Getpid() {
		t.Fatalf("running machine: expected status running and process pid: %+v", running)
	}
	if running.RAMBytes == 0 {
		t.Fatalf("QEMU process memory must be read from /proc, got 0")
	}
	if running.CPUReady {
		t.Fatalf("load without two samples must not be marked ready")
	}

	if resp.Totals.Machines != 2 || resp.Totals.Running != 1 {
		t.Fatalf("totals: expected 2 machines and 1 running, got %+v", resp.Totals)
	}
	if resp.Totals.RAMBytes != running.RAMBytes {
		t.Fatalf("total RAM must equal the sum over machines: %d != %d", resp.Totals.RAMBytes, running.RAMBytes)
	}
	if resp.Totals.DiskBytes != stopped.DiskBytes+running.DiskBytes {
		t.Fatalf("total disk must equal the sum over machines")
	}
}

// Status without lab view permission is hidden (404), like other responses about the lab.
func TestStatusHiddenWithoutView(t *testing.T) {
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "d.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := &config.Config{LabsDir: filepath.Join(dir, "labs"), InstancesDir: filepath.Join(dir, "instances"), LogsDir: filepath.Join(dir, "logs"), RunDir: filepath.Join(dir, "run")}
	as := auth.New(db)
	h := &Handler{DB: db, Auth: as, M: qemu.New(db, cfg, nil)}
	mux := http.NewServeMux()
	h.Register(mux)

	res, _ := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('admin','x','admin')`)
	adminID, _ := res.LastInsertId()
	res, _ = db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('u1','x','user')`)
	uid, _ := res.LastInsertId()
	lab := labtest.Lab(t, db, adminID, "lab", "none")
	tok, _, _, err := as.CreateSession(uid, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/labs/"+lab+"/status", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("without view permission expected 404, got %d", rec.Code)
	}
}

func TestDirAllocatedMissingDirIsZero(t *testing.T) {
	if n := qemu.DirAllocated(filepath.Join(t.TempDir(), "нет-такого")); n != 0 {
		t.Fatalf("nonexistent directory must give 0, got %d", n)
	}
}

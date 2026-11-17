// SPDX-License-Identifier: GPL-3.0-only

package qemu

import (
	"path/filepath"
	"testing"

	"netrave/internal/config"
	"netrave/internal/database"
	"netrave/internal/labfile"
	"netrave/internal/labtest"
)

// New environment: database, directories and one machine instance with state=stopped.
func newStateEnv(t *testing.T) (*Manager, *database.DB, int64) {
	t.Helper()
	root := t.TempDir()
	db, err := database.Open(filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := &config.Config{
		DataDir: root, ImagesDir: filepath.Join(root, "images"),
		LabsDir: filepath.Join(root, "labs"), RunDir: filepath.Join(root, "run"),
		QEMUPath: "qemu-system-x86_64",
	}
	res, _ := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('owner','x','user')`)
	uid, _ := res.LastInsertId()
	labID := labtest.Lab(t, db, uid, "lab", "")
	devID := labtest.Device(t, db, labID, labfile.Device{Name: "pc"})
	res, _ = db.Exec(`INSERT INTO lab_instances(lab_id,user_id) VALUES(?,?)`, labID, uid)
	instID, _ := res.LastInsertId()
	res, _ = db.Exec(`INSERT INTO instance_devices(instance_id,device_id) VALUES(?,?)`, instID, devID)
	rid, _ := res.LastInsertId()
	return New(db, cfg, nil), db, rid
}

func stateOf(t *testing.T, m *Manager, db *database.DB, rid int64) (string, int64) {
	t.Helper()
	var started int64
	if err := db.QueryRow(`SELECT started_at FROM instance_devices WHERE id=?`, rid).Scan(&started); err != nil {
		t.Fatal(err)
	}
	return m.State(rid), started
}

// Stopping a machine must not erase the start history: otherwise the lab disappears from "Running".
func TestStopKeepsStartHistory(t *testing.T) {
	m, db, rid := newStateEnv(t)
	m.setState(rid, "starting", "")
	m.setState(rid, "running", "")
	state, started := stateOf(t, m, db, rid)
	if state != "running" || started == 0 {
		t.Fatalf("запуск: состояние %q, started_at %d", state, started)
	}
	first := started

	m.setState(rid, "stopping", "")
	m.setState(rid, "stopped", "")
	state, started = stateOf(t, m, db, rid)
	if state != "stopped" {
		t.Fatalf("остановка: состояние %q", state)
	}
	if started != first {
		t.Fatalf("история запуска потеряна: было %d, стало %d", first, started)
	}

	// A crash does not erase the history either.
	m.setState(rid, "error", "QEMU завершился аварийно")
	if _, started = stateOf(t, m, db, rid); started != first {
		t.Fatalf("ошибка стёрла историю запуска: %d", started)
	}

	// A repeated start keeps the original time (the history is not overwritten).
	m.setState(rid, "running", "")
	if _, started = stateOf(t, m, db, rid); started != first {
		t.Fatalf("повторный запуск перезаписал историю: было %d, стало %d", first, started)
	}
}

// A start attempt records history at once, already in the starting state. If the machine is
// stopped before the first QMP report, the lab still stays under "Running" instead of vanishing.
func TestStartAttemptRecordsHistory(t *testing.T) {
	m, db, rid := newStateEnv(t)
	if _, _, err := m.allocPorts(rid); err != nil {
		t.Fatal(err)
	}
	state, started := stateOf(t, m, db, rid)
	if state != "starting" || started == 0 {
		t.Fatalf("начало запуска: состояние %q, started_at %d", state, started)
	}
	m.setState(rid, "stopped", "")
	state, again := stateOf(t, m, db, rid)
	if state != "stopped" || again != started {
		t.Fatalf("остановка после попытки запуска: состояние %q, started_at %d (было %d)", state, again, started)
	}
}

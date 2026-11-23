// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestRunningListIncludesStopped(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "root", "admin")
	lab := e.lab(t, admin.id)
	dev := e.device(t, lab)

	rec := e.do(t, admin, http.MethodGet, "/api/labs/running", nil)
	expect(t, rec, http.StatusOK, "list")
	var list []RunningLab
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 0 {
		t.Fatalf("before start the list must be empty: %+v", list)
	}

	// The machine was started at some point: the row remains even though the machine is stopped.
	if _, err := e.db.Exec(`INSERT INTO lab_instances(lab_id,user_id) VALUES(?,?)`, lab, admin.id); err != nil {
		t.Fatal(err)
	}
	var inst int64
	e.db.QueryRow(`SELECT id FROM lab_instances WHERE lab_id=?`, lab).Scan(&inst)
	if _, err := e.db.Exec(`INSERT INTO instance_devices(instance_id,device_id,started_at) VALUES(?,?,strftime('%s','now')-60)`, inst, dev); err != nil {
		t.Fatal(err)
	}
	rec = e.do(t, admin, http.MethodGet, "/api/labs/running", nil)
	expect(t, rec, http.StatusOK, "list")
	list = nil
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].Status != "stopped" || list[0].StartedAt == 0 {
		t.Fatalf("stopped previously started lab must be in the list, start time preserved: %+v", list)
	}

	// Stopping all machines does not remove the lab: the list is built from the start history in the database.
	rec = e.do(t, admin, http.MethodGet, "/api/labs/running", nil)
	list = nil
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].Status != "stopped" || list[0].Running != 0 {
		t.Fatalf("after stopping machines the lab must remain in the list: %+v", list)
	}

	// Wipe Lab is the only action that removes the lab from the list. The real
	// handler is called (the machine stop hook is not set, the wipe timestamp is written to the database).
	rec = e.do(t, admin, http.MethodPost, "/api/labs/"+labPath(lab)+"/wipe", nil)
	expect(t, rec, http.StatusOK, "Wipe Lab")
	rec = e.do(t, admin, http.MethodGet, "/api/labs/running", nil)
	list = nil
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 0 {
		t.Fatalf("after Wipe the lab must disappear from the list: %+v", list)
	}

	// Restarting the machine returns it to the list.
	// Start after Wipe: the start time is later than the wipe time.
	if _, err := e.db.Exec(`UPDATE instance_devices SET started_at=strftime('%s','now')+5`); err != nil {
		t.Fatal(err)
	}
	rec = e.do(t, admin, http.MethodGet, "/api/labs/running", nil)
	list = nil
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].Status != "stopped" {
		t.Fatalf("after start the lab must return to the list: %+v", list)
	}
}

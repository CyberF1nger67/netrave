// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"testing"

	"netrave/internal/database"
	"netrave/internal/labfile"
	"netrave/internal/labtest"
)

// An instance with a running machine and an instance with a stopped machine that was started at some point
// appear in the "Running" list; a lab without start history does not. The condition is the openedEver constant
// from lifecycle.go, the same query runningLabs executes (it does not filter owner and permissions: Go does).
func TestRunningQueryIncludesOpenedInstances(t *testing.T) {
	db, err := database.Open(t.TempDir() + "/r.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('u','x','user')`); err != nil {
		t.Fatal(err)
	}
	running := labtest.Lab(t, db, 1, "running", "none")
	stopped := labtest.Lab(t, db, 1, "stopped", "none")
	never := labtest.Lab(t, db, 1, "never", "none")
	a := labtest.Device(t, db, running, labfile.Device{Name: "a"})
	b := labtest.Device(t, db, stopped, labfile.Device{Name: "b"})
	c := labtest.Device(t, db, never, labfile.Device{Name: "c"})
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO lab_instances(lab_id,user_id) VALUES(?,1)`, []any{running}},
		{`INSERT INTO lab_instances(lab_id,user_id) VALUES(?,1)`, []any{stopped}},
		{`INSERT INTO lab_instances(lab_id,user_id) VALUES(?,1)`, []any{never}},
		{`INSERT INTO instance_devices(instance_id,device_id,started_at) VALUES(1,?,strftime('%s','now'))`, []any{a}},
		{`INSERT INTO instance_devices(instance_id,device_id,started_at) VALUES(2,?,strftime('%s','now')-3600)`, []any{b}},
		{`INSERT INTO instance_devices(instance_id,device_id,started_at) VALUES(3,?,0)`, []any{c}},
	} {
		if _, err := db.Exec(q.sql, q.args...); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}

	rows, err := db.Query(`SELECT i.id, i.lab_id FROM lab_instances i WHERE ` + openedEver)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var labs []int64
	for rows.Next() {
		var inst, lab int64
		if err := rows.Scan(&inst, &lab); err != nil {
			t.Fatal(err)
		}
		labs = append(labs, lab)
	}
	if len(labs) != 2 {
		t.Fatalf("ожидались запущенная и ранее открытая лаборатории, получено %v", labs)
	}
}

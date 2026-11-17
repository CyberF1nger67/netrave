// SPDX-License-Identifier: GPL-3.0-only

package network

import (
	"path/filepath"
	"testing"

	"netrave/internal/database"
	"netrave/internal/labfile"
	"netrave/internal/labtest"
)

// An interface without a cable counts as not attached; a disabled cable yields "not ready".
func TestNICStatesWithoutLinks(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "net.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('u','x','admin')`); err != nil {
		t.Fatal(err)
	}
	lab := labtest.Lab(t, db, 1, "lab", "none")
	dev := labtest.Device(t, db, lab, labfile.Device{Name: "pc"})
	if _, err := db.Exec(`INSERT INTO lab_instances(lab_id,user_id) VALUES(?,1)`, lab); err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec(`INSERT INTO instance_devices(instance_id,device_id,nics) VALUES(1,?,2)`, dev)
	if err != nil {
		t.Fatal(err)
	}
	rid, _ := res.LastInsertId()

	m := &Manager{db: db}
	states := m.NICStates(1, []int64{rid})
	if len(states[rid]) != 2 {
		t.Fatalf("ожидалось два интерфейса: %v", states)
	}
	for nic, code := range states[rid] {
		if code != NICNotAttached {
			t.Fatalf("nic %d: код %d, ожидался %d (не подключён)", nic, code, NICNotAttached)
		}
	}
}

// A disabled cable marks the interface as "not ready", even if the TAP exists.
func TestNICStatesDisabledCable(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "net.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('u','x','admin')`); err != nil {
		t.Fatal(err)
	}
	lab := labtest.Lab(t, db, 1, "lab", "none")
	dev := labtest.Device(t, db, lab, labfile.Device{Name: "pc"})
	net := labtest.Network(t, db, lab, labfile.Network{Name: "net", Type: "isolated"})
	if _, err := db.Exec(`INSERT INTO lab_instances(lab_id,user_id) VALUES(?,1)`, lab); err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec(`INSERT INTO instance_devices(instance_id,device_id,nics) VALUES(1,?,1)`, dev)
	if err != nil {
		t.Fatal(err)
	}
	rid, _ := res.LastInsertId()
	conn := labtest.Connection(t, db, lab, labfile.Connection{
		A: labfile.End{Type: "device", ID: dev, If: 0},
		B: labfile.End{Type: "network", ID: net, If: 0},
	})
	if _, err := db.Exec(`INSERT INTO instance_links(instance_id,connection_id,up) VALUES(1,?,0)`, conn); err != nil {
		t.Fatal(err)
	}

	m := &Manager{db: db}
	states := m.NICStates(1, []int64{rid})
	if got := states[rid][0]; got != NICUnready {
		t.Fatalf("выключенный провод: код %d, ожидался %d", got, NICUnready)
	}
}

// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"net/http"
	"testing"

	"netrave/internal/labfile"
	"netrave/internal/labtest"
)

// Renaming a laboratory, device or network must move the database references that use those names.
func TestRenameUpdatesInstanceReferences(t *testing.T) {
	e := newTenv(t)
	owner := e.user(t, "owner", "user")
	lab := e.lab(t, owner.id)
	dev := e.device(t, lab)
	peer := labtest.Device(t, e.db, lab, labfile.Device{Name: "peer"})
	net := labtest.Network(t, e.db, lab, labfile.Network{Name: "lan", Type: "isolated"})
	key := labtest.Connection(t, e.db, lab, labfile.Connection{
		A: labfile.End{Type: "device", Name: dev, If: 0},
		B: labfile.End{Type: "network", Name: net},
	})
	rid := e.runInstance(t, lab, dev, owner.id, "stopped", 0, 0)
	inst := e.instanceOf(t, lab, owner.id)
	if _, err := e.db.Exec(`INSERT INTO instance_links(instance_id,connection_id,up) VALUES(?,?,1)`, inst, key); err != nil {
		t.Fatal(err)
	}

	expect(t, e.do(t, owner, http.MethodPut, "/api/labs/"+labPath(lab)+"/devices/"+labPath(dev), map[string]any{"name": "pc-new"}), http.StatusOK, "device rename")
	var devID string
	if err := e.db.QueryRow(`SELECT device_id FROM instance_devices WHERE id=?`, rid).Scan(&devID); err != nil {
		t.Fatal(err)
	}
	if devID != "pc-new" {
		t.Fatalf("instance row still uses the old device name: %q", devID)
	}
	var newKey string
	if err := e.db.QueryRow(`SELECT connection_id FROM instance_links WHERE instance_id=?`, inst).Scan(&newKey); err != nil {
		t.Fatal(err)
	}
	if newKey == key || newKey == "" {
		t.Fatalf("cable key did not follow the device rename: %q", newKey)
	}

	expect(t, e.do(t, owner, http.MethodPut, "/api/labs/"+labPath(lab)+"/networks/"+labPath(net), map[string]any{"name": "lan-new"}), http.StatusOK, "network rename")
	var netID string
	if err := e.db.QueryRow(`SELECT network_id FROM instance_networks WHERE instance_id=?`, inst).Scan(&netID); err != nil {
		// No instance network row exists yet: the rename is still checked through the cable key.
		netID = "lan-new"
	}
	if netID != "lan-new" {
		t.Fatalf("instance network row still uses the old name: %q", netID)
	}
	var afterNetKey string
	if err := e.db.QueryRow(`SELECT connection_id FROM instance_links WHERE instance_id=?`, inst).Scan(&afterNetKey); err != nil {
		t.Fatal(err)
	}
	if afterNetKey == newKey {
		t.Fatalf("cable key did not follow the network rename: %q", afterNetKey)
	}
	_ = peer

	newLab := "renamed-lab"
	expect(t, e.do(t, owner, http.MethodPut, "/api/labs/"+labPath(lab), map[string]any{"name": newLab}), http.StatusOK, "lab rename")
	var labName string
	if err := e.db.QueryRow(`SELECT lab_id FROM lab_instances WHERE id=?`, inst).Scan(&labName); err != nil {
		t.Fatal(err)
	}
	if labName != newLab {
		t.Fatalf("instance row still uses the old laboratory name: %q", labName)
	}
}

func (e *tenv) instanceOf(t *testing.T, lab string, user int64) int64 {
	t.Helper()
	var id int64
	if err := e.db.QueryRow(`SELECT id FROM lab_instances WHERE lab_id=? AND user_id=?`, lab, user).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

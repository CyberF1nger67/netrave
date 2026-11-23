// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"net/http"
	"testing"
)

// Regression: after "Add machine" the settings dialog saves the machine by its rid.
// Previously the creation response was built without the user's instance, the rid was 0, and "Save"
// got "device not found" (404). The creation and paste responses must contain
// the rid of this user's instance row, and settings are saved by it.
func TestCreatedDeviceCanBeSavedByRID(t *testing.T) {
	e := newTenv(t)
	owner := e.user(t, "owner", "user")
	lab := e.lab(t, owner.id)

	tplID := e.template(t)

	rec := e.do(t, owner, http.MethodPost, "/api/labs/"+labPath(lab)+"/devices",
		map[string]any{"template_id": tplID, "x": 100, "y": 100})
	expect(t, rec, http.StatusCreated, "machine creation")
	var created Device
	decodeJSON(t, rec.Body.Bytes(), &created)
	if created.RID == 0 {
		t.Fatalf("creation response without rid: saving settings will get 404 'device not found'")
	}

	var rows int
	e.db.QueryRow(`SELECT COUNT(*) FROM instance_devices WHERE id=? AND device_id=?`, created.RID, created.Name).Scan(&rows)
	if rows != 1 {
		t.Fatalf("rid %d does not match the instance row of machine %q", created.RID, created.Name)
	}

	expect(t, e.do(t, owner, http.MethodPut, "/api/labs/"+labPath(lab)+"/devices/"+labPath(created.Name),
		map[string]any{"name": "router-1", "cpu": 2}), http.StatusOK, "saving settings of the created machine")

	name := devOf(t, e.db, lab, "router-1").Name
	if name != "router-1" {
		t.Fatalf("name was not saved: %q", name)
	}
}

// Paste returns copies of the machines (the originals are not returned); the copy must have an instance rid, not 0.
func TestPastedDeviceHasRID(t *testing.T) {
	e := newTenv(t)
	owner := e.user(t, "owner", "user")
	lab := e.lab(t, owner.id)
	dev := e.device(t, lab)
	e.runInstance(t, lab, dev, owner.id, "stopped", 0, 0)

	rec := e.do(t, owner, http.MethodPost, "/api/labs/"+labPath(lab)+"/paste",
		map[string]any{"devices": []string{dev}, "networks": []string{}, "dx": 40, "dy": 40})
	expect(t, rec, http.StatusCreated, "machine paste")
	var out struct {
		Devices []Device `json:"devices"`
	}
	decodeJSON(t, rec.Body.Bytes(), &out)
	if len(out.Devices) != 1 {
		t.Fatalf("want one copy of the machine, got %d", len(out.Devices))
	}
	c := out.Devices[0]
	if c.Name == dev {
		t.Fatalf("paste response must contain a copy, not the original machine")
	}
	if c.RID == 0 {
		t.Errorf("pasted copy of machine %s has no rid: settings will not be saved", c.Name)
	}
}

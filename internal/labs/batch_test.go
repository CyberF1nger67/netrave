// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"net/http"
	"testing"

	"netrave/internal/qemu"
)

// Batch add: count=N creates N separate machines in one request.
// We check name uniqueness, IDs, rid (and therefore MAC), diagram layout,
// settings independence and that no extra rows appear in the database.
func TestBatchCreateDevices(t *testing.T) {
	e := newTenv(t)
	owner := e.user(t, "owner", "user")
	lab := e.lab(t, owner.id)

	tplID := e.template(t)

	rec := e.do(t, owner, http.MethodPost, "/api/labs/"+labPath(lab)+"/devices",
		map[string]any{"template_id": tplID, "x": 100, "y": 100, "count": 5})
	expect(t, rec, http.StatusCreated, "batch add of 5 machines")
	var batch []Device
	decodeJSON(t, rec.Body.Bytes(), &batch)
	if len(batch) != 5 {
		t.Fatalf("want 5 machines, got %d", len(batch))
	}

	ids, rids, names, pos := map[string]bool{}, map[int64]bool{}, map[string]bool{}, map[[2]float64]bool{}
	macs := map[string]bool{}
	for _, d := range batch {
		if ids[d.Name] || rids[d.RID] || names[d.Name] {
			t.Fatalf("duplicate: id=%s rid=%d name=%q", d.Name, d.RID, d.Name)
		}
		if d.RID == 0 {
			t.Fatalf("machine %q has no rid: settings and disks will not be attached", d.Name)
		}
		ids[d.Name], rids[d.RID], names[d.Name] = true, true, true
		pos[[2]float64{d.X, d.Y}] = true
		// The MAC is built from the rid (qemu.MAC): every machine has its own.
		for nic := 0; nic < d.NICs; nic++ {
			m := qemu.MAC(d.RID, nic)
			if macs[m] {
				t.Fatalf("duplicate MAC %s on machine %q", m, d.Name)
			}
			macs[m] = true
		}
	}
	if len(pos) != 5 {
		t.Fatalf("machines overlap on the diagram: %d positions out of 5", len(pos))
	}

	devCount := devCountOf(e.db, lab)
	var instCount int
	e.db.QueryRow(`SELECT COUNT(*) FROM instance_devices WHERE instance_id IN (SELECT id FROM lab_instances WHERE lab_id=?)`, lab).Scan(&instCount)
	if devCount != 5 || instCount != 5 {
		t.Fatalf("DB rows: devices=%d instance_devices=%d, want 5 and 5", devCount, instCount)
	}

	// Independent settings: change one machine, the others keep their values.
	first := batch[0]
	expect(t, e.do(t, owner, http.MethodPut, "/api/labs/"+labPath(lab)+"/devices/"+labPath(first.Name),
		map[string]any{"name": "cisco-a", "cpu": 4}), http.StatusOK, "settings of the first machine")
	for _, d := range batch[1:] {
		var cpu int
		e.db.QueryRow(`SELECT cpu FROM instance_devices WHERE id=?`, d.RID).Scan(&cpu)
		if cpu == 4 {
			t.Fatalf("settings of machine %q were applied to a neighbour", first.Name)
		}
	}

	// A name set manually becomes the base for the batch: router-1, router-2, ...
	rec = e.do(t, owner, http.MethodPost, "/api/labs/"+labPath(lab)+"/devices",
		map[string]any{"template_id": tplID, "x": 300, "y": 100, "count": 3, "name": "router"})
	expect(t, rec, http.StatusCreated, "batch with a base name")
	var second []Device
	decodeJSON(t, rec.Body.Bytes(), &second)
	for _, d := range second {
		if names[d.Name] {
			t.Fatalf("name %q repeats an existing one", d.Name)
		}
		names[d.Name] = true
	}

	// A single add (count not set) still returns one object.
	rec = e.do(t, owner, http.MethodPost, "/api/labs/"+labPath(lab)+"/devices",
		map[string]any{"template_id": tplID, "x": 500, "y": 500})
	expect(t, rec, http.StatusCreated, "single add")
	var single Device
	decodeJSON(t, rec.Body.Bytes(), &single)
	if single.Name == "" || single.RID == 0 {
		t.Fatalf("single machine without id/rid: %+v", single)
	}

	// Bounds: 0 means "one machine" (default behaviour); negative and more than 20 - an error.
	expect(t, e.do(t, owner, http.MethodPost, "/api/labs/"+labPath(lab)+"/devices",
		map[string]any{"template_id": tplID, "x": 0, "y": 0, "count": 21}), http.StatusBadRequest, "more than 20 machines at once")
	expect(t, e.do(t, owner, http.MethodPost, "/api/labs/"+labPath(lab)+"/devices",
		map[string]any{"template_id": tplID, "x": 0, "y": 0, "count": -1}), http.StatusBadRequest, "negative count")

	// A template error before insertion creates no machine from the batch.
	before := devCountOf(e.db, lab)
	expect(t, e.do(t, owner, http.MethodPost, "/api/labs/"+labPath(lab)+"/devices",
		map[string]any{"template_id": 999999, "x": 0, "y": 0, "count": 3}), http.StatusBadRequest, "unknown template in batch")
	after := devCountOf(e.db, lab)
	if after != before {
		t.Fatalf("on error extra machines were created: was %d, now %d", before, after)
	}
}

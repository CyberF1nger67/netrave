// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"net/http"
	"testing"

	"netrave/internal/labfile"
	"netrave/internal/labtest"
	"netrave/internal/network"
)

// A new interface (number = current NIC count) extends the device and is immediately passed
// to live attach; a number beyond that remains an error.
func TestCreateConnGrowsNICs(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "admin", "admin")
	lab := e.lab(t, admin.id)
	a := e.device(t, lab)
	b := labtest.Device(t, e.db, lab, labfile.Device{Name: "pc2"})

	var attached []string
	mux := http.NewServeMux()
	(&Handler{DB: e.db, Auth: e.as, CableAttach: func(labName, key string) []error {
		attached = append(attached, key)
		return nil
	}}).Register(mux)
	e.srv = mux

	// e0 is occupied by an existing cable.
	expect(t, e.do(t, admin, http.MethodPost, "/api/labs/"+labPath(lab)+"/connections",
		map[string]any{"a_type": "device", "a_name": a, "a_if": 0, "b_type": "device", "b_name": b, "b_if": 0}), http.StatusCreated, "first cable")
	if len(attached) != 1 {
		t.Fatalf("live attach was not called: %v", attached)
	}

	// Reusing e0 is forbidden.
	rec := e.do(t, admin, http.MethodPost, "/api/labs/"+labPath(lab)+"/connections",
		map[string]any{"a_type": "device", "a_name": a, "a_if": 0, "b_type": "device", "b_name": b, "b_if": 1})
	expect(t, rec, http.StatusConflict, "occupied interface")

	// e1 == nics: a new port is created.
	rec = e.do(t, admin, http.MethodPost, "/api/labs/"+labPath(lab)+"/connections",
		map[string]any{"a_type": "device", "a_name": a, "a_if": 1, "b_type": "device", "b_name": b, "b_if": 1})
	expect(t, rec, http.StatusCreated, "new interface")
	if len(attached) != 2 {
		t.Fatalf("hot attach was not called: %v", attached)
	}

	if nics := devOf(t, e.db, lab, a).NICs; nics != 2 {
		t.Fatalf("device was not extended: nics=%d", nics)
	}

	// e3 > nics: there is no such interface.
	rec = e.do(t, admin, http.MethodPost, "/api/labs/"+labPath(lab)+"/connections",
		map[string]any{"a_type": "device", "a_name": a, "a_if": 3, "b_type": "device", "b_name": b, "b_if": 2})
	expect(t, rec, http.StatusBadRequest, "interface out of range")
}

// Removing a cable of a running machine goes through CablePlan/CableDetach and removes the database record.
func TestBulkDeleteConnectionRunsCableHooks(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "admin", "admin")
	lab := e.lab(t, admin.id)
	a := e.device(t, lab)
	b := labtest.Device(t, e.db, lab, labfile.Device{Name: "pc2"})
	key := labtest.Connection(t, e.db, lab, labfile.Connection{
		A: labfile.End{Type: "device", Name: a, If: 0},
		B: labfile.End{Type: "device", Name: b, If: 0},
	})

	planned, detached := false, false
	mux := http.NewServeMux()
	(&Handler{DB: e.db, Auth: e.as,
		CablePlan: func(labName string, keys []string) []network.Plug {
			planned = len(keys) == 1 && keys[0] == key
			return []network.Plug{{RID: 7, NIC: 0}}
		},
		CableDetach: func(plugs []network.Plug) []error {
			detached = len(plugs) == 1 && plugs[0].RID == 7 && plugs[0].NIC == 0
			return nil
		},
	}).Register(mux)
	e.srv = mux

	rec := e.do(t, admin, http.MethodPost, "/api/labs/"+labPath(lab)+"/delete", map[string]any{"connections": []string{key}})
	expect(t, rec, http.StatusOK, "cable deletion")
	if !planned || !detached {
		t.Fatalf("live detach was not called: plan=%v detach=%v", planned, detached)
	}
	if _, _, ok := e.db.Labs.Connection(lab, key); ok {
		t.Fatal("cable remained in the lab file")
	}
}

// The interface name format (ens/te/ge + starting number) is stored on the machine and its instance row,
// returned by the API and does not affect cables and interface numbers when changed.
func TestDeviceIfaceNamingPersists(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "admin", "admin")
	lab := e.lab(t, admin.id)
	tplID := e.template(t)

	rec := e.do(t, admin, http.MethodPost, "/api/labs/"+labPath(lab)+"/devices", map[string]any{
		"template_id": tplID, "x": 10, "y": 10, "nics": 3, "iface_type": "te", "iface_start": 2,
	})
	expect(t, rec, http.StatusCreated, "machine with te2… interfaces")
	var created Device
	decodeJSON(t, rec.Body.Bytes(), &created)
	if created.IfaceType != "te" || created.IfaceStart != 2 {
		t.Fatalf("format not saved in the response: %q/%d", created.IfaceType, created.IfaceStart)
	}

	peer := labtest.Device(t, e.db, lab, labfile.Device{Name: "peer1"})
	rec = e.do(t, admin, http.MethodPost, "/api/labs/"+labPath(lab)+"/connections", map[string]any{
		"a_type": "device", "a_name": created.Name, "a_if": 0, "b_type": "device", "b_name": peer, "b_if": 0,
	})
	expect(t, rec, http.StatusCreated, "cable on te2")
	var conn Conn
	decodeJSON(t, rec.Body.Bytes(), &conn)

	// Changing the format affects names only: the cable, interface number and adapter count stay the same.
	rec = e.do(t, admin, http.MethodPut, "/api/labs/"+labPath(lab)+"/devices/"+labPath(created.Name), map[string]any{"iface_type": "ge", "iface_start": 5})
	expect(t, rec, http.StatusOK, "changing the name format")
	var saved Device
	decodeJSON(t, rec.Body.Bytes(), &saved)
	if saved.IfaceType != "ge" || saved.IfaceStart != 5 || saved.NICs != 3 || saved.RID != created.RID {
		t.Fatalf("settings after format change: %q/%d nics=%d rid=%d", saved.IfaceType, saved.IfaceStart, saved.NICs, saved.RID)
	}

	var kind string
	var start int
	if err := e.db.QueryRow(`SELECT iface_type,iface_start FROM instance_devices WHERE id=?`, created.RID).Scan(&kind, &start); err != nil {
		t.Fatal(err)
	}
	if kind != "ge" || start != 5 {
		t.Fatalf("format not saved in the instance row: %q/%d", kind, start)
	}
	cable, _, _ := e.db.Labs.Connection(lab, conn.Key)
	if cable.A.If != 0 || cable.B.If != 0 {
		t.Fatalf("cable interface numbers changed: %d/%d", cable.A.If, cable.B.If)
	}

	// A new adapter (number = current interface count) extends the machine and preserves the chosen format.
	peer2 := labtest.Device(t, e.db, lab, labfile.Device{Name: "peer2"})
	rec = e.do(t, admin, http.MethodPost, "/api/labs/"+labPath(lab)+"/connections", map[string]any{
		"a_type": "device", "a_name": created.Name, "a_if": 3, "b_type": "device", "b_name": peer2, "b_if": 0,
	})
	expect(t, rec, http.StatusCreated, "new adapter to machine")
	if grown := devOf(t, e.db, lab, created.Name).NICs; grown != 4 {
		t.Fatalf("machine was not extended: nics=%d", grown)
	}
	if err := e.db.QueryRow(`SELECT iface_type,iface_start FROM instance_devices WHERE id=?`, created.RID).Scan(&kind, &start); err != nil {
		t.Fatal(err)
	}
	if kind != "ge" || start != 5 {
		t.Fatalf("format lost when adding an adapter: %q/%d", kind, start)
	}

	// Invalid values are rejected.
	rec = e.do(t, admin, http.MethodPut, "/api/labs/"+labPath(lab)+"/devices/"+labPath(created.Name), map[string]any{"iface_type": "eth"})
	expect(t, rec, http.StatusBadRequest, "unknown interface type")
	rec = e.do(t, admin, http.MethodPut, "/api/labs/"+labPath(lab)+"/devices/"+labPath(created.Name), map[string]any{"iface_start": 32})
	expect(t, rec, http.StatusBadRequest, "number out of range")
}

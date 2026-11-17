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
	b := e.device(t, lab)

	var attached []int64
	mux := http.NewServeMux()
	(&Handler{DB: e.db, Auth: e.as, CableAttach: func(cid int64) []error {
		attached = append(attached, cid)
		return nil
	}}).Register(mux)
	e.srv = mux

	// e0 is occupied by an existing wire.
	expect(t, e.do(t, admin, http.MethodPost, "/api/labs/"+itoa(lab)+"/connections",
		map[string]any{"a_type": "device", "a_id": a, "a_if": 0, "b_type": "device", "b_id": b, "b_if": 0}), http.StatusCreated, "первый провод")
	if len(attached) != 1 {
		t.Fatalf("живое подключение не вызвано: %v", attached)
	}

	// Reusing e0 is forbidden.
	rec := e.do(t, admin, http.MethodPost, "/api/labs/"+itoa(lab)+"/connections",
		map[string]any{"a_type": "device", "a_id": a, "a_if": 0, "b_type": "device", "b_id": b, "b_if": 1})
	expect(t, rec, http.StatusConflict, "занятый интерфейс")

	// e1 == nics: a new port is created.
	rec = e.do(t, admin, http.MethodPost, "/api/labs/"+itoa(lab)+"/connections",
		map[string]any{"a_type": "device", "a_id": a, "a_if": 1, "b_type": "device", "b_id": b, "b_if": 1})
	expect(t, rec, http.StatusCreated, "новый интерфейс")
	if len(attached) != 2 {
		t.Fatalf("горячее подключение не вызвано: %v", attached)
	}

	nics := devOf(t, e.db, a).NICs
	if nics != 2 {
		t.Fatalf("устройство не расширено: nics=%d", nics)
	}

	// e3 > nics: there is no such interface.
	rec = e.do(t, admin, http.MethodPost, "/api/labs/"+itoa(lab)+"/connections",
		map[string]any{"a_type": "device", "a_id": a, "a_if": 3, "b_type": "device", "b_id": b, "b_if": 2})
	expect(t, rec, http.StatusBadRequest, "интерфейс вне диапазона")
}

// Removing a wire of a running machine goes through CablePlan/CableDetach and removes the database record.
func TestBulkDeleteConnectionRunsCableHooks(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "admin", "admin")
	lab := e.lab(t, admin.id)
	a := e.device(t, lab)
	b := e.device(t, lab)
	cid := labtest.Connection(t, e.db, lab, labfile.Connection{
		A: labfile.End{Type: "device", ID: a, If: 0},
		B: labfile.End{Type: "device", ID: b, If: 0},
	})

	planned, detached := false, false
	mux := http.NewServeMux()
	(&Handler{DB: e.db, Auth: e.as,
		CablePlan: func(ids []int64) []network.Plug {
			planned = len(ids) == 1 && ids[0] == cid
			return []network.Plug{{RID: 7, NIC: 0}}
		},
		CableDetach: func(plugs []network.Plug) []error {
			detached = len(plugs) == 1 && plugs[0].RID == 7 && plugs[0].NIC == 0
			return nil
		},
	}).Register(mux)
	e.srv = mux

	rec := e.do(t, admin, http.MethodPost, "/api/labs/"+itoa(lab)+"/delete", map[string]any{"connections": []int64{cid}})
	expect(t, rec, http.StatusOK, "удаление провода")
	if !planned || !detached {
		t.Fatalf("живое отключение не вызвано: plan=%v detach=%v", planned, detached)
	}
	left := 0
	if _, _, ok := e.db.Labs.Connection(cid); ok {
		left = 1
	}
	if left != 0 {
		t.Fatal("провод остался в БД")
	}
}

// The interface name format (ens/te/ge + starting number) is stored on the machine and its instance row,
// returned by the API and does not affect wires and interface numbers when changed.
func TestDeviceIfaceNamingPersists(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "admin", "admin")
	lab := e.lab(t, admin.id)
	tplID := e.template(t)

	rec := e.do(t, admin, http.MethodPost, "/api/labs/"+itoa(lab)+"/devices", map[string]any{
		"template_id": tplID, "x": 10, "y": 10, "nics": 3, "iface_type": "te", "iface_start": 2,
	})
	expect(t, rec, http.StatusCreated, "машина с интерфейсами te2…")
	var created Device
	decodeJSON(t, rec.Body.Bytes(), &created)
	if created.IfaceType != "te" || created.IfaceStart != 2 {
		t.Fatalf("формат не сохранён в ответе: %q/%d", created.IfaceType, created.IfaceStart)
	}

	peer := e.device(t, lab)
	rec = e.do(t, admin, http.MethodPost, "/api/labs/"+itoa(lab)+"/connections", map[string]any{
		"a_type": "device", "a_id": created.ID, "a_if": 0, "b_type": "device", "b_id": peer, "b_if": 0,
	})
	expect(t, rec, http.StatusCreated, "провод на te2")
	var conn Conn
	decodeJSON(t, rec.Body.Bytes(), &conn)

	// Changing the format affects names only: the wire, interface number and adapter count stay the same.
	rec = e.do(t, admin, http.MethodPut, "/api/devices/"+itoa(created.RID), map[string]any{"iface_type": "ge", "iface_start": 5})
	expect(t, rec, http.StatusOK, "изменение формата имён")
	var saved Device
	decodeJSON(t, rec.Body.Bytes(), &saved)
	if saved.IfaceType != "ge" || saved.IfaceStart != 5 || saved.NICs != 3 || saved.RID != created.RID {
		t.Fatalf("настройки после смены формата: %q/%d nics=%d rid=%d", saved.IfaceType, saved.IfaceStart, saved.NICs, saved.RID)
	}

	var kind string
	var start int
	if err := e.db.QueryRow(`SELECT iface_type,iface_start FROM instance_devices WHERE id=?`, created.RID).Scan(&kind, &start); err != nil {
		t.Fatal(err)
	}
	if kind != "ge" || start != 5 {
		t.Fatalf("формат не сохранён в строке экземпляра: %q/%d", kind, start)
	}
	cable, _, _ := e.db.Labs.Connection(conn.ID)
	aIf, bIf := cable.A.If, cable.B.If
	if aIf != 0 || bIf != 0 {
		t.Fatalf("номера интерфейсов провода изменились: %d/%d", aIf, bIf)
	}

	// A new adapter (number = current interface count) extends the machine and preserves the chosen format:
	// the new port name is computed as ge<5+3>, no separate field is needed.
	peer2 := e.device(t, lab)
	rec = e.do(t, admin, http.MethodPost, "/api/labs/"+itoa(lab)+"/connections", map[string]any{
		"a_type": "device", "a_id": created.ID, "a_if": 3, "b_type": "device", "b_id": peer2, "b_if": 0,
	})
	expect(t, rec, http.StatusCreated, "новый адаптер к машине")
	grown := devOf(t, e.db, created.ID).NICs
	if grown != 4 {
		t.Fatalf("машина не расширена: nics=%d", grown)
	}
	if err := e.db.QueryRow(`SELECT iface_type,iface_start FROM instance_devices WHERE id=?`, created.RID).Scan(&kind, &start); err != nil {
		t.Fatal(err)
	}
	if kind != "ge" || start != 5 {
		t.Fatalf("формат потерян при добавлении адаптера: %q/%d", kind, start)
	}

	// Invalid values are rejected.
	rec = e.do(t, admin, http.MethodPut, "/api/devices/"+itoa(created.RID), map[string]any{"iface_type": "eth"})
	expect(t, rec, http.StatusBadRequest, "неизвестный тип интерфейса")
	rec = e.do(t, admin, http.MethodPut, "/api/devices/"+itoa(created.RID), map[string]any{"iface_start": 32})
	expect(t, rec, http.StatusBadRequest, "номер вне диапазона")
}

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

	rec := e.do(t, owner, http.MethodPost, "/api/labs/"+itoa(lab)+"/devices",
		map[string]any{"template_id": tplID, "x": 100, "y": 100, "count": 5})
	expect(t, rec, http.StatusCreated, "пакетное добавление 5 машин")
	var batch []Device
	decodeJSON(t, rec.Body.Bytes(), &batch)
	if len(batch) != 5 {
		t.Fatalf("ожидалось 5 машин, получено %d", len(batch))
	}

	ids, rids, names, pos := map[int64]bool{}, map[int64]bool{}, map[string]bool{}, map[[2]float64]bool{}
	macs := map[string]bool{}
	for _, d := range batch {
		if ids[d.ID] || rids[d.RID] || names[d.Name] {
			t.Fatalf("дубль: id=%d rid=%d name=%q", d.ID, d.RID, d.Name)
		}
		if d.RID == 0 {
			t.Fatalf("машина %q без rid: настройки и диски не будут привязаны", d.Name)
		}
		ids[d.ID], rids[d.RID], names[d.Name] = true, true, true
		pos[[2]float64{d.X, d.Y}] = true
		// The MAC is built from the rid (qemu.MAC): every machine has its own.
		for nic := 0; nic < d.NICs; nic++ {
			m := qemu.MAC(d.RID, nic)
			if macs[m] {
				t.Fatalf("повтор MAC %s у машины %q", m, d.Name)
			}
			macs[m] = true
		}
	}
	if len(pos) != 5 {
		t.Fatalf("машины на схеме наложены друг на друга: %d позиций из 5", len(pos))
	}

	devCount := devCountOf(e.db, lab)
	var instCount int
	e.db.QueryRow(`SELECT COUNT(*) FROM instance_devices WHERE instance_id IN (SELECT id FROM lab_instances WHERE lab_id=?)`, lab).Scan(&instCount)
	if devCount != 5 || instCount != 5 {
		t.Fatalf("строки в БД: devices=%d instance_devices=%d, ожидалось 5 и 5", devCount, instCount)
	}

	// Independent settings: change one machine, the others keep their values.
	first := batch[0]
	expect(t, e.do(t, owner, http.MethodPut, "/api/devices/"+itoa(first.RID),
		map[string]any{"name": "cisco-a", "cpu": 4}), http.StatusOK, "настройки первой машины")
	for _, d := range batch[1:] {
		var cpu int
		e.db.QueryRow(`SELECT cpu FROM instance_devices WHERE id=?`, d.RID).Scan(&cpu)
		if cpu == 4 {
			t.Fatalf("настройки машины %q применились к соседней", first.Name)
		}
	}

	// A name set manually becomes the base for the batch: router-1, router-2, ...
	rec = e.do(t, owner, http.MethodPost, "/api/labs/"+itoa(lab)+"/devices",
		map[string]any{"template_id": tplID, "x": 300, "y": 100, "count": 3, "name": "router"})
	expect(t, rec, http.StatusCreated, "пакет с базовым именем")
	var second []Device
	decodeJSON(t, rec.Body.Bytes(), &second)
	for _, d := range second {
		if names[d.Name] {
			t.Fatalf("имя %q повторяет уже существующее", d.Name)
		}
		names[d.Name] = true
	}

	// A single add (count not set) still returns one object.
	rec = e.do(t, owner, http.MethodPost, "/api/labs/"+itoa(lab)+"/devices",
		map[string]any{"template_id": tplID, "x": 500, "y": 500})
	expect(t, rec, http.StatusCreated, "одиночное добавление")
	var single Device
	decodeJSON(t, rec.Body.Bytes(), &single)
	if single.ID == 0 || single.RID == 0 {
		t.Fatalf("одиночная машина без id/rid: %+v", single)
	}

	// Bounds: 0 means "one machine" (default behaviour); negative and more than 20 - an error.
	expect(t, e.do(t, owner, http.MethodPost, "/api/labs/"+itoa(lab)+"/devices",
		map[string]any{"template_id": tplID, "x": 0, "y": 0, "count": 21}), http.StatusBadRequest, "больше 20 машин за раз")
	expect(t, e.do(t, owner, http.MethodPost, "/api/labs/"+itoa(lab)+"/devices",
		map[string]any{"template_id": tplID, "x": 0, "y": 0, "count": -1}), http.StatusBadRequest, "отрицательное количество")

	// A template error before insertion creates no machine from the batch.
	before := devCountOf(e.db, lab)
	expect(t, e.do(t, owner, http.MethodPost, "/api/labs/"+itoa(lab)+"/devices",
		map[string]any{"template_id": 999999, "x": 0, "y": 0, "count": 3}), http.StatusBadRequest, "несуществующий шаблон в пачке")
	after := devCountOf(e.db, lab)
	if after != before {
		t.Fatalf("при ошибке создались лишние машины: было %d, стало %d", before, after)
	}
}

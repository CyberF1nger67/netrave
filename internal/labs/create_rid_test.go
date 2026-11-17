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

	rec := e.do(t, owner, http.MethodPost, "/api/labs/"+itoa(lab)+"/devices",
		map[string]any{"template_id": tplID, "x": 100, "y": 100})
	expect(t, rec, http.StatusCreated, "создание машины")
	var created Device
	decodeJSON(t, rec.Body.Bytes(), &created)
	if created.RID == 0 {
		t.Fatalf("ответ создания без rid: при сохранении настроек будет 404 «устройство не найдено»")
	}

	var rows int
	e.db.QueryRow(`SELECT COUNT(*) FROM instance_devices WHERE id=? AND device_id=?`, created.RID, created.ID).Scan(&rows)
	if rows != 1 {
		t.Fatalf("rid %d не соответствует строке экземпляра машины %d", created.RID, created.ID)
	}

	expect(t, e.do(t, owner, http.MethodPut, "/api/devices/"+itoa(created.RID),
		map[string]any{"name": "router-1", "cpu": 2}), http.StatusOK, "сохранение настроек созданной машины")

	name := devOf(t, e.db, created.ID).Name
	if name != "router-1" {
		t.Fatalf("имя не сохранилось: %q", name)
	}
}

// Paste returns copies of the machines (the originals are not returned); the copy must have an instance rid, not 0.
func TestPastedDeviceHasRID(t *testing.T) {
	e := newTenv(t)
	owner := e.user(t, "owner", "user")
	lab := e.lab(t, owner.id)
	dev := e.device(t, lab)
	e.runInstance(t, lab, dev, owner.id, "stopped", 0, 0)

	rec := e.do(t, owner, http.MethodPost, "/api/labs/"+itoa(lab)+"/paste",
		map[string]any{"devices": []int64{dev}, "networks": []int64{}, "dx": 40, "dy": 40})
	expect(t, rec, http.StatusCreated, "вставка машины")
	var out struct {
		Devices []Device `json:"devices"`
	}
	decodeJSON(t, rec.Body.Bytes(), &out)
	if len(out.Devices) != 1 {
		t.Fatalf("ожидалась одна копия машины, получено %d", len(out.Devices))
	}
	c := out.Devices[0]
	if c.ID == dev {
		t.Fatalf("в ответе вставки должна быть копия, а не исходная машина")
	}
	if c.RID == 0 {
		t.Errorf("копия машины %d из вставки без rid: настройки не сохранятся", c.ID)
	}
}

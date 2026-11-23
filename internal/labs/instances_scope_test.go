// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"netrave/internal/auth"
	"netrave/internal/database"
	"netrave/internal/labfile"
	"netrave/internal/labtest"
	"netrave/internal/qemu"
)

// The "Running labs" summary describes the lab, not a single instance. An idle machine of
// another user must neither increase the lab's machine count nor distort the status.
func TestRunningStatusIgnoresOtherUsersIdleInstance(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "admin", "admin")
	u2 := e.user(t, "u2", "user")

	lab := e.lab(t, u2.id)
	dev := e.device(t, lab)
	e.runInstance(t, lab, dev, u2.id, "running", os.Getpid(), 1000) // the owner's machine is running
	e.runInstance(t, lab, dev, admin.id, "stopped", 0, 0)           // the administrator's same machine is off

	var out []RunningLab
	rec := e.do(t, admin, http.MethodGet, "/api/labs/running", nil)
	expect(t, rec, http.StatusOK, "running list")
	decodeJSON(t, rec.Body.Bytes(), &out)
	if len(out) != 1 {
		t.Fatalf("want one running lab, got %d", len(out))
	}
	l := out[0]
	if l.Devices != 1 {
		t.Errorf("the lab has one machine, the summary shows %d (machines of all instances are counted)", l.Devices)
	}
	if l.Status != "running" {
		t.Errorf("the owner's only machine is running, status must be running, not %q", l.Status)
	}
}

// Wiping a user's instance touches only their machines. The lab owner's and other instances'
// machines are not stopped or wiped, even if the user has the lab.wipe permission.
func TestWipeOwnInstanceLeavesOtherInstancesAlone(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "scope.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	as := auth.New(db)
	var wiped []int64
	mux := http.NewServeMux()
	(&Handler{
		DB: db, Auth: as,
		WipeDevice: func(id int64) error { wiped = append(wiped, id); return nil },
	}).Register(mux)
	e := &tenv{db: db, srv: mux, as: as, m: qemu.New(db, nil, nil)}

	owner := e.user(t, "owner", "user")
	u2 := e.user(t, "u2", "user")
	lab := e.lab(t, owner.id)
	d1 := e.device(t, lab)
	d2 := labtest.Device(t, e.db, lab, labfile.Device{Name: "pc2"})

	ownerRID := e.runInstance(t, lab, d1, owner.id, "running", os.Getpid(), 1000)
	u2RID1 := e.runInstance(t, lab, d1, u2.id, "stopped", 0, 0)
	u2RID2 := e.runInstance(t, lab, d2, u2.id, "stopped", 0, 0)

	labtest.Perm(t, e.db, lab, u2.id, "lab.view", true)
	labtest.Perm(t, e.db, lab, u2.id, "lab.wipe", true)

	expect(t, e.do(t, u2, http.MethodPost, "/api/labs/"+labPath(lab)+"/wipe", nil), http.StatusOK, "wipe of own instance")

	want := map[int64]bool{u2RID1: true, u2RID2: true}
	if len(wiped) != len(want) {
		t.Fatalf("only u2's machines should be wiped (%v), wiped %v", want, wiped)
	}
	for _, id := range wiped {
		if !want[id] {
			t.Errorf("another user's machine was wiped rid=%d (owner's machine rid=%d)", id, ownerRID)
		}
	}
}

// An instance of another lab cannot be passed to the Wipe request: the lab_id check prevents
// wiping machines of another lab, even if the user owns both.
func TestWipeRejectsInstanceFromAnotherLab(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "cross.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	as := auth.New(db)
	var wiped []int64
	mux := http.NewServeMux()
	(&Handler{
		DB: db, Auth: as,
		WipeDevice: func(id int64) error { wiped = append(wiped, id); return nil },
	}).Register(mux)
	e := &tenv{db: db, srv: mux, as: as, m: qemu.New(db, nil, nil)}

	owner := e.user(t, "owner", "user")
	labA := e.lab(t, owner.id)
	labB := e.namedLab(t, owner.id, "labB")
	devA := e.device(t, labA)
	devB := e.device(t, labB)
	aRID := e.runInstance(t, labA, devA, owner.id, "stopped", 0, 1000)
	bRID := e.runInstance(t, labB, devB, owner.id, "stopped", 0, 2000)
	var bInst int64
	if err := e.db.QueryRow(`SELECT instance_id FROM instance_devices WHERE id=?`, bRID).Scan(&bInst); err != nil {
		t.Fatal(err)
	}

	rec := e.do(t, owner, http.MethodPost, "/api/labs/"+labPath(labA)+"/wipe?instance="+itoa(bInst), nil)
	expect(t, rec, http.StatusNotFound, "foreign instance in Wipe")
	if len(wiped) != 0 {
		t.Fatalf("Wipe of a foreign lab wiped machines %v", wiped)
	}
	for rid, want := range map[int64]int64{aRID: 1000, bRID: 2000} {
		var started int64
		if err := e.db.QueryRow(`SELECT started_at FROM instance_devices WHERE id=?`, rid).Scan(&started); err != nil {
			t.Fatal(err)
		}
		if started != want {
			t.Errorf("machine %d: start history changed (%d instead of %d)", rid, started, want)
		}
	}
}

// Wiping one instance does not touch the start history or the rows of other instances of the same lab.
// Previously the wipe time was shared by the lab, so wiping one user hid all other
// instances from "Running" - including those of other users.
func TestWipeOneInstanceKeepsOtherInstancesInRunningList(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "admin", "admin")
	owner := e.user(t, "owner", "user")
	u2 := e.user(t, "u2", "user")

	lab := e.lab(t, owner.id)
	dev := e.device(t, lab)
	ownerRID := e.runInstance(t, lab, dev, owner.id, "stopped", 0, 1000)
	u2RID := e.runInstance(t, lab, dev, u2.id, "stopped", 0, 2000)
	var u2Inst int64
	if err := e.db.QueryRow(`SELECT instance_id FROM instance_devices WHERE id=?`, u2RID).Scan(&u2Inst); err != nil {
		t.Fatal(err)
	}

	fetch := func(u sess) []RunningLab {
		t.Helper()
		var out []RunningLab
		rec := e.do(t, u, http.MethodGet, "/api/labs/running", nil)
		expect(t, rec, http.StatusOK, "running list")
		decodeJSON(t, rec.Body.Bytes(), &out)
		return out
	}
	if rows := fetch(admin); len(rows) != 2 {
		t.Fatalf("before Wipe both instances must be visible: %+v", rows)
	}

	// The administrator wipes only u2's instance.
	rec := e.do(t, admin, http.MethodPost, "/api/labs/"+labPath(lab)+"/wipe?instance="+itoa(u2Inst), nil)
	expect(t, rec, http.StatusOK, "Wipe of the selected instance")

	rows := fetch(admin)
	if len(rows) != 1 || rows[0].InstanceUser != "owner" {
		t.Fatalf("after Wipe of one instance only the owner's instance must remain: %+v", rows)
	}
	if rows[0].StartedAt != 1000 {
		t.Errorf("start history of another instance changed: %d", rows[0].StartedAt)
	}
	if rows[0].WipedAt != 0 || rows[0].Status == "wiped" {
		t.Errorf("Wipe of another instance marked this instance as wiped: %+v", rows[0])
	}
	if got := fetch(owner); len(got) != 1 || got[0].InstanceUser != "owner" {
		t.Errorf("owner lost their instance after Wipe of another instance: %+v", got)
	}
	if got := fetch(u2); len(got) != 0 {
		t.Errorf("wiped instance must disappear from the list: %+v", got)
	}
	var started int64
	if err := e.db.QueryRow(`SELECT started_at FROM instance_devices WHERE id=?`, ownerRID).Scan(&started); err != nil {
		t.Fatal(err)
	}
	if started != 1000 {
		t.Errorf("Wipe of another instance erased start history: %d", started)
	}
}

// The "Running labs" section: one row per instance. The admin sees all users; the lab owner
// sees rows of their lab, but can open and manage only their own instance; an outsider sees nothing.
func TestRunningListOneRowPerInstance(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "admin", "admin")
	owner := e.user(t, "owner", "user")
	u2 := e.user(t, "u2", "user")

	lab := e.lab(t, owner.id)
	dev := e.device(t, lab)
	e.runInstance(t, lab, dev, owner.id, "running", os.Getpid(), 1000)
	e.runInstance(t, lab, dev, u2.id, "running", os.Getpid(), 2000)

	byUser := func(rows []RunningLab) map[string]RunningLab {
		m := map[string]RunningLab{}
		for _, r := range rows {
			m[r.InstanceUser] = r
		}
		return m
	}
	fetch := func(u sess) []RunningLab {
		t.Helper()
		var out []RunningLab
		rec := e.do(t, u, http.MethodGet, "/api/labs/running", nil)
		expect(t, rec, http.StatusOK, "running list")
		decodeJSON(t, rec.Body.Bytes(), &out)
		return out
	}

	adminRows := byUser(fetch(admin))
	if len(adminRows) != 2 || !adminRows["owner"].CanOpen || !adminRows["u2"].CanOpen {
		t.Fatalf("admin must see both rows and open any: %+v", adminRows)
	}

	ownerRows := byUser(fetch(owner))
	if len(ownerRows) != 2 {
		t.Fatalf("lab owner must see rows of their lab, sees %d", len(ownerRows))
	}
	if !ownerRows["owner"].CanOpen || ownerRows["u2"].CanOpen || ownerRows["u2"].CanStop {
		t.Fatalf("owner manages only their own instance: %+v", ownerRows)
	}

	// A user sees only their own instance; without lab permissions they can neither open it nor manage
	// machines. The lab owner's instance (even if opened) is not visible to them.
	u2Rows := byUser(fetch(u2))
	if len(u2Rows) != 1 {
		t.Fatalf("user must see only their own row, sees %d: %+v", len(u2Rows), u2Rows)
	}
	own := u2Rows["u2"]
	if !own.Mine || !own.Own {
		t.Fatalf("own instance must be marked mine/own: %+v", own)
	}
	if own.CanOpen || own.CanStart || own.CanStop || own.CanWipe {
		t.Fatalf("without lab permissions managing the instance must be forbidden: %+v", own)
	}
}

// The machine image is stored in the topology; an unknown image name is rejected and the old value remains.
func TestDeviceIconIsSavedAndValidated(t *testing.T) {
	e := newTenv(t)
	owner := e.user(t, "owner", "user")
	lab := e.lab(t, owner.id)
	dev := e.device(t, lab)
	e.runInstance(t, lab, dev, owner.id, "stopped", 0, 0)
	path := "/api/labs/" + labPath(lab) + "/devices/" + labPath(dev)

	expect(t, e.do(t, owner, http.MethodPut, path, map[string]any{"icon": "router"}), http.StatusOK, "choosing the router image")
	var icon string
	icon = devOf(t, e.db, lab, dev).Icon
	if icon != "router" {
		t.Fatalf("image was not saved: %q", icon)
	}

	expect(t, e.do(t, owner, http.MethodPut, path, map[string]any{"icon": "toaster"}), http.StatusBadRequest, "unknown image")
	icon = devOf(t, e.db, lab, dev).Icon
	if icon != "router" {
		t.Fatalf("after the rejection the image must stay the same, became %q", icon)
	}
}

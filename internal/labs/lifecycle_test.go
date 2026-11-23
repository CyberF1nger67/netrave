// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"netrave/internal/auth"
	"netrave/internal/database"
	"netrave/internal/journal"
	"netrave/internal/labfile"
	"netrave/internal/labtest"
	"netrave/internal/qemu"
)

// The "Running labs" section: the admin sees all labs, a user - only their own and visible ones.
func TestRunningListVisibilityAndStatus(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "admin", "admin")
	u1 := e.user(t, "u1", "user")
	u2 := e.user(t, "u2", "user")

	labA := e.lab(t, u2.id) // u2's lab with a live machine in u2's instance
	devA := e.device(t, labA)
	e.runInstance(t, labA, devA, u2.id, "running", os.Getpid(), 1000)

	labB := e.namedLab(t, admin.id, "labB") // the administrator's lab with a dead QEMU process in the administrator's instance
	devB := e.device(t, labB)
	e.runInstance(t, labB, devB, admin.id, "running", 2147483646, 0)

	labC := e.namedLab(t, u1.id, "labC") // u1's lab without running machines - must not appear in the list
	e.device(t, labC)

	get := func(u sess) []RunningLab {
		t.Helper()
		var out []RunningLab
		rec := e.do(t, u, http.MethodGet, "/api/labs/running", nil)
		expect(t, rec, http.StatusOK, "running list")
		decodeJSON(t, rec.Body.Bytes(), &out)
		return out
	}

	adminList := get(admin)
	if len(adminList) != 2 {
		t.Fatalf("administrator must see 2 running labs, sees %d", len(adminList))
	}
	byID := map[string]RunningLab{}
	for _, l := range adminList {
		byID[l.ID] = l
	}
	if l := byID[labA]; l.Owner != "u2" || l.Status != "running" || l.Running != 1 || l.Devices != 1 || l.StartedAt != 1000 {
		t.Fatalf("summary of u2's lab is wrong: %+v", l)
	}
	if l := byID[labB]; l.Status != "stale" || l.Stale != 1 {
		t.Fatalf("QEMU process not found, status must be stale: %+v", l)
	}

	// The user sees their own rows. Other users' instances (even opened ones) are not
	// shown to them: the list is built from lab instances.
	// u1 owns labC, but without running machines it is not in the list; other users' opened labs are not visible either.
	u1List := get(u1)
	if len(u1List) != 0 {
		t.Fatalf("u1 must not see any rows, sees %d: %+v", len(u1List), u1List)
	}
	for _, l := range u1List {
		if l.ID == labC {
			t.Fatalf("lab without start history must not be shown: %+v", l)
		}
	}
	// Own lab - with permissions; another user's lab (opened by the administrator but with its own instance
	// row) is not visible to the user.
	_ = labB
	u2List := get(u2)
	var ownRunning, foreign *RunningLab
	for i := range u2List {
		if u2List[i].ID == labA {
			ownRunning = &u2List[i]
		}
		if u2List[i].ID == labB {
			foreign = &u2List[i]
		}
	}
	if ownRunning == nil || !ownRunning.CanOpen || !ownRunning.CanWipe {
		t.Fatalf("u2 must see their running lab with the rights to open and wipe: %+v", u2List)
	}
	if foreign != nil && (foreign.CanOpen || foreign.CanWipe || foreign.CanStart) {
		t.Fatalf("foreign lab must not grant rights: %+v", foreign)
	}
	if adminList := get(admin); len(adminList) != 2 {
		t.Fatalf("administrator must see both running labs")
	}
}

// Wipe Lab: without lab.wipe - 403; with the permission (admin) - all devices are wiped, shutdown is performed, history and journal are written.
func TestWipeLabPermissionAndEffects(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	as := auth.New(db)
	wiped := []int64{}
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as,
		WipeDevice: func(id int64) error { wiped = append(wiped, id); return nil },
	}).Register(mux)
	e := &tenv{db: db, srv: mux, as: as, m: qemu.New(db, nil, nil)}

	admin := e.user(t, "admin", "admin")
	u1 := e.user(t, "u1", "user")
	lab := e.lab(t, u1.id)
	e.device(t, lab)
	labtest.Device(t, e.db, lab, labfile.Device{Name: "pc2"})

	// A user without lab.wipe cannot wipe the lab.
	other := e.user(t, "u3", "user")
	labtest.Perm(t, e.db, lab, other.id, "lab.view", true)
	expect(t, e.do(t, other, http.MethodPost, "/api/labs/"+labPath(lab)+"/wipe", nil), http.StatusForbidden, "wipe without lab.wipe")
	if len(wiped) != 0 {
		t.Fatal("nothing must run when the permission is denied")
	}

	// The administrator wipes another user's lab.
	expect(t, e.do(t, admin, http.MethodPost, "/api/labs/"+labPath(lab)+"/wipe", nil), http.StatusOK, "wipe by administrator")
	// Wipe affects the administrator's instance (their own by default): two machines, each wiped.
	if len(wiped) != 2 {
		t.Fatalf("not all machines of the instance were wiped: %v", wiped)
	}

	wipes, err := db.Journal.LabWipes(lab, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(wipes) != 1 || wipes[0].Username != "admin" || wipes[0].Data["devices"] != float64(2) {
		t.Fatalf("Wipe operation was not written to the journal: %+v", wipes)
	}
	admins, err := db.Journal.Tail(10, func(e journal.Entry) bool {
		return e.Action == "admin_lab_action" && e.Username == "admin"
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(admins) == 0 {
		t.Fatal("administrator action in a foreign lab was not written to the journal")
	}
	expect(t, e.do(t, admin, http.MethodGet, "/api/labs/"+labPath(lab)+"/wipes", nil), http.StatusOK, "wipe history")
}

// runInstance creates the user's instance (like the application) and its machine in the given state.
func (e *tenv) runInstance(t *testing.T, lab, dev string, user int64, state string, pid int, started int64) int64 {
	t.Helper()
	if _, err := e.db.Exec(`INSERT OR IGNORE INTO lab_instances(lab_id,user_id) VALUES(?,?)`, lab, user); err != nil {
		t.Fatal(err)
	}
	var inst int64
	if err := e.db.QueryRow(`SELECT id FROM lab_instances WHERE lab_id=? AND user_id=?`, lab, user).Scan(&inst); err != nil {
		t.Fatal(err)
	}
	res, err := e.db.Exec(`INSERT INTO instance_devices(instance_id,device_id,started_at) VALUES(?,?,?)`,
		inst, dev, started)
	if err != nil {
		t.Fatal(err)
	}
	rid, _ := res.LastInsertId()
	e.m.SetRuntime(rid, qemu.Runtime{State: state, PID: pid})
	return rid
}

// The user sees their running instance even if the lab belongs to someone else.
func TestRunningListShowsOwnInstanceInForeignLab(t *testing.T) {
	e := newTenv(t)
	owner := e.user(t, "owner", "user")
	guest := e.user(t, "guest", "user")
	lab := e.lab(t, owner.id)
	dev := e.device(t, lab)
	e.runInstance(t, lab, dev, guest.id, "running", os.Getpid(), 1000)

	var out []RunningLab
	rec := e.do(t, guest, http.MethodGet, "/api/labs/running", nil)
	expect(t, rec, http.StatusOK, "running list")
	decodeJSON(t, rec.Body.Bytes(), &out)
	if len(out) != 1 || out[0].InstanceUser != "guest" || !out[0].Own {
		t.Fatalf("own instance in a foreign lab is not visible: %+v", out)
	}
}

// SPDX-License-Identifier: GPL-3.0-only

package devices

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"netrave/internal/auth"
	"netrave/internal/config"
	"netrave/internal/database"
	"netrave/internal/labfile"
	"netrave/internal/labtest"
	"netrave/internal/qemu"
)

// Operations on a machine check their permission before accessing QEMU (M is not needed here to check refusal).
func TestDeviceActionsForbiddenWithoutPermission(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "d.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	as := auth.New(db)
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as}).Register(mux)

	res, _ := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('admin','x','admin')`)
	adminID, _ := res.LastInsertId()
	res, _ = db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('u1','x','user')`)
	uid, _ := res.LastInsertId()
	lab := labtest.Lab(t, db, adminID, "lab", "none")
	dev := labtest.Device(t, db, lab, labfile.Device{Name: "pc1"})
	res, _ = db.Exec(`INSERT INTO lab_instances(lab_id,user_id) VALUES(?,?)`, lab, uid)
	inst, _ := res.LastInsertId()
	res, _ = db.Exec(`INSERT INTO instance_devices(instance_id,device_id) VALUES(?,?)`, inst, dev)
	rid, _ := res.LastInsertId()
	labtest.Perm(t, db, lab, uid, "lab.view", true)
	tok, csrf, _, err := as.CreateSession(uid, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	for _, act := range []string{"start", "stop", "restart", "wipe"} {
		req := httptest.NewRequest(http.MethodPost, "/api/devices/"+itoa(rid)+"/"+act, nil)
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
		req.Header.Set("X-CSRF-Token", csrf)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s без права: ожидался HTTP 403, получен %d: %s", act, rec.Code, rec.Body.String())
		}
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// Role limit: while one lab is running, starting a second is rejected before accessing QEMU.
func TestStartLimitedByRole(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "d.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	as := auth.New(db)
	m := qemu.New(db, &config.Config{LabsDir: filepath.Join(t.TempDir(), "labs"), RunDir: filepath.Join(t.TempDir(), "run")}, nil)
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as, M: m}).Register(mux)

	db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('admin','x','admin')`)
	res, _ := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('u1','x','user')`)
	uid, _ := res.LastInsertId()

	role, err := db.Exec(`INSERT INTO roles(name,description,max_running_labs) VALUES('One','',1)`)
	if err != nil {
		t.Fatal(err)
	}
	roleID, _ := role.LastInsertId()
	db.Exec(`INSERT INTO user_roles(user_id,role_id) VALUES(?,?)`, uid, roleID)
	db.Exec(`INSERT INTO role_permissions(role_id,permission) VALUES(?,'laboratories.start_stop')`, roleID)

	// Two labs where the user is the owner; the first already has a running machine.
	var rids []int64
	for i := 0; i < 2; i++ {
		lab := labtest.Lab(t, db, uid, "lab", "none")
		dev := labtest.Device(t, db, lab, labfile.Device{Name: "pc"})
		res, _ = db.Exec(`INSERT INTO lab_instances(lab_id,user_id) VALUES(?,?)`, lab, uid)
		inst, _ := res.LastInsertId()
		state := "stopped"
		if i == 0 {
			state = "running"
		}
		res, _ = db.Exec(`INSERT INTO instance_devices(instance_id,device_id) VALUES(?,?)`, inst, dev)
		rid, _ := res.LastInsertId()
		m.SetRuntime(rid, qemu.Runtime{State: state})
		rids = append(rids, rid)
	}
	tok, csrf, _, err := as.CreateSession(uid, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	start := func(rid int64) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/devices/"+itoa(rid)+"/start", nil)
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
		req.Header.Set("X-CSRF-Token", csrf)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	if rec := start(rids[1]); rec.Code != http.StatusConflict {
		t.Fatalf("запуск второй лаборатории: ожидался 409, получен %d: %s", rec.Code, rec.Body.String())
	}
}

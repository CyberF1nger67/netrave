// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"netrave/internal/auth"
	"netrave/internal/catalog"
	"netrave/internal/config"
	"netrave/internal/database"
	"netrave/internal/labfile"
	"netrave/internal/labtest"
	"netrave/internal/notify"
	"netrave/internal/qemu"
)

type tenv struct {
	db  *database.DB
	srv http.Handler
	as  *auth.Service
	m   *qemu.Manager
}

type sess struct {
	id          int64
	token, csrf string
}

func newTenv(t *testing.T) *tenv {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.AttachFolders(filepath.Join(t.TempDir(), "folders.yaml")); err != nil {
		t.Fatal(err)
	}
	as := auth.New(db)
	m := qemu.New(db, &config.Config{LabsDir: filepath.Join(t.TempDir(), "labs"), RunDir: filepath.Join(t.TempDir(), "run")}, nil)
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as, M: m}).Register(mux)
	return &tenv{db: db, srv: mux, as: as, m: m}
}

func (e *tenv) user(t *testing.T, name, role string) sess {
	t.Helper()
	res, err := e.db.Exec(`INSERT INTO users(username,password_hash,role) VALUES(?,?,?)`, name, "x", role)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	token, csrf, _, err := e.as.CreateSession(id, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	return sess{id: id, token: token, csrf: csrf}
}

func (e *tenv) lab(t *testing.T, owner int64) int64 {
	t.Helper()
	return labtest.Lab(t, e.db, owner, "lab", "none")
}

// template creates a template for tests: built-in templates are no longer shipped,
// so a test cannot take the first one it finds in the catalog.
func (e *tenv) template(t *testing.T) int64 {
	t.Helper()
	id, err := e.db.Catalog.AddTemplate(catalog.Template{
		Name: "test", Type: "linux", Arch: "x86_64", Machine: "pc",
		CPU: 1, RAMMB: 512, NICs: 1, NICModel: "virtio-net-pci", DiskBus: "virtio",
		BIOS: "seabios", Console: "serial",
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *tenv) device(t *testing.T, lab int64) int64 {
	t.Helper()
	return labtest.Device(t, e.db, lab, labfile.Device{Name: "pc1"})
}

func (e *tenv) do(t *testing.T, u sess, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		raw = b
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: u.token})
	if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", u.csrf)
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	return rec
}

func (e *tenv) grant(t *testing.T, admin sess, lab, uid int64, changes map[string]bool) {
	t.Helper()
	rec := e.do(t, admin, http.MethodPut, "/api/labs/"+itoa(lab)+"/permissions/"+itoa(uid), map[string]any{"changes": changes})
	if rec.Code != http.StatusOK {
		t.Fatalf("выдача прав: HTTP %d: %s", rec.Code, rec.Body.String())
	}
}

func (e *tenv) dbValue(t *testing.T, lab, uid int64, perm string) (int, bool) {
	t.Helper()
	l, ok := e.db.Labs.Lab(lab)
	if !ok {
		return 0, false
	}
	for _, p := range l.Permissions {
		if p.UserID == uid && p.Perm == perm {
			if p.Value {
				return 1, true
			}
			return 0, true
		}
	}
	return 0, false
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

// Only the user whose permissions are changed receives the lab permission change notification.
// The initiator of the change must not receive a message about their own permissions.
func TestPermsNotificationTargetsOnlyChangedUser(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "pn.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	as := auth.New(db)
	svc := notify.New(db)
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as, Notify: svc}).Register(mux)
	e := &tenv{db: db, srv: mux, as: as}

	actor := e.user(t, "admin", "admin")
	target := e.user(t, "target", "user")
	otherAdmin := e.user(t, "admin2", "admin")
	lab := e.lab(t, actor.id)

	e.grant(t, actor, lab, target.id, map[string]bool{"machine.start": true})

	count := func(uid int64) int {
		var n int
		n = len(svc.Snapshot(uid))
		return n
	}
	if got := count(target.id); got != 1 {
		t.Fatalf("целевой пользователь должен получить одно уведомление, получил %d", got)
	}
	if got := count(actor.id); got != 0 {
		t.Fatalf("инициатор не должен получать уведомление о чужих правах, получил %d", got)
	}
	if got := count(otherAdmin.id); got != 0 {
		t.Fatalf("другой администратор не должен получать уведомление, получил %d", got)
	}
	var title, body string
	title, body = lastNote(svc, target.id)
	if title != "Права на лабораторию изменены" || !strings.Contains(body, "admin") {
		t.Fatalf("некорректное уведомление: %q / %q", title, body)
	}
}

// An admin who changed their own permissions receives a notification on their own account.
func TestPermsNotificationSelf(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "ps.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	as := auth.New(db)
	svc := notify.New(db)
	mux := http.NewServeMux()
	(&Handler{DB: db, Auth: as, Notify: svc}).Register(mux)
	e := &tenv{db: db, srv: mux, as: as}

	owner := e.user(t, "owner", "user")
	actor := e.user(t, "admin", "admin")
	lab := e.lab(t, owner.id)

	e.grant(t, actor, lab, actor.id, map[string]bool{"machine.start": true})

	var n int
	n = len(svc.Snapshot(actor.id))
	if n != 1 {
		t.Fatalf("при изменении собственных прав уведомление должно прийти себе, получено %d", n)
	}
}

func expect(t *testing.T, rec *httptest.ResponseRecorder, code int, what string) {
	t.Helper()
	if rec.Code != code {
		t.Fatalf("%s: ожидался HTTP %d, получен %d: %s", what, code, rec.Code, rec.Body.String())
	}
}

// Without view permission the lab is hidden: 404.
func TestNoAccessHidesLab(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "admin", "admin")
	u1 := e.user(t, "u1", "user")
	lab := e.lab(t, admin.id)
	expect(t, e.do(t, u1, http.MethodGet, "/api/labs/"+itoa(lab), nil), http.StatusNotFound, "чтение без права просмотра")
}

// A granted view permission opens the lab; a revocation closes it immediately.
func TestViewGrantAndImmediateRevoke(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "admin", "admin")
	u1 := e.user(t, "u1", "user")
	lab := e.lab(t, admin.id)
	e.grant(t, admin, lab, u1.id, map[string]bool{"lab.view": true})
	expect(t, e.do(t, u1, http.MethodGet, "/api/labs/"+itoa(lab), nil), http.StatusOK, "чтение после выдачи")
	e.grant(t, admin, lab, u1.id, map[string]bool{"lab.view": false})
	expect(t, e.do(t, u1, http.MethodGet, "/api/labs/"+itoa(lab), nil), http.StatusNotFound, "чтение после отзыва")
}

// Moving machines: without machine.move - 403 and the coordinates do not change; with the
// permission - 200 and the coordinates are updated.
func TestMoveNeedsMachineMove(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "admin", "admin")
	u1 := e.user(t, "u1", "user")
	lab := e.lab(t, admin.id)
	dev := e.device(t, lab)
	e.grant(t, admin, lab, u1.id, map[string]bool{"lab.view": true})
	body := map[string]any{"devices": []map[string]any{{"id": dev, "x": 111, "y": 222}}, "networks": []any{}}

	expect(t, e.do(t, u1, http.MethodPut, "/api/labs/"+itoa(lab)+"/positions", body), http.StatusForbidden, "перемещение без machine.move")
	var x float64
	x = devOf(t, e.db, dev).X
	if x == 111 {
		t.Fatal("координаты изменились, хотя права не было")
	}

	e.grant(t, admin, lab, u1.id, map[string]bool{"machine.move": true})
	expect(t, e.do(t, u1, http.MethodPut, "/api/labs/"+itoa(lab)+"/positions", body), http.StatusOK, "перемещение с machine.move")
	x = devOf(t, e.db, dev).X
	if x != 111 {
		t.Fatalf("координаты не сохранены: x=%v", x)
	}

	e.grant(t, admin, lab, u1.id, map[string]bool{"machine.move": false})
	expect(t, e.do(t, u1, http.MethodPut, "/api/labs/"+itoa(lab)+"/positions", body), http.StatusForbidden, "перемещение после отзыва")
}

// The icon size is stored on the machine, changes together with the layout and requires machine.move.
func TestIconSizeNeedsMachineMoveAndValidRange(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "admin", "admin")
	u1 := e.user(t, "u1", "user")
	lab := e.lab(t, admin.id)
	dev := e.device(t, lab)
	e.grant(t, admin, lab, u1.id, map[string]bool{"lab.view": true})
	path := "/api/labs/" + itoa(lab) + "/positions"
	body := map[string]any{"devices": []map[string]any{{"id": dev, "x": 10, "y": 20, "icon_size": 80}}, "networks": []any{}}

	expect(t, e.do(t, u1, http.MethodPut, path, body), http.StatusForbidden, "размер иконки без machine.move")
	if size := devOf(t, e.db, dev).IconSize; size != 0 {
		t.Fatalf("размер иконки изменился без права: %d", size)
	}

	e.grant(t, admin, lab, u1.id, map[string]bool{"machine.move": true})
	expect(t, e.do(t, u1, http.MethodPut, path, body), http.StatusOK, "размер иконки с machine.move")
	if size := devOf(t, e.db, dev).IconSize; size != 80 {
		t.Fatalf("размер иконки не сохранён: %d", size)
	}

	bad := map[string]any{"devices": []map[string]any{{"id": dev, "x": 10, "y": 20, "icon_size": 999}}, "networks": []any{}}
	expect(t, e.do(t, u1, http.MethodPut, path, bad), http.StatusBadRequest, "размер иконки вне диапазона")
	if size := devOf(t, e.db, dev).IconSize; size != 80 {
		t.Fatalf("некорректный размер иконки перезаписал прежний: %d", size)
	}
}

// Creating a cable requires link.create; deleting a cable requires link.delete.
func TestLinkNeedsPermission(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "admin", "admin")
	u1 := e.user(t, "u1", "user")
	lab := e.lab(t, admin.id)
	a := e.device(t, lab)
	b := e.device(t, lab)
	e.grant(t, admin, lab, u1.id, map[string]bool{"lab.view": true})
	conn := map[string]any{"a_type": "device", "a_id": a, "a_if": 0, "b_type": "device", "b_id": b, "b_if": 0}
	expect(t, e.do(t, u1, http.MethodPost, "/api/labs/"+itoa(lab)+"/connections", conn), http.StatusForbidden, "провод без link.create")
	e.grant(t, admin, lab, u1.id, map[string]bool{"link.create": true})
	expect(t, e.do(t, u1, http.MethodPost, "/api/labs/"+itoa(lab)+"/connections", conn), http.StatusCreated, "провод с link.create")
}

// Changing one permission does not reset the others.
func TestPartialUpdateKeepsOtherPerms(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "admin", "admin")
	u1 := e.user(t, "u1", "user")
	lab := e.lab(t, admin.id)
	e.grant(t, admin, lab, u1.id, map[string]bool{"lab.view": true, "machine.start": true, "link.create": true})
	e.grant(t, admin, lab, u1.id, map[string]bool{"machine.start": false})
	if v, ok := e.dbValue(t, lab, u1.id, "link.create"); !ok || v != 1 {
		t.Fatalf("link.create сброшен: value=%v ok=%v", v, ok)
	}
	if v, ok := e.dbValue(t, lab, u1.id, "lab.view"); !ok || v != 1 {
		t.Fatalf("lab.view сброшен: value=%v ok=%v", v, ok)
	}
	if v, _ := e.dbValue(t, lab, u1.id, "machine.start"); v != 0 {
		t.Fatalf("machine.start должен быть снят")
	}
}

// Permissions granted by an admin are visible through GET after a repeated request (what the
// dialog sees when reopened).
func TestPermissionsReadBackFromDB(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "admin", "admin")
	u1 := e.user(t, "u1", "user")
	lab := e.lab(t, admin.id)
	e.grant(t, admin, lab, u1.id, map[string]bool{"machine.wipe": true, "console.vnc": true, "console.clipboard": true})
	rec := e.do(t, admin, http.MethodGet, "/api/labs/"+itoa(lab)+"/permissions", nil)
	expect(t, rec, http.StatusOK, "чтение прав")
	var resp struct {
		Users []struct {
			ID    int64           `json:"id"`
			Perms map[string]bool `json:"perms"`
		} `json:"users"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	for _, u := range resp.Users {
		if u.ID == u1.id {
			if !u.Perms["machine.wipe"] || !u.Perms["console.vnc"] || !u.Perms["console.clipboard"] || u.Perms["machine.start"] {
				t.Fatalf("неверные права в ответе: %v", u.Perms)
			}
			return
		}
	}
	t.Fatal("пользователь не найден в ответе GET")
}

// You cannot change your own permissions or grant a permission the manager does not have.
func TestNoPrivilegeEscalation(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "admin", "admin")
	u1 := e.user(t, "u1", "user")
	u2 := e.user(t, "u2", "user")
	lab := e.lab(t, admin.id)
	e.grant(t, admin, lab, u1.id, map[string]bool{"lab.view": true, "lab.manage_access": true})

	expect(t, e.do(t, u1, http.MethodPut, "/api/labs/"+itoa(lab)+"/permissions/"+itoa(u1.id),
		map[string]any{"changes": map[string]bool{"machine.wipe": true}}), http.StatusForbidden, "самоповышение")
	expect(t, e.do(t, u1, http.MethodPut, "/api/labs/"+itoa(lab)+"/permissions/"+itoa(u2.id),
		map[string]any{"changes": map[string]bool{"machine.wipe": true}}), http.StatusForbidden, "выдача права, которого нет")
	expect(t, e.do(t, u1, http.MethodPut, "/api/labs/"+itoa(lab)+"/permissions/"+itoa(u2.id),
		map[string]any{"changes": map[string]bool{"lab.view": true}}), http.StatusOK, "выдача права, которое есть")
}

// A direct call without the permission - 403, even if the checkboxes in the UI are hidden.
func TestDirectAPIIsForbiddenWithoutPerm(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "admin", "admin")
	u1 := e.user(t, "u1", "user")
	lab := e.lab(t, admin.id)
	e.grant(t, admin, lab, u1.id, map[string]bool{"lab.view": true})
	expect(t, e.do(t, u1, http.MethodDelete, "/api/labs/"+itoa(lab), nil), http.StatusForbidden, "удаление лаборатории без lab.delete")
	expect(t, e.do(t, u1, http.MethodPut, "/api/labs/"+itoa(lab), map[string]any{"name": "x"}), http.StatusForbidden, "переименование без lab.edit_meta")
}

// decodeJSON parses the test response body.
func decodeJSON(t *testing.T, b []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("разбор ответа: %v: %s", err, b)
	}
}

// Lab permissions are inherited from the assigned role: the dialog shows the role as the source
// of the permission, a direct permission takes precedence over the role.
func TestLabPermissionsInheritedFromRole(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "admin", "admin")
	u1 := e.user(t, "u1", "user")
	lab := e.lab(t, admin.id)

	res, err := e.db.Exec(`INSERT INTO roles(name,description) VALUES('Оператор','')`)
	if err != nil {
		t.Fatal(err)
	}
	roleID, _ := res.LastInsertId()
	for _, key := range []string{"laboratories.start_stop", "machines.vnc"} {
		_, _ = e.db.Exec(`INSERT INTO role_permissions(role_id,permission) VALUES(?,?)`, roleID, key)
	}
	if _, err := e.db.Exec(`INSERT INTO user_roles(user_id,role_id) VALUES(?,?)`, u1.id, roleID); err != nil {
		t.Fatal(err)
	}
	// An explicit denial to stop machines outweighs the role.
	e.grant(t, admin, lab, u1.id, map[string]bool{"machine.stop": false})

	rec := e.do(t, admin, http.MethodGet, "/api/labs/"+itoa(lab)+"/permissions", nil)
	expect(t, rec, http.StatusOK, "чтение прав")
	var resp struct {
		Users []struct {
			ID        int64           `json:"id"`
			Perms     map[string]bool `json:"perms"`
			RolePerms map[string]bool `json:"role_perms"`
			OwnPerms  map[string]bool `json:"own_perms"`
		} `json:"users"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	for _, u := range resp.Users {
		if u.ID != u1.id {
			continue
		}
		if !u.Perms["machine.start"] || u.Perms["machine.stop"] {
			t.Fatalf("права из роли: %v", u.Perms)
		}
		if !u.RolePerms["machine.start"] || !u.RolePerms["console.vnc"] {
			t.Fatalf("унаследованные права не отмечены: %v", u.RolePerms)
		}
		if own, ok := u.OwnPerms["machine.stop"]; !ok || own {
			t.Fatalf("прямое разрешение не отделено от роли: %v", u.OwnPerms)
		}
		return
	}
	t.Fatal("пользователь не найден в ответе GET")
}

// lastNote returns the title and text of the user's last notification (from the service memory).
func lastNote(svc *notify.Service, uid int64) (string, string) {
	items := svc.Snapshot(uid)
	if len(items) == 0 {
		return "", ""
	}
	last := items[len(items)-1]
	return last.Title, last.Body
}

// devOf returns a machine from the lab topology (file); if it is missing, the test fails.
func devOf(t *testing.T, db *database.DB, id int64) labfile.Device {
	t.Helper()
	d, _, ok := db.Labs.Device(id)
	if !ok {
		t.Fatalf("машина %d не найдена", id)
	}
	return d
}

// devCountOf reports how many machines are in the lab topology.
func devCountOf(db *database.DB, lab int64) int {
	l, ok := db.Labs.Lab(lab)
	if !ok {
		return 0
	}
	return len(l.Devices)
}

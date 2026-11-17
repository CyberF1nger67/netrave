// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"netrave/internal/catalog"
	"testing"

	"netrave/internal/labfile"
	"netrave/internal/labtest"
)

// Deleting a user: their labs are removed entirely (with instances), their personal permissions in other
// labs are revoked, other users' permissions remain.
func TestDropUserRemovesOwnedLabsAndPermissions(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "admin", "admin")
	gone := e.user(t, "gone", "user")
	keep := e.user(t, "keep", "user")

	own := e.lab(t, gone.id)
	ownDev := e.device(t, own)
	e.runInstance(t, own, ownDev, gone.id, "stopped", 0, 0)

	foreign := e.lab(t, admin.id)
	labtest.Perm(t, e.db, foreign, gone.id, "lab.view", true)
	labtest.Perm(t, e.db, foreign, keep.id, "lab.view", true)

	h := &Handler{DB: e.db, Auth: e.as}
	if err := h.DropUser(gone.id); err != nil {
		t.Fatal(err)
	}

	if _, ok := e.db.Labs.Lab(own); ok {
		t.Fatal("лаборатория удаляемого пользователя осталась")
	}
	var n int
	e.db.QueryRow(`SELECT COUNT(*) FROM lab_instances WHERE lab_id=?`, own).Scan(&n)
	if n != 0 {
		t.Fatalf("экземпляры удалённой лаборатории остались: %d", n)
	}
	if _, ok := e.db.Labs.Lab(foreign); !ok {
		t.Fatal("чужая лаборатория пропала")
	}
	if _, ok := e.dbValue(t, foreign, gone.id, "lab.view"); ok {
		t.Fatal("личное право удалённого пользователя в чужой лаборатории осталось")
	}
	if _, ok := e.dbValue(t, foreign, keep.id, "lab.view"); !ok {
		t.Fatal("право другого пользователя пропало")
	}
}

// Deleting a template clears the reference to it on machines in all labs; machines with another template are untouched.
func TestClearTemplateRefsResetsOnlyThatTemplate(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "admin", "admin")
	lab := e.lab(t, admin.id)
	tplA, err := e.db.Catalog.AddTemplate(catalog.Template{Name: "tpl-a", Arch: "x86_64", BIOS: "seabios", Console: "serial"})
	if err != nil {
		t.Fatal(err)
	}
	tplB, err := e.db.Catalog.AddTemplate(catalog.Template{Name: "tpl-b", Arch: "x86_64", BIOS: "seabios", Console: "serial"})
	if err != nil {
		t.Fatal(err)
	}
	a := labtest.Device(t, e.db, lab, labfile.Device{Name: "a", TemplateID: tplA})
	b := labtest.Device(t, e.db, lab, labfile.Device{Name: "b", TemplateID: tplB})

	if err := clearTemplateRefs(e.db, tplA); err != nil {
		t.Fatal(err)
	}
	if got := devOf(t, e.db, a).TemplateID; got != 0 {
		t.Fatalf("ссылка на удалённый шаблон осталась: %d", got)
	}
	if got := devOf(t, e.db, b).TemplateID; got != tplB {
		t.Fatalf("ссылка на другой шаблон изменилась: %d", got)
	}
}

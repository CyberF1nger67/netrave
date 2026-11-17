// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"encoding/json"
	"net/http"
	"netrave/internal/labtest"
	"testing"
	"time"
)

// folder creates a folder in the shared tree.
func (e *tenv) folder(t *testing.T, name string) int64 {
	t.Helper()
	f, err := e.db.Folders.Create(nil, name, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	return f.ID
}

// assignFolder assigns a folder to a user.
func (e *tenv) assignFolder(t *testing.T, folder, uid int64) {
	t.Helper()
	if err := e.db.Folders.GrantUserFolder(folder, uid, 0); err != nil {
		t.Fatal(err)
	}
}

// placeLab puts a lab into a folder.
func (e *tenv) placeLab(t *testing.T, lab, folder int64) {
	t.Helper()
	if err := e.db.Folders.Place(lab, folder); err != nil {
		t.Fatal(err)
	}
}

func (e *tenv) labIDs(t *testing.T, u sess) []int64 {
	t.Helper()
	rec := e.do(t, u, http.MethodGet, "/api/labs", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("список лабораторий: HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var list []Lab
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, 0, len(list))
	for _, l := range list {
		ids = append(ids, l.ID)
	}
	return ids
}

func contains(ids []int64, want int64) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestFolderAccessControlsLabListAndGet(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "root", "admin")
	owner := e.user(t, "owner", "user")
	viewer := e.user(t, "viewer", "user")

	lab := e.lab(t, owner.id)
	labtest.Public(t, e.db, lab, "view")
	folder := e.folder(t, "Общая")

	// Without a folder the lab is visible to everyone allowed to view it.
	if !contains(e.labIDs(t, viewer), lab) {
		t.Fatal("лаборатория без папки должна быть видна")
	}
	// In a folder with no assignments it is neither visible nor accessible by direct id.
	e.placeLab(t, lab, folder)
	if contains(e.labIDs(t, viewer), lab) {
		t.Fatal("лаборатория из неназначенной папки попала в список")
	}
	if rec := e.do(t, viewer, http.MethodGet, "/api/labs/"+itoa(lab), nil); rec.Code != http.StatusNotFound {
		t.Errorf("прямой запрос скрытой лаборатории: ожидался 404, получено %d", rec.Code)
	}
	if rec := e.do(t, viewer, http.MethodGet, "/api/labs/"+itoa(lab)+"/topology", nil); rec.Code != http.StatusNotFound {
		t.Errorf("топология скрытой лаборатории: ожидался 404, получено %d", rec.Code)
	}
	// The administrator always sees the lab.
	if !contains(e.labIDs(t, admin), lab) {
		t.Error("администратор не видит лабораторию в папке")
	}
	// Assigning the folder makes the lab visible.
	e.assignFolder(t, folder, viewer.id)
	if !contains(e.labIDs(t, viewer), lab) {
		t.Fatal("после назначения папки лаборатория не появилась в списке")
	}
	if rec := e.do(t, viewer, http.MethodGet, "/api/labs/"+itoa(lab), nil); rec.Code != http.StatusOK {
		t.Errorf("после назначения прямой запрос: ожидался 200, получено %d", rec.Code)
	}
	// Revoking access hides the lab again.
	// The user's only assigned folder: remove all their assignments.
	if err := e.db.Folders.SetUserFolders(viewer.id, nil, 0); err != nil {
		t.Fatal(err)
	}
	if contains(e.labIDs(t, viewer), lab) {
		t.Fatal("после отзыва доступа лаборатория осталась в списке")
	}
	if rec := e.do(t, viewer, http.MethodGet, "/api/labs/"+itoa(lab), nil); rec.Code != http.StatusNotFound {
		t.Errorf("после отзыва доступа: ожидался 404, получено %d", rec.Code)
	}
}

func TestFolderFilterUsesSharedPlacement(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "root", "admin")
	owner := e.user(t, "owner", "user")
	lab := e.lab(t, owner.id)
	folder := e.folder(t, "Курс")
	e.placeLab(t, lab, folder)

	rec := e.do(t, admin, http.MethodGet, "/api/labs?folder="+itoa(folder), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("список по папке: %d %s", rec.Code, rec.Body)
	}
	var list []Lab
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].ID != lab || list[0].FolderID != folder {
		t.Fatalf("неверная выборка по папке: %+v", list)
	}
	// "No folder": request folder=0.
	if err := e.db.Folders.RemoveLab(lab); err != nil {
		t.Fatal(err)
	}
	rec = e.do(t, admin, http.MethodGet, "/api/labs?folder=0", nil)
	list = nil
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].FolderID != 0 {
		t.Fatalf("выборка «Без папки»: %+v", list)
	}
}

func TestFolderGateOverridesOwnershipAndPublicAccess(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "root", "admin")
	owner := e.user(t, "owner", "user")
	lab := e.lab(t, owner.id)
	labtest.Public(t, e.db, lab, "view")
	folder := e.folder(t, "Закрытая")
	e.placeLab(t, lab, folder)

	// The owner and "shared access" do not bypass the folder restriction: only the administrator assigns it.
	if rec := e.do(t, owner, http.MethodGet, "/api/labs/"+itoa(lab), nil); rec.Code != http.StatusNotFound {
		t.Errorf("владелец обошёл ограничение папки: %d", rec.Code)
	}
	// The restriction does not affect the administrator.
	if rec := e.do(t, admin, http.MethodGet, "/api/labs/"+itoa(lab), nil); rec.Code != http.StatusOK {
		t.Errorf("администратор не открыл лабораторию: %d", rec.Code)
	}
}

func TestDeletedFolderLeavesLabVisible(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "root", "admin")
	owner := e.user(t, "owner", "user")
	lab := e.lab(t, owner.id)
	folder := e.folder(t, "Удаляемая")
	e.placeLab(t, lab, folder)

	if _, _, err := e.db.Folders.Remove(folder, 0); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.db.Labs.Lab(lab); !ok {
		t.Fatal("лаборатория пропала вместе с папкой")
	}
	if _, ok := e.db.Folders.Placement(lab); ok {
		t.Fatal("раскладка не удалена вместе с папкой")
	}
	if !contains(e.labIDs(t, admin), lab) {
		t.Error("после удаления папки лаборатория не видна администратору")
	}
	if _, ok := e.db.Folders.Placement(lab); ok {
		t.Error("лаборатория должна быть в «Без папки»")
	}
}

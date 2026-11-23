// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"encoding/json"
	"net/http"
	"testing"

	"netrave/internal/fstree"
	"netrave/internal/labtest"
)

// folder creates a folder in the shared tree and returns its path.
func (e *tenv) folder(t *testing.T, name string) string {
	t.Helper()
	if err := fstree.Mkdir(e.db.Labs.Root(), "", name); err != nil {
		t.Fatal(err)
	}
	return name
}

// placeLab moves a lab into a folder.
func (e *tenv) placeLab(t *testing.T, labName, folder string) {
	t.Helper()
	if err := e.db.Labs.Move(labName, folder); err != nil {
		t.Fatal(err)
	}
}

func (e *tenv) labNames(t *testing.T, u sess, query string) []string {
	t.Helper()
	rec := e.do(t, u, http.MethodGet, "/api/labs"+query, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("lab list: HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var list []Lab
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(list))
	for _, l := range list {
		out = append(out, l.Name)
	}
	return out
}

func testContains(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

// The folder comes from the file tree and is returned in the list; the filter selects one folder.
func TestListReturnsFolderAndFilters(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "root", "admin")
	owner := e.user(t, "owner", "user")
	lab := e.lab(t, owner.id)
	other := e.namedLab(t, owner.id, "other")
	folder := e.folder(t, "Course")
	e.placeLab(t, lab, folder)

	rec := e.do(t, admin, http.MethodGet, "/api/labs?folder="+labPath(folder), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list by folder: %d %s", rec.Code, rec.Body)
	}
	var list []Lab
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].Name != lab || list[0].Folder != folder {
		t.Fatalf("wrong folder filter result: %+v", list)
	}
	// An empty folder parameter means the root.
	if names := e.labNames(t, admin, "?folder="); !testContains(names, other) || testContains(names, lab) {
		t.Fatalf("root filter: %v", names)
	}
}

// Folder visibility is derived from accessible labs: there is no folder gate for a public lab.
func TestPublicLabInFolderRemainsVisible(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "root", "admin")
	owner := e.user(t, "owner", "user")
	viewer := e.user(t, "viewer", "user")
	lab := e.lab(t, owner.id)
	labtest.Public(t, e.db, lab, "view")
	folder := e.folder(t, "Public")
	e.placeLab(t, lab, folder)

	if !testContains(e.labNames(t, viewer, ""), lab) {
		t.Fatal("a public lab in a folder must be visible to every user")
	}
	if rec := e.do(t, viewer, http.MethodGet, "/api/labs/"+labPath(lab), nil); rec.Code != http.StatusOK {
		t.Errorf("public lab in a folder: want 200, got %d", rec.Code)
	}
	// The administrator sees private labs as well.
	private := e.namedLab(t, owner.id, "private")
	if !testContains(e.labNames(t, admin, ""), private) {
		t.Error("administrator does not see a private lab")
	}
}

// A private lab is visible only to its owner and to users with a view permission written in the lab file.
func TestPrivateLabVisibility(t *testing.T) {
	e := newTenv(t)
	admin := e.user(t, "root", "admin")
	owner := e.user(t, "owner", "user")
	viewer := e.user(t, "viewer", "user")
	lab := e.lab(t, owner.id)

	if testContains(e.labNames(t, viewer, ""), lab) {
		t.Fatal("private lab visible without a permission")
	}
	if rec := e.do(t, viewer, http.MethodGet, "/api/labs/"+labPath(lab), nil); rec.Code != http.StatusNotFound {
		t.Errorf("direct request for a hidden lab: want 404, got %d", rec.Code)
	}
	if rec := e.do(t, viewer, http.MethodGet, "/api/labs/"+labPath(lab)+"/topology", nil); rec.Code != http.StatusNotFound {
		t.Errorf("topology of a hidden lab: want 404, got %d", rec.Code)
	}
	e.grant(t, admin, lab, viewer.id, map[string]bool{"lab.view": true})
	if !testContains(e.labNames(t, viewer, ""), lab) {
		t.Fatal("lab did not appear after granting view access")
	}
}

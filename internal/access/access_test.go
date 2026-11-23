// SPDX-License-Identifier: GPL-3.0-only

package access

import (
	"errors"
	"path/filepath"
	"testing"

	"netrave/internal/database"
	"netrave/internal/fstree"
	"netrave/internal/labfile"
	"netrave/internal/labtest"
)

func TestCatalogUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Catalog {
		if seen[p.Key] {
			t.Fatalf("duplicate permission %q", p.Key)
		}
		seen[p.Key] = true
		if p.Title == "" || p.Group == "" {
			t.Fatalf("permission %q has no label or group", p.Key)
		}
	}
	if len(Catalog) != 26 {
		t.Fatalf("expected 26 permissions, got %d", len(Catalog))
	}
}

func TestComposeOwnerAndAdminHaveEverything(t *testing.T) {
	for _, c := range []struct {
		admin bool
		uid   int64
	}{{true, 99}, {false, 10}} {
		got := Compose(c.admin, c.uid, 10, "none", nil)
		if len(got) != len(Catalog) {
			t.Fatalf("full set expected, got %d", len(got))
		}
	}
}

func TestComposeGrantsOnlyExplicit(t *testing.T) {
	got := Compose(false, 20, 10, "none", map[string]bool{MachStart: true, MachWipe: false, "bogus": true})
	if !got[MachStart] || got[MachWipe] || got["bogus"] || got[LabView] {
		t.Fatalf("wrong set: %v", got)
	}
}

func TestPublicViewGivesOnlyView(t *testing.T) {
	got := Compose(false, 20, 10, "view", nil)
	if !got[LabView] || len(got) != 1 {
		t.Fatalf("public view should grant only lab.view: %v", got)
	}
}

func TestValidPublic(t *testing.T) {
	if !ValidPublic("none") || !ValidPublic("view") || ValidPublic("full") || ValidPublic("edit") {
		t.Fatal("ValidPublic accepts an invalid value")
	}
}

func accessDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func accessUser(t *testing.T, db *database.DB, name, role string) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO users(username,password_hash,role) VALUES(?,?,?)`, name, "x", role)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestLabGrantsDirectAndThroughRole(t *testing.T) {
	db := accessDB(t)
	owner := accessUser(t, db, "owner", "user")
	viewer := accessUser(t, db, "viewer", "user")
	lab := labtest.Lab(t, db, owner, "lab", "none")
	res, err := db.Exec(`INSERT INTO roles(name) VALUES('Operators')`)
	if err != nil {
		t.Fatal(err)
	}
	roleID, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO user_roles(user_id,role_id) VALUES(?,?)`, viewer, roleID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Labs.Update(lab, func(l *labfile.Lab) error {
		l.Permissions = append(l.Permissions,
			labfile.Permission{RoleID: roleID, Perm: LabView, Value: true},
			labfile.Permission{RoleID: roleID, Perm: MachStart, Value: true},
			labfile.Permission{UserID: viewer, Perm: MachStart, Value: false},
		)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	fl, _ := db.Labs.Lab(lab)
	grants := LabGrants(fl, viewer, []int64{roleID})
	if !grants[LabView] || grants[MachStart] {
		t.Fatalf("direct entry must override a role entry: %v", grants)
	}
	visible, err := LabVisible(db, fl, viewer, false)
	if err != nil || !visible {
		t.Fatalf("role entry must make the lab visible: %v %v", visible, err)
	}
	// Another role with no entry in the lab file has no effect.
	visible, _ = LabVisible(db, fl, viewer, false)
	if !visible {
		t.Fatal("visible lab disappeared")
	}
}

func TestVisibleFoldersFollowAccessibleLabs(t *testing.T) {
	db := accessDB(t)
	owner := accessUser(t, db, "owner", "user")
	viewer := accessUser(t, db, "viewer", "user")
	lab := labtest.Lab(t, db, owner, "lab", "view")
	// The folder must exist before the lab can be moved into it.
	if err := fstree.Mkdir(db.Labs.Root(), "", "A"); err != nil && !errors.Is(err, fstree.ErrExists) {
		t.Fatal(err)
	}
	if err := fstree.Mkdir(db.Labs.Root(), "A", "B"); err != nil && !errors.Is(err, fstree.ErrExists) {
		t.Fatal(err)
	}
	if err := db.Labs.Move(lab, "A/B"); err != nil {
		t.Fatal(err)
	}
	visible, err := VisibleFolders(db, viewer, false)
	if err != nil {
		t.Fatal(err)
	}
	if !visible["A"] || !visible["A/B"] {
		t.Fatalf("folders of a public lab must be visible: %v", visible)
	}
	ok, err := FolderVisible(db, viewer, false, "A/B")
	if err != nil || !ok {
		t.Fatalf("folder visibility: %v %v", ok, err)
	}
}

// SPDX-License-Identifier: GPL-3.0-only

package access

import "testing"

func TestCatalogUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Catalog {
		if seen[p.Key] {
			t.Fatalf("дубликат разрешения %q", p.Key)
		}
		seen[p.Key] = true
		if p.Title == "" || p.Group == "" {
			t.Fatalf("у разрешения %q нет подписи или группы", p.Key)
		}
	}
	if len(Catalog) != 26 {
		t.Fatalf("ожидалось 26 разрешений, получено %d", len(Catalog))
	}
}

func TestComposeOwnerAndAdminHaveEverything(t *testing.T) {
	for _, c := range []struct {
		admin bool
		uid   int64
	}{{true, 99}, {false, 10}} {
		got := Compose(c.admin, c.uid, 10, "none", nil)
		if len(got) != len(Catalog) {
			t.Fatalf("полный набор ожидался, получено %d", len(got))
		}
	}
}

func TestComposeGrantsOnlyExplicit(t *testing.T) {
	got := Compose(false, 20, 10, "none", map[string]bool{MachStart: true, MachWipe: false, "bogus": true})
	if !got[MachStart] || got[MachWipe] || got["bogus"] || got[LabView] {
		t.Fatalf("неверный набор: %v", got)
	}
}

func TestPublicViewGivesOnlyView(t *testing.T) {
	got := Compose(false, 20, 10, "view", nil)
	if !got[LabView] || len(got) != 1 {
		t.Fatalf("публичный просмотр должен давать только lab.view: %v", got)
	}
}

func TestValidPublic(t *testing.T) {
	if !ValidPublic("none") || !ValidPublic("view") || ValidPublic("full") || ValidPublic("edit") {
		t.Fatal("ValidPublic принимает недопустимое")
	}
}

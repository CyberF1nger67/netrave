// SPDX-License-Identifier: GPL-3.0-only

package labfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"netrave/internal/fstree"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "labs")
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func sample() Lab {
	return Lab{
		OwnerID:     1,
		Name:        "Тест",
		PublicLevel: "none",
		Devices:     []Device{{Name: "r1", IfaceType: "ens"}, {Name: "pc1", IfaceType: "ens"}},
		Networks:    []Network{{Name: "lan", Type: "isolated"}},
	}
}

func TestSaveWritesFileAndSurvivesReopen(t *testing.T) {
	s, dir := openTemp(t)
	saved, err := s.Save(sample())
	if err != nil {
		t.Fatal(err)
	}
	if saved.Name != "Тест" {
		t.Fatalf("name changed: %q", saved.Name)
	}
	if _, err := os.Stat(filepath.Join(dir, "Тест.yaml")); err != nil {
		t.Fatalf("lab file not written: %v", err)
	}

	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := again.Lab("Тест")
	if !ok || got.Name != "Тест" || len(got.Devices) != 2 || len(got.Networks) != 1 {
		t.Fatalf("after re-reading: %+v %v", got, ok)
	}
}

func TestLabNamesAreUniqueCaseInsensitively(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.Save(sample()); err != nil {
		t.Fatal(err)
	}
	l := sample()
	l.Name = "тест"
	if _, err := s.Save(l); !errors.Is(err, fstree.ErrExists) {
		t.Fatalf("expected ErrExists, got %v", err)
	}
}

func TestConnectionNeedsExistingEnds(t *testing.T) {
	s, _ := openTemp(t)
	l, _ := s.Save(sample())
	l.Connections = []Connection{{A: End{Type: "device", Name: "r1", If: 0}, B: End{Type: "network", Name: "missing"}}}
	if _, err := s.Save(l); err == nil {
		t.Fatal("cable to a nonexistent network must be rejected")
	}
}

func TestPortCanHoldOneCable(t *testing.T) {
	s, _ := openTemp(t)
	l, _ := s.Save(sample())
	l.Connections = []Connection{
		{A: End{Type: "device", Name: "r1", If: 0}, B: End{Type: "network", Name: "lan"}},
		{A: End{Type: "device", Name: "r1", If: 0}, B: End{Type: "network", Name: "lan"}},
	}
	if _, err := s.Save(l); err == nil {
		t.Fatal("one port must not have two cables")
	}
}

func TestDeletingDeviceRemovesItsConnections(t *testing.T) {
	s, _ := openTemp(t)
	l, _ := s.Save(sample())
	l.Connections = []Connection{
		{A: End{Type: "device", Name: "r1", If: 0}, B: End{Type: "network", Name: "lan"}},
		{A: End{Type: "device", Name: "pc1", If: 0}, B: End{Type: "network", Name: "lan"}},
	}
	l, err := s.Save(l)
	if err != nil {
		t.Fatal(err)
	}
	// Delete the first device: its cable disappears, the second one remains.
	l.Devices = l.Devices[1:]
	saved, err := s.Save(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Connections) != 1 || saved.Connections[0].A.Name != "pc1" {
		t.Fatalf("cascade did not run: %+v", saved.Connections)
	}
}

func TestDeletingNetworkRemovesItsConnections(t *testing.T) {
	s, _ := openTemp(t)
	l, _ := s.Save(sample())
	l.Connections = []Connection{{A: End{Type: "device", Name: "r1", If: 1}, B: End{Type: "network", Name: "lan"}}}
	l, _ = s.Save(l)
	l.Networks = nil
	saved, err := s.Save(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Connections) != 0 {
		t.Fatalf("network cable remains: %+v", saved.Connections)
	}
}

func TestConnectionKeyIgnoresEndOrder(t *testing.T) {
	a := Connection{A: End{Type: "device", Name: "pc1", If: 2}, B: End{Type: "network", Name: "lan"}}
	b := Connection{A: End{Type: "network", Name: "lan"}, B: End{Type: "device", Name: "pc1", If: 2}}
	if a.Key() != b.Key() {
		t.Fatalf("keys differ: %q and %q", a.Key(), b.Key())
	}
	if a.Key() == (Connection{A: End{Type: "device", Name: "pc1", If: 1}, B: End{Type: "network", Name: "lan"}}).Key() {
		t.Fatal("different ports share a key")
	}
}

func TestReturnedLabIsACopy(t *testing.T) {
	s, _ := openTemp(t)
	saved, _ := s.Save(sample())
	saved.Devices[0].Name = "changed"
	got, _ := s.Lab(saved.Name)
	if got.Devices[0].Name != "r1" {
		t.Fatal("changing copy affected storage")
	}
}

func TestDeleteRemovesFile(t *testing.T) {
	s, dir := openTemp(t)
	saved, _ := s.Save(sample())
	if err := s.Delete(saved.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Тест.yaml")); !os.IsNotExist(err) {
		t.Fatalf("lab file remains: %v", err)
	}
	if _, ok := s.Lab(saved.Name); ok {
		t.Fatal("lab remains in memory")
	}
}

func TestUpdateAppliesChangeAtomically(t *testing.T) {
	s, _ := openTemp(t)
	saved, _ := s.Save(sample())
	got, err := s.Update(saved.Name, func(l *Lab) error {
		l.Description = "changed"
		return nil
	})
	if err != nil || got.Description != "changed" {
		t.Fatalf("update: %+v %v", got, err)
	}
	// An error in fn does not change the lab.
	if _, err := s.Update(saved.Name, func(l *Lab) error {
		l.Description = "cancel"
		return os.ErrInvalid
	}); err == nil {
		t.Fatal("fn error must be returned")
	}
	cur, _ := s.Lab(saved.Name)
	if cur.Description != "changed" {
		t.Fatalf("error did not roll back change: %q", cur.Description)
	}
	if _, err := s.Update("missing", func(*Lab) error { return nil }); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMoveAndRename(t *testing.T) {
	s, dir := openTemp(t)
	saved, _ := s.Save(sample())
	if err := os.Mkdir(filepath.Join(dir, "F"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := s.Move(saved.Name, "F"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "F", "Тест.yaml")); err != nil {
		t.Fatalf("lab not moved: %v", err)
	}
	if folder, ok := s.Folder(saved.Name); !ok || folder != "F" {
		t.Fatalf("folder not updated: %q %v", folder, ok)
	}
	renamed, err := s.Rename(saved.Name, "Новое")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "Новое" {
		t.Fatalf("name not changed: %q", renamed.Name)
	}
	if _, err := os.Stat(filepath.Join(dir, "F", "Новое.yaml")); err != nil {
		t.Fatalf("renamed lab not in its folder: %v", err)
	}
	if _, ok := s.Lab(saved.Name); ok {
		t.Fatal("old name still resolves")
	}
}

func TestRenameRejectsTakenName(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.Save(sample()); err != nil {
		t.Fatal(err)
	}
	other := sample()
	other.Name = "other"
	if _, err := s.Save(other); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename("other", "тест"); !errors.Is(err, fstree.ErrExists) {
		t.Fatalf("expected ErrExists, got %v", err)
	}
}

func TestSameObjectNamesInDifferentLabsDoNotCollide(t *testing.T) {
	s, _ := openTemp(t)
	a := sample()
	a.Name = "first"
	if _, err := s.Save(a); err != nil {
		t.Fatal(err)
	}
	b := sample()
	b.Name = "second"
	if _, err := s.Save(b); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.LabOf(KindDevice, "first", "r1"); !ok {
		t.Fatal("device lookup failed after the same name was used in another lab")
	}
	if _, ok := s.LabOf(KindDevice, "second", "pc1"); !ok {
		t.Fatal("device lookup failed for the second lab")
	}
}

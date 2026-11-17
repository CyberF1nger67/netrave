// SPDX-License-Identifier: GPL-3.0-only

package labfile

import (
	"os"
	"path/filepath"
	"testing"
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

func TestSaveAssignsIDsAndSurvivesReopen(t *testing.T) {
	s, dir := openTemp(t)
	saved, err := s.Save(sample())
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == 0 || saved.Devices[0].ID == 0 || saved.Networks[0].ID == 0 {
		t.Fatalf("номера не выданы: %+v", saved)
	}
	if saved.Devices[0].ID == saved.Devices[1].ID || saved.Devices[0].ID == saved.Networks[0].ID {
		t.Fatal("номера объектов совпали")
	}
	if _, err := os.Stat(filepath.Join(dir, "1", "lab.yaml")); err != nil {
		t.Fatalf("файл лаборатории не записан: %v", err)
	}

	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := again.Lab(saved.ID)
	if !ok || got.Name != "Тест" || len(got.Devices) != 2 || len(got.Networks) != 1 {
		t.Fatalf("после перечитывания: %+v %v", got, ok)
	}
}

func TestIDsAreNeverReused(t *testing.T) {
	s, dir := openTemp(t)
	first, _ := s.Save(sample())
	if err := s.Delete(first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := s.Save(sample())
	if err != nil {
		t.Fatal(err)
	}
	if second.ID <= first.ID {
		t.Fatalf("номер переиспользован: %d после %d", second.ID, first.ID)
	}
	// The counter survives a restart.
	again, _ := Open(dir)
	third, _ := again.Save(sample())
	if third.ID <= second.ID {
		t.Fatalf("после перезапуска номер переиспользован: %d", third.ID)
	}
}

func TestConnectionNeedsExistingEnds(t *testing.T) {
	s, _ := openTemp(t)
	l, _ := s.Save(sample())
	l.Connections = []Connection{{A: End{Type: "device", ID: l.Devices[0].ID, If: 0}, B: End{Type: "network", ID: 9999}}}
	if _, err := s.Save(l); err == nil {
		t.Fatal("провод к несуществующей сети должен отклоняться")
	}
}

func TestPortCanHoldOneCable(t *testing.T) {
	s, _ := openTemp(t)
	l, _ := s.Save(sample())
	r1, net := l.Devices[0].ID, l.Networks[0].ID
	l.Connections = []Connection{
		{A: End{Type: "device", ID: r1, If: 0}, B: End{Type: "network", ID: net}},
		{A: End{Type: "device", ID: r1, If: 0}, B: End{Type: "network", ID: net}},
	}
	if _, err := s.Save(l); err == nil {
		t.Fatal("один порт не должен иметь два провода")
	}
}

func TestDeletingDeviceRemovesItsConnections(t *testing.T) {
	s, _ := openTemp(t)
	l, _ := s.Save(sample())
	r1, pc, net := l.Devices[0].ID, l.Devices[1].ID, l.Networks[0].ID
	l.Connections = []Connection{
		{A: End{Type: "device", ID: r1, If: 0}, B: End{Type: "network", ID: net}},
		{A: End{Type: "device", ID: pc, If: 0}, B: End{Type: "network", ID: net}},
	}
	l, err := s.Save(l)
	if err != nil {
		t.Fatal(err)
	}
	// Delete the first device: its wire disappears, the second one remains.
	l.Devices = l.Devices[1:]
	saved, err := s.Save(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Connections) != 1 || saved.Connections[0].A.ID != pc {
		t.Fatalf("каскад не сработал: %+v", saved.Connections)
	}
}

func TestDeletingNetworkRemovesItsConnections(t *testing.T) {
	s, _ := openTemp(t)
	l, _ := s.Save(sample())
	r1, net := l.Devices[0].ID, l.Networks[0].ID
	l.Connections = []Connection{{A: End{Type: "device", ID: r1, If: 1}, B: End{Type: "network", ID: net}}}
	l, _ = s.Save(l)
	l.Networks = nil
	saved, err := s.Save(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Connections) != 0 {
		t.Fatalf("провод сети остался: %+v", saved.Connections)
	}
}

func TestSaveUnknownLabIsNotFound(t *testing.T) {
	s, _ := openTemp(t)
	l := sample()
	l.ID = 42
	if _, err := s.Save(l); err != ErrNotFound {
		t.Fatalf("ожидалась ErrNotFound, получено %v", err)
	}
}

func TestReturnedLabIsACopy(t *testing.T) {
	s, _ := openTemp(t)
	saved, _ := s.Save(sample())
	saved.Devices[0].Name = "changed"
	got, _ := s.Lab(saved.ID)
	if got.Devices[0].Name != "r1" {
		t.Fatal("изменение копии затронуло хранилище")
	}
}

func TestDeleteRemovesFile(t *testing.T) {
	s, dir := openTemp(t)
	saved, _ := s.Save(sample())
	if err := s.Delete(saved.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "1")); !os.IsNotExist(err) {
		t.Fatalf("каталог лаборатории остался: %v", err)
	}
	if _, ok := s.Lab(saved.ID); ok {
		t.Fatal("лаборатория осталась в памяти")
	}
}

func TestUpdateAppliesChangeAtomically(t *testing.T) {
	s, _ := openTemp(t)
	saved, _ := s.Save(sample())
	got, err := s.Update(saved.ID, func(l *Lab) error {
		l.Name = "Новое"
		return nil
	})
	if err != nil || got.Name != "Новое" {
		t.Fatalf("обновление: %+v %v", got, err)
	}
	// An error in fn does not change the lab.
	if _, err := s.Update(saved.ID, func(l *Lab) error {
		l.Name = "Отмена"
		return os.ErrInvalid
	}); err == nil {
		t.Fatal("ошибка fn должна вернуться")
	}
	cur, _ := s.Lab(saved.ID)
	if cur.Name != "Новое" {
		t.Fatalf("ошибка не откатила изменение: %q", cur.Name)
	}
	if _, err := s.Update(999, func(*Lab) error { return nil }); err != ErrNotFound {
		t.Fatalf("ожидалась ErrNotFound, получено %v", err)
	}
}

func TestReservedIDsCanBeUsedBeforeSave(t *testing.T) {
	s, _ := openTemp(t)
	devID := s.NewID()
	l := sample()
	l.Devices[0].ID = devID
	l.Connections = []Connection{{A: End{Type: "device", ID: devID, If: 0}, B: End{Type: "network", ID: l.Networks[0].ID}}}
	// A network without a number will get one on save: the wire needs a real network number, so save the network explicitly.
	l.Networks[0].ID = s.NewID()
	l.Connections[0].B.ID = l.Networks[0].ID
	saved, err := s.Save(l)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Devices[0].ID != devID {
		t.Fatalf("зарезервированный номер изменился: %d", saved.Devices[0].ID)
	}
	if len(saved.Connections) != 1 || saved.Connections[0].A.ID != devID {
		t.Fatalf("провод не сохранён: %+v", saved.Connections)
	}
}

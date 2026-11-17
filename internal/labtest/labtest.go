// SPDX-License-Identifier: GPL-3.0-only

// Package labtest provides test helpers: they create labs, machines, networks, cables and lab permissions
// directly in topology files (through the labfile store), the way the application does.
package labtest

import (
	"fmt"
	"testing"

	"netrave/internal/database"
	"netrave/internal/labfile"
)

// Lab creates a lab owned by owner with public level public (empty - "none") and returns its id.
func Lab(t testing.TB, db *database.DB, owner int64, name, public string) int64 {
	t.Helper()
	if public == "" {
		public = "none"
	}
	saved, err := db.Labs.Save(labfile.Lab{OwnerID: owner, Name: name, PublicLevel: public, CanvasJSON: "{}"})
	if err != nil {
		t.Fatalf("создание лаборатории: %v", err)
	}
	return saved.ID
}

// Device adds a machine to the lab and returns its id. Empty settings are filled with default
// values, as when creating a machine in the application (otherwise the CHECK constraints in the
// instance table fail).
func Device(t testing.TB, db *database.DB, lab int64, d labfile.Device) int64 {
	t.Helper()
	defaults := []struct {
		empty *string
		value string
	}{
		{&d.Arch, "x86_64"}, {&d.Machine, "pc"}, {&d.NICModel, "virtio-net-pci"}, {&d.DiskBus, "virtio"},
		{&d.BIOS, "seabios"}, {&d.Console, "serial"}, {&d.IfaceType, "ens"},
	}
	for _, f := range defaults {
		if *f.empty == "" {
			*f.empty = f.value
		}
	}
	if d.CPU == 0 {
		d.CPU = 1
	}
	if d.RAMMB == 0 {
		d.RAMMB = 1024
	}
	if d.NICs == 0 {
		d.NICs = 1
	}
	got, err := db.Labs.Update(lab, func(l *labfile.Lab) error {
		l.Devices = append(l.Devices, d)
		return nil
	})
	if err != nil {
		t.Fatalf("добавление машины: %v", err)
	}
	return got.Devices[len(got.Devices)-1].ID
}

// Network adds a network to the lab and returns its id.
func Network(t testing.TB, db *database.DB, lab int64, n labfile.Network) int64 {
	t.Helper()
	got, err := db.Labs.Update(lab, func(l *labfile.Lab) error {
		l.Networks = append(l.Networks, n)
		return nil
	})
	if err != nil {
		t.Fatalf("добавление сети: %v", err)
	}
	return got.Networks[len(got.Networks)-1].ID
}

// Connection adds a cable to the lab and returns its id. The ends are machines and networks of this lab.
func Connection(t testing.TB, db *database.DB, lab int64, c labfile.Connection) int64 {
	t.Helper()
	got, err := db.Labs.Update(lab, func(l *labfile.Lab) error {
		l.Connections = append(l.Connections, c)
		return nil
	})
	if err != nil {
		t.Fatalf("добавление провода: %v", err)
	}
	return got.Connections[len(got.Connections)-1].ID
}

// Perm sets a user's permission on the lab (value=false - explicit deny).
func Perm(t testing.TB, db *database.DB, lab, uid int64, perm string, value bool) {
	t.Helper()
	if _, err := db.Labs.Update(lab, func(l *labfile.Lab) error {
		for i := range l.Permissions {
			if l.Permissions[i].UserID == uid && l.Permissions[i].Perm == perm {
				l.Permissions[i].Value = value
				return nil
			}
		}
		l.Permissions = append(l.Permissions, labfile.Permission{UserID: uid, Perm: perm, Value: value})
		return nil
	}); err != nil {
		t.Fatalf("право лаборатории: %v", err)
	}
}

// Public changes the lab's public level.
func Public(t testing.TB, db *database.DB, lab int64, level string) {
	t.Helper()
	if _, err := db.Labs.Update(lab, func(l *labfile.Lab) error {
		l.PublicLevel = level
		return nil
	}); err != nil {
		t.Fatalf("публичный уровень: %v", err)
	}
}

// Set modifies a lab machine with fn (for example, nics or icon).
func Set(t testing.TB, db *database.DB, lab int64, devID int64, fn func(*labfile.Device)) {
	t.Helper()
	if _, err := db.Labs.Update(lab, func(l *labfile.Lab) error {
		for i := range l.Devices {
			if l.Devices[i].ID == devID {
				fn(&l.Devices[i])
				return nil
			}
		}
		return fmt.Errorf("машина %d не найдена", devID)
	}); err != nil {
		t.Fatalf("изменение машины: %v", err)
	}
}

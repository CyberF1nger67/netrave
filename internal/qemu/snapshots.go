// SPDX-License-Identifier: GPL-3.0-only

package qemu

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"netrave/internal/catalog"
)

var snapRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,39}$`)

// Snapshot is a machine snapshot; stored in the catalog (YAML), here it is only an alias.
type Snapshot = catalog.Snapshot

func (m *Manager) ListSnapshots(devID int64) ([]Snapshot, error) {
	out := []Snapshot{}
	out = append(out, m.db.Catalog.SnapshotsOf(devID)...)
	return out, nil
}

// GetSnapshot returns a snapshot by id.
func (m *Manager) GetSnapshot(id int64) (*Snapshot, error) {
	s, ok := m.db.Catalog.Snapshot(id)
	if !ok {
		return nil, errors.New("снимок не найден")
	}
	return &s, nil
}

func hmpErr(out string) error {
	out = strings.TrimSpace(out)
	if out != "" {
		return errors.New(out)
	}
	return nil
}

// offlineDisks returns the working disks of a stopped device; an error if the disks are not created yet.
func (m *Manager) offlineDisks(devID int64) ([]string, error) {
	s, _, err := m.loadSpec(devID)
	if err != nil {
		return nil, err
	}
	files := overlayFiles(m.paths(s).Dir)
	if len(files) == 0 {
		return nil, errors.New("диски устройства ещё не созданы — запустите устройство хотя бы один раз")
	}
	return files, nil
}

// CreateSnapshot: for a running VM - savevm (disks + memory), for a stopped one - a snapshot of each working disk.
func (m *Manager) CreateSnapshot(devID int64, name string) error {
	if !snapRe.MatchString(name) {
		return errors.New("имя снимка: 1–40 символов (буквы, цифры, _ . -)")
	}
	l, ok := m.lock(devID)
	if !ok {
		return ErrBusy
	}
	defer l.Unlock()
	_, state, err := m.loadSpec(devID)
	if err != nil {
		return err
	}
	for _, sn := range m.db.Catalog.SnapshotsOf(devID) {
		if sn.Name == name {
			return errors.New("снимок с таким именем уже существует")
		}
	}
	switch {
	case (state == "running" || state == "paused") && m.get(devID) != nil:
		out, err := qmpHMP(m.get(devID).qmp, "savevm "+name, 10*time.Minute)
		if err != nil {
			return err
		}
		if err := hmpErr(out); err != nil {
			return err
		}
	case state == "stopped" || state == "error":
		files, err := m.offlineDisks(devID)
		if err != nil {
			return err
		}
		for _, f := range files {
			if _, err := m.qemuImg("snapshot", "-c", name, f); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("снимок недоступен в состоянии %s", state)
	}
	_, err = m.db.Catalog.AddSnapshot(devID, name, time.Now().Unix())
	return err
}

// RestoreSnapshot returns the disks (and state, if saved) to the snapshot.
func (m *Manager) RestoreSnapshot(snapID int64) error {
	sn, err := m.GetSnapshot(snapID)
	if err != nil {
		return err
	}
	l, ok := m.lock(sn.DeviceID)
	if !ok {
		return ErrBusy
	}
	defer l.Unlock()
	_, state, err := m.loadSpec(sn.DeviceID)
	if err != nil {
		return err
	}
	switch {
	case (state == "running" || state == "paused") && m.get(sn.DeviceID) != nil:
		out, err := qmpHMP(m.get(sn.DeviceID).qmp, "loadvm "+sn.Name, 10*time.Minute)
		if err != nil {
			return err
		}
		return hmpErr(out)
	case state == "stopped" || state == "error":
		files, err := m.offlineDisks(sn.DeviceID)
		if err != nil {
			return err
		}
		for _, f := range files {
			if _, err := m.qemuImg("snapshot", "-a", sn.Name, f); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("восстановление недоступно в состоянии %s", state)
}

func (m *Manager) DeleteSnapshot(snapID int64) error {
	sn, err := m.GetSnapshot(snapID)
	if err != nil {
		return err
	}
	l, ok := m.lock(sn.DeviceID)
	if !ok {
		return ErrBusy
	}
	defer l.Unlock()
	_, state, err := m.loadSpec(sn.DeviceID)
	if err != nil {
		return err
	}
	switch {
	case (state == "running" || state == "paused") && m.get(sn.DeviceID) != nil:
		out, err := qmpHMP(m.get(sn.DeviceID).qmp, "delvm "+sn.Name, 5*time.Minute)
		if err != nil {
			return err
		}
		if err := hmpErr(out); err != nil {
			return err
		}
	case state == "stopped" || state == "error":
		files, err := m.offlineDisks(sn.DeviceID)
		if err == nil {
			for _, f := range files {
				if _, err := m.qemuImg("snapshot", "-d", sn.Name, f); err != nil {
					return err
				}
			}
		}
	default:
		return fmt.Errorf("удаление снимка недоступно в состоянии %s", state)
	}
	return m.db.Catalog.DeleteSnapshot(snapID)
}

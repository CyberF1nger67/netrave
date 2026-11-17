// SPDX-License-Identifier: GPL-3.0-only

package ksm

import (
	"os"
	"path/filepath"
	"testing"
)

// prepare replaces the KSM and UKSM directories with temporary ones and restores the originals after the test.
func prepare(t *testing.T) (string, string) {
	t.Helper()
	dirStand := filepath.Join(t.TempDir(), "ksm")
	dirUksm := filepath.Join(t.TempDir(), "uksm")
	SetDirs(dirStand, dirUksm)
	return dirStand, dirUksm
}

func write(t *testing.T, dir, name, value string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(value+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Enabling KSM disables UKSM and vice versa: they never run simultaneously.
func TestMutualExclusion(t *testing.T) {
	dirKSM, dirUKSM := prepare(t)
	write(t, dirKSM, "run", "0")
	write(t, dirKSM, "pages_sharing", "64")
	write(t, dirUKSM, "run", "1")
	write(t, dirUKSM, "pages_sharing", "8")

	status := Read()
	if !status.UKSM.Enabled || status.KSM.Enabled {
		t.Fatalf("начальное состояние: %+v", status)
	}

	if err := SetEnabled(KindKSM, true); err != nil {
		t.Fatal(err)
	}
	status = Read()
	if !status.KSM.Enabled || status.UKSM.Enabled {
		t.Fatalf("включение KSM должно выключить UKSM: %+v", status)
	}
	if got := read(t, dirUKSM, "run"); got != "0" {
		t.Fatalf("UKSM не остановлен: run=%q", got)
	}

	if err := SetEnabled(KindUKSM, true); err != nil {
		t.Fatal(err)
	}
	status = Read()
	if !status.UKSM.Enabled || status.KSM.Enabled {
		t.Fatalf("включение UKSM должно выключить KSM: %+v", status)
	}
	if got := read(t, dirKSM, "run"); got != "0" {
		t.Fatalf("KSM не остановлен: run=%q", got)
	}
}

// Both mechanisms can be disabled.
func TestBothDisabled(t *testing.T) {
	dirKSM, dirUKSM := prepare(t)
	write(t, dirKSM, "run", "1")
	write(t, dirUKSM, "run", "1")

	if err := SetEnabled(KindKSM, false); err != nil {
		t.Fatal(err)
	}
	if err := SetEnabled(KindUKSM, false); err != nil {
		t.Fatal(err)
	}
	status := Read()
	if status.KSM.Enabled || status.UKSM.Enabled {
		t.Fatalf("оба механизма должны быть выключены: %+v", status)
	}
}

// Each mechanism has its own saved-memory state.
func TestStatusPerMechanism(t *testing.T) {
	dirKSM, dirUKSM := prepare(t)
	write(t, dirKSM, "run", "1")
	write(t, dirKSM, "pages_sharing", "128")
	write(t, dirUKSM, "run", "0")
	write(t, dirUKSM, "pages_sharing", "16")

	status := Read()
	if status.KSM.Merged != 128 || status.UKSM.Merged != 16 {
		t.Fatalf("показатели перепутаны: %+v", status)
	}
	if !status.KSM.Available || !status.UKSM.Available {
		t.Fatalf("оба механизма должны быть доступны: %+v", status)
	}
}

// A mechanism without parameters in sysfs is unavailable, but this does not prevent controlling the other.
func TestIndependentAvailability(t *testing.T) {
	dirKSM, _ := prepare(t)
	write(t, dirKSM, "run", "0")

	status := Read()
	if !status.KSM.Supported || status.UKSM.Supported {
		t.Fatalf("штамм uksm должен быть недоступен: %+v", status)
	}
	if err := SetEnabled(KindKSM, true); err != nil {
		t.Fatal(err)
	}
	if err := SetEnabled(KindUKSM, true); err != ErrUnavailable {
		t.Fatalf("включение отсутствующего UKSM: ожидалась ErrUnavailable, получено %v", err)
	}
	if !Read().KSM.Enabled {
		t.Fatal("KSM не должен выключиться из-за неудачного включения UKSM")
	}
}

func TestUnknownKind(t *testing.T) {
	prepare(t)
	if err := SetEnabled("other", true); err != ErrUnknownKind {
		t.Fatalf("ожидалась ErrUnknownKind, получено %v", err)
	}
}

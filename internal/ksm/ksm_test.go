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
		t.Fatalf("initial state: %+v", status)
	}

	if err := SYSFS.Set(KindKSM, true); err != nil {
		t.Fatal(err)
	}
	status = Read()
	if !status.KSM.Enabled || status.UKSM.Enabled {
		t.Fatalf("enabling KSM must disable UKSM: %+v", status)
	}
	if got := read(t, dirUKSM, "run"); got != "0" {
		t.Fatalf("UKSM not stopped: run=%q", got)
	}

	if err := SYSFS.Set(KindUKSM, true); err != nil {
		t.Fatal(err)
	}
	status = Read()
	if !status.UKSM.Enabled || status.KSM.Enabled {
		t.Fatalf("enabling UKSM must disable KSM: %+v", status)
	}
	if got := read(t, dirKSM, "run"); got != "0" {
		t.Fatalf("KSM not stopped: run=%q", got)
	}
}

// Both mechanisms can be disabled.
func TestBothDisabled(t *testing.T) {
	dirKSM, dirUKSM := prepare(t)
	write(t, dirKSM, "run", "1")
	write(t, dirUKSM, "run", "1")

	if err := SYSFS.Set(KindKSM, false); err != nil {
		t.Fatal(err)
	}
	if err := SYSFS.Set(KindUKSM, false); err != nil {
		t.Fatal(err)
	}
	status := Read()
	if status.KSM.Enabled || status.UKSM.Enabled {
		t.Fatalf("both mechanisms must be disabled: %+v", status)
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
		t.Fatalf("metrics mixed up: %+v", status)
	}
	if !status.KSM.Available || !status.UKSM.Available {
		t.Fatalf("both mechanisms must be available: %+v", status)
	}
}

// A mechanism without parameters in sysfs is unavailable, but this does not prevent controlling the other.
func TestIndependentAvailability(t *testing.T) {
	dirKSM, _ := prepare(t)
	write(t, dirKSM, "run", "0")

	status := Read()
	if !status.KSM.Supported || status.UKSM.Supported {
		t.Fatalf("uksm strain must be unavailable: %+v", status)
	}
	if err := SYSFS.Set(KindKSM, true); err != nil {
		t.Fatal(err)
	}
	if err := SYSFS.Set(KindUKSM, true); err != ErrUnavailable {
		t.Fatalf("enabling missing UKSM: expected ErrUnavailable, got %v", err)
	}
	if !Read().KSM.Enabled {
		t.Fatal("KSM must not turn off due to failed UKSM enable")
	}
}

func TestUnknownKind(t *testing.T) {
	prepare(t)
	if err := SYSFS.Set("other", true); err != ErrUnknownKind {
		t.Fatalf("expected ErrUnknownKind, got %v", err)
	}
}

// SPDX-License-Identifier: GPL-3.0-only

package qemu

import (
	"os"
	"path/filepath"
	"testing"

	"netrave/internal/config"
)

// The log of a machine is named by owner, laboratory and device; names that cannot be path elements give no path.
func TestLogFileUsesOwnerLabAndDevice(t *testing.T) {
	logs := filepath.Join("/var", "lib", "netrave", "logs")
	if got, want := LogFile(logs, "alice", "perimeter", "fw1"), filepath.Join(logs, "alice", "perimeter", "fw1.log"); got != want {
		t.Fatalf("log file: %q, want %q", got, want)
	}
	for _, bad := range [][3]string{{"", "lab", "dev"}, {"user", "..", "dev"}, {"user", "lab", "a/b"}, {"user", "lab", "."}} {
		if got := LogFile(logs, bad[0], bad[1], bad[2]); got != "" {
			t.Fatalf("unsafe names %v produced a path: %q", bad, got)
		}
	}
}

// Machine files keep the disks under owner, laboratory and machine, and the log under the same names.
func TestDevicePathsSeparateWorkDirAndLog(t *testing.T) {
	s := &Spec{ID: 7, InstanceID: 3, Owner: "alice", LabID: "perimeter", DeviceID: "fw1"}
	p := DevicePaths("/data/instances", "/data/logs", "/run/netrave", s)
	if p.Dir != filepath.Join("/data/instances", "alice", "perimeter", "fw1") {
		t.Fatalf("work dir: %q", p.Dir)
	}
	if p.Log != filepath.Join("/data/logs", "alice", "perimeter", "fw1.log") {
		t.Fatalf("log: %q", p.Log)
	}
	if p.QMP != filepath.Join("/run/netrave", "d7.qmp") {
		t.Fatalf("qmp: %q", p.QMP)
	}
}

// Renaming a laboratory removes its logs for every owner and keeps other laboratories.
func TestRemoveLabLogsOfAllKeepsOtherLabs(t *testing.T) {
	logs := t.TempDir()
	m := &Manager{cfg: &config.Config{LogsDir: logs}}
	for _, f := range []string{"alice/old/fw1.log", "bob/old/sw1.log", "alice/other/fw1.log"} {
		writeTestFile(t, filepath.Join(logs, filepath.FromSlash(f)))
	}
	m.RemoveLabLogsOfAll("old")
	if _, err := os.Stat(filepath.Join(logs, "alice", "old")); !os.IsNotExist(err) {
		t.Fatalf("alice's logs of the old laboratory remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(logs, "bob", "old")); !os.IsNotExist(err) {
		t.Fatalf("bob's logs of the old laboratory remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(logs, "alice", "other", "fw1.log")); err != nil {
		t.Fatalf("other laboratory's log removed: %v", err)
	}
}

// Renaming a machine removes its log for every owner of the laboratory and keeps the other machines' logs.
func TestRemoveDeviceLogKeepsOtherDevices(t *testing.T) {
	logs := t.TempDir()
	m := &Manager{cfg: &config.Config{LogsDir: logs}}
	writeTestFile(t, filepath.Join(logs, "alice", "lab", "fw1.log"))
	writeTestFile(t, filepath.Join(logs, "bob", "lab", "fw1.log"))
	writeTestFile(t, filepath.Join(logs, "alice", "lab", "sw1.log"))
	m.RemoveDeviceLog("lab", "fw1")
	for _, f := range []string{"alice/lab/fw1.log", "bob/lab/fw1.log"} {
		if _, err := os.Stat(filepath.Join(logs, filepath.FromSlash(f))); !os.IsNotExist(err) {
			t.Fatalf("%s remains: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(logs, "alice", "lab", "sw1.log")); err != nil {
		t.Fatalf("other machine's log removed: %v", err)
	}
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("log\n"), 0o640); err != nil {
		t.Fatal(err)
	}
}

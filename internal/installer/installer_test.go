// SPDX-License-Identifier: GPL-3.0-only

package installer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"netrave/internal/config"
	initpkg "netrave/internal/init"
)

type fakeRunner struct{}

func (fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	joined := name + " " + strings.Join(args, " ")
	if strings.Contains(joined, "is-active") || strings.Contains(joined, "status") {
		return "active", nil
	}
	return "", nil
}

func TestInstallAndUninstall(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.WriteFile(source, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	options := Options{
		Root: root, BinarySource: source, InitSystem: "systemd", Runner: fakeRunner{},
	}
	result, err := Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Adapter != "systemd" || result.Status != "active" {
		t.Fatalf("unexpected result: %+v", result)
	}
	binary := filepath.Join(root, "usr/local/bin/netrave")
	info, err := os.Stat(binary)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("binary mode: %v", info.Mode().Perm())
	}
	configPath := filepath.Join(root, "etc/netrave/config.yaml")
	info, err = os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("config mode: %v", info.Mode().Perm())
	}
	installed, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if installed.SocketPath != "/run/netrave/daemon.sock" || installed.LogFile != "/var/log/netrave/netrave.log" {
		t.Fatalf("installed config paths: socket=%s log=%s", installed.SocketPath, installed.LogFile)
	}
	unit, err := os.ReadFile(filepath.Join(root, "etc/systemd/system/netrave.service"))
	if err != nil || !strings.Contains(string(unit), "daemon --config") {
		t.Fatalf("unit: %v %s", err, unit)
	}
	if _, err := Uninstall(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(binary); !os.IsNotExist(err) {
		t.Fatalf("binary should be removed")
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config must be preserved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "var/lib/netrave")); err != nil {
		t.Fatalf("data must be preserved: %v", err)
	}
}

func TestPurgeRemovesData(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.WriteFile(source, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	options := Options{Root: root, BinarySource: source, InitSystem: "systemd", Runner: fakeRunner{}, Purge: true}
	if _, err := Install(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"etc/netrave", "var/lib/netrave", "var/log/netrave"} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("%s should be removed", path)
		}
	}
}

var _ initpkg.Runner = fakeRunner{}

func TestIsActiveStatus(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{"active", true},
		{"inactive", false},
		{"failed", false},
		{"activating", false},
		{"run: netrave: (pid 12) 5s", true},
		{"down: netrave: 3s", false},
		{"* status: started", true},
		{"* status: stopped", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isActiveStatus(tc.status); got != tc.want {
			t.Errorf("isActiveStatus(%q) = %v, want %v", tc.status, got, tc.want)
		}
	}
}

func TestReinstallKeepsExistingConfig(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.WriteFile(source, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	options := Options{Root: root, BinarySource: source, InitSystem: "systemd", Runner: fakeRunner{}}
	first, err := Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if first.ConfigKept {
		t.Fatal("first install must not report a kept configuration")
	}
	configPath := filepath.Join(root, "etc/netrave/config.yaml")
	custom := []byte("addr: \":9999\"\n")
	if err := os.WriteFile(configPath, custom, 0o640); err != nil {
		t.Fatal(err)
	}
	second, err := Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if !second.ConfigKept {
		t.Fatal("reinstall must keep the existing configuration")
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(custom) {
		t.Fatalf("configuration was overwritten: %s", got)
	}
}

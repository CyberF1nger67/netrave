// SPDX-License-Identifier: GPL-3.0-only

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFileAndFlagOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := "addr: \":9090\"\ndata_dir: " + filepath.Join(dir, "data") + "\nrestart_sec: 5\n"
	if err := os.WriteFile(path, []byte(data), 0o640); err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseArgs([]string{"--config", path, "--addr", ":9191"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":9191" {
		t.Fatalf("flag did not override file: %s", cfg.Addr)
	}
	if cfg.DataDir != filepath.Join(dir, "data") || cfg.DisksDir != filepath.Join(dir, "data", "disks") {
		t.Fatalf("derived paths: %+v", cfg)
	}
	if cfg.RestartSec != 5 {
		t.Fatalf("restart_sec: %d", cfg.RestartSec)
	}
}

func TestDataFlagRebasesDerivedPaths(t *testing.T) {
	cfg, err := ParseArgs([]string{"--data", "/tmp/netrave-config-test"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DisksDir != "/tmp/netrave-config-test/disks" || cfg.LogFile != "/tmp/netrave-config-test/netrave.log" {
		t.Fatalf("derived paths not rebased: %+v", cfg)
	}
}

// Saving settings changes only the settings section: comments and other keys remain.
func TestSaveSettingsKeepsOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "# my config\naddr: \":9090\"\nsettings:\n  session_ttl_hours: 24\n"
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetRoleAdminDescription("Admin"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"# my config", "addr: \":9090\"", "role_admin_description: Admin", "session_ttl_hours: 24"} {
		if !strings.Contains(text, want) {
			t.Fatalf("saved config lacks %q:\n%s", want, text)
		}
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.Settings.RoleAdminDescription != "Admin" || again.Addr != ":9090" {
		t.Fatalf("reloaded config: %+v", again)
	}
}

// The example from the repository must load: otherwise the documentation diverges from the code.
func TestExampleConfigLoads(t *testing.T) {
	cfg, err := Load("../../configs/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8080" || cfg.Settings.SessionTTLHours != 24 || cfg.Settings.MaxUploadMB != 20480 {
		t.Fatalf("example config values: %+v", cfg)
	}
	if cfg.SocketPath != "/run/netrave/daemon.sock" || cfg.LogFile != "/var/log/netrave/netrave.log" {
		t.Fatalf("example config paths: socket=%s log=%s", cfg.SocketPath, cfg.LogFile)
	}
}

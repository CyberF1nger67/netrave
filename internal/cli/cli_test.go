// SPDX-License-Identifier: GPL-3.0-only

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHelpAndUnknownCommand(t *testing.T) {
	if code := Run([]string{"help"}); code != 0 {
		t.Fatalf("help exit code: %d", code)
	}
	if code := Run([]string{"unknown"}); code != 2 {
		t.Fatalf("unknown command exit code: %d", code)
	}
	if code := Run([]string{"version"}); code != 0 {
		t.Fatalf("version exit code: %d", code)
	}
}

func TestDoctorExitCode(t *testing.T) {
	if code := Run([]string{"doctor", "--json"}); code != 0 {
		t.Fatalf("doctor exit code: %d", code)
	}
}

// The service and the CLI must agree on the daemon socket. `netrave logs` used
// to ignore the installed configuration and dial <data_dir>/run/daemon.sock
// instead of the socket written by the installer.
func TestLoadCommandConfigUsesNETRAVEConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "addr: \":9090\"\nsocket_path: /run/netrave/daemon.sock\nlog_file: /var/log/netrave/netrave.log\n"
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NETRAVE_CONFIG", path)
	cfg, err := loadCommandConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SocketPath != "/run/netrave/daemon.sock" || cfg.LogFile != "/var/log/netrave/netrave.log" {
		t.Fatalf("installed configuration ignored: %+v", cfg)
	}
}

func TestLoadCommandConfigExplicitFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("socket_path: /tmp/test/daemon.sock\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NETRAVE_CONFIG", "")
	cfg, err := loadCommandConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SocketPath != "/tmp/test/daemon.sock" {
		t.Fatalf("explicit configuration ignored: %+v", cfg)
	}
}

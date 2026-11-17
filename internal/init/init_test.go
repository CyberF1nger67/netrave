// SPDX-License-Identifier: GPL-3.0-only

package init

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"netrave/internal/platform"
)

type fakeRunner struct{ commands []string }

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	f.commands = append(f.commands, name+" "+strings.Join(args, " "))
	if strings.Contains(name, "is-active") || strings.Contains(name, "status") {
		return "active", nil
	}
	return "", nil
}

func service() ServiceConfig {
	return ServiceConfig{Name: "netrave", Binary: "/usr/local/bin/netrave", ConfigFile: "/etc/netrave/config.yaml",
		DataDir: "/var/lib/netrave", LogFile: "/var/log/netrave/netrave.log", Socket: "/run/netrave/daemon.sock"}
}

func TestSystemdInstallAndCommands(t *testing.T) {
	root := t.TempDir()
	runner := &fakeRunner{}
	adapter, err := New(platform.InitSystemd, root, runner, service())
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Install(service()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "etc/systemd/system/netrave.service"))
	if err != nil {
		t.Fatal(err)
	}
	unit := string(data)
	for _, want := range []string{"ExecStart=/usr/local/bin/netrave daemon", "Restart=on-failure", "WantedBy=multi-user.target"} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit misses %q:\n%s", want, unit)
		}
	}
	if err := adapter.Enable(); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Start(); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) < 3 {
		t.Fatalf("commands not executed: %v", runner.commands)
	}
}

func TestOpenRCAndRunitInstall(t *testing.T) {
	for _, kind := range []platform.InitKind{platform.InitOpenRC, platform.InitRunit} {
		root := t.TempDir()
		runner := &fakeRunner{}
		adapter, err := New(kind, root, runner, service())
		if err != nil {
			t.Fatal(err)
		}
		if err := adapter.Install(service()); err != nil {
			t.Fatalf("%s install: %v", kind, err)
		}
		if err := adapter.Enable(); err != nil {
			t.Fatalf("%s enable: %v", kind, err)
		}
		switch kind {
		case platform.InitOpenRC:
			script := filepath.Join(root, "etc/init.d/netrave")
			if _, err := os.Stat(script); err != nil {
				t.Fatal(err)
			}
			if err := exec.Command("sh", "-n", script).Run(); err != nil {
				t.Fatalf("openrc script syntax: %v", err)
			}
		case platform.InitRunit:
			run := filepath.Join(root, "etc/sv/netrave/run")
			if _, err := os.Stat(run); err != nil {
				t.Fatal(err)
			}
			if err := exec.Command("sh", "-n", run).Run(); err != nil {
				t.Fatalf("runit script syntax: %v", err)
			}
		}
	}
}

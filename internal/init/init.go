// SPDX-License-Identifier: GPL-3.0-only

// Package init installs and controls the project service for systemd, OpenRC
// and runit. Adapters only translate lifecycle operations into the native
// service manager; no business logic lives here.
package init

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"netrave/internal/platform"
)

// Runner abstracts command execution so adapters can be tested without root.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

// ExecRunner executes real commands.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

// ServiceConfig is the complete description of the installed service.
type ServiceConfig struct {
	Name       string
	Binary     string
	ConfigFile string
	DataDir    string
	LogFile    string
	Socket     string
	User       string
	Group      string
}

// Adapter is the common service-management interface.
type Adapter interface {
	Kind() platform.InitKind
	UnitPath() string
	Install(cfg ServiceConfig) error
	Remove() error
	Enable() error
	Disable() error
	Start() error
	Stop() error
	Restart() error
	Status() (string, error)
	Logs(lines int) (string, error)
}

// Base contains shared filesystem and command helpers.
type Base struct {
	Root   string
	Runner Runner
	Config ServiceConfig
}

func (b Base) path(path string) string {
	if b.Root == "" || filepath.IsAbs(path) && b.Root == "/" {
		return path
	}
	return filepath.Join(b.Root, strings.TrimPrefix(path, "/"))
}

func (b Base) run(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	output, err := b.Runner.Run(ctx, name, args...)
	if err != nil {
		if output != "" {
			return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, output)
		}
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func (b Base) writeFile(path string, mode os.FileMode, content string) error {
	full := b.path(path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(full, []byte(content), mode); err != nil {
		return err
	}
	return os.Chmod(full, mode)
}

func (b Base) remove(path string) error {
	err := os.Remove(b.path(path))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// New returns the adapter for the requested init system.
func New(kind platform.InitKind, root string, runner Runner, cfg ServiceConfig) (Adapter, error) {
	if runner == nil {
		runner = ExecRunner{}
	}
	if cfg.User == "" {
		cfg.User = "root"
	}
	if cfg.Group == "" {
		cfg.Group = "root"
	}
	base := Base{Root: root, Runner: runner, Config: cfg}
	switch kind {
	case platform.InitSystemd:
		return &Systemd{Base: base}, nil
	case platform.InitOpenRC:
		return &OpenRC{Base: base}, nil
	case platform.InitRunit:
		return &Runit{Base: base}, nil
	default:
		return nil, fmt.Errorf("unsupported init system %q", kind)
	}
}

func contextWithTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 20*time.Second)
}

// tailFile returns the last lines of a log file without loading the whole file
// when it is large.
func tailFile(path string, lines int) (string, error) {
	if lines <= 0 {
		lines = 100
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	parts := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n"), nil
}

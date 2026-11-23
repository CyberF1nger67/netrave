// SPDX-License-Identifier: GPL-3.0-only

// Package installer performs a complete host installation: configuration,
// directories, binary, service unit and autostart.
package installer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"netrave/internal/hostarch"
	"netrave/internal/yamlfmt"

	"netrave/internal/config"
	initpkg "netrave/internal/init"
	"netrave/internal/platform"
	"netrave/internal/version"
)

// Options controls installation and removal.
type Options struct {
	Root         string
	BinarySource string
	BinaryPath   string
	ConfigPath   string
	DataDir      string
	LogDir       string
	SocketPath   string
	InitSystem   string
	Addr         string
	Purge        bool
	Runner       initpkg.Runner
}

// Result summarizes the installed service.
type Result struct {
	Platform   platform.Info `json:"platform"`
	Adapter    string        `json:"adapter"`
	Binary     string        `json:"binary"`
	Config     string        `json:"config"`
	DataDir    string        `json:"data_dir"`
	LogFile    string        `json:"log_file"`
	Socket     string        `json:"socket"`
	Status     string        `json:"status"`
	ConfigKept bool          `json:"config_kept"`
}

func (o *Options) applyDefaults() error {
	if o.Root == "" {
		o.Root = "/"
	}
	if o.Root == "/" && os.Geteuid() != 0 {
		return fmt.Errorf("installation requires root (use sudo) or --root for a staging directory")
	}
	if o.BinarySource == "" {
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		o.BinarySource = executable
	}
	if o.BinaryPath == "" {
		o.BinaryPath = "/usr/local/bin/netrave"
	}
	if o.ConfigPath == "" {
		o.ConfigPath = DefaultConfigPath
	}
	if o.DataDir == "" {
		o.DataDir = "/var/lib/netrave"
	}
	if o.LogDir == "" {
		o.LogDir = "/var/log/netrave"
	}
	if o.SocketPath == "" {
		o.SocketPath = "/run/netrave/daemon.sock"
	}
	if o.Addr == "" {
		o.Addr = ":8080"
	}
	return nil
}

// DefaultConfigPath is the configuration file of the installation.
const DefaultConfigPath = "/etc/netrave/config.yaml"

func hostPath(root, path string) string {
	if root == "" || root == "/" {
		return path
	}
	return filepath.Join(root, strings.TrimPrefix(path, "/"))
}

func (o *Options) detect() (platform.Info, error) {
	info, err := platform.Detect(o.InitSystem)
	if err != nil {
		return platform.Info{}, err
	}
	if info.Init == platform.InitUnknown {
		for _, candidate := range info.Adapter.InitCandidates {
			if candidate != platform.InitUnknown {
				info.Init = candidate
				break
			}
		}
	}
	if info.Init == platform.InitUnknown {
		return info, fmt.Errorf("init system not detected; pass --init systemd|openrc|runit")
	}
	return info, nil
}

// Install configures and starts the service.
func Install(ctx context.Context, options Options) (Result, error) {
	if err := options.applyDefaults(); err != nil {
		return Result{}, err
	}
	info, err := options.detect()
	if err != nil {
		return Result{}, err
	}
	cfg := config.Config{
		Addr: options.Addr, DataDir: options.DataDir,
		DisksDir:     filepath.Join(options.DataDir, "disks"),
		LabsDir:      filepath.Join(options.DataDir, "labs"),
		InstancesDir: filepath.Join(options.DataDir, "instances"),
		LogsDir:      filepath.Join(options.DataDir, "logs"),
		IconsDir:     filepath.Join(options.DataDir, "icons"),
		RunDir:       "/run/netrave", TmpDir: filepath.Join(options.DataDir, "tmp"),
		DBPath:      filepath.Join(options.DataDir, "netrave.db"),
		QEMUPath:    hostarch.QEMUBinary(),
		SocketPath:  options.SocketPath,
		LogFile:     filepath.Join(options.LogDir, "netrave.log"),
		RestartSec:  2,
		ServiceName: "netrave",
	}
	configFile := hostPath(options.Root, options.ConfigPath)
	configKept := false
	if fileExists(configFile) {
		// Existing settings belong to the administrator; never overwrite them.
		configKept = true
	} else if err := writeConfig(configFile, cfg); err != nil {
		return Result{}, err
	}
	for _, dir := range []string{options.DataDir, filepath.Join(options.DataDir, "disks"),
		filepath.Join(options.DataDir, "labs"), filepath.Join(options.DataDir, "instances"),
		filepath.Join(options.DataDir, "logs"), filepath.Join(options.DataDir, "icons"),
		"/run/netrave", filepath.Join(options.DataDir, "tmp"), options.LogDir} {
		if err := os.MkdirAll(hostPath(options.Root, dir), 0o750); err != nil {
			return Result{}, fmt.Errorf("create %s: %w", dir, err)
		}
	}
	if err := installBinary(options.BinarySource, hostPath(options.Root, options.BinaryPath)); err != nil {
		return Result{}, err
	}
	service := initpkg.ServiceConfig{
		Name: "netrave", Binary: options.BinaryPath, ConfigFile: options.ConfigPath,
		DataDir: options.DataDir, LogFile: cfg.LogFile, Socket: options.SocketPath,
		User: "root", Group: "root",
	}
	adapter, err := initpkg.New(info.Init, options.Root, options.Runner, service)
	if err != nil {
		return Result{}, err
	}
	wasRunning := isActiveStatus(statusOf(adapter))
	if err := adapter.Install(service); err != nil {
		return Result{}, fmt.Errorf("install service: %w", err)
	}
	if err := adapter.Enable(); err != nil {
		return Result{}, fmt.Errorf("enable service: %w", err)
	}
	if wasRunning {
		err = adapter.Restart()
	} else {
		err = adapter.Start()
	}
	if err != nil {
		return Result{}, fmt.Errorf("start service: %w", err)
	}
	status := ""
	for attempt := 0; attempt < 15; attempt++ {
		status = statusOf(adapter)
		if isActiveStatus(status) {
			break
		}
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return Result{Platform: info, Adapter: string(info.Init), Binary: options.BinaryPath,
		Config: options.ConfigPath, DataDir: options.DataDir, LogFile: cfg.LogFile,
		Socket: options.SocketPath, Status: status, ConfigKept: configKept}, nil
}

func statusOf(adapter initpkg.Adapter) string {
	status, _ := adapter.Status()
	return status
}

// isActiveStatus reports whether a service manager status string means the
// service is running. Matching is exact on purpose: "inactive" contains
// "active" and must not be treated as running.
func isActiveStatus(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	return s == "active" || strings.HasPrefix(s, "run:") || strings.Contains(s, "status: started")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func writeConfig(path string, cfg config.Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := yamlfmt.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o640); err != nil {
		return err
	}
	return os.Chmod(path, 0o640)
}

func installBinary(source, destination string) error {
	if source == destination {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open binary: %w", err)
	}
	defer input.Close()
	temp := destination + ".tmp"
	output, err := os.OpenFile(temp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		os.Remove(temp)
		return err
	}
	if err := output.Close(); err != nil {
		os.Remove(temp)
		return err
	}
	if err := os.Chmod(temp, 0o755); err != nil {
		os.Remove(temp)
		return err
	}
	return os.Rename(temp, destination)
}

// Uninstall stops the service and removes program files. User data and the
// configuration are preserved unless Purge is explicitly requested.
func Uninstall(ctx context.Context, options Options) (string, error) {
	if err := options.applyDefaults(); err != nil {
		return "", err
	}
	info, err := options.detect()
	if err != nil {
		return "", err
	}
	service := initpkg.ServiceConfig{
		Name: "netrave", Binary: options.BinaryPath, ConfigFile: options.ConfigPath,
		DataDir: options.DataDir, LogFile: filepath.Join(options.LogDir, "netrave.log"),
		Socket: options.SocketPath, User: "root", Group: "root",
	}
	adapter, err := initpkg.New(info.Init, options.Root, options.Runner, service)
	if err != nil {
		return "", err
	}
	_ = adapter.Stop()
	_ = adapter.Disable()
	if err := adapter.Remove(); err != nil {
		return "", err
	}
	_ = os.Remove(hostPath(options.Root, options.BinaryPath))
	if options.Purge {
		_ = os.RemoveAll(hostPath(options.Root, options.DataDir))
		_ = os.RemoveAll(hostPath(options.Root, options.LogDir))
		_ = os.RemoveAll(hostPath(options.Root, filepath.Dir(options.ConfigPath)))
	}
	return string(info.Init), nil
}

// Doctor prints platform and dependency diagnostics.
func Doctor(initOverride string) (platform.Info, []string) {
	info, err := platform.Detect(initOverride)
	if err != nil {
		return platform.Info{}, []string{err.Error()}
	}
	problems := append([]string{}, info.Warnings...)
	if !info.Root {
		problems = append(problems, "not running as root; QEMU networking will be unavailable")
	}
	if info.Init == platform.InitUnknown {
		problems = append(problems, "unknown init system")
	}
	return info, problems
}

// Version is reported by the installer for diagnostics.
func Version() string { return version.Version }

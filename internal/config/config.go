// SPDX-License-Identifier: GPL-3.0-only

// Package config loads runtime configuration from a YAML file, environment
// variables and command-line flags, in that order of precedence.
package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
	"netrave/internal/hostarch"
	"netrave/internal/msgs"
	"netrave/internal/yamlfmt"
)

// Settings - system settings the administrator changes in the UI. Stored in the "settings"
// section of the configuration file (not in the database) and read on every access.
type Settings struct {
	SessionTTLHours      int    `json:"session_ttl_hours" yaml:"session_ttl_hours"`
	MaxUploadMB          int    `json:"max_upload_mb" yaml:"max_upload_mb"`
	RoleAdminDescription string `json:"role_admin_description" yaml:"role_admin_description"`
}

const (
	defaultSessionTTLHours = 24
	defaultMaxUploadMB     = 20480
)

// ErrNoConfigFile - settings cannot be saved: the process was started without a configuration file (-config).
var ErrNoConfigFile = errors.New("No configuration file (-config): the settings were not saved")

// settingsMu protects Settings in all Config instances.
var settingsMu sync.RWMutex

// SettingValid validates a setting value as a string (the way the UI sends it).
// known=false - there is no such setting.
func SettingValid(key, value string) (known, ok bool) {
	switch key {
	case "session_ttl_hours":
		n, err := strconv.Atoi(value)
		return true, err == nil && n >= 1 && n <= 720
	case "max_upload_mb":
		n, err := strconv.Atoi(value)
		return true, err == nil && n >= 1 && n <= 1048576
	}
	return false, false
}

// SettingsSnapshot returns a copy of the settings (defaults for nil).
func (c *Config) SettingsSnapshot() Settings {
	settingsMu.RLock()
	defer settingsMu.RUnlock()
	if c == nil {
		return Settings{SessionTTLHours: defaultSessionTTLHours, MaxUploadMB: defaultMaxUploadMB}
	}
	return c.Settings
}

// SessionTTL - session lifetime.
func (c *Config) SessionTTL() time.Duration {
	h := c.SettingsSnapshot().SessionTTLHours
	if h < 1 || h > 720 {
		h = defaultSessionTTLHours
	}
	return time.Duration(h) * time.Hour
}

// MaxUploadBytes - the maximum uploadable image size.
func (c *Config) MaxUploadBytes() int64 {
	mb := c.SettingsSnapshot().MaxUploadMB
	if mb <= 0 {
		mb = defaultMaxUploadMB
	}
	return int64(mb) << 20
}

// UpdateSettings changes settings by values from the UI and saves them to the configuration file.
// Values are validated by the caller via SettingValid. On a save error memory is not changed.
func (c *Config) UpdateSettings(in map[string]string) error {
	if c == nil {
		return ErrNoConfigFile
	}
	settingsMu.Lock()
	defer settingsMu.Unlock()
	next := c.Settings
	for k, v := range in {
		n, _ := strconv.Atoi(v)
		switch k {
		case "session_ttl_hours":
			next.SessionTTLHours = n
		case "max_upload_mb":
			next.MaxUploadMB = n
		}
	}
	return c.save(next)
}

// SetRoleAdminDescription saves the administrator role description.
func (c *Config) SetRoleAdminDescription(desc string) error {
	if c == nil {
		return ErrNoConfigFile
	}
	settingsMu.Lock()
	defer settingsMu.Unlock()
	next := c.Settings
	next.RoleAdminDescription = desc
	return c.save(next)
}

// save writes the settings to the configuration file, keeping other keys and comments. Call under settingsMu.
func (c *Config) save(next Settings) error {
	if c.ConfigPath == "" {
		return ErrNoConfigFile
	}
	var doc yaml.Node
	data, err := os.ReadFile(c.ConfigPath)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parse config %s: %w", c.ConfigPath, err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return err
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return msgs.New("Config {path}: the file root must be a mapping", map[string]string{"path": c.ConfigPath})
	}
	var sec yaml.Node
	if err := sec.Encode(next); err != nil {
		return err
	}
	setMappingKey(doc.Content[0], "settings", &sec)
	out, err := yamlfmt.Marshal(&doc)
	if err != nil {
		return err
	}
	tmp := c.ConfigPath + ".tmp"
	if err := os.WriteFile(tmp, out, 0o640); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.ConfigPath); err != nil {
		return err
	}
	c.Settings = next
	return nil
}

// setMappingKey replaces the key value in the mapping or appends the key at the end.
func setMappingKey(m *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = value
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

// Config contains all runtime paths and server parameters.
type Config struct {
	Addr          string `json:"addr" yaml:"addr"`
	DataDir       string `json:"data_dir" yaml:"data_dir"`
	DisksDir      string `json:"disks_dir" yaml:"disks_dir"`
	LabsDir       string `json:"labs_dir" yaml:"labs_dir"`
	InstancesDir  string `json:"instances_dir" yaml:"instances_dir"`
	LogsDir       string `json:"logs_dir" yaml:"logs_dir"`
	IconsDir      string `json:"icons_dir" yaml:"icons_dir"`
	ThemesDir     string `json:"themes_dir" yaml:"themes_dir"`
	RunDir        string `json:"run_dir" yaml:"run_dir"`
	TmpDir        string `json:"tmp_dir" yaml:"tmp_dir"`
	DBPath        string `json:"db_path" yaml:"db_path"`
	QEMUPath      string `json:"qemu_path" yaml:"qemu_path"`
	StopVMsOnExit bool   `json:"stop_vms_on_exit" yaml:"stop_vms_on_exit"`

	ConfigPath  string `json:"-" yaml:"-"`
	SocketPath  string `json:"socket_path" yaml:"socket_path"`
	LogFile     string `json:"log_file" yaml:"log_file"`
	InitSystem  string `json:"init_system" yaml:"init_system"`
	RestartSec  int    `json:"restart_sec" yaml:"restart_sec"`
	Command     string `json:"command" yaml:"command"`
	ServiceName string `json:"service_name" yaml:"service_name"`

	Settings Settings `json:"settings" yaml:"settings"`
}

func envOr(key, def string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return def
}

func defaults() *Config {
	data := envOr("NETRAVE_DATA", "/var/lib/netrave")
	c := &Config{
		Addr:        envOr("NETRAVE_ADDR", ":8080"),
		DataDir:     data,
		QEMUPath:    envOr("NETRAVE_QEMU", hostarch.QEMUBinary()),
		ConfigPath:  envOr("NETRAVE_CONFIG", ""),
		SocketPath:  envOr("NETRAVE_SOCKET", ""),
		LogFile:     envOr("NETRAVE_LOG", ""),
		InitSystem:  envOr("NETRAVE_INIT", ""),
		RestartSec:  2,
		Command:     "",
		ServiceName: envOr("NETRAVE_SERVICE", "netrave"),
	}
	c.DisksDir = envOr("NETRAVE_DISKS", "")
	c.LabsDir = envOr("NETRAVE_LABS", "")
	c.InstancesDir = envOr("NETRAVE_INSTANCES", "")
	c.LogsDir = envOr("NETRAVE_LOGS", "")
	c.IconsDir = envOr("NETRAVE_ICONS", "")
	c.DBPath = envOr("NETRAVE_DB", "")
	c.finish()
	return c
}

func (c *Config) rebaseDerived(oldData string) {
	if c.DataDir == oldData {
		return
	}
	rebase := func(current, suffix string) string {
		if current == filepath.Join(oldData, suffix) {
			return filepath.Join(c.DataDir, suffix)
		}
		return current
	}
	c.DisksDir = rebase(c.DisksDir, "disks")
	c.LabsDir = rebase(c.LabsDir, "labs")
	c.InstancesDir = rebase(c.InstancesDir, "instances")
	c.LogsDir = rebase(c.LogsDir, "logs")
	c.IconsDir = rebase(c.IconsDir, "icons")
	c.ThemesDir = rebase(c.ThemesDir, "themes")
	c.DBPath = rebase(c.DBPath, "netrave.db")
	c.RunDir = rebase(c.RunDir, "run")
	c.TmpDir = rebase(c.TmpDir, "tmp")
	c.SocketPath = rebase(c.SocketPath, filepath.Join("run", "daemon.sock"))
	c.LogFile = rebase(c.LogFile, "netrave.log")
}

func (c *Config) finish() {
	if c.DisksDir == "" {
		c.DisksDir = filepath.Join(c.DataDir, "disks")
	}
	if c.LabsDir == "" {
		c.LabsDir = filepath.Join(c.DataDir, "labs")
	}
	if c.InstancesDir == "" {
		c.InstancesDir = filepath.Join(c.DataDir, "instances")
	}
	if c.LogsDir == "" {
		c.LogsDir = filepath.Join(c.DataDir, "logs")
	}
	if c.IconsDir == "" {
		c.IconsDir = filepath.Join(c.DataDir, "icons")
	}
	if c.ThemesDir == "" {
		c.ThemesDir = filepath.Join(c.DataDir, "themes")
	}
	if c.DBPath == "" {
		c.DBPath = filepath.Join(c.DataDir, "netrave.db")
	}
	if c.RunDir == "" {
		c.RunDir = filepath.Join(c.DataDir, "run")
	}
	if c.TmpDir == "" {
		c.TmpDir = filepath.Join(c.DataDir, "tmp")
	}
	if c.SocketPath == "" {
		c.SocketPath = filepath.Join(c.RunDir, "daemon.sock")
	}
	if c.LogFile == "" {
		c.LogFile = filepath.Join(c.DataDir, "netrave.log")
	}
	if c.RestartSec <= 0 {
		c.RestartSec = 2
	}
	if c.Settings.SessionTTLHours == 0 {
		c.Settings.SessionTTLHours = defaultSessionTTLHours
	}
	if c.Settings.MaxUploadMB == 0 {
		c.Settings.MaxUploadMB = defaultMaxUploadMB
	}
}

// LoadFile applies a YAML configuration file over the defaults.
func (c *Config) LoadFile(path string) error {
	if path == "" {
		return nil
	}
	oldData := c.DataDir
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, c); err != nil {
		return fmt.Errorf("parse config %s: %w", path, err)
	}
	c.ConfigPath = path
	c.rebaseDerived(oldData)
	c.finish()
	return nil
}

// Load parses a config file and returns a complete configuration.
func Load(path string) (*Config, error) {
	c := defaults()
	if err := c.LoadFile(path); err != nil {
		return nil, err
	}
	c.finish()
	return c, nil
}

// ParseArgs parses flags without terminating the process. It returns
// flag.ErrHelp for -h/-help.
func ParseArgs(args []string) (*Config, error) {
	configPath := envOr("NETRAVE_CONFIG", "")
	for index, arg := range args {
		switch {
		case arg == "-config" || arg == "--config":
			if index+1 < len(args) {
				configPath = args[index+1]
			}
		case strings.HasPrefix(arg, "--config="):
			configPath = strings.TrimPrefix(arg, "--config=")
		case strings.HasPrefix(arg, "-config="):
			configPath = strings.TrimPrefix(arg, "-config=")
		}
	}
	c, err := Load(configPath)
	if err != nil {
		return nil, err
	}
	oldData := c.DataDir
	fs := flag.NewFlagSet("netrave", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&c.Addr, "addr", c.Addr, "HTTP listen address")
	fs.StringVar(&c.DataDir, "data", c.DataDir, "data directory")
	fs.StringVar(&c.DisksDir, "disks", c.DisksDir, "disk image directory")
	fs.StringVar(&c.LabsDir, "labs", c.LabsDir, "lab directory")
	fs.StringVar(&c.InstancesDir, "instances", c.InstancesDir, "instance working directory")
	fs.StringVar(&c.LogsDir, "logs", c.LogsDir, "log directory")
	fs.StringVar(&c.IconsDir, "icons", c.IconsDir, "icon directory")
	fs.StringVar(&c.DBPath, "db", c.DBPath, "SQLite database path")
	fs.StringVar(&c.QEMUPath, "qemu", c.QEMUPath, "path to the QEMU system emulator of this host")
	fs.StringVar(&c.SocketPath, "socket", c.SocketPath, "daemon Unix socket path")
	fs.StringVar(&c.LogFile, "log", c.LogFile, "daemon log file")
	fs.StringVar(&c.InitSystem, "init", c.InitSystem, "init system override (systemd, openrc, runit)")
	fs.StringVar(&c.ConfigPath, "config", c.ConfigPath, "YAML configuration file")
	fs.IntVar(&c.RestartSec, "restart-sec", c.RestartSec, "child restart delay in seconds")
	fs.BoolVar(&c.StopVMsOnExit, "stop-vms-on-exit", c.StopVMsOnExit, "stop all VMs when the server exits")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if c.DataDir != oldData {
		c.rebaseDerived(oldData)
	}
	c.finish()
	return c, nil
}

// EnsureDirs creates all runtime directories with restrictive permissions.
func (c *Config) EnsureDirs() error {
	for _, dir := range []string{c.DataDir, c.DisksDir, c.LabsDir, c.InstancesDir, c.LogsDir, c.IconsDir, c.ThemesDir, c.RunDir, c.TmpDir, filepath.Dir(c.LogFile)} {
		if dir == "" || dir == "." {
			continue
		}
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	return nil
}

// Validate checks invariants shared by the server and the daemon.
func (c *Config) Validate() error {
	if c.Addr == "" {
		return errors.New("empty listen address")
	}
	if c.DataDir == "" {
		return errors.New("empty data directory")
	}
	if c.RestartSec < 0 {
		return errors.New("restart-sec cannot be negative")
	}
	return nil
}

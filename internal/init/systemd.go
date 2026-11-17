// SPDX-License-Identifier: GPL-3.0-only

package init

import (
	"fmt"
	"path/filepath"
	"strings"

	"netrave/internal/platform"
)

// Systemd writes a native unit and delegates lifecycle operations to
// systemctl. The daemon runs in the foreground so systemd can supervise it.
type Systemd struct{ Base }

func (s *Systemd) Kind() platform.InitKind { return platform.InitSystemd }

func (s *Systemd) UnitPath() string {
	return filepath.Join("/etc/systemd/system", s.Config.Name+".service")
}

func (s *Systemd) Install(cfg ServiceConfig) error {
	unit := fmt.Sprintf(`[Unit]
Description=NetRave virtual network labs (%s)
Documentation=https://github.com/netrave/netrave
After=network-online.target
Wants=network-online.target
ConditionPathExists=%s

[Service]
Type=simple
User=root
Group=root
ExecStart=%s daemon --config %s --foreground
Restart=on-failure
RestartSec=3
TimeoutStopSec=90
KillMode=mixed
RuntimeDirectory=%s
RuntimeDirectoryMode=0750
WorkingDirectory=%s
Environment=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
`, cfg.Name, cfg.Binary, cfg.Binary, cfg.ConfigFile, cfg.Name, cfg.DataDir)
	if err := s.writeFile(s.UnitPath(), 0o644, unit); err != nil {
		return err
	}
	return s.run("systemctl", "daemon-reload")
}

func (s *Systemd) Remove() error {
	_ = s.Disable()
	_ = s.Stop()
	if err := s.remove(s.UnitPath()); err != nil {
		return err
	}
	return s.run("systemctl", "daemon-reload")
}

func (s *Systemd) Enable() error  { return s.run("systemctl", "enable", s.Config.Name+".service") }
func (s *Systemd) Disable() error { return s.run("systemctl", "disable", s.Config.Name+".service") }
func (s *Systemd) Start() error   { return s.run("systemctl", "start", s.Config.Name+".service") }
func (s *Systemd) Stop() error    { return s.run("systemctl", "stop", s.Config.Name+".service") }
func (s *Systemd) Restart() error { return s.run("systemctl", "restart", s.Config.Name+".service") }

func (s *Systemd) Status() (string, error) {
	ctx, cancel := contextWithTimeout()
	defer cancel()
	output, err := s.Runner.Run(ctx, "systemctl", "is-active", s.Config.Name+".service")
	if err != nil {
		return strings.TrimSpace(output), nil
	}
	return strings.TrimSpace(output), nil
}

func (s *Systemd) Logs(lines int) (string, error) {
	ctx, cancel := contextWithTimeout()
	defer cancel()
	return s.Runner.Run(ctx, "journalctl", "-u", s.Config.Name+".service", "-n", fmt.Sprint(lines), "--no-pager")
}

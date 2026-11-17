[English](README.md) | [Русский](README.ru.md)

# NetRave

NetRave is a self-hosted platform for virtual network laboratories. It runs
QEMU/KVM machines, builds topologies, connects virtual cables, manages images
and templates, and provides serial and VNC consoles in the browser.

The Go backend is the single source of truth for the API, permissions and
QEMU/network lifecycle. The React frontend is embedded in the same binary.

## Contents

- [Features](#features)
- [Architecture](#architecture)
- [Supported distributions](#supported-distributions)
- [Supported init systems](#supported-init-systems)
- [Requirements](#requirements)
- [Quick install with install.sh](#quick-install-with-installsh)
- [Install from a built binary](#install-from-a-built-binary)
- [Build from source](#build-from-source)
- [Run in the foreground](#run-in-the-foreground)
- [Daemon](#daemon)
- [Configuration](#configuration)
- [CLI](#cli)
- [Diagnostics](#diagnostics)
- [Logs](#logs)
- [Uninstall](#uninstall)
- [Security](#security)
- [Troubleshooting](#troubleshooting)
- [Testing](#testing)
- [Helper scripts in tools](#helper-scripts-in-tools)
- [Contributing](#contributing)
- [Code authorship](#code-authorship)
- [License](#license)

## Features

- Laboratory catalog with folders, drag and drop, exports and per-lab rights.
- Visual topology editor: devices, networks, cables, zoom, selection, snapshots.
- QEMU/KVM lifecycle: start, stop, restart, pause, resume, wipe, clone.
- Image folders, disk overlays, CD-ROMs and image QEMU defaults.
- Serial terminal, VNC client, multi-console dock and detached console page.
- Templates catalog, custom PNG icon library and per-user themes.
- Users, roles, global permissions, per-laboratory permissions and audit log.
- Real-time notification center over WebSocket with polling fallback.
- Dashboard with live CPU, RAM, disk and QEMU process counters.
- English and Russian UI. English is the default; the language switch is in the
  top bar (globe icon) and on the login screen.
- systemd, OpenRC and runit service integration.
- Unix-socket daemon control channel, structured logs and diagnostics.

## Architecture

| Area | Location |
|---|---|
| HTTP API and WebSocket server | `internal/api`, `internal/console`, `internal/notify` |
| Core lab, device and network logic | `internal/labs`, `internal/devices`, `internal/network`, `internal/qemu` |
| Daemon supervisor and IPC | `internal/daemon` |
| Distribution detection | `internal/platform` |
| Init adapters | `internal/init` |
| Installer | `internal/installer` |
| CLI | `internal/cli` |
| Embedded frontend | `internal/web/static` |
| React source | `frontend/src` |

The daemon does not duplicate domain logic. It supervises the HTTP server
process, restarts it after failures and exposes a small control API over a Unix
socket. The HTTP server owns all business operations.

## Supported distributions

| Distribution | Family | Package manager | Init adapters |
|---|---|---|---|
| Debian | Debian | apt-get | systemd |
| Ubuntu | Debian | apt-get | systemd |
| RHEL | Red Hat | dnf | systemd |
| Fedora | Red Hat | dnf | systemd |
| Rocky Linux | Red Hat | dnf | systemd |
| AlmaLinux | Red Hat | dnf | systemd |
| Void Linux | Independent | xbps-install | runit, OpenRC |
| Alpine Linux | Independent | apk | OpenRC, runit |
| Arch Linux | Arch | pacman | systemd |
| Manjaro | Arch | pacman | systemd |

Detection uses `/etc/os-release`. Distribution data is registered in
`internal/platform/platform.go`; adding a distribution only requires another
adapter entry. Init detection is separate and can be overridden with `--init`.

## Supported init systems

| Init | Integration |
|---|---|
| systemd | `/etc/systemd/system/netrave.service`, `systemctl`, journald |
| OpenRC | `/etc/init.d/netrave`, `rc-update`, `rc-service` |
| runit | `/etc/sv/netrave`, `sv`, service symlink in `/var/service` or `/etc/service` |

## Requirements

- Linux x86_64 or aarch64. The project is tested on x86_64, and the QEMU labs
  are designed for `qemu-system-x86_64`.
- Go 1.26+ to build the backend.
- Node.js 20.19+ or 22.12+ and npm to build the frontend (required by Vite 7).
- QEMU/KVM: `qemu-system-x86_64`, `qemu-img`.
- Networking: `ip`, `nftables`, `dnsmasq` for NAT and DHCP.
- root for TAP bridges, nftables and KVM networking.
- `/dev/kvm` for hardware acceleration.

## Quick install with install.sh

One script installs the dependencies, builds the frontend and the binary, and
registers the service (systemd, OpenRC or runit):

```bash
sudo ./install.sh                      # install or update
sudo ./install.sh --addr :9090         # different HTTP address
sudo ./install.sh --no-deps            # do not install system packages
sudo ./install.sh -y                   # do not ask for confirmation
sudo ./install.sh --uninstall          # remove, keep data
sudo ./install.sh --uninstall --purge  # remove with data (asks for confirmation)
```

Only missing packages are installed; the system is not upgraded. Go and Node.js
are used from the system when suitable, otherwise official archives (SHA-256
verified) are unpacked to `/opt/netrave/toolchain`. The build runs as the user
who invoked sudo, so no root-owned files appear in the repository. Re-running
keeps the existing configuration (`/etc/netrave/config.yaml`) and restarts a
running service with the new binary. At the end the installer prints the
`admin` login and password; the password is shown only once.

Supported families: Debian/Ubuntu, RHEL/Fedora/Rocky/Alma, Arch/Manjaro, Void and
Alpine. For other distributions use `--no-deps` and install the packages
manually.

## Install from a built binary

```bash
sudo ./netrave install --init systemd
```

This copies the current executable to `/usr/local/bin/netrave`, writes
`/etc/netrave/config.yaml`, creates data and log directories, installs the
service, enables autostart and starts it. On OpenRC or runit use
`--init openrc` or `--init runit`.

## Build from source

```bash
cd frontend
npm ci
npm run build        # output: ../internal/web/static
cd ..
go build -o netrave .
```

The frontend is embedded with `//go:embed static`, so rebuild the Go binary
after every frontend build. Built frontend files are not stored in the
repository: the root `.gitignore` excludes `internal/web/static/assets/`,
`index.html`, `console.html` and `img/`. Build the frontend once before the Go
build, or run `./install.sh`, which does both.

A Makefile wraps the same steps:

```bash
make            # frontend + backend
make test       # go test and frontend typecheck and unit tests
make check      # test + gofmt + go vet
make install    # build and run sudo ./netrave install
make clean
```

## Run in the foreground

```bash
sudo ./netrave serve -addr :8080 -data /var/lib/netrave
```

Running `netrave` with no arguments also starts the server in the foreground.

On first start the service creates `admin` with a random password. The installer
prints it at the end of installation, and the service also writes it to
`/var/log/netrave/netrave.log`.

Open `http://SERVER:8080/`.

For UI-only development without root:

```bash
./netrave serve -addr 127.0.0.1:8080 -data /tmp/netrave-dev
cd frontend
NETRAVE_BACKEND=http://127.0.0.1:8080 npm run dev
```

Then open `http://127.0.0.1:5173/`.

## Daemon

```bash
sudo netrave daemon --config /etc/netrave/config.yaml --foreground
sudo netrave daemon --config /etc/netrave/config.yaml --background
```

- Foreground mode is used by systemd, OpenRC and runit.
- Background mode detaches from the terminal and writes a PID file.
- A lock file prevents multiple daemons: `/run/netrave/daemon.lock`.
- IPC socket: `/run/netrave/daemon.sock` (mode 0660, peer credentials checked).
- After an unexpected exit the daemon restarts the HTTP child after a configurable
  delay (`restart_sec`).
- SIGTERM and SIGINT stop the child gracefully; after 90 seconds the process
  group is killed with SIGKILL.

## Configuration

Example configuration (`/etc/netrave/config.yaml`):

```yaml
addr: ":8080"
data_dir: /var/lib/netrave
images_dir: /var/lib/netrave/images
labs_dir: /var/lib/netrave/labs
icons_dir: /var/lib/netrave/icons
db_path: /var/lib/netrave/netrave.db
socket_path: /run/netrave/daemon.sock
log_file: /var/log/netrave/netrave.log
restart_sec: 2
qemu_path: qemu-system-x86_64
```

Other keys the configuration understands: `themes_dir`, `run_dir`, `tmp_dir`,
`session_ttl_hours`, `max_upload_mb`, `stop_vms_on_exit`, `init_system`,
`service_name`, `command`, and `settings`.

Precedence: command-line flags, then the configuration file, then environment
variables, then built-in defaults.

Environment variables use the `NETRAVE_` prefix: `NETRAVE_ADDR`,
`NETRAVE_DATA`, `NETRAVE_IMAGES`, `NETRAVE_LABS`, `NETRAVE_ICONS`, `NETRAVE_DB`,
`NETRAVE_QEMU`, `NETRAVE_SOCKET`, `NETRAVE_LOG`, `NETRAVE_CONFIG`,
`NETRAVE_INIT`, `NETRAVE_SERVICE`.

## CLI

```text
netrave serve       Run the HTTP server in the foreground (default)
netrave daemon      Run the supervisor daemon
netrave status      Show daemon or service status (IPC)
netrave start       Start the installed service
netrave stop        Stop the installed service
netrave restart     Restart the installed service
netrave install     Install binary, configuration, service and autostart
netrave uninstall   Remove the service and program files
netrave doctor      Platform, init and dependency diagnostics
netrave netdiag     Show TAP, bridge, nftables and br_netfilter state (JSON)
netrave logs        Show daemon logs
netrave version     Print version and build metadata
netrave help        Show help; use "netrave <command> -h" for flags
```

Examples:

```bash
netrave status --json
netrave logs -n 200
sudo netrave doctor
sudo netrave start --init systemd
```

## Diagnostics

```bash
sudo netrave doctor
sudo netrave doctor --json
sudo netrave netdiag
```

The doctor report includes distribution, family, architecture, detected init
system, root status, KVM availability, `ip_forward`, required tools and package
installation hints. `netdiag` prints the state of network resources as JSON.

## Logs

- File: `/var/log/netrave/netrave.log`
- systemd: `journalctl -u netrave -f`
- IPC: `netrave logs -n 200`
- Daemon logs are structured JSON (`log/slog`).

## Uninstall

```bash
sudo netrave uninstall
```

Stops and disables the service, removes the unit or init script and the binary.
Configuration and user data are kept. For a full removal:

```bash
sudo netrave uninstall --purge --yes
```

`--purge` removes `/etc/netrave`, `/var/lib/netrave` and `/var/log/netrave`.

## Security

- The service runs as root only because TAP, bridges, nftables and KVM need it.
- IPC socket permissions are `0660`; the peer UID is checked with `SO_PEERCRED`.
- Session cookies are `HttpOnly`, `SameSite=Strict`, and `Secure` under TLS.
- Mutating API calls require the `X-CSRF-Token` header and same-origin checks.
- The HTTP server sends a strict Content-Security-Policy.
- Uploads are validated on both client and server.
- No secrets are written to logs.
- Service files and configuration use restrictive permissions (`0644`, `0640`, `0750`).

## Troubleshooting

| Problem | Solution |
|---|---|
| `address already in use` | Find the listener with `sudo ss -ltnp \| grep :8080`; stop it or use another `-addr`. |
| QEMU machines do not start | Check `netrave doctor`, `/dev/kvm`, KVM access and root. |
| NAT or DHCP unavailable | Install `nftables` and `dnsmasq`; forwarding is enabled when a NAT network is created. |
| Frontend changes are not visible | Run `npm run build` and rebuild `netrave`. |
| Service does not start | Check `journalctl -u netrave` or `netrave logs`. |
| Unknown init system | Pass `--init systemd`, `--init openrc` or `--init runit`. |

## Testing

```bash
go test ./...
cd frontend
npm run typecheck
npm test
npm run e2e   # needs a running server and NETRAVE_PASSWORD; tests are skipped without it
```

## Helper scripts in tools

The `tools/` directory holds diagnostic and test scripts that are not part of the
build:

- `instances-sql-check.py` checks every SQL query in the Go code against the
  current schema using an in-memory SQLite database.
- `l2-selftest.py` checks the L2 network model on a real Linux kernel without QEMU.
- `nat-shared-test.py` checks the shared NAT network: DHCP pool, group isolation
  and gateway access. Requires root, iproute2, nftables and dnsmasq.
- `live-cable-test.py` and `live_api_helper.py` test cables through the HTTP API
  of a running server. The address comes from `NETRAVE_URL`.
- `net-diagnose.sh` prints bridges, TAP interfaces, nftables rules, dnsmasq
  processes and the network interfaces of running QEMU instances. Run as root.

## Contributing

1. Create a focused branch.
2. Keep API contracts and permissions compatible.
3. Add tests for new behavior.
4. Run backend and frontend checks before opening a pull request.
5. Update both README files when user-visible behavior changes.

## Code authorship

The code in this repository was generated with DeepSeek v4.1 Flash and Claude
(Haiku and Sonnet models).

## License

Copyright (C) 2026 CyberF1nger67 and SlaysBro

GPL-3.0-only. See [LICENSE](LICENSE). Third-party components and their license
texts are listed in [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md).

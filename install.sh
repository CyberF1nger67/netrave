#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
#
# install.sh: installs netrave from source on Linux (systemd, OpenRC or runit).
#
# What it does:
#   1. Installs missing system packages: QEMU, iproute2, nftables, dnsmasq, curl.
#      The system is not upgraded and existing packages are left alone.
#   2. Checks Go and Node.js. Suitable system versions are used as they are.
#      Otherwise the official archives are installed into /opt/netrave/toolchain;
#      no other system files are changed.
#   3. Builds the frontend (npm) and the Go binary that embeds it.
#   4. Runs `netrave install`, which writes the configuration, creates the
#      directories and installs and starts the service. Then it prints the
#      administrator credentials that the service wrote to its log.
#
# Re-running is safe: an existing configuration is never overwritten, and a
# running service is restarted with the new binary.

set -Eeuo pipefail

readonly TOOLCHAIN_DIR="${NETRAVE_TOOLCHAIN_DIR:-/opt/netrave/toolchain}"
REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly REPO_DIR
# sbin directories are needed to find nft and dnsmasq when the script is not run as root.
export PATH="$PATH:/usr/local/sbin:/usr/sbin:/sbin"

# Must match installer.DefaultConfigPath.
readonly CONFIG_FILE="/etc/netrave/config.yaml"
readonly LOG_FILE_DEFAULT="/var/log/netrave/netrave.log"
LOG_FILE="$LOG_FILE_DEFAULT"
# The service logs this line once, when it creates the first administrator.
readonly ADMIN_LOG_MARKER="Created administrator: admin / "

# The log file can be moved by the configuration. This is used after the
# service has been installed and on re-installs with a custom log_file.
configured_log_file() {
  local path
  path="$($SUDO sed -n 's/^log_file:[[:space:]]*"\{0,1\}\([^"[:space:],]*\).*/\1/p' "$CONFIG_FILE" 2>/dev/null | sed -n '1p' || true)"
  printf '%s\n' "${path:-$LOG_FILE_DEFAULT}"
}

ADDR=":8080"
ADDR_SET=0
INIT_OVERRIDE=""
SKIP_DEPS=0
ASSUME_YES=0
ACTION="install"
PURGE=0
SUDO=""
OS_ID="unknown"
OS_LIKE=""
OS_PRETTY="Linux"
ACTIVE_ADDR=""
ADMIN_PASSWORD=""

log()  { printf '==> %s\n' "$*"; }
note() { printf '    %s\n' "$*"; }
warn() { printf '[!] %s\n' "$*" >&2; }
die()  { printf '[x] %s\n' "$*" >&2; exit 1; }

trap 'printf "[x] Installation aborted at line %s (exit code %s). Fix the cause and run the script again; re-running is safe.\n" "$LINENO" "$?" >&2' ERR

usage() {
  cat <<'USAGE'
Usage: sudo ./install.sh [options]

  (no options)             install or update netrave
  --addr HOST:PORT         HTTP address of the service (default: :8080)
  --init systemd|openrc|runit
                           init system to use (normally detected automatically)
  --no-deps                do not install system packages or download a toolchain
  -y, --yes                do not ask for confirmation
  --uninstall              remove the service and the binary, keep the data
  --uninstall --purge      also remove the configuration, database and labs
  -h, --help               show this help
USAGE
}

parse_args() {
  while [ $# -gt 0 ]; do
    case "$1" in
      --addr)      [ $# -ge 2 ] || die "--addr requires a value"; ADDR="$2"; ADDR_SET=1; shift 2 ;;
      --addr=*)    ADDR="${1#*=}"; ADDR_SET=1; shift ;;
      --init)      [ $# -ge 2 ] || die "--init requires a value"; INIT_OVERRIDE="$2"; shift 2 ;;
      --init=*)    INIT_OVERRIDE="${1#*=}"; shift ;;
      --no-deps)   SKIP_DEPS=1; shift ;;
      -y|--yes)    ASSUME_YES=1; shift ;;
      --uninstall) ACTION="uninstall"; shift ;;
      --purge)     PURGE=1; shift ;;
      -h|--help)   usage; exit 0 ;;
      *)           die "unknown option: $1 (see --help)" ;;
    esac
  done
  case "$INIT_OVERRIDE" in
    ""|systemd|openrc|runit) ;;
    *) die "--init: use systemd, openrc or runit" ;;
  esac
  case "$ADDR" in
    *:*) ;;
    *) die "--addr must be HOST:PORT or :PORT" ;;
  esac
  if [ "$PURGE" -eq 1 ] && [ "$ACTION" != "uninstall" ]; then
    die "--purge can only be used together with --uninstall"
  fi
}

require_privileges() {
  if [ "$(id -u)" -ne 0 ]; then
    command -v sudo >/dev/null 2>&1 || die "root privileges are required: run the script with sudo"
    SUDO="sudo"
  fi
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "command '$1' not found (install it, or do not use --no-deps)"
}

# ask "question": returns success only on an explicit yes; the default is no.
ask() {
  local answer
  read -r -p "$1 [y/N] " answer || return 1
  case "$answer" in
    y|Y|yes|YES) return 0 ;;
    *) return 1 ;;
  esac
}

prepend_path() {
  PATH="$1:$PATH"
  export PATH
}

read_os_release() {
  if [ -r /etc/os-release ]; then
    OS_ID="$(. /etc/os-release && echo "${ID:-unknown}")"
    OS_LIKE="$(. /etc/os-release && echo "${ID_LIKE:-}")"
    OS_PRETTY="$(. /etc/os-release && echo "${PRETTY_NAME:-Linux}")"
  fi
}

pkg_family() {
  case " $OS_ID $OS_LIKE " in
    *" debian "* | *" ubuntu "*)              echo apt ;;
    *" fedora "* | *" rhel "* | *" centos "*) echo dnf ;;
    *" arch "*)                               echo pacman ;;
    *" void "*)                               echo xbps ;;
    *" alpine "*)                             echo apk ;;
    *)                                        echo unknown ;;
  esac
}

is_musl() {
  compgen -G '/lib/ld-musl-*' >/dev/null 2>&1
}

# Returns 0 if version $1 >= version $2 (numeric-aware comparison).
version_ge() {
  [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" = "$2" ]
}

go_required() {
  awk '$1 == "go" { print $2; exit }' "$REPO_DIR/go.mod"
}

go_version_of() {
  GOTOOLCHAIN=local "$1" env GOVERSION 2>/dev/null | sed 's/^go//' || true
}

goarch() {
  case "$(uname -m)" in
    x86_64|amd64)   echo amd64 ;;
    aarch64|arm64)  echo arm64 ;;
    *)              return 1 ;;
  esac
}

nodearch() {
  case "$(uname -m)" in
    x86_64|amd64)   echo x64 ;;
    aarch64|arm64)  echo arm64 ;;
    *)              return 1 ;;
  esac
}

node_version_of() {
  "$1" -v 2>/dev/null | sed 's/^v//' || true
}

# Vite 7 requires Node.js ^20.19.0 || >=22.12.0.
node_ok() {
  local major minor
  IFS=. read -r major minor _ <<<"$1"
  [ -n "$major" ] || return 1
  if [ "$major" -eq 20 ]; then
    [ "${minor:-0}" -ge 19 ]
  elif [ "$major" -eq 22 ]; then
    [ "${minor:-0}" -ge 12 ]
  else
    [ "$major" -gt 22 ]
  fi
}

# retry runs a command again after a failure: a dropped connection or a partial download
# is the usual cause, and every step here is safe to repeat.
retry() {
  local attempt=1
  until "$@"; do
    [ "$attempt" -lt 4 ] || return 1
    warn "attempt $attempt failed, retrying in $((attempt * 5)) s: $*"
    sleep $((attempt * 5))
    attempt=$((attempt + 1))
  done
}

# qemu_arch returns the QEMU target of this machine. KVM runs only guests of the host's own
# architecture, so an aarch64 host gets the aarch64 emulator and firmware, and x86_64 gets the x86 ones.
qemu_arch() {
  case "$(uname -m)" in
    x86_64|amd64)   echo x86_64 ;;
    aarch64|arm64)  echo aarch64 ;;
    *)              return 1 ;;
  esac
}

# Package names for the host architecture (families: apt, dnf, pacman, xbps, apk).
# Each list contains the QEMU system emulator, the UEFI firmware, qemu-img, the network
# tools, dnsmasq and the tools used by this script.
lab_packages() {
  local family="$1" arch
  arch="$(qemu_arch)" || return 1
  case "$family:$arch" in
    apt:x86_64)      echo "sudo qemu-system-x86 qemu-utils ovmf iproute2 nftables dnsmasq-base curl ca-certificates xz-utils tar" ;;
    apt:aarch64)     echo "sudo qemu-system-arm qemu-utils qemu-efi-aarch64 iproute2 nftables dnsmasq-base curl ca-certificates xz-utils tar" ;;
    dnf:x86_64)      echo "sudo qemu-system-x86 qemu-img edk2-ovmf iproute nftables dnsmasq curl ca-certificates xz tar" ;;
    dnf:aarch64)     echo "sudo qemu-system-aarch64 qemu-img edk2-aarch64 iproute nftables dnsmasq curl ca-certificates xz tar" ;;
    pacman:x86_64)   echo "sudo qemu-base qemu-system-x86 qemu-img edk2-ovmf iproute2 nftables dnsmasq curl ca-certificates xz tar" ;;
    pacman:aarch64)  echo "sudo qemu-system-aarch64 qemu-img edk2-armvirt iproute2 nftables dnsmasq curl ca-certificates xz tar" ;;
    xbps:x86_64)     echo "sudo qemu edk2-ovmf iproute2 nftables dnsmasq curl ca-certificates xz tar" ;;
    xbps:aarch64)    echo "sudo qemu edk2-aarch64 iproute2 nftables dnsmasq curl ca-certificates xz tar" ;;
    apk:x86_64)      echo "sudo qemu-system-x86_64 qemu-img ovmf iproute2 nftables dnsmasq curl ca-certificates xz tar" ;;
    # Alpine ships the aarch64 firmware in 'edk2' (QEMU_CODE.fd, see qemu/args.go) and the virtio-gpu
    # module in its own package: without it qemu rejects '-device virtio-gpu-pci'.
    apk:aarch64)     echo "sudo qemu-system-aarch64 qemu-hw-display-virtio-gpu-pci qemu-img edk2 iproute2 nftables dnsmasq curl ca-certificates xz tar" ;;
    *)               return 1 ;;
  esac
}

# Installs the packages in two steps: first everything is downloaded, then installed from the
# downloaded files. A failed download therefore stops the script before anything is changed.
install_system_packages() {
  local family="$1" pkgs
  pkgs="$(lab_packages "$family")" || die "no package list for $family on $(uname -m)"
  case "$family" in
    apt)
      retry $SUDO env DEBIAN_FRONTEND=noninteractive apt-get update
      retry $SUDO env DEBIAN_FRONTEND=noninteractive apt-get install -y --download-only $pkgs
      $SUDO env DEBIAN_FRONTEND=noninteractive apt-get install -y $pkgs
      ;;
    dnf)
      retry $SUDO dnf install -y $pkgs
      ;;
    pacman)
      retry $SUDO pacman -Sw --noconfirm $pkgs \
        || die "pacman could not download the packages. If the package database is outdated, run 'pacman -Syu' (partial upgrades on Arch are not supported)"
      $SUDO pacman -S --needed --noconfirm $pkgs
      ;;
    xbps)
      retry $SUDO xbps-install -Sy $pkgs
      ;;
    apk)
      retry $SUDO apk update
      retry $SUDO apk add --no-cache $pkgs
      ;;
    *)
      return 1
      ;;
  esac
  verify_lab_tools
}

install_go_toolchain() {
  local version="$1" arch file tmp expected actual stage
  arch="$(goarch)" || die "no official Go archive for $(uname -m): install Go $version manually"
  file="go${version}.linux-${arch}.tar.gz"
  tmp="$(mktemp -d)"
  log "Downloading Go $version ($arch) from dl.google.com"
  curl -fsSL --retry 3 -o "$tmp/$file" "https://dl.google.com/go/$file"
  curl -fsSL --retry 3 -o "$tmp/$file.sha256" "https://dl.google.com/go/$file.sha256"
  expected="$(awk '{ print $1; exit }' "$tmp/$file.sha256")"
  actual="$(sha256sum "$tmp/$file" | awk '{ print $1 }')"
  [ -n "$expected" ] && [ "$expected" = "$actual" ] \
    || die "Go checksum mismatch: the archive is corrupt or has been tampered with"
  $SUDO mkdir -p "$TOOLCHAIN_DIR"
  stage="$($SUDO mktemp -d "$TOOLCHAIN_DIR/.stage.XXXXXX")"
  $SUDO tar -xzf "$tmp/$file" -C "$stage"
  $SUDO rm -rf "$TOOLCHAIN_DIR/go-$version"
  $SUDO mv "$stage/go" "$TOOLCHAIN_DIR/go-$version"
  $SUDO rm -rf "$stage"
  $SUDO ln -sfn "go-$version" "$TOOLCHAIN_DIR/go"
  rm -rf "$tmp"
}

ensure_go() {
  local need have
  need="$(go_required)"
  [ -n "$need" ] || die "no Go version is set in go.mod"

  if command -v go >/dev/null 2>&1; then
    have="$(go_version_of "$(command -v go)")"
    if version_ge "$have" "$need"; then
      note "Go: $have (system)"
      return 0
    fi
  fi
  if [ -x "$TOOLCHAIN_DIR/go/bin/go" ]; then
    have="$(go_version_of "$TOOLCHAIN_DIR/go/bin/go")"
    if version_ge "$have" "$need"; then
      prepend_path "$TOOLCHAIN_DIR/go/bin"
      note "Go: $have ($TOOLCHAIN_DIR/go)"
      return 0
    fi
  fi

  need_cmd curl
  install_go_toolchain "$need"
  prepend_path "$TOOLCHAIN_DIR/go/bin"
  note "Go: $(go_version_of "$TOOLCHAIN_DIR/go/bin/go") (installed in $TOOLCHAIN_DIR/go)"
}

install_node_toolchain() {
  local arch base file tmp expected actual version
  arch="$(nodearch)" || die "no official Node.js archive for $(uname -m): install Node.js 22 manually"
  base="https://nodejs.org/dist/latest-v22.x"
  tmp="$(mktemp -d)"
  log "Downloading Node.js 22 LTS ($arch) from nodejs.org"
  curl -fsSL --retry 3 -o "$tmp/SHASUMS256.txt" "$base/SHASUMS256.txt"
  file="$(awk '{ print $2 }' "$tmp/SHASUMS256.txt" \
    | grep -E "^node-v[0-9.]+-linux-${arch}\.tar\.xz$" | sed -n '1p' || true)"
  [ -n "$file" ] || die "no Node.js archive for $arch found in SHASUMS256.txt"
  curl -fsSL --retry 3 -o "$tmp/$file" "$base/$file"
  expected="$(awk -v f="$file" '$2 == f { print $1 }' "$tmp/SHASUMS256.txt")"
  actual="$(sha256sum "$tmp/$file" | awk '{ print $1 }')"
  [ -n "$expected" ] && [ "$expected" = "$actual" ] \
    || die "Node.js checksum mismatch: the archive is corrupt or has been tampered with"
  version="$(echo "$file" | sed -E 's/^node-(v[0-9.]+)-linux-.*$/\1/')"
  $SUDO mkdir -p "$TOOLCHAIN_DIR/node-$version"
  $SUDO tar -xJf "$tmp/$file" -C "$TOOLCHAIN_DIR/node-$version" --strip-components=1
  $SUDO ln -sfn "node-$version" "$TOOLCHAIN_DIR/node"
  rm -rf "$tmp"
}

ensure_node() {
  local have
  if command -v node >/dev/null 2>&1 && command -v npm >/dev/null 2>&1; then
    have="$(node_version_of "$(command -v node)")"
    if node_ok "$have"; then
      note "Node.js: v$have (system)"
      return 0
    fi
  fi
  if [ -x "$TOOLCHAIN_DIR/node/bin/node" ]; then
    have="$(node_version_of "$TOOLCHAIN_DIR/node/bin/node")"
    if node_ok "$have"; then
      prepend_path "$TOOLCHAIN_DIR/node/bin"
      note "Node.js: v$have ($TOOLCHAIN_DIR/node)"
      return 0
    fi
  fi

  if is_musl; then
    # Official Node.js binaries are built for glibc and do not run on musl (Alpine).
    [ "$SKIP_DEPS" -eq 0 ] || die "Node.js 20.19+ or 22.12+ not found (remove --no-deps or install it manually)"
    log "Installing Node.js from the Alpine repository (musl)"
    $SUDO apk add --no-cache nodejs npm
    have="$(node_version_of "$(command -v node)")"
    node_ok "$have" || die "the Alpine repository provides Node.js v$have, but 20.19+ or 22.12+ is required"
    note "Node.js: v$have (from the Alpine repository)"
    return 0
  fi

  need_cmd curl
  need_cmd tar
  need_cmd xz
  install_node_toolchain
  prepend_path "$TOOLCHAIN_DIR/node/bin"
  note "Node.js: $(node_version_of "$TOOLCHAIN_DIR/node/bin/node") (installed in $TOOLCHAIN_DIR/node)"
}

# verify_lab_tools stops the script when a required tool is missing after the package step.
verify_lab_tools() {
  local tool missing=""
  for tool in "qemu-system-$(qemu_arch)" qemu-img ip nft dnsmasq curl tar xz; do
    command -v "$tool" >/dev/null 2>&1 || missing="$missing $tool"
  done
  [ -z "$missing" ] || die "after the package installation these are still missing:$missing"
}

check_lab_tools() {
  local tool missing=""
  for tool in "qemu-system-$(qemu_arch)" qemu-img ip nft dnsmasq; do
    command -v "$tool" >/dev/null 2>&1 || missing="$missing $tool"
  done
  if [ -n "$missing" ]; then
    warn "not found:$missing. Networking and VMs will not work until they are installed (diagnostics: netrave doctor)"
  fi
}

# fetch_sources downloads everything the build needs before the build starts: Go modules and
# npm packages. The build itself then runs without network access.
fetch_sources() {
  log "Downloading Go modules and npm packages"
  retry run_as_builder go -C "$REPO_DIR" mod download
  retry run_as_builder npm --prefix "$REPO_DIR/frontend" ci --no-audit --no-fund
}

# Runs a command as the user who invoked sudo, so the build does not leave
# root-owned files in the repository or in the npm and Go caches.
run_as_builder() {
  if [ "$(id -u)" -eq 0 ] && [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ]; then
    sudo -u "$SUDO_USER" -H env "PATH=$PATH" GOTOOLCHAIN=local "$@"
  else
    env GOTOOLCHAIN=local "$@"
  fi
}

build_frontend() {
  log "Building frontend (npm run build)"
  (
    cd "$REPO_DIR/frontend"
    run_as_builder npm run build
  )
  [ -f "$REPO_DIR/internal/web/static/index.html" ] \
    || die "frontend build failed: internal/web/static/index.html is missing"
}

build_backend() {
  local version commit date
  version="$(run_as_builder git -C "$REPO_DIR" describe --tags --always --dirty 2>/dev/null || echo dev)"
  commit="$(run_as_builder git -C "$REPO_DIR" rev-parse HEAD 2>/dev/null || echo unknown)"
  date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  log "Building netrave binary ($version)"
  (
    cd "$REPO_DIR"
    run_as_builder go build -trimpath \
      -ldflags "-s -w -X netrave/internal/version.Version=$version -X netrave/internal/version.Commit=$commit -X netrave/internal/version.Date=$date" \
      -o netrave .
  )
  [ -x "$REPO_DIR/netrave" ] || die "the netrave binary was not built"
}

# The address the service actually uses. An existing configuration is kept on
# re-install, so --addr does not change it.
effective_addr() {
  local configured
  configured="$($SUDO sed -n 's/^addr:[[:space:]]*"\{0,1\}\([^"[:space:],]*\).*/\1/p' "$CONFIG_FILE" 2>/dev/null | sed -n '1p' || true)"
  printf '%s\n' "${configured:-$ADDR}"
}

# Returns 0 if a service manager status string means the service is running.
is_active_status() {
  case "$1" in
    active|run:*|*"status: started"*) return 0 ;;
    *) return 1 ;;
  esac
}

install_service() {
  local args=(install --addr "$ADDR") output status
  if [ -n "$INIT_OVERRIDE" ]; then
    args+=(--init "$INIT_OVERRIDE")
  fi
  log "Installing binary, configuration and service"
  output="$($SUDO "$REPO_DIR/netrave" "${args[@]}")"
  printf '%s\n' "$output" | sed 's/^/    /'
  LOG_FILE="$(configured_log_file)"

  status="$(printf '%s\n' "$output" | sed -n 's/^status: //p' | sed -n '1p')"
  if ! is_active_status "$status"; then
    warn "the service is not running (status: ${status:-unknown}). Last log lines:"
    tail -n 30 "$LOG_FILE" 2>/dev/null || true
    die "installation failed: check 'netrave logs' or 'journalctl -u netrave'"
  fi

  if [ "$ADDR_SET" -eq 1 ] && [ "$(effective_addr)" != "$ADDR" ]; then
    warn "existing configuration kept, --addr $ADDR was not applied. Address from $CONFIG_FILE: $(effective_addr)"
  fi
  ACTIVE_ADDR="$(effective_addr)"
}

# Prints the administrator password from the log, or nothing if it is not there yet.
read_admin_password() {
  $SUDO awk -v m="$ADMIN_LOG_MARKER" '
    index($0, m) { pw = substr($0, index($0, m) + length(m)) }
    END { sub(/[ \t\r]+$/, "", pw); if (pw != "") print pw }
  ' "$LOG_FILE" 2>/dev/null || true
}

# Waits for the service to write the generated password to the log.
wait_for_admin_password() {
  local i
  for i in $(seq 1 30); do
    ADMIN_PASSWORD="$(read_admin_password)"
    [ -n "$ADMIN_PASSWORD" ] && return 0
    sleep 1
  done
  return 1
}

web_url() {
  local host port
  port="${ACTIVE_ADDR##*:}"
  host="${ACTIVE_ADDR%:*}"
  case "$host" in
    ""|0.0.0.0|"[::]"|"::") host="localhost" ;;
  esac
  printf 'http://%s:%s/\n' "$host" "$port"
}

print_summary() {
  printf '\nDone: netrave is installed and running.\n'
  note "Web interface:  $(web_url)"
  note "Status:         netrave status"
  note "Logs:           netrave logs -n 100  (systemd: journalctl -u netrave -f)"
  note "Update:         run sudo ./install.sh again (the configuration is kept)"
  note "Uninstall:      sudo ./install.sh --uninstall"
  printf '\nAdministrator credentials:\n'
  if [ -n "$ADMIN_PASSWORD" ]; then
    note "Login:     admin"
    note "Password:  $ADMIN_PASSWORD"
    note "Save this password and change it after the first login."
  else
    warn "the administrator password was not found in $LOG_FILE"
  fi
}

uninstall_netrave() {
  local bin args=(uninstall)
  if [ -x /usr/local/bin/netrave ]; then
    bin="/usr/local/bin/netrave"
  elif [ -x "$REPO_DIR/netrave" ]; then
    bin="$REPO_DIR/netrave"
  else
    die "netrave was not found: nothing to uninstall"
  fi
  if [ -n "$INIT_OVERRIDE" ]; then
    args+=(--init "$INIT_OVERRIDE")
  fi
  if [ "$PURGE" -eq 1 ]; then
    warn "--purge will delete the configuration, database, VM images and logs (/etc/netrave, /var/lib/netrave, /var/log/netrave)"
    if [ "$ASSUME_YES" -eq 0 ]; then
      [ -t 0 ] || die "--purge without a terminal requires -y"
      ask "Permanently delete all netrave data?" || die "cancelled"
    fi
    args+=(--purge --yes)
  fi
  log "Removing the service and program files"
  $SUDO "$bin" "${args[@]}"
  note "The toolchain in $TOOLCHAIN_DIR and the system packages were not removed."
}

main() {
  parse_args "$@"
  [ "$(uname -s)" = "Linux" ] || die "only Linux is supported"
  require_privileges
  read_os_release
  LOG_FILE="$(configured_log_file)"

  if [ "$ACTION" = "uninstall" ]; then
    uninstall_netrave
    return 0
  fi

  local f
  for f in go.mod main.go frontend/package.json; do
    [ -f "$REPO_DIR/$f" ] || die "run this script from the root of the netrave repository (file $f is missing)"
  done

  local family answer
  family="$(pkg_family)"
  log "Installing netrave from source"
  note "System:      $OS_PRETTY (family: $family, $(uname -m))"
  note "Directory:   $REPO_DIR"
  note "HTTP address: $ADDR"
  if [ "$SKIP_DEPS" -eq 1 ]; then
    note "Packages:    skipped (--no-deps)"
  else
    note "Packages:    install missing ones"
  fi
  if [ ! -e /dev/kvm ]; then
    warn "/dev/kvm is not available: VMs will run without hardware acceleration"
  fi
  if pgrep -f 'qemu-system-' >/dev/null 2>&1; then
    warn "virtual machines are running; restarting the service may interrupt them"
  fi

  if [ -t 0 ] && [ "$ASSUME_YES" -eq 0 ]; then
    printf 'Continue? [Y/n] '
    read -r answer || answer=""
    case "$answer" in
      n|N|no|NO) die "cancelled by the user" ;;
    esac
  fi

  if [ "$SKIP_DEPS" -eq 0 ]; then
    if [ "$family" = "unknown" ]; then
      warn "distribution ($OS_ID) is not supported automatically. Install manually: qemu-system-$(qemu_arch), qemu-img, OVMF or AAVMF firmware, ip, nft, dnsmasq, curl, xz, tar"
    else
      log "System packages"
      install_system_packages "$family"
    fi
  fi

  log "Go and Node.js"
  ensure_go
  ensure_node
  check_lab_tools
  fetch_sources

  build_frontend
  build_backend

  install_service

  # The password is read from the log on every run: after a fresh install the
  # service writes it on first start, and on updates or reinstalls the line
  # written by the earlier installation is still there.
  log "Waiting for the administrator account"
  if ! wait_for_admin_password; then
    warn "the password was not written to $LOG_FILE within 30 seconds"
  fi
  print_summary
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  main "$@"
fi

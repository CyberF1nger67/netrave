// SPDX-License-Identifier: GPL-3.0-only

// Package platform detects the Linux distribution, init system and host
// capabilities. Distribution-specific data lives in small adapters so adding
// a new distribution does not require touching the installer or the daemon.
package platform

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"netrave/internal/hostarch"
)

// Family is a package-manager and layout family.
type Family string

const (
	FamilyDebian Family = "debian"
	FamilyRedHat Family = "redhat"
	FamilyVoid   Family = "void"
	FamilyAlpine Family = "alpine"
	FamilyArch   Family = "arch"
	FamilyOther  Family = "other"
)

// InitKind names a supported service manager.
type InitKind string

const (
	InitSystemd InitKind = "systemd"
	InitOpenRC  InitKind = "openrc"
	InitRunit   InitKind = "runit"
	InitUnknown InitKind = "unknown"
)

// Adapter describes one distribution. Adding a distribution means registering
// another Adapter; detection and install logic stay unchanged.
type Adapter struct {
	ID             string
	Name           string
	Family         Family
	PackageManager string
	Packages       map[string]string
	InitCandidates []InitKind
	ServiceRoot    string
}

var adapters = map[string]Adapter{}

func register(adapter Adapter) { adapters[adapter.ID] = adapter }

// qemuSystemPkg returns the package with the QEMU system emulator (or UEFI firmware) of the host
// architecture: x86_64 hosts get x86 packages, arm64 hosts get aarch64 packages.
func qemuSystemPkg(x86, arm string) string {
	if hostarch.QEMUArch() == "aarch64" {
		return arm
	}
	return x86
}

func init() {
	debian := func(id, name string) Adapter {
		return Adapter{ID: id, Name: name, Family: FamilyDebian, PackageManager: "apt-get",
			Packages: map[string]string{
				"qemu-system": qemuSystemPkg("qemu-system-x86", "qemu-system-arm"), "uefi": qemuSystemPkg("ovmf", "qemu-efi-aarch64"),
				"qemu-img": "qemu-utils", "ip": "iproute2",
				"nft": "nftables", "dnsmasq": "dnsmasq-base",
			}, InitCandidates: []InitKind{InitSystemd}, ServiceRoot: "/etc/systemd/system"}
	}
	register(debian("debian", "Debian"))
	register(debian("ubuntu", "Ubuntu"))

	redhatPackages := map[string]string{
		"qemu-system": qemuSystemPkg("qemu-system-x86", "qemu-system-aarch64"), "uefi": qemuSystemPkg("edk2-ovmf", "edk2-aarch64"),
		"qemu-img": "qemu-img", "ip": "iproute",
		"nft": "nftables", "dnsmasq": "dnsmasq",
	}
	register(Adapter{ID: "rhel", Name: "Red Hat Enterprise Linux", Family: FamilyRedHat, PackageManager: "dnf",
		Packages: redhatPackages, InitCandidates: []InitKind{InitSystemd}, ServiceRoot: "/etc/systemd/system"})
	register(Adapter{ID: "fedora", Name: "Fedora", Family: FamilyRedHat, PackageManager: "dnf",
		Packages: redhatPackages, InitCandidates: []InitKind{InitSystemd}, ServiceRoot: "/etc/systemd/system"})
	register(Adapter{ID: "rocky", Name: "Rocky Linux", Family: FamilyRedHat, PackageManager: "dnf",
		Packages: redhatPackages, InitCandidates: []InitKind{InitSystemd}, ServiceRoot: "/etc/systemd/system"})
	register(Adapter{ID: "almalinux", Name: "AlmaLinux", Family: FamilyRedHat, PackageManager: "dnf",
		Packages: redhatPackages, InitCandidates: []InitKind{InitSystemd}, ServiceRoot: "/etc/systemd/system"})

	register(Adapter{ID: "void", Name: "Void Linux", Family: FamilyVoid, PackageManager: "xbps-install",
		Packages: map[string]string{
			"qemu-system": "qemu", "uefi": qemuSystemPkg("edk2-ovmf", "edk2-aarch64"), "qemu-img": "qemu-tools", "ip": "iproute2",
			"nft": "nftables", "dnsmasq": "dnsmasq",
		}, InitCandidates: []InitKind{InitRunit, InitOpenRC}, ServiceRoot: "/etc/sv"})

	register(Adapter{ID: "alpine", Name: "Alpine Linux", Family: FamilyAlpine, PackageManager: "apk",
		Packages: map[string]string{
			"qemu-system": qemuSystemPkg("qemu-system-x86_64", "qemu-system-aarch64"), "uefi": qemuSystemPkg("ovmf", "edk2"),
			"qemu-img": "qemu-img", "ip": "iproute2",
			"nft": "nftables", "dnsmasq": "dnsmasq",
		}, InitCandidates: []InitKind{InitOpenRC, InitRunit}, ServiceRoot: "/etc/init.d"})

	arch := Adapter{ID: "arch", Name: "Arch Linux", Family: FamilyArch, PackageManager: "pacman",
		Packages: map[string]string{
			"qemu-system": qemuSystemPkg("qemu-system-x86", "qemu-system-aarch64"), "uefi": qemuSystemPkg("edk2-ovmf", "edk2-armvirt"),
			"qemu-img": "qemu-img", "ip": "iproute2",
			"nft": "nftables", "dnsmasq": "dnsmasq",
		}, InitCandidates: []InitKind{InitSystemd}, ServiceRoot: "/etc/systemd/system"}
	register(arch)
	register(Adapter{ID: "manjaro", Name: "Manjaro", Family: FamilyArch, PackageManager: "pacman",
		Packages: arch.Packages, InitCandidates: arch.InitCandidates, ServiceRoot: arch.ServiceRoot})
	register(Adapter{ID: "other", Name: "Linux", Family: FamilyOther, PackageManager: "",
		Packages: map[string]string{}, InitCandidates: []InitKind{InitSystemd, InitOpenRC, InitRunit}, ServiceRoot: "/etc"})
}

// AdapterFor returns the adapter for a distribution ID and falls back to the
// generic adapter.
func AdapterFor(id string) Adapter {
	if adapter, ok := adapters[id]; ok {
		return adapter
	}
	return adapters["other"]
}

// OSRelease is the parsed subset of /etc/os-release used by the project.
type OSRelease struct {
	ID        string `json:"id"`
	IDLike    string `json:"id_like"`
	Name      string `json:"name"`
	Pretty    string `json:"pretty_name"`
	VersionID string `json:"version_id"`
}

// Info is the complete host environment description.
type Info struct {
	OSRelease OSRelease         `json:"os_release"`
	Adapter   Adapter           `json:"adapter"`
	Family    Family            `json:"family"`
	Arch      string            `json:"arch"`
	Init      InitKind          `json:"init"`
	Root      bool              `json:"root"`
	KVM       bool              `json:"kvm"`
	IPForward bool              `json:"ip_forward"`
	Tools     map[string]bool   `json:"tools"`
	Warnings  []string          `json:"warnings"`
	Paths     map[string]string `json:"paths"`
}

// ParseOSRelease parses os-release data. The format is KEY=value with optional
// single or double quotes.
func ParseOSRelease(data []byte) OSRelease {
	values := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
		values[strings.TrimSpace(key)] = value
	}
	return OSRelease{
		ID: values["ID"], IDLike: values["ID_LIKE"], Name: values["NAME"],
		Pretty: values["PRETTY_NAME"], VersionID: values["VERSION_ID"],
	}
}

// ReadOSRelease reads <root>/etc/os-release.
func ReadOSRelease(root string) (OSRelease, error) {
	base := root
	if base == "" {
		base = "/"
	}
	path := filepath.Join(base, "etc", "os-release")
	data, err := os.ReadFile(path)
	if err != nil {
		return OSRelease{}, fmt.Errorf("read %s: %w", path, err)
	}
	return ParseOSRelease(data), nil
}

// DetectInit determines the init system using the live filesystem or an
// explicit override. root is empty for the live system.
func DetectInit(root, override string) InitKind {
	base := root
	if base == "" {
		base = "/"
	}
	if override != "" {
		kind := InitKind(strings.ToLower(override))
		switch kind {
		case InitSystemd, InitOpenRC, InitRunit:
			return kind
		default:
			return InitUnknown
		}
	}
	exists := func(path string) bool {
		_, err := os.Stat(filepath.Join(base, path))
		return err == nil
	}
	if exists("run/systemd/system") || (exists("usr/bin/systemctl") && exists("etc/systemd/system")) {
		return InitSystemd
	}
	if exists("sbin/openrc-run") || exists("sbin/rc-service") || (exists("etc/init.d") && exists("sbin/rc-update")) {
		return InitOpenRC
	}
	if exists("etc/runit") || exists("sbin/runit") || exists("usr/bin/sv") || exists("etc/sv") {
		return InitRunit
	}
	return InitUnknown
}

// Detect inspects the host and returns a complete Info value.
func Detect(initOverride string) (Info, error) {
	release, err := ReadOSRelease("")
	if err != nil {
		return Info{}, err
	}
	adapter := AdapterFor(release.ID)
	if adapter.ID == "other" && release.IDLike != "" {
		for _, id := range strings.Fields(release.IDLike) {
			if candidate, ok := adapters[id]; ok {
				adapter = candidate
				break
			}
		}
	}
	kind := DetectInit("", initOverride)
	if kind == InitUnknown {
		for _, candidate := range adapter.InitCandidates {
			if candidate != "" && initAvailable("", candidate) {
				kind = candidate
				break
			}
		}
	}
	info := Info{
		OSRelease: release,
		Adapter:   adapter,
		Family:    adapter.Family,
		Arch:      runtime.GOARCH,
		Init:      kind,
		Root:      os.Geteuid() == 0,
		Tools:     map[string]bool{},
		Paths: map[string]string{
			"data": "/var/lib/netrave", "config": "/etc/netrave/config.yaml",
			"binary": "/usr/local/bin/netrave", "log": "/var/log/netrave/netrave.log",
			"socket": "/run/netrave/daemon.sock",
		},
	}
	for _, tool := range []string{hostarch.QEMUBinary(), "qemu-img", "ip", "nft", "dnsmasq"} {
		_, err := exec.LookPath(tool)
		info.Tools[tool] = err == nil
		if err != nil {
			info.Warnings = append(info.Warnings, fmt.Sprintf("%s is not installed", tool))
		}
	}
	if _, err := os.Stat("/dev/kvm"); err == nil {
		info.KVM = true
	} else {
		info.Warnings = append(info.Warnings, "/dev/kvm is unavailable")
	}
	if data, err := os.ReadFile("/proc/sys/net/ipv4/ip_forward"); err == nil && strings.TrimSpace(string(data)) == "1" {
		info.IPForward = true
	}
	if kind == InitUnknown {
		info.Warnings = append(info.Warnings, "init system was not detected; pass --init explicitly")
	}
	return info, nil
}

func initAvailable(root string, kind InitKind) bool {
	base := root
	if base == "" {
		base = "/"
	}
	exists := func(path string) bool {
		_, err := os.Stat(filepath.Join(base, path))
		return err == nil
	}
	switch kind {
	case InitSystemd:
		return exists("run/systemd/system")
	case InitOpenRC:
		return exists("sbin/openrc-run") || exists("sbin/rc-service")
	case InitRunit:
		return exists("etc/runit") || exists("usr/bin/sv") || exists("etc/sv")
	default:
		return false
	}
}

// InstallCommand returns a safe package-manager command hint for missing tools.
func (a Adapter) InstallCommand() string {
	if a.PackageManager == "" {
		return ""
	}
	packages := make([]string, 0, len(a.Packages))
	for _, pkg := range a.Packages {
		packages = append(packages, pkg)
	}
	switch a.Family {
	case FamilyArch:
		return "pacman -S " + strings.Join(packages, " ")
	case FamilyVoid:
		return "xbps-install -S " + strings.Join(packages, " ")
	case FamilyAlpine:
		return "apk add " + strings.Join(packages, " ")
	default:
		return a.PackageManager + " install " + strings.Join(packages, " ")
	}
}

// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"netrave/internal/instances"
	"netrave/internal/qemu"
)

type querier interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

type execer interface {
	querier
	Exec(query string, args ...any) (sql.Result, error)
}

// ---------- virtual machine parameters ----------

// Params - common QEMU machine parameters (for templates and devices).
type Params struct {
	Arch     string `json:"arch" yaml:"arch"`
	Machine  string `json:"machine" yaml:"machine"`
	CPU      int    `json:"cpu" yaml:"cpu"`
	RAMMB    int    `json:"ram_mb" yaml:"ram_mb"`
	NICs     int    `json:"nics" yaml:"nics"`
	NICModel string `json:"nic_model" yaml:"nic_model"`
	// Naming of network interfaces in the guest and in the topology: interface N is <IfaceType><IfaceStart+N>.
	IfaceType  string `json:"iface_type" yaml:"iface_type"`
	IfaceStart int    `json:"iface_start" yaml:"iface_start"`
	DiskBus    string `json:"disk_bus" yaml:"disk_bus"`
	BIOS       string `json:"bios" yaml:"bios"`
	Console    string `json:"console" yaml:"console"`
	ExtraArgs  string `json:"extra_args" yaml:"extra_args"`
}

type paramsPatch struct {
	Arch       *string `json:"arch"`
	Machine    *string `json:"machine"`
	CPU        *int    `json:"cpu"`
	RAMMB      *int    `json:"ram_mb"`
	NICs       *int    `json:"nics"`
	NICModel   *string `json:"nic_model"`
	IfaceType  *string `json:"iface_type"`
	IfaceStart *int    `json:"iface_start"`
	DiskBus    *string `json:"disk_bus"`
	BIOS       *string `json:"bios"`
	Console    *string `json:"console"`
	ExtraArgs  *string `json:"extra_args"`
}

func set(v ...string) map[string]bool {
	m := map[string]bool{}
	for _, s := range v {
		m[s] = true
	}
	return m
}

var (
	nicModels  = set("virtio-net-pci", "e1000", "e1000e", "rtl8139", "vmxnet3", "i82559er", "ne2k_pci")
	ifaceTypes = set("ens", "te", "ge")
	diskBuses  = set("virtio", "ide", "sata", "scsi")
	arches     = set("x86_64", "i386")
	tplTypes   = set("linux", "windows", "router", "switch", "firewall", "server", "generic")
	machineRe  = regexp.MustCompile(`^[a-z0-9_.-]{1,32}$`)
	nameRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$`)
	ifaceRe    = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,15}$`)
	suffixRe   = regexp.MustCompile(`-\d+$`)
	baseCleanR = regexp.MustCompile(`[^A-Za-z0-9_.]`)
)

// Normalize substitutes the default interface type (ens<start+N>) if it is not set.
func (p *Params) Normalize() {
	if p.IfaceType == "" {
		p.IfaceType = "ens"
	}
}

// Validate checks that the parameters are acceptable.
func (p *Params) Validate() error {
	p.Normalize()
	switch {
	case !arches[p.Arch]:
		return errors.New("неподдерживаемая архитектура")
	case !machineRe.MatchString(p.Machine):
		return errors.New("некорректный тип машины QEMU")
	case p.CPU < 1 || p.CPU > 64:
		return errors.New("CPU: от 1 до 64")
	case p.RAMMB < 64 || p.RAMMB > 524288:
		return errors.New("RAM: от 64 до 524288 МБ")
	case p.NICs < 1 || p.NICs > 32:
		return errors.New("сетевых интерфейсов: от 1 до 32")
	case !nicModels[p.NICModel]:
		return errors.New("неподдерживаемая модель сетевой карты")
	case !ifaceTypes[p.IfaceType]:
		return errors.New("тип сетевого интерфейса: ens, te или ge")
	case p.IfaceStart < -1 || p.IfaceStart > 31:
		return errors.New("начальный номер интерфейса: от -1 до 31 (-1 — сдвиг: nic0 = MGMT, nic1 = ge0)")
	case !diskBuses[p.DiskBus]:
		return errors.New("неподдерживаемый тип диска")
	case p.BIOS != "seabios" && p.BIOS != "uefi":
		return errors.New("BIOS: seabios или uefi")
	case p.Console != "serial" && p.Console != "vnc":
		return errors.New("консоль: serial или vnc")
	case len(p.ExtraArgs) > 512:
		return errors.New("дополнительные параметры QEMU: не более 512 символов")
	}
	for _, r := range p.ExtraArgs {
		if r < 32 || r == 127 {
			return errors.New("недопустимые символы в параметрах QEMU")
		}
	}
	if _, err := qemu.ParseExtraArgs(p.ExtraArgs); err != nil {
		return err
	}
	return nil
}

func (p *Params) apply(pp *paramsPatch) bool {
	changed := false
	if pp.Arch != nil {
		p.Arch, changed = *pp.Arch, true
	}
	if pp.Machine != nil {
		p.Machine, changed = *pp.Machine, true
	}
	if pp.CPU != nil {
		p.CPU, changed = *pp.CPU, true
	}
	if pp.RAMMB != nil {
		p.RAMMB, changed = *pp.RAMMB, true
	}
	if pp.NICs != nil {
		p.NICs, changed = *pp.NICs, true
	}
	if pp.NICModel != nil {
		p.NICModel, changed = *pp.NICModel, true
	}
	if pp.IfaceType != nil {
		p.IfaceType, changed = *pp.IfaceType, true
	}
	if pp.IfaceStart != nil {
		p.IfaceStart, changed = *pp.IfaceStart, true
	}
	if pp.DiskBus != nil {
		p.DiskBus, changed = *pp.DiskBus, true
	}
	if pp.BIOS != nil {
		p.BIOS, changed = *pp.BIOS, true
	}
	if pp.Console != nil {
		p.Console, changed = *pp.Console, true
	}
	if pp.ExtraArgs != nil {
		p.ExtraArgs, changed = *pp.ExtraArgs, true
	}
	return changed
}

// ---------- models ----------

type Lab struct {
	ID          int64           `json:"id"`
	OwnerID     int64           `json:"owner_id"`
	Owner       string          `json:"owner"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Canvas      json.RawMessage `json:"canvas"`
	CreatedAt   int64           `json:"created_at"`
	UpdatedAt   int64           `json:"updated_at"`
	Devices     int             `json:"devices"`
	Networks    int             `json:"networks"`
	PublicLevel string          `json:"public_level"`
	Perms       map[string]bool `json:"perms"`     // effective permissions of the current user (computed, not stored)
	FolderID    int64           `json:"folder_id"` // current user's folder (0 - root); filled in the list, not stored in labs
}

func validCanvas(b []byte) bool {
	t := strings.TrimSpace(string(b))
	return len(t) > 1 && len(t) <= 4096 && t[0] == '{' && json.Valid([]byte(t))
}

// Bounds of the machine icon side on the diagram; 0 means "default size".
const (
	MinIconSize = 24
	MaxIconSize = 256
)

// validIconSize accepts 0 (default) or a value within the allowed range.
func validIconSize(size int) bool {
	return size == 0 || (size >= MinIconSize && size <= MaxIconSize)
}

type Device struct {
	ID         int64   `json:"id"`
	LabID      int64   `json:"lab_id"`
	TemplateID *int64  `json:"template_id"`
	ImageID    *int64  `json:"image_id"`
	Name       string  `json:"name"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Params
	State      string `json:"state"`
	PID        int    `json:"pid"`
	VNCPort    int    `json:"vnc_port"`
	SerialPort int    `json:"serial_port"`
	LastError  string `json:"last_error"`
	Type       string `json:"type"`
	Icon       string `json:"icon"`        // selected machine picture: standard, custom (own) or empty - by template type
	CustomIcon string `json:"custom_icon"` // hash of the uploaded icon (empty - none)
	IconURL    string `json:"icon_url"`    // URL of the custom icon for the web interface; empty - standard
	// IconSize - the machine icon side on the diagram in world units; 0 - default size.
	IconSize int   `json:"icon_size"`
	RID      int64 `json:"rid"` // machine row in the instance: processes and consoles are started from it
	// Pending - parameters changed in settings but not applied: the machine is running with other values.
	// They will be applied on the next start (restart) of the machine.
	Pending []string `json:"pending"`
	// IPv4 - network interface state by NIC number for the editor diagram (see Handler.NICState).
	IPv4 map[string]int `json:"ipv4,omitempty"`
}

// pendingFields lists parameters changed in settings but not applied yet: the process is running
// with other values. For a stopped machine the list is empty.
func pendingFields(d *Device, applied string) []string {
	if applied == "" {
		return nil
	}
	var a instances.Launch
	if json.Unmarshal([]byte(applied), &a) != nil {
		return nil
	}
	var out []string
	add := func(changed bool, key string) {
		if changed {
			out = append(out, key)
		}
	}
	add(a.Machine != d.Machine, "machine")
	add(a.CPU != d.CPU, "cpu")
	add(a.RAMMB != d.RAMMB, "ram_mb")
	add(a.NICs != d.NICs, "nics")
	add(a.NICModel != d.NICModel, "nic_model")
	add(a.DiskBus != d.DiskBus, "disk_bus")
	add(a.BIOS != d.BIOS, "bios")
	add(a.Console != d.Console, "console")
	add(a.ExtraArgs != d.ExtraArgs, "extra_args")
	return out
}

type Network struct {
	ID         int64   `json:"id"`
	LabID      int64   `json:"lab_id"`
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Subnet     string  `json:"subnet"`
	Gateway    string  `json:"gateway"`
	DHCP       bool    `json:"dhcp_enabled"`
	DHCPStart  string  `json:"dhcp_start"`
	DHCPEnd    string  `json:"dhcp_end"`
	HostIface  string  `json:"host_iface"`
	BridgeName string  `json:"bridge_name"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
}

type Conn struct {
	ID    int64  `json:"id"`
	LabID int64  `json:"lab_id"`
	AType string `json:"a_type"`
	AID   int64  `json:"a_id"`
	AIf   int    `json:"a_if"`
	BType string `json:"b_type"`
	BID   int64  `json:"b_id"`
	BIf   int    `json:"b_if"`
	Up    bool   `json:"up"` // wire enabled in the instance (a disabled wire does not carry traffic)
	// Warnings - what could not be applied to running machines when the wire was created (the wire is saved).
	Warnings []string `json:"warnings,omitempty"`
}

type Topology struct {
	Devices     []Device
	Networks    []Network
	Connections []Conn
}

// ---------- names ----------

func baseName(s string) string {
	s = suffixRe.ReplaceAllString(strings.TrimSpace(s), "")
	s = baseCleanR.ReplaceAllString(s, "")
	if len(s) > 24 {
		s = s[:24]
	}
	if s == "" || !regexp.MustCompile(`^[A-Za-z0-9]`).MatchString(s) {
		s = "node"
	}
	return s
}

// uniqueName picks a name of the form base-N that is not in names and occupies it.
func uniqueName(names map[string]bool, base string) string {
	base = baseName(base)
	for n := 1; ; n++ {
		c := fmt.Sprintf("%s-%d", base, n)
		if !names[c] {
			names[c] = true
			return c
		}
	}
}

// DeviceIcons - allowed machine pictures: standard icons from static/img
// (SVG files with the same names), custom - an own uploaded icon (see CustomIcon).
// An empty string - automatically by template type. The set must match
// STANDARD_ICONS in frontend/src/lib/deviceIcon.ts.
var DeviceIcons = map[string]bool{
	"": true, "custom": true,
	"pc": true, "workstation": true, "laptop": true, "monitor": true,
	"server": true, "server-linux": true,
	"switch": true, "router": true, "firewall": true, "cloud": true,
}

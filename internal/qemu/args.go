// SPDX-License-Identifier: GPL-3.0-only

package qemu

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"netrave/internal/hostarch"
	"netrave/internal/msgs"
)

// DiskSpec is one device disk: a file from the image folder (read-only) and the device's working layer on top of it.
type DiskSpec struct {
	Base    string // path to the disk file in the image folder
	BaseFmt string // qcow2 | raw
	Overlay string // device working disk (qcow2 with Base as backing file)
}

// Spec holds everything needed to start a single virtual machine.
type Spec struct {
	ID         int64  // rid: machine row in the instance (instance_devices.id) - key for process, TAP, ports, overlays
	DeviceID   string // machine name in the topology - used for wires and for the log file name
	InstanceID int64  // user's lab instance
	LabID      string // lab name
	Owner      string // login name of the instance owner - names the log directory
	Name       string
	Arch       string
	Machine    string
	CPU        int
	RAMMB      int
	NICs       int
	NICModel   string
	DiskBus    string
	BIOS       string
	Console    string
	ExtraArgs  string
	Disks      []DiskSpec // disks in hda, hdb, ... order
	CDROMs     []string   // ISO files from the image folder (read-only)
}

// Paths are the device files.
type Paths struct{ Dir, Vars, Log, QMP string }

// workPaths returns the working directory of a machine: <instances>/<owner>/<lab>/<device>/, where the disks
// live. Dir is empty when a name cannot be used as a path element.
func workPaths(instancesDir, owner, lab, device string) Paths {
	if !safeElement(owner) || !safeElement(lab) || !safeElement(device) {
		return Paths{}
	}
	dir := filepath.Join(instancesDir, owner, lab, device)
	return Paths{Dir: dir, Vars: filepath.Join(dir, "OVMF_VARS.fd")}
}

// LogFile returns the QEMU log of a machine: <logs>/<owner>/<lab>/<device>.log. It returns an empty string
// when a name cannot be used as a path element.
func LogFile(logsDir, owner, lab, device string) string {
	if !safeElement(owner) || !safeElement(lab) || !safeElement(device) {
		return ""
	}
	return filepath.Join(logsDir, owner, lab, device+".log")
}

// safeElement reports whether name is usable as one path element: not empty, not a relative link,
// and without separators.
func safeElement(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\\x00")
}

// DevicePaths returns the files of an instance machine described by s: working directory, OVMF variables,
// QEMU log and QMP socket. The QMP socket lives under the run directory.
func DevicePaths(instancesDir, logsDir, runDir string, s *Spec) Paths {
	p := workPaths(instancesDir, s.Owner, s.LabID, s.DeviceID)
	p.Log = LogFile(logsDir, s.Owner, s.LabID, s.DeviceID)
	p.QMP = filepath.Join(runDir, fmt.Sprintf("d%d.qmp", s.ID))
	return p
}

// Plumber connects VM network interfaces to the host (implemented by the network package).
type Plumber interface {
	Netdev(s *Spec, nic int) (string, error)
	Release(s *Spec)
	// DownNICs - machine interfaces with disconnected wires (for set_link up=false at startup).
	DownNICs(s *Spec) []int
}

// ---------- extra parameters ----------

var extraAllowed = map[string]bool{
	"-device": true, "-rtc": true, "-smbios": true, "-boot": true, "-global": true, "-vga": true, "-cpu": true,
	"-no-acpi": true, "-usb": true, "-no-hpet": true, "-k": true,
}
var extraNoValue = map[string]bool{"-no-acpi": true, "-usb": true, "-no-hpet": true}
var extraDeny = []string{"/", "file=", "path=", "drive=", "vfio", "usb-host", "ivshmem", "virtfs", "9p", "chardev", "serial", "pci-assign"}

// ParseExtraArgs parses user-supplied QEMU parameters without going through a shell.
func ParseExtraArgs(s string) ([]string, error) {
	toks := strings.Fields(s)
	var out []string
	for i := 0; i < len(toks); i++ {
		opt := toks[i]
		if !extraAllowed[opt] {
			return nil, msgs.New("QEMU parameter «{param}» is not allowed (allowed: -device -rtc -smbios -boot -global -vga -cpu -no-acpi -usb -no-hpet -k)", map[string]string{"param": opt})
		}
		out = append(out, opt)
		if extraNoValue[opt] {
			continue
		}
		i++
		if i >= len(toks) || strings.HasPrefix(toks[i], "-") {
			return nil, msgs.New("QEMU parameter {param} has no value", map[string]string{"param": opt})
		}
		low := strings.ToLower(toks[i])
		for _, bad := range extraDeny {
			if strings.Contains(low, bad) {
				return nil, msgs.New("Value «{value}» is not allowed for security reasons", map[string]string{"value": toks[i]})
			}
		}
		out = append(out, toks[i])
	}
	return out, nil
}

// displayDevice returns the graphics adapter for the VNC console. The x86 machines have a VGA
// adapter; the aarch64 "virt" machine has none, so a virtio GPU provides the framebuffer.
func displayDevice(arch string) []string {
	if arch == "aarch64" {
		return []string{"-device", "virtio-gpu-pci"}
	}
	return []string{"-vga", "std"}
}

// ---------- UEFI ----------

type OVMF struct{ Code, Vars, Single string }

// ovmfPairs lists the firmware files of the common distributions: OVMF on x86_64 and AAVMF
// (edk2 for ARM) on aarch64. Only the files of the host architecture exist, so the first
// matching pair is the right one.
var ovmfPairs = [][2]string{
	{"/usr/share/OVMF/OVMF_CODE_4M.fd", "/usr/share/OVMF/OVMF_VARS_4M.fd"},
	{"/usr/share/OVMF/OVMF_CODE.fd", "/usr/share/OVMF/OVMF_VARS.fd"},
	{"/usr/share/edk2/ovmf/OVMF_CODE.fd", "/usr/share/edk2/ovmf/OVMF_VARS.fd"},
	{"/usr/share/edk2-ovmf/x64/OVMF_CODE.fd", "/usr/share/edk2-ovmf/x64/OVMF_VARS.fd"},
	{"/usr/share/qemu/OVMF_CODE.fd", "/usr/share/qemu/OVMF_VARS.fd"},
	{"/usr/share/AAVMF/AAVMF_CODE.fd", "/usr/share/AAVMF/AAVMF_VARS.fd"},
	{"/usr/share/edk2/aarch64/QEMU_CODE.fd", "/usr/share/edk2/aarch64/QEMU_VARS.fd"},
	{"/usr/share/edk2-armvirt/aarch64/QEMU_CODE.fd", "/usr/share/edk2-armvirt/aarch64/QEMU_VARS.fd"},
}
var ovmfSingles = []string{"/usr/share/qemu/OVMF.fd", "/usr/share/ovmf/OVMF.fd", "/usr/share/OVMF/OVMF.fd"}

func FindOVMF() (*OVMF, error) {
	for _, p := range ovmfPairs {
		if fileExists(p[0]) && fileExists(p[1]) {
			return &OVMF{Code: p[0], Vars: p[1]}, nil
		}
	}
	for _, p := range ovmfSingles {
		if fileExists(p) {
			return &OVMF{Single: p}, nil
		}
	}
	return nil, errors.New("UEFI needs firmware: OVMF (x86_64) or AAVMF (aarch64), see install.sh")
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// ---------- command line assembly ----------

func esc(s string) string { return strings.ReplaceAll(s, ",", ",,") }

func MAC(devID int64, nic int) string {
	return fmt.Sprintf("52:54:%02x:%02x:%02x:%02x", byte(devID>>16), byte(devID>>8), byte(devID), byte(nic))
}

// diskArgs attaches all device disks. The first disk is the boot disk (with no disks, the CD-ROM boots).
func diskArgs(s *Spec) ([]string, error) {
	bus := s.DiskBus
	if bus == "ide" && strings.HasPrefix(s.Machine, "q35") {
		bus = "sata" // q35 has no IDE controller
	}
	n := len(s.Disks)
	var a []string
	switch bus {
	case "sata":
		if n > 6 {
			return nil, errors.New("No more than 6 disks per device are supported for SATA")
		}
		if n > 0 {
			a = append(a, "-device", "ahci,id=ahci0")
		}
	case "ide":
		if n > 4 {
			return nil, errors.New("No more than 4 disks per device are supported for IDE; choose virtio or SATA")
		}
	case "scsi":
		if n > 0 {
			a = append(a, "-device", "virtio-scsi-pci,id=scsi0")
		}
	}
	for i, d := range s.Disks {
		id := fmt.Sprintf("hd%d", i)
		boot := ""
		if i == 0 {
			boot = ",bootindex=1"
		}
		drv := "file=" + esc(d.Overlay) + ",format=qcow2,if=none,id=" + id + ",cache=writeback"
		switch bus {
		case "sata":
			a = append(a, "-drive", drv, "-device", fmt.Sprintf("ide-hd,drive=%s,bus=ahci0.%d%s", id, i, boot))
		case "ide":
			a = append(a, "-drive", drv, "-device", fmt.Sprintf("ide-hd,drive=%s,bus=ide.%d,unit=%d%s", id, i/2, i%2, boot))
		case "scsi":
			a = append(a, "-drive", drv, "-device", fmt.Sprintf("scsi-hd,drive=%s,bus=scsi0.0,scsi-id=%d%s", id, i, boot))
		default:
			a = append(a, "-drive", drv, "-device", fmt.Sprintf("virtio-blk-pci,drive=%s%s", id, boot))
		}
	}
	return a, nil
}

// cdromArgs attaches image folder ISO files as read-only CD-ROMs via a separate AHCI controller.
func cdromArgs(s *Spec) ([]string, error) {
	if len(s.CDROMs) == 0 {
		return nil, nil
	}
	if len(s.CDROMs) > 4 {
		return nil, errors.New("No more than 4 CD-ROMs per device are supported")
	}
	base := 1
	if len(s.Disks) > 0 {
		base = 2 // the first disk already has bootindex=1
	}
	a := []string{"-device", "ahci,id=ahcicd"}
	for i, path := range s.CDROMs {
		id := fmt.Sprintf("cd%d", i)
		a = append(a,
			"-drive", "file="+esc(path)+",format=raw,media=cdrom,readonly=on,if=none,id="+id,
			"-device", fmt.Sprintf("ide-cd,drive=%s,bus=ahcicd.%d,bootindex=%d", id, i, base+i))
	}
	return a, nil
}

// spareSlots is the reserve of PCIe slots for hot-plugging interfaces on q35.
const spareSlots = 8

func isQ35(machine string) bool { return strings.HasPrefix(machine, "q35") }

// nicSlotCount is how many pcie-root-port devices are created at startup (interfaces + hotplug reserve).
func nicSlotCount(nics int) int {
	n := nics + spareSlots
	if n < spareSlots {
		n = spareSlots
	}
	if n > 32 {
		n = 32
	}
	return n
}

// nicSlotName is the interface's PCIe slot id. On q35 the pcie.0 slot does not support
// hotplug, so each interface is placed on its own pcie-root-port.
func nicSlotName(nic int) string { return fmt.Sprintf("nicslot%d", nic) }

// nicSlot returns the interface slot for device_add (empty - machine without PCIe slots).
func nicSlot(s *Spec, nic int) string {
	if !isQ35(s.Machine) {
		return ""
	}
	return nicSlotName(nic)
}

// BuildArgs builds the qemu-system-* arguments from the device settings.
// normalizeForArch adapts a device to its architecture. Devices created before the architecture
// was stored have no Arch: they run on the host. On aarch64 the "virt" machine has no PC chipset
// (pc, q35), no legacy BIOS and no IDE controller, so the matching virt variants are used.
func normalizeForArch(s *Spec) {
	if s.Arch == "" {
		s.Arch = hostarch.QEMUArch()
	}
	if s.Arch != "aarch64" {
		return
	}
	if s.Machine == "" || strings.HasPrefix(s.Machine, "pc") || strings.HasPrefix(s.Machine, "q35") {
		s.Machine = "virt"
	}
	s.BIOS = "uefi"
	if s.DiskBus == "ide" {
		s.DiskBus = "sata"
	}
}

func BuildArgs(s *Spec, p Paths, vncDisplay, serialPort int, pl Plumber, ov *OVMF) ([]string, error) {
	normalizeForArch(s)
	extra, err := ParseExtraArgs(s.ExtraArgs)
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("netrave-d%d", s.ID)
	a := []string{
		"-name", name + ",process=" + name,
		"-machine", s.Machine + ",accel=kvm",
		"-cpu", "host",
		"-m", strconv.Itoa(s.RAMMB),
		"-smp", strconv.Itoa(s.CPU),
		"-uuid", fmt.Sprintf("00000000-0000-4000-8000-%012x", s.ID),
		"-rtc", "base=utc",
		"-nodefaults", "-no-user-config",
	}
	if s.BIOS == "uefi" && ov != nil {
		if ov.Single != "" {
			a = append(a, "-bios", ov.Single)
		} else {
			a = append(a,
				"-drive", "if=pflash,format=raw,unit=0,readonly=on,file="+esc(ov.Code),
				"-drive", "if=pflash,format=raw,unit=1,file="+esc(p.Vars))
		}
	}
	disks, err := diskArgs(s)
	if err != nil {
		return nil, err
	}
	a = append(a, disks...)
	cds, err := cdromArgs(s)
	if err != nil {
		return nil, err
	}
	a = append(a, cds...)
	if isQ35(s.Machine) {
		for i := 0; i < nicSlotCount(s.NICs); i++ {
			a = append(a, "-device", fmt.Sprintf("pcie-root-port,id=%s,bus=pcie.0,chassis=%d", nicSlotName(i), i+1))
		}
	}
	for i := 0; i < s.NICs; i++ {
		nd, err := pl.Netdev(s, i)
		if err != nil {
			return nil, msgs.Wrap(err, "Network of interface e{nic}: {detail}", map[string]string{"nic": fmt.Sprint(i), "detail": err.Error()})
		}
		// id=nic<N> - interface name for QMP set_link (link control without restarting the machine).
		// On q35 the interface sits on its own pcie-root-port so it can be removed via QMP.
		dev := fmt.Sprintf("%s,id=nic%d,netdev=net%d,mac=%s", s.NICModel, i, i, MAC(s.ID, i))
		if slot := nicSlot(s, i); slot != "" {
			dev += ",bus=" + slot
		}
		a = append(a, "-netdev", nd, "-device", dev)
	}
	a = append(a,
		"-display", "none",
		// lossy=on allows Tight-JPEG only if the client itself requested a quality level
		// (RFB pseudo-encoding). Without a client request QEMU stays lossless.
		"-vnc", fmt.Sprintf("127.0.0.1:%d,share=force-shared,lossy=on", vncDisplay),
		"-serial", fmt.Sprintf("tcp:127.0.0.1:%d,server,nowait", serialPort),
		"-qmp", "unix:"+esc(p.QMP)+",server,nowait",
	)
	a = append(a, displayDevice(s.Arch)...)
	a = append(a, extra...)
	return a, nil
}

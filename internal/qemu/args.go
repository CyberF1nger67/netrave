// SPDX-License-Identifier: GPL-3.0-only

package qemu

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DiskSpec is one device disk: a file from the image folder (read-only) and the device's working layer on top of it.
type DiskSpec struct {
	Base    string // path to the disk file in the image folder
	BaseFmt string // qcow2 | raw
	Overlay string // device working disk (qcow2 with Base as backing file)
}

// Spec holds everything needed to start a single virtual machine.
type Spec struct {
	ID         int64 // rid: machine row in the instance (instance_devices.id) - key for process, TAP, ports, overlays
	DeviceID   int64 // machine in the topology (devices.id) - used for wires
	InstanceID int64 // user's lab instance
	LabID      int64
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

func DevicePaths(labsDir, runDir string, labID, devID int64) Paths {
	dir := filepath.Join(labsDir, strconv.FormatInt(labID, 10), strconv.FormatInt(devID, 10))
	return Paths{
		Dir:  dir,
		Vars: filepath.Join(dir, "OVMF_VARS.fd"),
		Log:  filepath.Join(dir, "qemu.log"),
		QMP:  filepath.Join(runDir, fmt.Sprintf("d%d.qmp", devID)),
	}
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
			return nil, fmt.Errorf("параметр QEMU %q не разрешён (допустимы: -device -rtc -smbios -boot -global -vga -cpu -no-acpi -usb -no-hpet -k)", opt)
		}
		out = append(out, opt)
		if extraNoValue[opt] {
			continue
		}
		i++
		if i >= len(toks) || strings.HasPrefix(toks[i], "-") {
			return nil, fmt.Errorf("у параметра %s нет значения", opt)
		}
		low := strings.ToLower(toks[i])
		for _, bad := range extraDeny {
			if strings.Contains(low, bad) {
				return nil, fmt.Errorf("значение %q недопустимо для безопасности", toks[i])
			}
		}
		out = append(out, toks[i])
	}
	return out, nil
}

// ---------- UEFI ----------

type OVMF struct{ Code, Vars, Single string }

var ovmfPairs = [][2]string{
	{"/usr/share/OVMF/OVMF_CODE_4M.fd", "/usr/share/OVMF/OVMF_VARS_4M.fd"},
	{"/usr/share/OVMF/OVMF_CODE.fd", "/usr/share/OVMF/OVMF_VARS.fd"},
	{"/usr/share/edk2/ovmf/OVMF_CODE.fd", "/usr/share/edk2/ovmf/OVMF_VARS.fd"},
	{"/usr/share/edk2-ovmf/x64/OVMF_CODE.fd", "/usr/share/edk2-ovmf/x64/OVMF_VARS.fd"},
	{"/usr/share/qemu/OVMF_CODE.fd", "/usr/share/qemu/OVMF_VARS.fd"},
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
	return nil, errors.New("для UEFI нужен пакет OVMF (например: apt install ovmf)")
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
			return nil, errors.New("для SATA поддерживается не более 6 дисков на устройство")
		}
		if n > 0 {
			a = append(a, "-device", "ahci,id=ahci0")
		}
	case "ide":
		if n > 4 {
			return nil, errors.New("для IDE поддерживается не более 4 дисков; выберите virtio или SATA")
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
		return nil, errors.New("поддерживается не более 4 CD-ROM на устройство")
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
func BuildArgs(s *Spec, p Paths, vncDisplay, serialPort int, pl Plumber, ov *OVMF) ([]string, error) {
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
			return nil, fmt.Errorf("сеть интерфейса e%d: %w", i, err)
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
		"-vga", "std",
		"-display", "none",
		// lossy=on allows Tight-JPEG only if the client itself requested a quality level
		// (RFB pseudo-encoding). Without a client request QEMU stays lossless.
		"-vnc", fmt.Sprintf("127.0.0.1:%d,share=force-shared,lossy=on", vncDisplay),
		"-serial", fmt.Sprintf("tcp:127.0.0.1:%d,server,nowait", serialPort),
		"-qmp", "unix:"+esc(p.QMP)+",server,nowait",
	)
	a = append(a, extra...)
	return a, nil
}

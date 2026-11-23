// SPDX-License-Identifier: GPL-3.0-only

package qemu

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"netrave/internal/config"
	"netrave/internal/database"
	"netrave/internal/hostarch"
	"netrave/internal/instances"
	"netrave/internal/msgs"
)

var (
	ErrBusy     = errors.New("Another operation is already running on this device")
	ErrNotFound = errors.New("Device not found")
)

type vm struct {
	spec       *Spec
	pid        int
	cmd        *exec.Cmd // nil for machines whose process has already exited
	qmp        string
	done       chan struct{}
	mu         sync.Mutex
	expectStop bool
	cpuTicks   uint64
	cpuAt      time.Time
	cpuPct     float64
	cpuReady   bool // load measured from at least two samples

	// A machine's mutable state lives only in the server's memory. After a server
	// restart all machines are considered stopped (see Recover).
	state      string
	vncPort    int
	serialPort int
	lastError  string
}

// Runtime is a snapshot of a machine's mutable state for the API and other packages.
type Runtime struct {
	State      string `json:"state"`
	PID        int    `json:"pid"`
	VNCPort    int    `json:"vnc_port"`
	SerialPort int    `json:"serial_port"`
	LastError  string `json:"last_error"`
}

// Notifier sends notifications about machine events. The interface breaks the import cycle
// qemu → notify → console → qemu.
type Notifier interface {
	SendInstance(instanceID int64, level, category, title, body, link string, params ...map[string]string)
}

// Manager manages the lifecycle of QEMU processes.
type Manager struct {
	db  *database.DB
	cfg *config.Config
	pl  Plumber
	// Notify is the notification centre: startup errors, crashes and machine wipes (nil - not preserved).
	Notify Notifier
	mu     sync.Mutex
	vms    map[int64]*vm // running processes
	states map[int64]*vm // state of all known machines, including stopped ones
	locks  map[int64]*sync.Mutex
	portMu sync.Mutex
}

func New(db *database.DB, cfg *config.Config, pl Plumber) *Manager {
	return &Manager{db: db, cfg: cfg, pl: pl, vms: map[int64]*vm{}, states: map[int64]*vm{}, locks: map[int64]*sync.Mutex{}}
}

// runtimeVM returns a machine's state record, creating it on first access.
// Call with m.mu held.
func (m *Manager) runtimeVM(id int64) *vm {
	v := m.states[id]
	if v == nil {
		v = &vm{spec: &Spec{ID: id}, state: "stopped"}
		m.states[id] = v
	}
	return v
}

// State returns the machine's current state. An unknown machine is considered stopped.
func (m *Manager) State(id int64) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v := m.states[id]; v != nil && v.state != "" {
		return v.state
	}
	return "stopped"
}

// Runtime returns a snapshot of the machine's mutable state.
func (m *Manager) Runtime(id int64) Runtime {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := m.states[id]
	if v == nil || v.state == "" {
		return Runtime{State: "stopped"}
	}
	return Runtime{State: v.state, PID: v.pid, VNCPort: v.vncPort, SerialPort: v.serialPort, LastError: v.lastError}
}

// SetRuntime sets the in-memory snapshot of the machine's mutable state. The database stores only
// the start history, so the process state is restored by this method (including in tests).
func (m *Manager) SetRuntime(id int64, rt Runtime) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := m.runtimeVM(id)
	v.state, v.pid = rt.State, rt.PID
	v.vncPort, v.serialPort, v.lastError = rt.VNCPort, rt.SerialPort, rt.LastError
}

// Active reports that the machine is not stopped and not in an error state.
func (m *Manager) Active(id int64) bool {
	s := m.State(id)
	return s != "stopped" && s != "error"
}

// Running reports that the machine process is running (including paused).
func (m *Manager) Running(id int64) bool {
	s := m.State(id)
	return s == "running" || s == "paused"
}

// RunningCount returns the number of machines in the running state.
func (m *Manager) RunningCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, v := range m.states {
		if v.state == "running" {
			n++
		}
	}
	return n
}

// ---------- helpers ----------

func (m *Manager) lock(id int64) (*sync.Mutex, bool) {
	m.mu.Lock()
	l := m.locks[id]
	if l == nil {
		l = &sync.Mutex{}
		m.locks[id] = l
	}
	m.mu.Unlock()
	return l, l.TryLock()
}

func (m *Manager) get(id int64) *vm {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.vms[id]
}

func (m *Manager) paths(s *Spec) Paths {
	return DevicePaths(m.cfg.InstancesDir, m.cfg.LogsDir, m.cfg.RunDir, s)
}

func (m *Manager) setState(id int64, state, lastErr string) {
	m.mu.Lock()
	v := m.runtimeVM(id)
	v.state, v.lastError = state, lastErr
	m.mu.Unlock()

	// A machine's start time is the history of "the machine was opened", not an indication of a running process:
	// it is preserved on stop, error or crash, otherwise the lab
	// would disappear from "Running" right after the machines are shut down. Only Wipe resets the history.
	// This is the only start-related field kept in the database: process state lives in memory.
	if state == "running" || state == "paused" || state == "starting" {
		_, _ = m.db.Exec(`UPDATE instance_devices SET
			started_at=CASE WHEN started_at=0 THEN strftime('%s','now') ELSE started_at END WHERE id=?`, id)
	}
}

func (m *Manager) clearRuntime(id int64) {
	m.mu.Lock()
	v := m.runtimeVM(id)
	v.pid, v.vncPort, v.serialPort = 0, 0, 0
	m.mu.Unlock()
	// applied is a permanent record of the parameters a machine was started with, so
	// its storage and cleanup remain in the database unchanged.
	_, _ = m.db.Exec(`UPDATE instance_devices SET applied='' WHERE id=?`, id)
}

// applyLinkStates sets interfaces with disabled wires to the link down state.
func (m *Manager) applyLinkStates(s *Spec) {
	if m.pl == nil {
		return
	}
	p := m.paths(s)
	for _, nic := range m.pl.DownNICs(s) {
		_, _ = qmpDo(p.QMP, "set_link", map[string]any{"name": fmt.Sprintf("nic%d", nic), "up": false}, 5*time.Second)
	}
}

// SetNICLink enables or disables the link of a running machine's interface (QMP set_link, no restart).
func (m *Manager) SetNICLink(rid int64, nic int, up bool) error {
	v := m.get(rid)
	if v == nil {
		return nil // machine is not running: the link will appear at the next start
	}
	_, err := qmpDo(v.qmp, "set_link", map[string]any{"name": fmt.Sprintf("nic%d", nic), "up": up}, 5*time.Second)
	return err
}

// HotplugNIC adds a network interface to a running machine via QMP: dynamically creates a TAP
// (connected to the current segment), then netdev_add and device_add. The MAC and qdev id match
// interfaces created at startup, so the topology stays index-based.
func (m *Manager) HotplugNIC(rid int64, nic int, tap string) error {
	v := m.get(rid)
	if v == nil {
		return nil // machine is not running: the interface will appear at the next start
	}
	if nic < 0 || nic >= 32 {
		return msgs.New("Interface e{nic} is out of range", map[string]string{"nic": fmt.Sprint(nic)})
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, err := qmpStatus(v.qmp); err != nil {
		return msgs.Wrap(err, "QMP is unavailable: {detail}", map[string]string{"detail": err.Error()})
	}
	devID, netID := fmt.Sprintf("nic%d", nic), fmt.Sprintf("net%d", nic)
	if qmpDeviceExists(v.qmp, devID) {
		return nil
	}
	netdev := map[string]any{
		"type": "tap", "id": netID, "ifname": tap, "script": "no", "downscript": "no",
	}
	if _, err := qmpDo(v.qmp, "netdev_add", netdev, 5*time.Second); err != nil {
		// Leftover of a partially completed hotplug: free the name and retry.
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return fmt.Errorf("netdev_add: %w", err)
		}
		if _, derr := qmpDo(v.qmp, "netdev_del", map[string]string{"id": netID}, 5*time.Second); derr != nil {
			return fmt.Errorf("netdev_add: %w", err)
		}
		if _, err := qmpDo(v.qmp, "netdev_add", netdev, 5*time.Second); err != nil {
			return fmt.Errorf("netdev_add: %w", err)
		}
	}
	dev := map[string]any{
		"driver": v.spec.NICModel, "id": devID, "netdev": netID, "mac": MAC(v.spec.ID, nic),
	}
	if slot := nicSlot(v.spec, nic); slot != "" {
		if !qmpDeviceExists(v.qmp, slot) {
			_, _ = qmpDo(v.qmp, "netdev_del", map[string]string{"id": netID}, 5*time.Second)
			return msgs.New("No free PCIe slot ({slot}): restart the machine to add slots", map[string]string{"slot": slot})
		}
		dev["bus"] = slot
	}
	if _, err := qmpDo(v.qmp, "device_add", dev, 10*time.Second); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") && qmpDeviceExists(v.qmp, devID) {
			return nil // interface already added
		}
		_, _ = qmpDo(v.qmp, "netdev_del", map[string]string{"id": netID}, 5*time.Second)
		return fmt.Errorf("device_add: %w", err)
	}
	if nic >= v.spec.NICs {
		v.spec.NICs = nic + 1
	}
	return nil
}

// UnplugNIC hot-unplugs a running machine's interface: device_del, wait for removal,
// netdev_del and TAP deletion. If QEMU does not support hot-unplug, an error is returned
// and the caller falls back to set_link down (the interface is not considered removed).
func (m *Manager) UnplugNIC(rid int64, nic int) error {
	v := m.get(rid)
	if v == nil {
		return nil
	}
	if nic < 0 || nic >= 32 {
		return msgs.New("Interface e{nic} is out of range", map[string]string{"nic": fmt.Sprint(nic)})
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, err := qmpStatus(v.qmp); err != nil {
		return msgs.Wrap(err, "QMP is unavailable: {detail}", map[string]string{"detail": err.Error()})
	}
	devID, netID := fmt.Sprintf("nic%d", nic), fmt.Sprintf("net%d", nic)
	if !qmpDeviceExists(v.qmp, devID) {
		// Device is gone: release any leftover backend (partial hotplug).
		_, _ = qmpDo(v.qmp, "netdev_del", map[string]string{"id": netID}, 5*time.Second)
		return nil
	}
	if _, err := qmpDo(v.qmp, "device_del", map[string]string{"id": devID}, 5*time.Second); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			return nil
		}
		return fmt.Errorf("device_del: %w", err)
	}
	// device_del is asynchronous: wait briefly for it to disappear from QOM so unplugging stays fast.
	deadline := time.Now().Add(1200 * time.Millisecond)
	for qmpDeviceExists(v.qmp, devID) {
		if time.Now().After(deadline) {
			// The device may have been removed, but the event/QOM tree is lagging.
			// Try to release the backend: if it is no longer in use, the unplug is complete.
			if _, err := qmpDo(v.qmp, "netdev_del", map[string]string{"id": netID}, 5*time.Second); err == nil {
				return nil
			}
			return errors.New("QEMU did not confirm the interface removal (device_del)")
		}
		time.Sleep(25 * time.Millisecond)
	}
	if _, err := qmpDo(v.qmp, "netdev_del", map[string]string{"id": netID}, 5*time.Second); err != nil {
		return fmt.Errorf("netdev_del: %w", err)
	}
	return nil
}

func (m *Manager) loadSpec(id int64) (*Spec, string, error) {
	var s Spec
	var dirname, layoutJSON string
	var imageID string
	// Configuration comes from the instance row (user settings), name and topology come from devices.
	// Name, lab and architecture come from the topology (file); settings come from the instance row,
	// and the process's mutable state comes only from the manager's memory.
	err := m.db.QueryRow(`SELECT r.id,r.device_id,r.machine,r.cpu,r.ram_mb,r.nics,r.nic_model,r.disk_bus,r.bios,r.console,r.extra_args,
		COALESCE(r.image_id,''),r.instance_id,i.lab_id,u.username
		FROM instance_devices r JOIN lab_instances i ON i.id=r.instance_id JOIN users u ON u.id=i.user_id
		WHERE r.id=?`, id).
		Scan(&s.ID, &s.DeviceID, &s.Machine, &s.CPU, &s.RAMMB, &s.NICs, &s.NICModel, &s.DiskBus, &s.BIOS, &s.Console, &s.ExtraArgs, &imageID, &s.InstanceID, &s.LabID, &s.Owner)
	if err == nil {
		// Image comes from the catalog; if there is no image, the folder and layout are empty.
		layoutJSON = "{}"
		if imageID != "" {
			if im, ok := m.db.Catalog.ImageByDirname(imageID); ok {
				dirname, layoutJSON = im.Dirname, im.LayoutJSON
			}
		}
		fd, _, ok := m.db.Labs.Device(s.LabID, s.DeviceID)
		if !ok {
			return nil, "", ErrNotFound
		}
		s.Name, s.Arch = fd.Name, fd.Arch
	}
	if err == sql.ErrNoRows {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	if dirname != "" {
		var lay struct {
			Disks []struct {
				Name   string `json:"name"`
				Format string `json:"format"`
			} `json:"disks"`
			CDROMs []string `json:"cdroms"`
		}
		_ = json.Unmarshal([]byte(layoutJSON), &lay)
		dir := filepath.Join(m.cfg.DisksDir, dirname)
		p := m.paths(&s)
		for i, d := range lay.Disks {
			s.Disks = append(s.Disks, DiskSpec{
				Base:    filepath.Join(dir, d.Name),
				BaseFmt: d.Format,
				Overlay: filepath.Join(p.Dir, fmt.Sprintf("disk%d.qcow2", i)),
			})
		}
		for _, c := range lay.CDROMs {
			s.CDROMs = append(s.CDROMs, filepath.Join(dir, c))
		}
	}
	return &s, m.State(id), nil
}

func (m *Manager) binary(arch string) (string, error) {
	if arch == hostarch.QEMUArch() {
		p, err := exec.LookPath(m.cfg.QEMUPath)
		if err != nil {
			return "", msgs.New("QEMU not found: {path}", map[string]string{"path": m.cfg.QEMUPath})
		}
		return p, nil
	}
	if arch == "i386" && hostarch.QEMUArch() == "x86_64" {
		p, err := exec.LookPath("qemu-system-i386")
		if err != nil {
			return "", errors.New("qemu-system-i386 not found")
		}
		return p, nil
	}
	return "", msgs.New("Architecture {arch} does not run on this host ({host})", map[string]string{"arch": arch, "host": hostarch.QEMUArch()})
}

func (m *Manager) qemuImg(args ...string) (string, error) {
	bin, err := FindQemuImg(m.cfg.QEMUPath)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("qemu-img %s: %s", args[0], strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func logTail(path string, n int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	off := st.Size() - n
	if off < 0 {
		off = 0
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return ""
	}
	b, _ := io.ReadAll(f)
	return string(b)
}

// LogTail returns the tail of the device's startup log.
func (m *Manager) LogTail(devID int64, n int64) (string, error) {
	s, _, err := m.loadSpec(devID)
	if err != nil {
		return "", err
	}
	return logTail(m.paths(s).Log, n), nil
}

// ---------- checks and resources ----------

func (m *Manager) preflight(s *Spec) error {
	// Before any check: the firmware choice and the BIOS check below depend on the normalized device.
	normalizeForArch(s)
	if s.Arch != hostarch.QEMUArch() && !(s.Arch == "i386" && hostarch.QEMUArch() == "x86_64") {
		return msgs.New("Architecture {arch} does not run on this host ({host}): KVM runs only guests of its own architecture", map[string]string{"arch": s.Arch, "host": hostarch.QEMUArch()})
	}
	if s.Arch == "aarch64" && s.BIOS != "uefi" {
		return errors.New("aarch64 machines boot only through UEFI")
	}
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return msgs.Wrap(err, "KVM is unavailable: {detail}", map[string]string{"detail": err.Error()})
	}
	f.Close()
	if _, err := m.binary(s.Arch); err != nil {
		return err
	}
	if _, err := FindQemuImg(m.cfg.QEMUPath); err != nil {
		return err
	}
	if len(s.Disks)+len(s.CDROMs) == 0 {
		return errors.New("The device has no image folder selected or it is empty (hda.qcow2 and/or ISO disks are needed)")
	}
	for _, d := range s.Disks {
		if !fileExists(d.Base) {
			return msgs.New("Disk file not found: {file}", map[string]string{"file": filepath.Base(d.Base)})
		}
	}
	for _, c := range s.CDROMs {
		if !fileExists(c) {
			return msgs.New("ISO not found: {file}", map[string]string{"file": filepath.Base(c)})
		}
	}
	if _, err := ParseExtraArgs(s.ExtraArgs); err != nil {
		return err
	}
	need := uint64(s.RAMMB)<<20 + 256<<20
	if avail := MemAvailable(); avail > 0 && avail < need {
		return msgs.New("Not enough free memory: {need} MB needed, {available} MB available", map[string]string{"need": fmt.Sprint(need >> 20), "available": fmt.Sprint(avail >> 20)})
	}
	missing := false
	for _, d := range s.Disks {
		if !fileExists(d.Overlay) {
			missing = true
		}
	}
	if missing {
		if free := DiskFree(m.cfg.InstancesDir); free < 2<<30 {
			return msgs.New("Not enough disk space: {free} MB free, at least 2048 MB needed", map[string]string{"free": fmt.Sprint(free >> 20)})
		}
	}
	return nil
}

func (m *Manager) allocPorts(id int64) (vncDisplay, serial int, err error) {
	m.portMu.Lock()
	defer m.portMu.Unlock()
	usedV, usedS := map[int]bool{}, map[int]bool{}
	m.mu.Lock()
	for rid, v := range m.states {
		if rid == id {
			continue
		}
		if v.vncPort > 0 {
			usedV[v.vncPort] = true
		}
		if v.serialPort > 0 {
			usedS[v.serialPort] = true
		}
	}
	m.mu.Unlock()

	startV := rand.Intn(800)
	for i := 0; i < 800; i++ {
		d := 100 + (startV+i)%800
		if !usedV[5900+d] && PortFree(5900+d) {
			vncDisplay = d
			break
		}
	}
	startS := rand.Intn(10000)
	for i := 0; i < 10000; i++ {
		p := 30000 + (startS+i)%10000
		if !usedS[p] && PortFree(p) {
			serial = p
			break
		}
	}
	if vncDisplay == 0 || serial == 0 {
		return 0, 0, errors.New("Could not find free console ports")
	}
	m.mu.Lock()
	v := m.runtimeVM(id)
	v.state, v.lastError, v.vncPort, v.serialPort = "starting", "", 5900+vncDisplay, serial
	m.mu.Unlock()
	_, err = m.db.Exec(`UPDATE instance_devices SET
		started_at=CASE WHEN started_at=0 THEN strftime('%s','now') ELSE started_at END WHERE id=?`, id)
	return vncDisplay, serial, err
}

// rebaseMarker is a file next to the working disk that records the base disk of the previous
// generation. The marker is set when the image was overwritten while the machine was running: the disk
// itself is switched before the next start, when QEMU no longer holds it open.
func rebaseMarker(overlay string) string { return overlay + ".rebase" }

// rebaseMarkers returns pending switches in the device directory.
func rebaseMarkers(dir string) []string {
	g, _ := filepath.Glob(filepath.Join(dir, "disk*.qcow2.rebase"))
	return g
}

// applyPendingRebases returns the machine's working disks to the saved images of previous
// generations. Called before startup, while no one is writing to the disk files yet.
func (m *Manager) applyPendingRebases(s *Spec) error {
	for _, d := range s.Disks {
		marker := rebaseMarker(d.Overlay)
		data, err := os.ReadFile(marker)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return msgs.Wrap(err, "Reading the deferred disk switch: {detail}", map[string]string{"detail": err.Error()})
		}
		if !fileExists(d.Overlay) {
			_ = os.Remove(marker) // no disk - a new one will be created from the current image
			continue
		}
		oldBase := strings.TrimSpace(string(data))
		if oldBase == "" {
			_ = os.Remove(marker)
			continue
		}
		if !fileExists(oldBase) {
			return msgs.New("Previous disk image not found: {file}", map[string]string{"file": filepath.Base(oldBase)})
		}
		format := d.BaseFmt
		if format == "" {
			format = "qcow2"
		}
		if err := m.rebaseLayer(d.Overlay, oldBase, format); err != nil {
			return msgs.Wrap(err, "Reverting the disk to the previous image: {detail}", map[string]string{"detail": err.Error()})
		}
		if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
			return msgs.Wrap(err, "Removing the disk switch mark: {detail}", map[string]string{"detail": err.Error()})
		}
	}
	return nil
}

func (m *Manager) ensureDisks(s *Spec, p Paths) error {
	for _, d := range s.Disks {
		if fileExists(d.Overlay) {
			continue
		}
		if free := DiskFree(m.cfg.InstancesDir); free < 1<<30 {
			return errors.New("Not enough disk space to create the device disks")
		}
		if _, err := m.qemuImg("create", "-f", "qcow2", "-F", d.BaseFmt, "-b", d.Base, d.Overlay); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

// ---------- startup ----------

// Start checks the conditions synchronously and starts the VM in the background (state starting -> running/error).
func (m *Manager) Start(id int64) error {
	l, ok := m.lock(id)
	if !ok {
		return ErrBusy
	}
	spec, state, err := m.loadSpec(id)
	if err != nil {
		l.Unlock()
		return err
	}
	if state != "stopped" && state != "error" {
		l.Unlock()
		return msgs.New("The device is already running (state: {state})", map[string]string{"state": state})
	}
	if err := m.preflight(spec); err != nil {
		l.Unlock()
		return err
	}
	vnc, serial, err := m.allocPorts(id)
	if err != nil {
		l.Unlock()
		return err
	}
	go func() {
		defer l.Unlock()
		if err := m.launch(spec, vnc, serial); err != nil {
			m.markFailed(spec, err)
		}
	}()
	return nil
}

func (m *Manager) markFailed(s *Spec, err error) {
	if m.Notify != nil {
		m.Notify.SendInstance(s.InstanceID, "error", "machine", "Machine failed to start",
			s.Name+": "+firstLine(err.Error()), labLink(s.LabID))
	}
	m.setState(s.ID, "error", err.Error())
	m.clearRuntime(s.ID)
	m.pl.Release(s)
}

func (m *Manager) launch(s *Spec, vncDisplay, serialPort int) error {
	p := m.paths(s)
	if err := os.MkdirAll(p.Dir, 0o750); err != nil {
		return err
	}
	_ = os.Remove(p.QMP)
	if err := m.applyPendingRebases(s); err != nil {
		return err
	}
	if err := m.ensureDisks(s, p); err != nil {
		return err
	}
	var ov *OVMF
	if s.BIOS == "uefi" {
		var err error
		if ov, err = FindOVMF(); err != nil {
			return err
		}
		if ov.Vars != "" && !fileExists(p.Vars) {
			if err := copyFile(ov.Vars, p.Vars); err != nil {
				return msgs.Wrap(err, "Copying OVMF_VARS: {detail}", map[string]string{"detail": err.Error()})
			}
		}
	}
	args, err := BuildArgs(s, p, vncDisplay, serialPort, m.pl, ov)
	if err != nil {
		return err
	}
	log.Printf("qemu: machine %d (%s): adapter %s, interfaces %d", s.ID, s.Name, s.NICModel, s.NICs)
	for i := 0; i < s.NICs; i++ {
		log.Printf("qemu: machine %d nic%d: model=%s mac=%s", s.ID, i, s.NICModel, MAC(s.ID, i))
	}
	bin, err := m.binary(s.Arch)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.Log), 0o750); err != nil {
		return err
	}
	logf, err := os.OpenFile(p.Log, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	fmt.Fprintf(logf, "$ %s %s\n", bin, strings.Join(args, " "))
	cmd := exec.Command(bin, args...)
	cmd.Stdout, cmd.Stderr, cmd.Dir = logf, logf, p.Dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // The VM survives a server restart
	if err := cmd.Start(); err != nil {
		logf.Close()
		return msgs.Wrap(err, "Could not start QEMU: {detail}", map[string]string{"detail": err.Error()})
	}
	logf.Close()

	// Remember the parameters the process was started with: they show what is waiting for a restart.
	snap := instances.Launch{Machine: s.Machine, CPU: s.CPU, RAMMB: s.RAMMB, NICs: s.NICs, NICModel: s.NICModel,
		DiskBus: s.DiskBus, BIOS: s.BIOS, Console: s.Console, ExtraArgs: s.ExtraArgs,
	}
	_, _ = m.db.Exec(`UPDATE instance_devices SET applied=? WHERE id=?`, snap.JSON(), s.ID)

	m.mu.Lock()
	v := m.runtimeVM(s.ID)
	v.spec, v.pid, v.cmd, v.qmp, v.done = s, cmd.Process.Pid, cmd, p.QMP, make(chan struct{})
	v.state, v.lastError = "starting", ""
	v.vncPort, v.serialPort = 5900+vncDisplay, serialPort
	m.vms[s.ID] = v
	m.mu.Unlock()
	go func() {
		werr := cmd.Wait()
		m.handleExit(v, werr)
	}()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-v.done:
			return nil // handleExit has already recorded the reason
		default:
		}
		if st, err := qmpStatus(p.QMP); err == nil {
			m.setState(s.ID, stateFromQMP(st), "")
			m.applyLinkStates(s)
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	m.kill(v)
	return errors.New("QEMU did not respond over QMP within 15 seconds")
}

func (m *Manager) kill(v *vm) {
	_ = syscall.Kill(-v.pid, syscall.SIGKILL)
	_ = syscall.Kill(v.pid, syscall.SIGKILL)
}

// handleExit is called once when the QEMU process exits.
func (m *Manager) handleExit(v *vm, waitErr error) {
	m.mu.Lock()
	if m.vms[v.spec.ID] == v {
		delete(m.vms, v.spec.ID)
	}
	m.mu.Unlock()
	select {
	case <-v.done:
		return
	default:
	}
	v.mu.Lock()
	expect := v.expectStop
	v.mu.Unlock()
	p := m.paths(v.spec)
	_ = os.Remove(p.QMP)
	m.pl.Release(v.spec)
	if expect || waitErr == nil {
		m.setState(v.spec.ID, "stopped", "")
	} else {
		tail := strings.TrimSpace(logTail(p.Log, 1500))
		if m.Notify != nil {
			m.Notify.SendInstance(v.spec.InstanceID, "error", "machine", "Machine crashed",
				"{name}: QEMU exited with an error. Details are in the machine start log.", labLink(v.spec.LabID), map[string]string{"name": v.spec.Name})
		}
		m.setState(v.spec.ID, "error", "QEMU exited unexpectedly: "+waitErr.Error()+"\n"+tail)
	}
	m.clearRuntime(v.spec.ID)
	// A restart waits for v.done to start a new process. The signal is closed only after cleaning up
	// the old pid/ports: otherwise the record of a new start could be overwritten by the old process's exit.
	close(v.done)
}

// ---------- stopping and control ----------

func (m *Manager) waitExit(v *vm, d time.Duration) bool {
	select {
	case <-v.done:
		return true
	case <-time.After(d):
		return false
	}
}

func (m *Manager) terminate(v *vm, force bool) {
	if !force {
		_, _ = qmpDo(v.qmp, "cont", nil, 3*time.Second) // a paused VM will not handle ACPI
		_, _ = qmpDo(v.qmp, "system_powerdown", nil, 3*time.Second)
		if m.waitExit(v, 20*time.Second) {
			return
		}
	}
	if _, err := qmpDo(v.qmp, "quit", nil, 3*time.Second); err == nil {
		if m.waitExit(v, 5*time.Second) {
			return
		}
	}
	// QMP is unavailable or did not terminate the process - kill QEMU. This is safe: Wipe and Stop
	// wait for v.done before working with disks.
	m.kill(v)
	m.waitExit(v, 5*time.Second)
}

// Stop stops the VM: ACPI powerdown -> quit via QMP -> SIGKILL. With force, quit immediately.
func (m *Manager) Stop(id int64, force bool) error {
	l, ok := m.lock(id)
	if !ok {
		return ErrBusy
	}
	_, state, err := m.loadSpec(id)
	if err != nil {
		l.Unlock()
		return err
	}
	v := m.get(id)
	if v == nil {
		if state != "stopped" {
			m.setState(id, "stopped", "")
			m.clearRuntime(id)
		}
		l.Unlock()
		return nil
	}
	m.setState(id, "stopping", "")
	v.mu.Lock()
	v.expectStop = true
	v.mu.Unlock()
	go func() {
		defer l.Unlock()
		m.terminate(v, force)
	}()
	return nil
}

func (m *Manager) Restart(id int64) error {
	l, ok := m.lock(id)
	if !ok {
		return ErrBusy
	}
	_, state, err := m.loadSpec(id)
	if err != nil {
		l.Unlock()
		return err
	}
	v := m.get(id)
	if v == nil {
		l.Unlock()
		return m.Start(id)
	}
	if state != "running" && state != "paused" {
		l.Unlock()
		return msgs.New("Restart is not available in state {state}", map[string]string{"state": state})
	}
	m.setState(id, "stopping", "")
	v.mu.Lock()
	v.expectStop = true
	v.mu.Unlock()
	go func() {
		defer l.Unlock()
		m.terminate(v, false)
		spec, _, err := m.loadSpec(id)
		if err != nil {
			return
		}
		if err := m.preflight(spec); err != nil {
			m.markFailed(spec, err)
			return
		}
		vnc, serial, err := m.allocPorts(id)
		if err != nil {
			m.markFailed(spec, err)
			return
		}
		if err := m.launch(spec, vnc, serial); err != nil {
			m.markFailed(spec, err)
		}
	}()
	return nil
}

func (m *Manager) Suspend(id int64) error { return m.pauseResume(id, "stop", "running", "paused") }
func (m *Manager) Resume(id int64) error  { return m.pauseResume(id, "cont", "paused", "running") }

func (m *Manager) pauseResume(id int64, cmd, from, to string) error {
	l, ok := m.lock(id)
	if !ok {
		return ErrBusy
	}
	defer l.Unlock()
	_, state, err := m.loadSpec(id)
	if err != nil {
		return err
	}
	v := m.get(id)
	if v == nil || state != from {
		return msgs.New("The operation is not available in state {state}", map[string]string{"state": state})
	}
	if _, err := qmpDo(v.qmp, cmd, nil, 5*time.Second); err != nil {
		return err
	}
	m.setState(id, to, "")
	return nil
}

// Wipe deletes the device's disk: on the next start a clean disk is created from the image.
// Wipe returns the machine to the administrator's reference image. If it is running or paused,
// it stops it itself while holding the device lock: deleting overlay disks with a live
// QEMU is not allowed. Configuration, parameters, network, MAC addresses and the assigned image do not change.
func (m *Manager) Wipe(id int64) error {
	l, ok := m.lock(id)
	if !ok {
		return ErrBusy
	}
	defer l.Unlock()
	s, state, err := m.loadSpec(id)
	if err != nil {
		return err
	}
	// While a start or stop is in progress, the disk may be held by the QEMU process.
	if state == "starting" || state == "stopping" {
		return msgs.Wrap(ErrBusy, "The machine is busy: wait for the operation to finish (state: {state})", map[string]string{"state": state})
	}
	if v := m.get(id); v != nil {
		m.setState(id, "stopping", "")
		v.mu.Lock()
		v.expectStop = true
		v.mu.Unlock()
		// The disk contents are deleted anyway, so terminate QEMU immediately (QMP quit -> SIGKILL).
		m.terminate(v, true)
		select {
		case <-v.done:
			// The process has exited; handleExit has already cleaned up pid/ports and state.
		default:
			return errors.New("Could not stop the machine: the QEMU process did not exit")
		}
	}
	p := m.paths(s)
	var problems []string
	for _, f := range overlayFiles(p.Dir) {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			problems = append(problems, filepath.Base(f)+": "+err.Error())
			continue
		}
		// The switch marker refers to a deleted disk: without it the new
		// disk will be created from the current (new) image.
		if err := os.Remove(rebaseMarker(f)); err != nil && !os.IsNotExist(err) {
			problems = append(problems, filepath.Base(rebaseMarker(f))+": "+err.Error())
		}
	}
	// Markers without a disk are leftovers of past attempts: there is no disk, so there is nothing to bind.
	for _, marker := range rebaseMarkers(p.Dir) {
		if fileExists(strings.TrimSuffix(marker, ".rebase")) {
			continue // the disk was not deleted: keep its binding to the previous image
		}
		_ = os.Remove(marker)
	}
	if err := os.Remove(p.Vars); err != nil && !os.IsNotExist(err) {
		problems = append(problems, filepath.Base(p.Vars)+": "+err.Error())
	}
	// Even if deletion fails, the machine stays powered off: a partially wiped disk must not be started.
	m.clearRuntime(id)
	m.setState(id, "stopped", "")
	if len(problems) > 0 {
		return msgs.New("Could not delete the working disks: {detail}", map[string]string{"detail": strings.Join(problems, "; ")})
	}
	return nil
}

// firstLine returns the first line of text (for short notifications).
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// labLink is a link to the lab for a notification (interface route).
func labLink(labName string) string { return "/labs/" + url.PathEscape(labName) + "/editor" }

// overlayFiles returns the device's working disks (disk0.qcow2, disk1.qcow2, ...) in the device directory.
func overlayFiles(dir string) []string {
	if dir == "" {
		return nil
	}
	g, _ := filepath.Glob(filepath.Join(dir, "disk*.qcow2"))
	return g
}

// CloneFiles copies the disk of a stopped device src to device dst.
func (m *Manager) CloneFiles(src, dst int64) error {
	ps, err := WorkDir(m.db, m.cfg.InstancesDir, src)
	if err != nil {
		return err
	}
	pd, err := WorkDir(m.db, m.cfg.InstancesDir, dst)
	if err != nil {
		return err
	}
	files := overlayFiles(ps.Dir)
	if len(files) == 0 {
		return nil // disks are not created yet - they will be created on first start
	}
	if DiskFree(m.cfg.InstancesDir) < 2<<30 {
		return errors.New("Not enough disk space for cloning")
	}
	if err := os.MkdirAll(pd.Dir, 0o750); err != nil {
		return err
	}
	for _, f := range files {
		if err := copyFile(f, filepath.Join(pd.Dir, filepath.Base(f))); err != nil {
			return err
		}
	}
	// The pending switch refers to the contents of the copied disk: the copy
	// must be applied the same way as the original on its next start.
	for _, f := range rebaseMarkers(ps.Dir) {
		if err := copyFile(f, filepath.Join(pd.Dir, filepath.Base(f))); err != nil {
			return err
		}
	}
	if fileExists(ps.Vars) {
		_ = copyFile(ps.Vars, pd.Vars)
	}
	return nil
}

// IsStopped reports that the device is not running.
func (m *Manager) IsStopped(id int64) bool {
	_, state, err := m.loadSpec(id)
	return err == nil && (state == "stopped" || state == "error")
}

// RemoveFiles deletes the files of a removed device and releases network objects.
func (m *Manager) RemoveFiles(instanceID, devID int64) {
	s := &Spec{ID: devID, InstanceID: instanceID}
	if inf, err := instances.Device(m.db, devID); err == nil {
		s.LabID, s.DeviceID, s.Owner = inf.LabID, inf.DeviceID, inf.Owner
	}
	m.pl.Release(s)
	if dir := workPaths(m.cfg.InstancesDir, s.Owner, s.LabID, s.DeviceID).Dir; dir != "" {
		_ = os.RemoveAll(dir)
	}
	if log := m.paths(s).Log; log != "" {
		_ = os.Remove(log)
	}
	m.mu.Lock()
	delete(m.locks, devID)
	delete(m.states, devID)
	delete(m.vms, devID)
	m.mu.Unlock()
}

// RemoveLabDir deletes the working directories of all instances of a lab.
func (m *Manager) RemoveLabDir(labName string) {
	rows, err := m.db.Query(`SELECT id FROM lab_instances WHERE lab_id=?`, labName)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			m.removeInstanceDirs(id)
		}
	}
}

// RemoveInstanceFiles deletes the working files and logs of one instance; the instance row must still exist.
func (m *Manager) RemoveInstanceFiles(instanceID int64) {
	m.removeInstanceDirs(instanceID)
}

// removeInstanceDirs deletes the working files and logs of one instance: the owner's directory of the
// laboratory and the logs. The instance row must still exist to name them.
func (m *Manager) removeInstanceDirs(instanceID int64) {
	owner, lab, err := instances.Scope(m.db, instanceID)
	if err != nil {
		return
	}
	if safeElement(owner) && safeElement(lab) {
		_ = os.RemoveAll(filepath.Join(m.cfg.InstancesDir, owner, lab))
	}
	m.RemoveLabLogs(owner, lab)
}

// RemoveLabLogs deletes the logs of one laboratory of one owner: <logs>/<owner>/<lab>/.
func (m *Manager) RemoveLabLogs(owner, lab string) {
	if !safeElement(owner) || !safeElement(lab) {
		return
	}
	_ = os.RemoveAll(filepath.Join(m.cfg.LogsDir, owner, lab))
}

// RemoveLabLogsOfAll deletes the logs of a laboratory for every owner, as after its rename.
func (m *Manager) RemoveLabLogsOfAll(lab string) {
	if !safeElement(lab) {
		return
	}
	owners, err := os.ReadDir(m.cfg.LogsDir)
	if err != nil {
		return
	}
	for _, o := range owners {
		if o.IsDir() {
			_ = os.RemoveAll(filepath.Join(m.cfg.LogsDir, o.Name(), lab))
		}
	}
}

// RemoveDeviceLog deletes the log of one machine of a laboratory for every owner, as after its rename.
func (m *Manager) RemoveDeviceLog(lab, device string) {
	if !safeElement(lab) || !safeElement(device) {
		return
	}
	owners, err := os.ReadDir(m.cfg.LogsDir)
	if err != nil {
		return
	}
	for _, o := range owners {
		if o.IsDir() {
			_ = os.Remove(LogFile(m.cfg.LogsDir, o.Name(), lab, device))
		}
	}
}

// WaitLeftStarting waits until the device is no longer in the starting state.
func (m *Manager) WaitLeftStarting(id int64, max time.Duration) {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		_, st, err := m.loadSpec(id)
		if err != nil || st != "starting" {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// ---------- statistics ----------

type Stat struct {
	CPU   float64 `json:"cpu_pct"`
	RSS   uint64  `json:"rss_bytes"`
	PID   int     `json:"pid"`
	Ready bool    `json:"cpu_ready"` // false - load has not been measured yet (two samples a second apart are needed)
}

func (m *Manager) Stats(ids []int64) map[int64]Stat {
	out := map[int64]Stat{}
	for _, id := range ids {
		v := m.get(id)
		if v == nil {
			continue
		}
		v.mu.Lock()
		now := time.Now()
		if ticks, ok := readCPUTicks(v.pid); ok {
			switch {
			case v.cpuAt.IsZero():
				v.cpuTicks, v.cpuAt = ticks, now
			case now.Sub(v.cpuAt) >= time.Second && ticks >= v.cpuTicks:
				dt := now.Sub(v.cpuAt).Seconds()
				v.cpuPct = float64(ticks-v.cpuTicks) / 100.0 / dt * 100.0
				v.cpuTicks, v.cpuAt = ticks, now
				v.cpuReady = true
			}
		} else {
			// The process is no longer read: the old load value is not shown.
			v.cpuPct, v.cpuReady = 0, false
		}
		pct, pid, ready := v.cpuPct, v.pid, v.cpuReady
		v.mu.Unlock()
		out[id] = Stat{CPU: pct, RSS: readRSS(pid), PID: pid, Ready: ready}
	}
	return out
}

// ---------- recovery and background control ----------

// Recover is called at server startup. Process state does not survive a restart:
// all machines are considered stopped, and QEMU processes left over from the previous server
// process are forcibly terminated. This does not affect the start history (started_at) - it is in the database.
func (m *Manager) Recover() {
	// State exists only in the new process's memory: there are no records of running machines.
	m.mu.Lock()
	m.vms = map[int64]*vm{}
	m.states = map[int64]*vm{}
	m.mu.Unlock()

	// All QEMU processes named netrave-d<id> belong to the previous server run:
	// they must not be adopted or restored.
	ents, _ := os.ReadDir("/proc")
	for _, e := range ents {
		var pid int
		if _, err := fmt.Sscanf(e.Name(), "%d", &pid); err != nil || fmt.Sprint(pid) != e.Name() {
			continue
		}
		if _, ok := deviceIDFromArgs(cmdlineArgs(pid)); !ok {
			continue
		}
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

// Run is background synchronization of the running/paused state with QMP.
func (m *Manager) Run(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.mu.Lock()
			list := make([]*vm, 0, len(m.vms))
			for _, v := range m.vms {
				list = append(list, v)
			}
			m.mu.Unlock()
			for _, v := range list {
				state := m.State(v.spec.ID)
				if state != "running" && state != "paused" {
					continue
				}
				if st, err := qmpStatus(v.qmp); err == nil {
					if ns := stateFromQMP(st); ns != state {
						m.setState(v.spec.ID, ns, "")
					}
				}
			}
		}
	}
}

// StopAll stops all running VMs (used with -stop-vms-on-exit).
func (m *Manager) StopAll(timeout time.Duration) {
	m.mu.Lock()
	list := make([]*vm, 0, len(m.vms))
	for _, v := range m.vms {
		list = append(list, v)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, v := range list {
		v.mu.Lock()
		v.expectStop = true
		v.mu.Unlock()
		wg.Add(1)
		go func(v *vm) {
			defer wg.Done()
			m.terminate(v, false)
		}(v)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// StopLab stops all devices of a lab before it is deleted.
// If the devices do not stop gracefully within timeout, the QEMU processes are forcibly terminated
// and the state is reset - deleting a lab does not depend on the state of its devices.
func (m *Manager) StopLab(labName string, timeout time.Duration) error {
	inLab := map[string]bool{}
	if fl, ok := m.db.Labs.Lab(labName); ok {
		for _, d := range fl.Devices {
			inLab[d.Name] = true
		}
	}
	rows, err := m.db.Query(`SELECT r.id,r.device_id FROM instance_devices r`)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		var devID string
		if rows.Scan(&id, &devID) == nil && inLab[devID] {
			ids = append(ids, id)
		}
	}
	rows.Close()
	return m.stopMachinesAndWait(ids, timeout)
}

// StopInstance stops all machines of one instance before the instance is removed with its owner.
// It has the same timeout behaviour as StopLab.
func (m *Manager) StopInstance(instanceID int64, timeout time.Duration) error {
	rows, err := m.db.Query(`SELECT id FROM instance_devices WHERE instance_id=?`, instanceID)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	return m.stopMachinesAndWait(ids, timeout)
}

// stopMachinesAndWait stops the machines with the given row numbers and waits up to timeout for them to stop.
// Machines still running after that are terminated forcibly.
func (m *Manager) stopMachinesAndWait(ids []int64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		pending := false
		for _, id := range ids {
			if m.IsStopped(id) {
				continue
			}
			pending = true
			_ = m.Stop(id, true) // ErrBusy - the operation is already in progress
		}
		if !pending {
			return nil
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	for _, id := range ids {
		if m.IsStopped(id) {
			continue
		}
		if pid := m.Runtime(id).PID; pid > 0 {
			if got, ok := deviceIDFromArgs(cmdlineArgs(pid)); ok && got == id {
				_ = syscall.Kill(-pid, syscall.SIGKILL)
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
		m.setState(id, "stopped", "")
		m.clearRuntime(id)
	}
	return nil
}

// RemoveDeviceFiles deletes the working directory and the log of a machine removed from a laboratory,
// for every owner of the laboratory.
func (m *Manager) RemoveDeviceFiles(lab, dev string) {
	if !safeElement(lab) || !safeElement(dev) {
		return
	}
	for _, owner := range m.ownerNames() {
		_ = os.RemoveAll(filepath.Join(m.cfg.InstancesDir, owner, lab, dev))
	}
	m.RemoveDeviceLog(lab, dev)
}

// ownerNames returns the owner folders under the instances directory: the owners' login names.
func (m *Manager) ownerNames() []string {
	ents, err := os.ReadDir(m.cfg.InstancesDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() && safeElement(e.Name()) {
			names = append(names, e.Name())
		}
	}
	return names
}

// RenameLabFiles moves the working directories of a laboratory to its new name, for every owner.
// It returns a function that undoes the moves; the caller runs it if the rename does not complete.
func (m *Manager) RenameLabFiles(from, to string) (func(), error) {
	var pairs [][2]string
	for _, owner := range m.ownerNames() {
		pairs = append(pairs, [2]string{
			filepath.Join(m.cfg.InstancesDir, owner, from),
			filepath.Join(m.cfg.InstancesDir, owner, to),
		})
	}
	return moveDirs(pairs)
}

// RenameDeviceFiles moves the working directories of a machine to its new name, for every owner of the
// laboratory. Machines cannot be renamed while running, so no disk is in use during the move.
// It returns a function that undoes the moves.
func (m *Manager) RenameDeviceFiles(lab, from, to string) (func(), error) {
	var pairs [][2]string
	for _, owner := range m.ownerNames() {
		base := filepath.Join(m.cfg.InstancesDir, owner, lab)
		pairs = append(pairs, [2]string{filepath.Join(base, from), filepath.Join(base, to)})
	}
	return moveDirs(pairs)
}

// moveDirs renames every source that exists to its target. If a move fails, the moves made so far are undone.
// It returns a function that undoes all moves made.
func moveDirs(pairs [][2]string) (func(), error) {
	var done [][2]string
	undo := func() {
		for i := len(done) - 1; i >= 0; i-- {
			_ = os.Rename(done[i][1], done[i][0])
		}
	}
	for _, p := range pairs {
		if _, err := os.Stat(p[0]); err != nil {
			continue
		}
		if err := os.Rename(p[0], p[1]); err != nil {
			undo()
			return func() {}, err
		}
		done = append(done, p)
	}
	return undo, nil
}

// WorkDir returns the working files of the instance machine with row number rid. Its directory is named
// after the owner, the laboratory and the machine: <instances>/<owner>/<lab>/<device>/.
func WorkDir(db *database.DB, instancesDir string, rid int64) (Paths, error) {
	inf, err := instances.Device(db, rid)
	if err != nil {
		return Paths{}, err
	}
	return workPaths(instancesDir, inf.Owner, inf.LabID, inf.DeviceID), nil
}

// HasDisks reports whether the machine has working disks (overlays). If it does, the disk cannot be changed without Wipe:
// the overlay is bound to the base image that was selected at creation.
func (m *Manager) HasDisks(rid int64) bool {
	p, err := WorkDir(m.db, m.cfg.InstancesDir, rid)
	if err != nil {
		return false
	}
	return len(overlayFiles(p.Dir)) > 0
}

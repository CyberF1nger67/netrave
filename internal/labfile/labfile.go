// SPDX-License-Identifier: GPL-3.0-only

// Package labfile - lab projects in files. A lab is a file <name>.yaml inside a folder of the
// laboratory directory: its parameters, devices, networks, cables and user permissions.
// The directory tree is the only record of placement (see package fstree).
//
// A laboratory name is its identifier and is unique across the whole tree, compared
// case-insensitively. Device and network names are unique within a lab; a cable is identified
// by its ends, so it needs no separate number.
//
// Integrity is checked on save: a cable references existing ends of the same lab,
// one device port is occupied by at most one cable, deleting a device or network deletes their cables.
package labfile

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
	"netrave/internal/fstree"
	"netrave/internal/msgs"
	"netrave/internal/names"
	"netrave/internal/yamlfmt"
)

// Device - a virtual machine in the lab.
type Device struct {
	TemplateID int64 `yaml:"template_id,omitempty"`
	// ImageID - the image folder name (see catalog.Image.Dirname); empty - no disk.
	ImageID    string  `yaml:"image_id,omitempty"`
	Name       string  `yaml:"name"`
	X          float64 `yaml:"x"`
	Y          float64 `yaml:"y"`
	Arch       string  `yaml:"arch"`
	Machine    string  `yaml:"machine"`
	CPU        int     `yaml:"cpu"`
	RAMMB      int     `yaml:"ram_mb"`
	NICs       int     `yaml:"nics"`
	NICModel   string  `yaml:"nic_model"`
	DiskBus    string  `yaml:"disk_bus"`
	BIOS       string  `yaml:"bios"`
	Console    string  `yaml:"console"`
	ExtraArgs  string  `yaml:"extra_args,omitempty"`
	IfaceType  string  `yaml:"iface_type"`
	IfaceStart int     `yaml:"iface_start"`
	Icon       string  `yaml:"icon,omitempty"`
	CustomIcon string  `yaml:"custom_icon,omitempty"`
	// IconSize - the machine icon side on the diagram in world units (0 - default size).
	// Stored on the machine because each icon is scaled separately.
	IconSize  int   `yaml:"icon_size,omitempty"`
	CreatedAt int64 `yaml:"created_at"`
}

// Network - a lab network.
type Network struct {
	Name        string  `yaml:"name"`
	Type        string  `yaml:"type"`
	Subnet      string  `yaml:"subnet,omitempty"`
	Gateway     string  `yaml:"gateway,omitempty"`
	DHCPEnabled bool    `yaml:"dhcp_enabled"`
	DHCPStart   string  `yaml:"dhcp_start,omitempty"`
	DHCPEnd     string  `yaml:"dhcp_end,omitempty"`
	HostIface   string  `yaml:"host_iface,omitempty"`
	BridgeName  string  `yaml:"bridge_name,omitempty"`
	X           float64 `yaml:"x"`
	Y           float64 `yaml:"y"`
}

// End - a cable end: a device (Type "device") or a network (Type "network") and, for a device,
// the port number.
type End struct {
	Type string `yaml:"type"`
	Name string `yaml:"name"`
	If   int    `yaml:"if"`
}

// Connection - a cable between two ends.
type Connection struct {
	A End `yaml:"a"`
	B End `yaml:"b"`
}

// Key returns the stable key of the cable, derived from its ends. The ends are sorted so that
// the key does not depend on the order in which they are written.
func (c Connection) Key() string {
	a, b := endKey(c.A), endKey(c.B)
	if a > b {
		a, b = b, a
	}
	return a + "~" + b
}

func endKey(e End) string {
	if e.Type == "network" {
		return "network:" + e.Name
	}
	return "device:" + e.Name + ":" + strconv.Itoa(e.If)
}

// Permission - a user's or a role's permission on the lab. UserID and RoleID are the numeric
// identifiers of the database rows; exactly one of them is set.
type Permission struct {
	UserID    int64  `yaml:"user_id,omitempty"`
	RoleID    int64  `yaml:"role_id,omitempty"`
	Perm      string `yaml:"perm"`
	Value     bool   `yaml:"value"`
	UpdatedAt int64  `yaml:"updated_at"`
	UpdatedBy int64  `yaml:"updated_by,omitempty"`
}

// Lab - a whole lab project.
type Lab struct {
	OwnerID     int64        `yaml:"owner_id"`
	Name        string       `yaml:"name"`
	Description string       `yaml:"description,omitempty"`
	CanvasJSON  string       `yaml:"canvas_json,omitempty"`
	PublicLevel string       `yaml:"public_level"`
	CreatedAt   int64        `yaml:"created_at"`
	UpdatedAt   int64        `yaml:"updated_at"`
	Devices     []Device     `yaml:"devices,omitempty"`
	Networks    []Network    `yaml:"networks,omitempty"`
	Connections []Connection `yaml:"connections,omitempty"`
	Permissions []Permission `yaml:"permissions,omitempty"`
}

// ErrNotFound - the lab does not exist.
var ErrNotFound = errors.New("Laboratory not found")

// Store - all labs in memory. Writes go through a temporary file and rename; memory changes
// only after a successful write. Safe for concurrent use.
type Store struct {
	mu   sync.Mutex
	dir  string
	labs map[string]Lab // lower-cased name -> lab
	// folder - the folder path of each lab, keyed by the lower-cased name.
	folder map[string]string
	// owner - an "object -> lab" index for lookup by name.
	owner map[objKey]bool
}

type objKey struct {
	kind string
	name string
	lab  string
}

// Kinds of objects inside a lab.
const (
	KindDevice     = "device"
	KindNetwork    = "network"
	KindConnection = "connection"
)

func labKey(name string) string { return strings.ToLower(name) }

func (s *Store) index(l Lab) {
	for _, d := range l.Devices {
		s.owner[objKey{KindDevice, d.Name, labKey(l.Name)}] = true
	}
	for _, n := range l.Networks {
		s.owner[objKey{KindNetwork, n.Name, labKey(l.Name)}] = true
	}
	for _, c := range l.Connections {
		s.owner[objKey{KindConnection, c.Key(), labKey(l.Name)}] = true
	}
}

func (s *Store) unindex(l Lab) {
	for _, d := range l.Devices {
		delete(s.owner, objKey{KindDevice, d.Name, labKey(l.Name)})
	}
	for _, n := range l.Networks {
		delete(s.owner, objKey{KindNetwork, n.Name, labKey(l.Name)})
	}
	for _, c := range l.Connections {
		delete(s.owner, objKey{KindConnection, c.Key(), labKey(l.Name)})
	}
}

// Open reads the laboratory directory. An empty or missing directory - an empty store.
func Open(dir string) (*Store, error) {
	s := &Store{dir: dir, labs: map[string]Lab{}, folder: map[string]string{}, owner: map[objKey]bool{}}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	tree, err := fstree.Scan(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range tree.Labs {
		data, err := os.ReadFile(fstree.Abs(dir, fstree.LabPath(entry.Folder, entry.Name)))
		if err != nil {
			return nil, err
		}
		var l Lab
		if err := yaml.Unmarshal(data, &l); err != nil {
			return nil, msgs.Wrap(err, "Laboratory {name}: {detail}", map[string]string{"name": entry.Name, "detail": err.Error()})
		}
		// The file name is the identifier and the source of truth for the name.
		l.Name = entry.Name
		s.labs[labKey(l.Name)] = l
		s.folder[labKey(l.Name)] = entry.Folder
		s.index(l)
	}
	return s, nil
}

// Labs returns copies of all labs sorted by name.
func (s *Store) Labs() []Lab {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Lab, 0, len(s.labs))
	for _, l := range s.labs {
		out = append(out, clone(l))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Lab returns a copy of the lab by name.
func (s *Store) Lab(name string) (Lab, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.labs[labKey(name)]
	if !ok {
		return Lab{}, false
	}
	return clone(l), true
}

// refreshLocked reads the current folder of every lab from the directory tree. The tree is
// changed by the folders package, so the cached placement may be stale between calls.
func (s *Store) refreshLocked() {
	tree, err := fstree.Scan(s.dir)
	if err != nil {
		return
	}
	for _, entry := range tree.Labs {
		key := labKey(entry.Name)
		if _, ok := s.labs[key]; ok {
			s.folder[key] = entry.Folder
		}
	}
}

// Root returns the laboratory directory.
func (s *Store) Root() string { return s.dir }

// Tree returns the current directory tree of the laboratory directory.
func (s *Store) Tree() (fstree.Tree, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fstree.Scan(s.dir)
}

// Folder returns the folder path of a lab ("" - the root).
func (s *Store) Folder(name string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshLocked()
	f, ok := s.folder[labKey(name)]
	if !ok {
		return "", false
	}
	return f, true
}

// Save creates or replaces a lab. A new lab is created in the root folder; an existing lab stays
// where it is. Validates cable references, port occupancy and cascade.
func (s *Store) Save(l Lab) (Lab, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(l)
}

func (s *Store) saveLocked(l Lab) (Lab, error) {
	s.refreshLocked()
	if s.dir == "" {
		return Lab{}, errors.New("The laboratory directory is not set")
	}
	l.Name = strings.TrimSpace(l.Name)
	if err := names.Check("Laboratory name", l.Name); err != nil {
		return Lab{}, err
	}
	key := labKey(l.Name)
	out := clone(l)
	prev, exists := s.labs[key]
	if err := normalize(&out, prev); err != nil {
		return Lab{}, err
	}
	folder := s.folder[key]
	data, err := yamlfmt.Marshal(out)
	if err != nil {
		return Lab{}, err
	}
	if err := fstree.WriteLab(s.dir, folder, out.Name, data); err != nil {
		return Lab{}, err
	}
	if exists {
		s.unindex(prev)
	}
	s.labs[key] = out
	s.folder[key] = folder
	s.index(out)
	return clone(out), nil
}

// Update changes the lab atomically: reads the current version, calls fn and saves the result.
// While fn runs, other operations on the store wait - so parallel requests do not lose edits.
// An error from fn cancels the change.
func (s *Store) Update(name string, fn func(*Lab) error) (Lab, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.labs[labKey(name)]
	if !ok {
		return Lab{}, ErrNotFound
	}
	next := clone(cur)
	if err := fn(&next); err != nil {
		return Lab{}, err
	}
	return s.saveLocked(next)
}

// Delete deletes the lab together with its file.
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshLocked()
	key := labKey(name)
	l, ok := s.labs[key]
	if !ok {
		return ErrNotFound
	}
	if err := fstree.RemoveLab(s.dir, s.folder[key], l.Name); err != nil {
		return err
	}
	s.unindex(l)
	delete(s.labs, key)
	delete(s.folder, key)
	return nil
}

// Move moves a lab file into another folder ("" - the root). The lab keeps its name.
func (s *Store) Move(name, folder string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshLocked()
	key := labKey(name)
	l, ok := s.labs[key]
	if !ok {
		return ErrNotFound
	}
	from := s.folder[key]
	if err := fstree.MoveLab(s.dir, from, l.Name, folder); err != nil {
		return err
	}
	s.folder[key] = folder
	return nil
}

// Rename changes the lab name, keeping its folder and content.
func (s *Store) Rename(oldName, newName string) (Lab, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshLocked()
	oldKey := labKey(oldName)
	l, ok := s.labs[oldKey]
	if !ok {
		return Lab{}, ErrNotFound
	}
	newName = strings.TrimSpace(newName)
	if err := names.Check("Laboratory name", newName); err != nil {
		return Lab{}, err
	}
	if oldKey == labKey(newName) && l.Name == newName {
		return clone(l), nil
	}
	folder := s.folder[oldKey]
	out := clone(l)
	out.Name = newName
	data, err := yamlfmt.Marshal(out)
	if err != nil {
		return Lab{}, err
	}
	if err := fstree.WriteLab(s.dir, folder, newName, data); err != nil {
		return Lab{}, err
	}
	if err := fstree.RemoveLab(s.dir, folder, l.Name); err != nil {
		_ = fstree.RemoveLab(s.dir, folder, newName)
		return Lab{}, err
	}
	s.unindex(l)
	delete(s.labs, oldKey)
	delete(s.folder, oldKey)
	newKey := labKey(newName)
	s.labs[newKey] = out
	s.folder[newKey] = folder
	s.index(out)
	return clone(out), nil
}

// LabOf returns the lab that owns the object kind with name, if it is the lab labName.
func (s *Store) LabOf(kind, labName, objectName string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.owner[objKey{kind, objectName, labKey(labName)}] {
		return "", false
	}
	return s.labs[labKey(labName)].Name, true
}

// Device returns a device by name and the lab that owns it.
func (s *Store) Device(labName, name string) (Device, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.labs[labKey(labName)]
	if !ok {
		return Device{}, "", false
	}
	for _, d := range l.Devices {
		if d.Name == name {
			return d, l.Name, true
		}
	}
	return Device{}, "", false
}

// Network returns a network by name and the lab that owns it.
func (s *Store) Network(labName, name string) (Network, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.labs[labKey(labName)]
	if !ok {
		return Network{}, "", false
	}
	for _, n := range l.Networks {
		if n.Name == name {
			return n, l.Name, true
		}
	}
	return Network{}, "", false
}

// Connection returns a cable by its key and the lab that owns it.
func (s *Store) Connection(labName, key string) (Connection, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.labs[labKey(labName)]
	if !ok {
		return Connection{}, "", false
	}
	for _, c := range l.Connections {
		if c.Key() == key {
			return c, l.Name, true
		}
	}
	return Connection{}, "", false
}

// AllDevices returns devices of all labs (copies).
func (s *Store) AllDevices() []Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Device
	for _, l := range s.labs {
		out = append(out, l.Devices...)
	}
	return out
}

// normalize validates references and applies the cascade. A cable whose end was in the previous
// version of the lab and is deleted now is removed together with the object. A reference to an
// unknown object is an error.
func normalize(l *Lab, prev Lab) error {
	devices := map[string]bool{}
	for _, d := range l.Devices {
		if err := names.Check("Device name", d.Name); err != nil {
			return err
		}
		if devices[d.Name] {
			return msgs.New("Device {name} is listed twice", map[string]string{"name": d.Name})
		}
		devices[d.Name] = true
	}
	networks := map[string]bool{}
	for _, n := range l.Networks {
		if err := names.Check("Network name", n.Name); err != nil {
			return err
		}
		if networks[n.Name] {
			return msgs.New("Network {name} is listed twice", map[string]string{"name": n.Name})
		}
		networks[n.Name] = true
	}
	prevDevices := map[string]bool{}
	for _, d := range prev.Devices {
		prevDevices[d.Name] = true
	}
	prevNetworks := map[string]bool{}
	for _, n := range prev.Networks {
		prevNetworks[n.Name] = true
	}
	// state: 0 - the end exists, 1 - was deleted (cascade), 2 - unknown.
	state := func(e End) int {
		switch e.Type {
		case "device":
			if devices[e.Name] {
				return 0
			}
			if prevDevices[e.Name] {
				return 1
			}
		case "network":
			if networks[e.Name] {
				return 0
			}
			if prevNetworks[e.Name] {
				return 1
			}
		}
		return 2
	}
	kept := make([]Connection, 0, len(l.Connections))
	seenConn := map[string]bool{}
	for _, c := range l.Connections {
		sa, sb := state(c.A), state(c.B)
		if sa == 2 || sb == 2 {
			return msgs.New("Cable {key}: end not found in the laboratory", map[string]string{"key": c.Key()})
		}
		if seenConn[c.Key()] {
			return msgs.New("Cable {key} is listed twice", map[string]string{"key": c.Key()})
		}
		seenConn[c.Key()] = true
		if sa == 0 && sb == 0 {
			kept = append(kept, c)
		}
	}
	l.Connections = kept

	ports := map[string]bool{}
	for _, c := range l.Connections {
		for _, e := range []End{c.A, c.B} {
			if e.Type != "device" {
				continue
			}
			key := fmt.Sprintf("%s:%d", e.Name, e.If)
			if ports[key] {
				return msgs.New("Port {port} of device {name} is already connected by a cable", map[string]string{"port": strconv.Itoa(e.If), "name": e.Name})
			}
			ports[key] = true
		}
	}
	return nil
}

func clone(l Lab) Lab {
	out := l
	out.Devices = append([]Device(nil), l.Devices...)
	out.Networks = append([]Network(nil), l.Networks...)
	out.Connections = append([]Connection(nil), l.Connections...)
	out.Permissions = append([]Permission(nil), l.Permissions...)
	return out
}

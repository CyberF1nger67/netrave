// SPDX-License-Identifier: GPL-3.0-only

// Package labfile - lab projects in files. Each lab lives in its own folder
// <labs directory>/<id>/lab.yaml: its parameters, devices, networks, wires and user permissions.
// The shared identifier counter lives in <labs directory>/index.yaml.
//
// Identifiers are issued in increasing order and are not reused, so references from other stores
// (lab instances) remain unambiguous after objects are deleted.
//
// Integrity is checked on save: a wire references existing ends of the same lab,
// one device port is occupied by at most one wire, deleting a device or network deletes their wires.
package labfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"

	"gopkg.in/yaml.v3"
)

// Device - a virtual machine in the lab. The fields mirror the former devices table.
type Device struct {
	ID         int64   `yaml:"id"`
	TemplateID int64   `yaml:"template_id,omitempty"`
	ImageID    int64   `yaml:"image_id,omitempty"`
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
	ID          int64   `yaml:"id"`
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

// End - a wire end: a device (Type "device") or a network (Type "network") and the port number.
type End struct {
	Type string `yaml:"type"`
	ID   int64  `yaml:"id"`
	If   int    `yaml:"if"`
}

// Connection - a wire between two ends.
type Connection struct {
	ID int64 `yaml:"id"`
	A  End   `yaml:"a"`
	B  End   `yaml:"b"`
}

// Permission - a user's permission on the lab (the former lab_permissions table).
type Permission struct {
	UserID    int64  `yaml:"user_id"`
	Perm      string `yaml:"perm"`
	Value     bool   `yaml:"value"`
	UpdatedAt int64  `yaml:"updated_at"`
	UpdatedBy int64  `yaml:"updated_by,omitempty"`
}

// Lab - a whole lab project.
type Lab struct {
	ID          int64        `yaml:"id"`
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
var ErrNotFound = errors.New("лаборатория не найдена")

const (
	indexFile = "index.yaml"
	labFile   = "lab.yaml"
)

type index struct {
	NextID int64 `yaml:"next_id"`
}

// Store - all labs in memory. Writes go through a temporary file and rename; memory changes
// only after a successful write. Safe for concurrent use.
type Store struct {
	mu   sync.Mutex
	dir  string
	next int64
	labs map[int64]Lab
	// owner - an "object -> lab" index for lookup by number: devices, networks, wires.
	owner map[objKey]int64
}

type objKey struct {
	kind string
	id   int64
}

// Kinds of objects that have their own numbers inside a lab.
const (
	KindDevice     = "device"
	KindNetwork    = "network"
	KindConnection = "connection"
)

func (s *Store) reindex(l Lab) {
	for _, d := range l.Devices {
		s.owner[objKey{KindDevice, d.ID}] = l.ID
	}
	for _, n := range l.Networks {
		s.owner[objKey{KindNetwork, n.ID}] = l.ID
	}
	for _, c := range l.Connections {
		s.owner[objKey{KindConnection, c.ID}] = l.ID
	}
}

func (s *Store) unindex(l Lab) {
	for _, d := range l.Devices {
		delete(s.owner, objKey{KindDevice, d.ID})
	}
	for _, n := range l.Networks {
		delete(s.owner, objKey{KindNetwork, n.ID})
	}
	for _, c := range l.Connections {
		delete(s.owner, objKey{KindConnection, c.ID})
	}
}

// LabOf returns the lab that owns the object kind with number id.
func (s *Store) LabOf(kind string, id int64) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lab, ok := s.owner[objKey{kind, id}]
	return lab, ok
}

// Open reads the labs directory. An empty or missing directory - an empty store.
func Open(dir string) (*Store, error) {
	s := &Store{dir: dir, next: 1, labs: map[int64]Lab{}, owner: map[objKey]int64{}}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	if data, err := os.ReadFile(filepath.Join(dir, indexFile)); err == nil {
		var idx index
		if err := yaml.Unmarshal(data, &idx); err != nil {
			return nil, fmt.Errorf("индекс лабораторий: %w", err)
		}
		if idx.NextID > s.next {
			s.next = idx.NextID
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id, err := strconv.ParseInt(e.Name(), 10, 64)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name(), labFile))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var l Lab
		if err := yaml.Unmarshal(data, &l); err != nil {
			return nil, fmt.Errorf("лаборатория %d: %w", id, err)
		}
		if l.ID != id {
			return nil, fmt.Errorf("лаборатория в каталоге %d записана как %d", id, l.ID)
		}
		s.labs[id] = l
		s.reindex(l)
		if id >= s.next {
			s.next = id + 1
		}
	}
	return s, nil
}

// Labs returns copies of all labs by number.
func (s *Store) Labs() []Lab {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Lab, 0, len(s.labs))
	for _, l := range s.labs {
		out = append(out, clone(l))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Lab returns a copy of the lab.
func (s *Store) Lab(id int64) (Lab, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.labs[id]
	if !ok {
		return Lab{}, false
	}
	return clone(l), true
}

// Save creates or replaces a lab. Objects with zero number receive new numbers.
// Validates wire references, port occupancy and cascade: wires whose ends are deleted are removed.
func (s *Store) Save(l Lab) (Lab, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(l)
}

func (s *Store) saveLocked(l Lab) (Lab, error) {
	if s.dir == "" {
		return Lab{}, errors.New("каталог лабораторий не задан")
	}
	if l.Name == "" {
		return Lab{}, errors.New("у лаборатории нет названия")
	}
	next := s.next
	alloc := func() int64 { v := next; next++; return v }

	out := clone(l)
	if out.ID == 0 {
		out.ID = alloc()
	} else if _, ok := s.labs[out.ID]; !ok {
		return Lab{}, ErrNotFound
	}
	assignIDs(&out, alloc)
	if err := normalize(&out, s.labs[out.ID]); err != nil {
		return Lab{}, err
	}

	data, err := yaml.Marshal(out)
	if err != nil {
		return Lab{}, err
	}
	labDir := filepath.Join(s.dir, strconv.FormatInt(out.ID, 10))
	if err := os.MkdirAll(labDir, 0o750); err != nil {
		return Lab{}, err
	}
	// Counter first: after a failure numbers will not be issued again.
	if next != s.next {
		if err := s.writeIndex(next); err != nil {
			return Lab{}, err
		}
	}
	if err := writeAtomic(filepath.Join(labDir, labFile), data); err != nil {
		return Lab{}, err
	}
	s.next = next
	s.unindex(s.labs[out.ID])
	s.labs[out.ID] = out
	s.reindex(out)
	return clone(out), nil
}

// NewID reserves a number for an object that will be saved later in the same lab (e.g. a wire
// referencing a device from the same batch). The number issued by Save and the reserved one do not overlap.
// The reservation is not written to disk until the nearest Save: without it the number is not taken.
func (s *Store) NewID() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.next
	s.next++
	return v
}

// Update changes the lab atomically: reads the current version, calls fn and saves the result.
// While fn runs, other operations on the store wait - so parallel requests do not lose edits.
// An error from fn cancels the change.
func (s *Store) Update(id int64, fn func(*Lab) error) (Lab, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.labs[id]
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
func (s *Store) Delete(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.labs[id]; !ok {
		return ErrNotFound
	}
	labDir := filepath.Join(s.dir, strconv.FormatInt(id, 10))
	if err := os.Remove(filepath.Join(labDir, labFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = os.Remove(labDir) // delete the directory only if it is empty
	s.unindex(s.labs[id])
	delete(s.labs, id)
	return nil
}

func (s *Store) writeIndex(next int64) error {
	data, err := yaml.Marshal(index{NextID: next})
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(s.dir, indexFile), data)
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// assignIDs assigns numbers to new devices, networks and wires.
func assignIDs(l *Lab, alloc func() int64) {
	for i := range l.Devices {
		if l.Devices[i].ID == 0 {
			l.Devices[i].ID = alloc()
		}
	}
	for i := range l.Networks {
		if l.Networks[i].ID == 0 {
			l.Networks[i].ID = alloc()
		}
	}
	for i := range l.Connections {
		if l.Connections[i].ID == 0 {
			l.Connections[i].ID = alloc()
		}
	}
}

// normalize validates references and applies the cascade. A wire whose end was in the previous version of the lab and
// is deleted now is removed together with the object. A reference to an unknown object is an error.
func normalize(l *Lab, prev Lab) error {
	devices := map[int64]bool{}
	for _, d := range l.Devices {
		if devices[d.ID] {
			return fmt.Errorf("устройство %d указано дважды", d.ID)
		}
		devices[d.ID] = true
	}
	networks := map[int64]bool{}
	for _, n := range l.Networks {
		if networks[n.ID] {
			return fmt.Errorf("сеть %d указана дважды", n.ID)
		}
		networks[n.ID] = true
	}
	prevDevices := map[int64]bool{}
	for _, d := range prev.Devices {
		prevDevices[d.ID] = true
	}
	prevNetworks := map[int64]bool{}
	for _, n := range prev.Networks {
		prevNetworks[n.ID] = true
	}
	// state: 0 - the end exists, 1 - was deleted (cascade), 2 - unknown.
	state := func(e End) int {
		switch e.Type {
		case "device":
			if devices[e.ID] {
				return 0
			}
			if prevDevices[e.ID] {
				return 1
			}
		case "network":
			if networks[e.ID] {
				return 0
			}
			if prevNetworks[e.ID] {
				return 1
			}
		}
		return 2
	}
	kept := make([]Connection, 0, len(l.Connections))
	for _, c := range l.Connections {
		sa, sb := state(c.A), state(c.B)
		if sa == 2 || sb == 2 {
			return fmt.Errorf("провод %d: конец не найден в лаборатории", c.ID)
		}
		if sa == 0 && sb == 0 {
			kept = append(kept, c)
		}
	}
	l.Connections = kept

	ports := map[[2]int64]bool{}
	connIDs := map[int64]bool{}
	for _, c := range l.Connections {
		if connIDs[c.ID] {
			return fmt.Errorf("провод %d указан дважды", c.ID)
		}
		connIDs[c.ID] = true
		for _, e := range []End{c.A, c.B} {
			if e.Type != "device" {
				continue
			}
			key := [2]int64{e.ID, int64(e.If)}
			if ports[key] {
				return fmt.Errorf("порт %d устройства %d уже подключён кабелем", e.If, e.ID)
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

// Device returns a device by number and the lab that owns it.
func (s *Store) Device(id int64) (Device, int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	labID, ok := s.owner[objKey{KindDevice, id}]
	if !ok {
		return Device{}, 0, false
	}
	for _, d := range s.labs[labID].Devices {
		if d.ID == id {
			return d, labID, true
		}
	}
	return Device{}, 0, false
}

// Network returns a network by number and the lab that owns it.
func (s *Store) Network(id int64) (Network, int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	labID, ok := s.owner[objKey{KindNetwork, id}]
	if !ok {
		return Network{}, 0, false
	}
	for _, n := range s.labs[labID].Networks {
		if n.ID == id {
			return n, labID, true
		}
	}
	return Network{}, 0, false
}

// Connection returns a wire by number and the lab that owns it.
func (s *Store) Connection(id int64) (Connection, int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	labID, ok := s.owner[objKey{KindConnection, id}]
	if !ok {
		return Connection{}, 0, false
	}
	for _, c := range s.labs[labID].Connections {
		if c.ID == id {
			return c, labID, true
		}
	}
	return Connection{}, 0, false
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

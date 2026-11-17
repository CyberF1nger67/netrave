// SPDX-License-Identifier: GPL-3.0-only

package catalog

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"

	"netrave/internal/names"
)

// Image is a folder with disks/ISOs (kind=image) or a layer on top of a parent image (kind=snapshot).
type Image struct {
	ID         int64  `yaml:"id"`
	Name       string `yaml:"name"`
	Dirname    string `yaml:"dirname"`
	Kind       string `yaml:"kind"`
	ParentID   int64  `yaml:"parent_id,omitempty"`
	DeviceType string `yaml:"device_type,omitempty"`
	LayoutJSON string `yaml:"layout_json,omitempty"`
	QemuJSON   string `yaml:"qemu_json,omitempty"`
	SizeBytes  int64  `yaml:"size_bytes,omitempty"`
	CreatedAt  int64  `yaml:"created_at"`
}

// Template is a machine template: parameters used to create lab machines.
type Template struct {
	ID         int64  `yaml:"id"`
	Name       string `yaml:"name"`
	Vendor     string `yaml:"vendor,omitempty"`
	Type       string `yaml:"type,omitempty"`
	Category   string `yaml:"category,omitempty"`
	Arch       string `yaml:"arch"`
	Machine    string `yaml:"machine"`
	CPU        int    `yaml:"cpu"`
	RAMMB      int    `yaml:"ram_mb"`
	NICs       int    `yaml:"nics"`
	NICModel   string `yaml:"nic_model"`
	DiskBus    string `yaml:"disk_bus"`
	BIOS       string `yaml:"bios"`
	Console    string `yaml:"console"`
	ExtraArgs  string `yaml:"extra_args,omitempty"`
	IfaceType  string `yaml:"iface_type"`
	IfaceStart int    `yaml:"iface_start"`
	ImageID    int64  `yaml:"image_id,omitempty"`
	Icon       string `yaml:"icon,omitempty"`
	CustomIcon string `yaml:"custom_icon,omitempty"`
	// VNC - VNC console image settings; apply to machines created from the template.
	VNC VNCQuality `yaml:"vnc"`
}

// Snapshot is a state snapshot of an instance machine (device_id is the machine's instance row, rid).
type Snapshot struct {
	ID        int64  `yaml:"id" json:"id"`
	DeviceID  int64  `yaml:"device_id" json:"device_id"`
	Name      string `yaml:"name" json:"name"`
	CreatedAt int64  `yaml:"created_at" json:"created_at"`
}

// Store - all catalogs.
type Store struct {
	images    *table[Image]
	templates *table[Template]
	snapshots *table[Snapshot]
}

var customIconRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Open opens the catalog directory. dir contains the images, templates and snapshots folders: in each
// an entry is a separate YAML file named after the entry, and index.yaml stores the number counter.
func Open(dir string) (*Store, error) {
	imgs, _, err := openTable(filepath.Join(dir, "images"),
		func(r *Image) *int64 { return &r.ID }, func(r *Image) string { return r.Dirname })
	if err != nil {
		return nil, err
	}
	tpls, _, err := openTable(filepath.Join(dir, "templates"),
		func(r *Template) *int64 { return &r.ID }, func(r *Template) string { return r.Name })
	if err != nil {
		return nil, err
	}
	snaps, _, err := openTable(filepath.Join(dir, "snapshots"),
		func(r *Snapshot) *int64 { return &r.ID }, snapshotKey)
	if err != nil {
		return nil, err
	}
	return &Store{images: imgs, templates: tpls, snapshots: snaps}, nil
}

// snapshotKey is the snapshot file name: the machine and the snapshot name (the name is unique within the machine).
func snapshotKey(s *Snapshot) string {
	return strconv.FormatInt(s.DeviceID, 10) + "-" + s.Name
}

// ---------- images ----------

// Images returns all images.
func (s *Store) Images() []Image { return s.images.All() }

// Image returns an image by number.
func (s *Store) Image(id int64) (Image, bool) { return s.images.Get(id) }

// ImageByDirname looks up an image by folder name.
func (s *Store) ImageByDirname(dirname string) (Image, bool) {
	for _, im := range s.images.All() {
		if im.Dirname == dirname {
			return im, true
		}
	}
	return Image{}, false
}

// ImageChildren is how many layers depend on the image.
func (s *Store) ImageChildren(parentID int64) int {
	n := 0
	for _, im := range s.images.All() {
		if im.ParentID == parentID {
			n++
		}
	}
	return n
}

// AddImage adds an image, checking folder uniqueness, type and parent; the number is assigned here.
func (s *Store) AddImage(im Image) (int64, error) {
	if err := checkImage(s, im); err != nil {
		return 0, err
	}
	var id int64
	err := s.images.update(func(rows *[]Image, newID func() int64) error {
		for _, r := range *rows {
			if r.Dirname == im.Dirname {
				return fmt.Errorf("папка образа %q: %w", im.Dirname, ErrExists)
			}
		}
		id = newID()
		im.ID = id
		*rows = append(*rows, im)
		return nil
	})
	return id, err
}

// UpdateImage changes the image with fn. An error from fn or validation leaves the image unchanged.
func (s *Store) UpdateImage(id int64, fn func(*Image) error) error {
	return s.images.update(func(rows *[]Image, _ func() int64) error {
		for i := range *rows {
			if (*rows)[i].ID == id {
				cp := (*rows)[i]
				if err := fn(&cp); err != nil {
					return err
				}
				if err := checkImage(s, cp); err != nil {
					return err
				}
				(*rows)[i] = cp
				return nil
			}
		}
		return ErrNotFound
	})
}

// DeleteImage deletes an image. Layers that depend on it prevent deletion (ON DELETE RESTRICT);
// templates that referenced it lose the reference (ON DELETE SET NULL).
func (s *Store) DeleteImage(id int64) error {
	if s.ImageChildren(id) > 0 {
		return ErrInUse
	}
	if err := s.images.update(func(rows *[]Image, _ func() int64) error {
		found := false
		kept := (*rows)[:0]
		for _, r := range *rows {
			if r.ID == id {
				found = true
				continue
			}
			kept = append(kept, r)
		}
		if !found {
			return ErrNotFound
		}
		*rows = kept
		return nil
	}); err != nil {
		return err
	}
	return s.templates.update(func(rows *[]Template, _ func() int64) error {
		for i := range *rows {
			if (*rows)[i].ImageID == id {
				(*rows)[i].ImageID = 0
			}
		}
		return nil
	})
}

// ErrInUse - the entry cannot be deleted while it is referenced.
var ErrInUse = fmt.Errorf("запись используется")

// ErrExists - an entry with this name or folder already exists.
var ErrExists = fmt.Errorf("запись уже существует")

func checkImage(s *Store, im Image) error {
	switch im.Kind {
	case "image":
		if im.ParentID != 0 {
			return fmt.Errorf("образ не может опираться на родителя")
		}
	case "snapshot":
		if im.ParentID == 0 {
			return fmt.Errorf("слой должен опираться на родительский образ")
		}
		if _, ok := s.images.Get(im.ParentID); !ok {
			return fmt.Errorf("родительский образ %d не найден", im.ParentID)
		}
	default:
		return fmt.Errorf("неизвестный вид образа %q", im.Kind)
	}
	if im.Dirname == "" || im.Name == "" {
		return fmt.Errorf("у образа должны быть имя и папка")
	}
	if err := names.Check("папка образа", im.Dirname); err != nil {
		return err
	}
	return names.Check("имя образа", im.Name)
}

// ---------- templates ----------

// Templates returns all templates.
func (s *Store) Templates() []Template { return s.templates.All() }

// Template returns a template by number.
func (s *Store) Template(id int64) (Template, bool) { return s.templates.Get(id) }

// AddTemplate adds a template; the name is unique, values are validated as previously by table constraints.
func (s *Store) AddTemplate(t Template) (int64, error) {
	if err := s.checkTemplate(t); err != nil {
		return 0, err
	}
	var id int64
	err := s.templates.update(func(rows *[]Template, newID func() int64) error {
		for _, r := range *rows {
			if r.Name == t.Name {
				return fmt.Errorf("шаблон %q: %w", t.Name, ErrExists)
			}
		}
		id = newID()
		t.ID = id
		*rows = append(*rows, t)
		return nil
	})
	return id, err
}

// UpdateTemplate changes the template with fn.
func (s *Store) UpdateTemplate(id int64, fn func(*Template) error) error {
	return s.templates.update(func(rows *[]Template, _ func() int64) error {
		for i := range *rows {
			if (*rows)[i].ID == id {
				cp := (*rows)[i]
				if err := fn(&cp); err != nil {
					return err
				}
				if err := s.checkTemplate(cp); err != nil {
					return err
				}
				for j, r := range *rows {
					if j != i && r.Name == cp.Name {
						return fmt.Errorf("шаблон %q: %w", cp.Name, ErrExists)
					}
				}
				(*rows)[i] = cp
				return nil
			}
		}
		return ErrNotFound
	})
}

// DeleteTemplate deletes the template.
func (s *Store) DeleteTemplate(id int64) error {
	return s.templates.update(func(rows *[]Template, _ func() int64) error {
		for i, r := range *rows {
			if r.ID == id {
				*rows = append((*rows)[:i], (*rows)[i+1:]...)
				return nil
			}
		}
		return ErrNotFound
	})
}

func (s *Store) checkTemplate(t Template) error {
	if t.Name == "" {
		return fmt.Errorf("у шаблона должно быть имя")
	}
	if err := names.Check("шаблон", t.Name); err != nil {
		return err
	}
	if t.BIOS != "seabios" && t.BIOS != "uefi" {
		return fmt.Errorf("неизвестный BIOS %q", t.BIOS)
	}
	if t.Console != "serial" && t.Console != "vnc" {
		return fmt.Errorf("неизвестная консоль %q", t.Console)
	}
	if err := t.VNC.Validate(); err != nil {
		return err
	}
	if t.CustomIcon != "" && !customIconRe.MatchString(t.CustomIcon) {
		return fmt.Errorf("некорректный хеш иконки")
	}
	if t.ImageID != 0 {
		if _, ok := s.images.Get(t.ImageID); !ok {
			return fmt.Errorf("образ %d не найден", t.ImageID)
		}
	}
	return nil
}

// ---------- snapshots ----------

// SnapshotsOf returns the snapshots of an instance machine in creation order.
func (s *Store) SnapshotsOf(deviceID int64) []Snapshot {
	var out []Snapshot
	for _, sn := range s.snapshots.All() {
		if sn.DeviceID == deviceID {
			out = append(out, sn)
		}
	}
	return out
}

// Snapshot returns a snapshot by number.
func (s *Store) Snapshot(id int64) (Snapshot, bool) { return s.snapshots.Get(id) }

// AddSnapshot adds a snapshot. The name is unique within the machine.
func (s *Store) AddSnapshot(deviceID int64, name string, createdAt int64) (Snapshot, error) {
	if err := names.Check("снапшот", name); err != nil {
		return Snapshot{}, err
	}
	var out Snapshot
	err := s.snapshots.update(func(rows *[]Snapshot, newID func() int64) error {
		for _, r := range *rows {
			if r.DeviceID == deviceID && r.Name == name {
				return fmt.Errorf("снапшот %q: %w", name, ErrExists)
			}
		}
		out = Snapshot{ID: newID(), DeviceID: deviceID, Name: name, CreatedAt: createdAt}
		*rows = append(*rows, out)
		return nil
	})
	return out, err
}

// DeleteSnapshot deletes a snapshot.
func (s *Store) DeleteSnapshot(id int64) error {
	return s.snapshots.update(func(rows *[]Snapshot, _ func() int64) error {
		for i, r := range *rows {
			if r.ID == id {
				*rows = append((*rows)[:i], (*rows)[i+1:]...)
				return nil
			}
		}
		return ErrNotFound
	})
}

// DropSnapshotsNotIn deletes snapshots of machines that are not in live (their instance rows are gone).
// Instance row numbers can be reused, so such a snapshot must not be left behind.
func (s *Store) DropSnapshotsNotIn(live map[int64]bool) error {
	return s.snapshots.update(func(rows *[]Snapshot, _ func() int64) error {
		kept := (*rows)[:0]
		for _, r := range *rows {
			if live[r.DeviceID] {
				kept = append(kept, r)
			}
		}
		*rows = kept
		return nil
	})
}

// DeleteSnapshotsOf deletes all snapshots of machines listed in devices (instance rows).
// Previously this was done by a foreign key cascade when an instance row was deleted.
func (s *Store) DeleteSnapshotsOf(devices ...int64) error {
	if len(devices) == 0 {
		return nil
	}
	drop := map[int64]bool{}
	for _, d := range devices {
		drop[d] = true
	}
	return s.snapshots.update(func(rows *[]Snapshot, _ func() int64) error {
		kept := (*rows)[:0]
		for _, r := range *rows {
			if !drop[r.DeviceID] {
				kept = append(kept, r)
			}
		}
		*rows = kept
		return nil
	})
}

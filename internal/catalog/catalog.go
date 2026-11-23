// SPDX-License-Identifier: GPL-3.0-only

package catalog

import (
	"fmt"
	"path/filepath"
	"regexp"

	"netrave/internal/msgs"
	"netrave/internal/names"
)

// Image is a folder with disks and ISOs.
type Image struct {
	ID         int64  `yaml:"id"`
	Name       string `yaml:"name"`
	Dirname    string `yaml:"dirname"`
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

// Store - all catalogs.
type Store struct {
	images    *table[Image]
	templates *table[Template]
}

var customIconRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Open opens the catalog directory. dir contains the images and templates folders: in each
// an entry is a separate YAML file named after the entry.
func Open(dir string) (*Store, error) {
	imgs, err := openTable(filepath.Join(dir, "images"),
		func(r *Image) *int64 { return &r.ID }, func(r *Image) string { return r.Dirname })
	if err != nil {
		return nil, err
	}
	tpls, err := openTable(filepath.Join(dir, "templates"),
		func(r *Template) *int64 { return &r.ID }, func(r *Template) string { return r.Name })
	if err != nil {
		return nil, err
	}
	return &Store{images: imgs, templates: tpls}, nil
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

// AddImage adds an image, checking folder uniqueness and names; the number is assigned here.
func (s *Store) AddImage(im Image) (int64, error) {
	if err := checkImage(s, im); err != nil {
		return 0, err
	}
	var id int64
	err := s.images.update(func(rows *[]Image, newID func() int64) error {
		for _, r := range *rows {
			if r.Dirname == im.Dirname {
				return msgs.Wrap(ErrExists, "Image folder «{name}»: {detail}", map[string]string{"name": im.Dirname, "detail": ErrExists.Error()})
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

// DeleteImage deletes an image. Templates that referenced it lose the reference (ON DELETE SET NULL).
func (s *Store) DeleteImage(id int64) error {
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

// ErrExists - an entry with this name or folder already exists.
var ErrExists = fmt.Errorf("The record already exists")

func checkImage(s *Store, im Image) error {
	if im.Dirname == "" || im.Name == "" {
		return fmt.Errorf("The image must have a name and a folder")
	}
	if err := names.Check("Image folder", im.Dirname); err != nil {
		return err
	}
	return names.Check("Image name", im.Name)
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
				return msgs.Wrap(ErrExists, "Template «{name}»: {detail}", map[string]string{"name": t.Name, "detail": ErrExists.Error()})
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
						return msgs.Wrap(ErrExists, "Template «{name}»: {detail}", map[string]string{"name": cp.Name, "detail": ErrExists.Error()})
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
		return fmt.Errorf("The template must have a name")
	}
	if err := names.Check("Template", t.Name); err != nil {
		return err
	}
	if t.BIOS != "seabios" && t.BIOS != "uefi" {
		return msgs.New("Unknown BIOS «{bios}»", map[string]string{"bios": t.BIOS})
	}
	if t.Console != "serial" && t.Console != "vnc" {
		return msgs.New("Unknown console «{console}»", map[string]string{"console": t.Console})
	}
	if err := t.VNC.Validate(); err != nil {
		return err
	}
	if t.CustomIcon != "" && !customIconRe.MatchString(t.CustomIcon) {
		return fmt.Errorf("Invalid icon hash")
	}
	if t.ImageID != 0 {
		if _, ok := s.images.Get(t.ImageID); !ok {
			return msgs.New("Image {id} not found", map[string]string{"id": fmt.Sprint(t.ImageID)})
		}
	}
	return nil
}

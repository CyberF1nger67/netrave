// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"time"

	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"netrave/internal/database"
	"netrave/internal/labfile"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"netrave/internal/access"
	"netrave/internal/auth"
	"netrave/internal/httpx"
	"netrave/internal/icons"
	"netrave/internal/natcloud"
	"netrave/internal/notify"
)

// Lab export file format - YAML.
type ExpDevice struct {
	Ref      int64  `json:"ref" yaml:"ref"`
	Name     string `json:"name" yaml:"name"`
	Template string `json:"template,omitempty" yaml:"template,omitempty"`
	Icon     string `json:"icon,omitempty" yaml:"icon,omitempty"`
	// IconPNG - a custom machine picture (a file in base64; PNG, JPEG, WEBP or SVG). Optional field: files without it are read as before.
	IconPNG  string  `json:"icon_png,omitempty" yaml:"icon_png,omitempty"`
	IconSize int     `json:"icon_size,omitempty" yaml:"icon_size,omitempty"`
	Image    string  `json:"image,omitempty" yaml:"image,omitempty"`
	X        float64 `json:"x" yaml:"x"`
	Y        float64 `json:"y" yaml:"y"`
	Params   `yaml:",inline"`
}

type ExpNetwork struct {
	Ref       int64   `json:"ref" yaml:"ref"`
	Name      string  `json:"name" yaml:"name"`
	Type      string  `json:"type" yaml:"type"`
	DHCP      bool    `json:"dhcp_enabled" yaml:"dhcp_enabled"`
	HostIface string  `json:"host_iface,omitempty" yaml:"host_iface,omitempty"`
	X         float64 `json:"x" yaml:"x"`
	Y         float64 `json:"y" yaml:"y"`
}

type ExpConn struct {
	AType string `json:"a_type" yaml:"a_type"`
	ARef  int64  `json:"a_ref" yaml:"a_ref"`
	AIf   int    `json:"a_if" yaml:"a_if"`
	BType string `json:"b_type" yaml:"b_type"`
	BRef  int64  `json:"b_ref" yaml:"b_ref"`
	BIf   int    `json:"b_if" yaml:"b_if"`
}

type Export struct {
	Format      string `json:"format" yaml:"format"`
	Version     int    `json:"version" yaml:"version"`
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	// Canvas - workspace settings as a regular map: in YAML it is written as a structure, not a string.
	Canvas      any          `json:"canvas" yaml:"canvas,omitempty"`
	Devices     []ExpDevice  `json:"devices" yaml:"devices"`
	Networks    []ExpNetwork `json:"networks" yaml:"networks"`
	Connections []ExpConn    `json:"connections" yaml:"connections"`
}

// exportCanvas turns the saved canvas JSON into a value for YAML. An empty or corrupted canvas is skipped.
func exportCanvas(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}

func (h *Handler) buildExport(db *database.DB, lab *Lab) (*Export, error) {
	e := &Export{Format: "netrave-lab", Version: 1, Name: lab.Name, Description: lab.Description, Canvas: exportCanvas(lab.Canvas),
		Devices: []ExpDevice{}, Networks: []ExpNetwork{}, Connections: []ExpConn{}}
	fl, ok := labData(db, lab.ID)
	if !ok {
		return nil, errNotFound
	}
	tplNames, err := templateNames(db)
	if err != nil {
		return nil, err
	}
	imgDirs, err := imageDirs(db)
	if err != nil {
		return nil, err
	}
	for _, fd := range fl.Devices {
		d := ExpDevice{
			Ref: fd.ID, Name: fd.Name, Template: tplNames[fd.TemplateID], Image: imgDirs[fd.ImageID],
			X: fd.X, Y: fd.Y,
			Params: Params{Arch: fd.Arch, Machine: fd.Machine, CPU: fd.CPU, RAMMB: fd.RAMMB, NICs: fd.NICs, NICModel: fd.NICModel,
				IfaceType: fd.IfaceType, IfaceStart: fd.IfaceStart, DiskBus: fd.DiskBus, BIOS: fd.BIOS, Console: fd.Console, ExtraArgs: fd.ExtraArgs},
			Icon:     fd.Icon,
			IconSize: fd.IconSize,
		}
		if fd.CustomIcon != "" {
			if data, err := h.Icons.Read(fd.CustomIcon); err == nil {
				d.IconPNG = base64.StdEncoding.EncodeToString(data)
			}
		}
		e.Devices = append(e.Devices, d)
	}
	for _, fn := range fl.Networks {
		n := networkView(fn, lab.ID)
		e.Networks = append(e.Networks, ExpNetwork{Ref: n.ID, Name: n.Name, Type: n.Type, DHCP: n.DHCP, HostIface: n.HostIface, X: n.X, Y: n.Y})
	}
	for _, c := range fl.Connections {
		e.Connections = append(e.Connections, ExpConn{c.A.Type, c.A.ID, c.A.If, c.B.Type, c.B.ID, c.B.If})
	}
	return e, nil
}

// templateNames returns template names by number.
func templateNames(db *database.DB) (map[int64]string, error) {
	out := map[int64]string{}
	for _, t := range db.Catalog.Templates() {
		out[t.ID] = t.Name
	}
	return out, nil
}

// imageDirs returns image folder names by number.
func imageDirs(db *database.DB) (map[int64]string, error) {
	out := map[int64]string{}
	for _, im := range db.Catalog.Images() {
		out[im.ID] = im.Dirname
	}
	return out, nil
}

// importIcon validates the machine picture from the export file. An invalid picture does not abort the import:
// the machine gets the standard icon, and the reason goes into the warnings.
func (h *Handler) importIcon(d ExpDevice, dn string) (string, string, []string) {
	var warns []string
	icon := d.Icon
	if icon != "" && !DeviceIcons[icon] {
		warns = append(warns, fmt.Sprintf("иконка %q для %s не поддерживается — выбрана автоматически", icon, dn))
		icon = ""
	}
	custom := ""
	if d.IconPNG != "" {
		if len(d.IconPNG) > icons.MaxBytes*4/3+8 {
			warns = append(warns, fmt.Sprintf("иконка для %s слишком большая — выбрана автоматически", dn))
		} else if raw, err := base64.StdEncoding.DecodeString(d.IconPNG); err != nil {
			warns = append(warns, fmt.Sprintf("иконка для %s повреждена — выбрана автоматически", dn))
		} else if clean, info, err := icons.Clean(raw); err != nil {
			warns = append(warns, fmt.Sprintf("иконка для %s не принята: %v — выбрана автоматически", dn, err))
		} else if err := h.Icons.Save(clean, info.Hash); err != nil {
			warns = append(warns, fmt.Sprintf("иконка для %s не сохранена — выбрана автоматически", dn))
		} else {
			custom = info.Hash
		}
	}
	if icon == "custom" && custom == "" {
		warns = append(warns, fmt.Sprintf("своя иконка для %s не загружена — выбрана автоматически", dn))
		icon = ""
	}
	return icon, custom, warns
}

// importLab creates a new lab from the description. All values go through the same validation as in the API.
// The lab is written in one file: either all its objects or nothing.
func (h *Handler) importLab(db *database.DB, ownerID int64, e *Export, name string) (int64, []string, error) {
	var warns []string
	if e.Format != "netrave-lab" || e.Version != 1 {
		return 0, nil, errors.New("неизвестный формат файла (ожидается netrave-lab версии 1)")
	}
	if len(e.Devices) > 500 || len(e.Networks) > 200 || len(e.Connections) > 2000 {
		return 0, nil, errors.New("слишком большая лаборатория")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "imported-lab"
	}
	if msg := checkLabText(name, e.Description); msg != "" {
		return 0, nil, errors.New(msg)
	}
	canvas := "{}"
	if e.Canvas != nil {
		if raw, err := json.Marshal(e.Canvas); err == nil && validCanvas(raw) {
			canvas = string(raw)
		}
	}
	now := time.Now().Unix()
	fl := labfile.Lab{
		OwnerID:     ownerID,
		Name:        name,
		Description: e.Description,
		CanvasJSON:  canvas,
		PublicLevel: access.DefaultPublic(db, ownerID),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	names := map[string]bool{}
	devMap := map[int64]int64{} // reference from the file -> device number
	devNICs := map[int64]int{}
	for _, d := range e.Devices {
		p := d.Params
		if err := p.Validate(); err != nil {
			return 0, nil, fmt.Errorf("устройство %q: %v", d.Name, err)
		}
		if !nameRe.MatchString(d.Name) {
			return 0, nil, fmt.Errorf("недопустимое имя устройства %q", d.Name)
		}
		dn := d.Name
		if names[dn] {
			dn = uniqueName(names, dn)
		}
		names[dn] = true
		var tplID int64
		var imgID *int64
		if d.Template != "" {
			for _, t := range db.Catalog.Templates() {
				if t.Name == d.Template {
					tplID = t.ID
					break
				}
			}
		}
		if d.Image != "" {
			found := false
			for _, im := range db.Catalog.Images() {
				if im.Dirname == d.Image {
					id := im.ID
					imgID, found = &id, true
					break
				}
			}
			if !found {
				warns = append(warns, fmt.Sprintf("образ %q для %s не найден — выберите его вручную", d.Image, dn))
			}
		}
		fd := newDevice(0, tplID, imgID, dn, d.X, d.Y, p)
		fd.ID = db.Labs.NewID()
		icon, custom, iw := h.importIcon(d, dn)
		warns = append(warns, iw...)
		fd.Icon, fd.CustomIcon = icon, custom
		if validIconSize(d.IconSize) {
			fd.IconSize = d.IconSize
		} else if d.IconSize != 0 {
			warns = append(warns, fmt.Sprintf("размер иконки %s вне диапазона — используется размер по умолчанию", dn))
		}
		devMap[d.Ref] = fd.ID
		devNICs[d.Ref] = p.NICs
		fl.Devices = append(fl.Devices, fd)
	}

	netMap := map[int64]int64{}
	for _, n := range e.Networks {
		if n.Type != "isolated" && n.Type != "nat" && n.Type != "cloud" {
			return 0, nil, fmt.Errorf("недопустимый тип сети %q", n.Type)
		}
		if !nameRe.MatchString(n.Name) {
			return 0, nil, fmt.Errorf("недопустимое имя сети %q", n.Name)
		}
		nn := n.Name
		if names[nn] {
			nn = uniqueName(names, nn)
		}
		names[nn] = true
		net := &Network{Name: nn, Type: n.Type, X: n.X, Y: n.Y}
		if n.Type == "nat" {
			// Single NAT cloud: addresses are shared by all labs (see natcloud).
			net.Subnet, net.Gateway, net.DHCPStart, net.DHCPEnd = natcloud.Fields()
			net.DHCP = true
		}
		if n.Type == "cloud" && n.HostIface != "" {
			if !ifaceRe.MatchString(n.HostIface) {
				return 0, nil, fmt.Errorf("недопустимое имя интерфейса %q", n.HostIface)
			}
			net.HostIface = n.HostIface
		}
		fn := newNetwork(net, n.X, n.Y)
		fn.ID = db.Labs.NewID()
		netMap[n.Ref] = fn.ID
		fl.Networks = append(fl.Networks, fn)
	}

	used := map[string]bool{}
	for _, c := range e.Connections {
		var ids [2]int64
		for i, ep := range []struct {
			t   string
			ref int64
			ifc int
		}{{c.AType, c.ARef, c.AIf}, {c.BType, c.BRef, c.BIf}} {
			switch ep.t {
			case "device":
				id, ok := devMap[ep.ref]
				if !ok || ep.ifc < 0 || ep.ifc >= devNICs[ep.ref] {
					return 0, nil, errors.New("некорректное соединение в файле")
				}
				key := fmt.Sprintf("d%d/%d", id, ep.ifc)
				if used[key] {
					return 0, nil, errors.New("интерфейс устройства используется в двух соединениях")
				}
				used[key] = true
				ids[i] = id
			case "network":
				id, ok := netMap[ep.ref]
				if !ok {
					return 0, nil, errors.New("некорректное соединение в файле")
				}
				ids[i] = id
			default:
				return 0, nil, errors.New("некорректное соединение в файле")
			}
		}
		if c.AType == "network" && c.BType == "network" {
			return 0, nil, errors.New("соединение сеть—сеть не поддерживается")
		}
		fl.Connections = append(fl.Connections, labfile.Connection{
			A: labfile.End{Type: c.AType, ID: ids[0], If: c.AIf},
			B: labfile.End{Type: c.BType, ID: ids[1], If: c.BIf},
		})
	}
	saved, err := db.Labs.Save(fl)
	if err != nil {
		return 0, nil, err
	}
	return saved.ID, warns, nil
}

// runImport creates a lab from the description under the shared topology lock.
func (h *Handler) runImport(w http.ResponseWriter, ownerID int64, e *Export, name string) (int64, []string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id, warns, err := h.importLab(h.DB, ownerID, e, name)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return 0, nil, false
	}
	h.changed()
	return id, warns, true
}

func (h *Handler) exportHandler(w http.ResponseWriter, r *http.Request) {
	lab, ok := h.labFor(w, r, access.LabExport)
	if !ok {
		return
	}
	e, err := h.buildExport(h.DB, lab)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка экспорта")
		return
	}
	data, err := yaml.Marshal(e)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка экспорта")
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="netrave-lab-%d.yaml"`, lab.ID))
	_, _ = w.Write(data)
	h.audit(r, "lab_export", lab.Name)
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.Notify.Activity(ss.User.ID, lab.OwnerID, notify.Success, notify.CatBackup, "Резервная копия лаборатории создана",
			"«"+lab.Name+"» экспортирована в файл.", "/labs/"+strconv.FormatInt(lab.ID, 10)+"/editor")
	}
}

func (h *Handler) importHandler(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	if !h.canCreateLab(ss.User) {
		httpx.Err(w, http.StatusForbidden, "нет права: создание лабораторий")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<20))
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "файл слишком большой или не читается")
		return
	}
	var e Export
	if err := yaml.Unmarshal(body, &e); err != nil {
		httpx.Err(w, http.StatusBadRequest, "файл не разобран как YAML: "+err.Error())
		return
	}
	id, warns, ok := h.runImport(w, ss.User.ID, &e, e.Name)
	if !ok {
		return
	}
	h.audit(r, "lab_import", e.Name)
	h.Notify.Activity(ss.User.ID, 0, notify.Success, notify.CatBackup, "Лаборатория восстановлена из копии", "«"+e.Name+"» добавлена в список.", "/labs/"+strconv.FormatInt(id, 10)+"/editor")
	httpx.JSON(w, http.StatusCreated, map[string]any{"id": id, "warnings": warns})
}

func (h *Handler) copyLab(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	lab, ok := h.labFor(w, r, access.LabExport)
	if !ok {
		return
	}
	e, err := h.buildExport(h.DB, lab)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка копирования")
		return
	}
	id, _, ok := h.runImport(w, ss.User.ID, e, lab.Name+" (копия)")
	if !ok {
		return
	}
	h.audit(r, "lab_copy", lab.Name)
	h.Notify.Send(ss.User.ID, notify.Success, "lab", "Копия лаборатории создана", "«"+lab.Name+"» скопирована в вашу копию: это отдельная лаборатория.", "/labs/"+strconv.FormatInt(id, 10)+"/editor")
	httpx.JSON(w, http.StatusCreated, map[string]any{"id": id})
}

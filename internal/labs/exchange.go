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
	"netrave/internal/yamlfmt"

	"netrave/internal/access"
	"netrave/internal/auth"
	"netrave/internal/httpx"
	"netrave/internal/icons"
	"netrave/internal/msgs"
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
	fl, ok := labData(db, lab.Name)
	if !ok {
		return nil, errNotFound
	}
	tplNames, err := templateNames(db)
	if err != nil {
		return nil, err
	}
	devRef := map[string]int64{}
	netRef := map[string]int64{}
	for i, fd := range fl.Devices {
		ref := int64(i + 1)
		devRef[fd.Name] = ref
		d := ExpDevice{
			Ref: ref, Name: fd.Name, Template: tplNames[fd.TemplateID], Image: fd.ImageID,
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
	for i, fn := range fl.Networks {
		ref := int64(i + 1)
		netRef[fn.Name] = ref
		n := networkView(fn, lab.Name)
		e.Networks = append(e.Networks, ExpNetwork{Ref: ref, Name: n.Name, Type: n.Type, DHCP: n.DHCP, HostIface: n.HostIface, X: n.X, Y: n.Y})
	}
	for _, c := range fl.Connections {
		e.Connections = append(e.Connections, ExpConn{c.A.Type, devRef[c.A.Name] + netRef[c.A.Name], c.A.If, c.B.Type, devRef[c.B.Name] + netRef[c.B.Name], c.B.If})
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

// importIcon validates the machine picture from the export file. An invalid picture does not abort the import:
// the machine gets the standard icon, and the reason goes into the warnings.
func (h *Handler) importIcon(d ExpDevice, dn string) (string, string, []msgs.Message) {
	var warns []msgs.Message
	icon := d.Icon
	if icon != "" && !DeviceIcons[icon] {
		warns = append(warns, msgs.M("Icon {icon} for {device} is not supported — chosen automatically", map[string]string{"icon": strconv.Quote(icon), "device": dn}))
		icon = ""
	}
	custom := ""
	if d.IconPNG != "" {
		if len(d.IconPNG) > icons.MaxBytes*4/3+8 {
			warns = append(warns, msgs.M("Icon for {device} is too large — chosen automatically", map[string]string{"device": dn}))
		} else if raw, err := base64.StdEncoding.DecodeString(d.IconPNG); err != nil {
			warns = append(warns, msgs.M("Icon for {device} is damaged — chosen automatically", map[string]string{"device": dn}))
		} else if clean, info, err := icons.Clean(raw); err != nil {
			warns = append(warns, msgs.M("Icon for {device} was not accepted: {detail} — chosen automatically", map[string]string{"device": dn, "detail": err.Error()}))
		} else if err := h.Icons.Save(clean, info.Hash); err != nil {
			warns = append(warns, msgs.M("Icon for {device} was not saved — chosen automatically", map[string]string{"device": dn}))
		} else {
			custom = info.Hash
		}
	}
	if icon == "custom" && custom == "" {
		warns = append(warns, msgs.M("Custom icon for {device} was not uploaded — chosen automatically", map[string]string{"device": dn}))
		icon = ""
	}
	return icon, custom, warns
}

// importLab creates a new lab from the description. All values go through the same validation as in the API.
// The lab is written in one file: either all its objects or nothing.
func (h *Handler) importLab(db *database.DB, ownerID int64, e *Export, name string) (string, []msgs.Message, error) {
	var warns []msgs.Message
	if e.Format != "netrave-lab" || e.Version != 1 {
		return "", nil, errors.New("Unknown file format (netrave-lab version 1 expected)")
	}
	if len(e.Devices) > 500 || len(e.Networks) > 200 || len(e.Connections) > 2000 {
		return "", nil, errors.New("The laboratory is too large")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "imported-lab"
	}
	if err := checkLabText(name, e.Description); err != nil {
		return "", nil, err
	}
	if _, exists := db.Labs.Lab(name); exists {
		return "", nil, msgs.New("A laboratory with this name already exists", nil)
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
	devMap := map[string]string{} // reference from the file -> device name
	devNICs := map[string]int{}
	for _, d := range e.Devices {
		p := d.Params
		if err := p.Validate(); err != nil {
			return "", nil, msgs.Wrap(err, "Device «{name}»: {detail}", map[string]string{"name": d.Name, "detail": err.Error()})
		}
		if !validName(d.Name) {
			return "", nil, msgs.New("Invalid device name «{name}»", map[string]string{"name": d.Name})
		}
		dn := d.Name
		if names[dn] {
			dn = uniqueName(names, dn)
		}
		names[dn] = true
		var tplID int64
		var imgID *string
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
					dir := im.Dirname
					imgID, found = &dir, true
					break
				}
			}
			if !found {
				warns = append(warns, msgs.M("Image {image} for {device} not found — choose it manually", map[string]string{"image": strconv.Quote(d.Image), "device": dn}))
			}
		}
		fd := newDevice("", tplID, imgID, dn, d.X, d.Y, p)
		icon, custom, iw := h.importIcon(d, dn)
		warns = append(warns, iw...)
		fd.Icon, fd.CustomIcon = icon, custom
		if validIconSize(d.IconSize) {
			fd.IconSize = d.IconSize
		} else if d.IconSize != 0 {
			warns = append(warns, msgs.M("Icon size of {device} is out of range — default size used", map[string]string{"device": dn}))
		}
		ref := strconv.FormatInt(d.Ref, 10)
		devMap[ref] = fd.Name
		devNICs[ref] = p.NICs
		fl.Devices = append(fl.Devices, fd)
	}

	netMap := map[string]string{}
	for _, n := range e.Networks {
		if n.Type != "isolated" && n.Type != "nat" && n.Type != "cloud" {
			return "", nil, msgs.New("Invalid network type «{type}»", map[string]string{"type": n.Type})
		}
		if !validName(n.Name) {
			return "", nil, msgs.New("Invalid network name «{name}»", map[string]string{"name": n.Name})
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
				return "", nil, msgs.New("Invalid interface name «{name}»", map[string]string{"name": n.HostIface})
			}
			net.HostIface = n.HostIface
		}
		fn := newNetwork(net, n.X, n.Y)
		netMap[strconv.FormatInt(n.Ref, 10)] = fn.Name
		fl.Networks = append(fl.Networks, fn)
	}

	used := map[string]bool{}
	for _, c := range e.Connections {
		var names [2]string
		for i, ep := range []struct {
			t   string
			ref int64
			ifc int
		}{{c.AType, c.ARef, c.AIf}, {c.BType, c.BRef, c.BIf}} {
			ref := strconv.FormatInt(ep.ref, 10)
			switch ep.t {
			case "device":
				name, ok := devMap[ref]
				if !ok || ep.ifc < 0 || ep.ifc >= devNICs[ref] {
					return "", nil, errors.New("Invalid connection in the file")
				}
				key := fmt.Sprintf("d%s/%d", name, ep.ifc)
				if used[key] {
					return "", nil, errors.New("The device interface is used in two connections")
				}
				used[key] = true
				names[i] = name
			case "network":
				name, ok := netMap[ref]
				if !ok {
					return "", nil, errors.New("Invalid connection in the file")
				}
				names[i] = name
			default:
				return "", nil, errors.New("Invalid connection in the file")
			}
		}
		if c.AType == "network" && c.BType == "network" {
			return "", nil, errors.New("Network-to-network connection is not supported")
		}
		fl.Connections = append(fl.Connections, labfile.Connection{
			A: labfile.End{Type: c.AType, Name: names[0], If: c.AIf},
			B: labfile.End{Type: c.BType, Name: names[1], If: c.BIf},
		})
	}
	saved, err := db.Labs.Save(fl)
	if err != nil {
		return "", nil, err
	}
	return saved.Name, warns, nil
}

// runImport creates a lab from the description under the shared topology lock.
func (h *Handler) runImport(w http.ResponseWriter, ownerID int64, e *Export, name string) (string, []msgs.Message, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id, warns, err := h.importLab(h.DB, ownerID, e, name)
	if err != nil {
		httpx.ErrFrom(w, http.StatusBadRequest, err)
		return "", nil, false
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
		httpx.Err(w, http.StatusInternalServerError, "Export error")
		return
	}
	data, err := yamlfmt.Marshal(e)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Export error")
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="netrave-lab-%s.yaml"`, slugFile(lab.Name)))
	_, _ = w.Write(data)
	h.audit(r, "lab_export", lab.Name)
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.Notify.Activity(ss.User.ID, lab.OwnerID, notify.Success, notify.CatBackup, "Laboratory backup created",
			"«{name}» exported to a file.", labLink(lab.Name), notify.P("name", lab.Name))
	}
}

func (h *Handler) importHandler(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	if !h.canCreateLab(ss.User) {
		httpx.Err(w, http.StatusForbidden, "No permission: creating laboratories")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<20))
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "The file is too large or cannot be read")
		return
	}
	var e Export
	if err := yaml.Unmarshal(body, &e); err != nil {
		httpx.ErrKey(w, http.StatusBadRequest, "File could not be parsed as YAML: {detail}", map[string]string{"detail": err.Error()})
		return
	}
	id, warns, ok := h.runImport(w, ss.User.ID, &e, e.Name)
	if !ok {
		return
	}
	h.audit(r, "lab_import", e.Name)
	h.Notify.Activity(ss.User.ID, 0, notify.Success, notify.CatBackup, "Laboratory restored from a backup", "«{name}» added to the list.", labLink(id), notify.P("name", e.Name))
	httpx.JSON(w, http.StatusCreated, map[string]any{"id": id, "warnings": warns})
}

// slugFile builds a safe file name from a lab name.
func slugFile(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
		if b.Len() >= 60 {
			break
		}
	}
	if b.Len() == 0 {
		return "lab"
	}
	return b.String()
}

// copyName returns the name of a copy: the lab name with the first free number from 2 ("lab 2").
func copyName(db *database.DB, base string) string {
	cand := base + " 2"
	for n := 3; n < 10000; n++ {
		_, exists := db.Labs.Lab(cand)
		if !exists && !db.NameTakenAtRoot(cand, "") {
			return cand
		}
		cand = base + " " + strconv.Itoa(n)
	}
	return cand
}

func (h *Handler) copyLab(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	lab, ok := h.labFor(w, r, access.LabExport)
	if !ok {
		return
	}
	e, err := h.buildExport(h.DB, lab)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Copy error")
		return
	}
	id, _, ok := h.runImport(w, ss.User.ID, e, copyName(h.DB, lab.Name))
	if !ok {
		return
	}
	h.audit(r, "lab_copy", lab.Name)
	h.Notify.Send(ss.User.ID, notify.Success, "lab", "Copy of laboratory created", "«{name}» was copied to your copy: this is a separate laboratory.", labLink(id), notify.P("name", lab.Name))
	httpx.JSON(w, http.StatusCreated, map[string]any{"id": id})
}

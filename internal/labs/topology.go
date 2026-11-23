// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"encoding/json"
	"net/http"
	"netrave/internal/labfile"
	"netrave/internal/msgs"
	"strconv"
	"strings"
	"time"

	"netrave/internal/access"
	"netrave/internal/auth"
	"netrave/internal/database"
	"netrave/internal/httpx"
	"netrave/internal/instances"
	"netrave/internal/names"
	"netrave/internal/natcloud"
	"netrave/internal/network"
	"netrave/internal/notify"
	"netrave/internal/qemu"
)

const maxCoord = 1e6

func okCoord(v float64) bool { return v > -maxCoord && v < maxCoord }

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

// sameString compares optional image references.
func sameString(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func dedupe(names []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func (h *Handler) topology(w http.ResponseWriter, r *http.Request) {
	lab, ok := h.labFor(w, r, access.LabView)
	if !ok {
		return
	}
	iid, iok := h.instanceFor(w, r, lab)
	if !iok {
		return
	}
	t, err := loadTopology(h.DB, lab.Name, iid, h.M)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	// Port state comes from the network manager: wire colors and attachment points
	// reflect the actual operation of interfaces, not just the presence of a connection.
	if h.NICState != nil {
		rids := make([]int64, 0, len(t.Devices))
		for i := range t.Devices {
			if rid := t.Devices[i].RID; rid > 0 {
				rids = append(rids, rid)
			}
		}
		states := h.NICState(lab.Name, rids)
		for i := range t.Devices {
			ports := states[t.Devices[i].RID]
			if len(ports) == 0 {
				continue
			}
			out := make(map[string]int, len(ports))
			for nic, code := range ports {
				out[strconv.Itoa(nic)] = code
			}
			t.Devices[i].IPv4 = out
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"lab": lab, "instance": iid, "devices": t.Devices, "networks": t.Networks, "connections": t.Connections})
}

// networkDiag returns the lab's network path state (available to the administrator).
func (h *Handler) networkDiag(w http.ResponseWriter, r *http.Request) {
	lab, ok := h.labFor(w, r, access.LabView)
	if !ok {
		return
	}
	if h.NetworkDiag == nil {
		httpx.Err(w, http.StatusServiceUnavailable, "Network diagnostics are unavailable")
		return
	}
	httpx.JSON(w, http.StatusOK, h.NetworkDiag(lab.Name))
}

// ---------- devices ----------

// maxBatch is how many identical machines can be added in one request.
const maxBatch = 20

// batchCols is the number of columns when arranging a batch of machines on the diagram (step in X and Y - see batchStepX/Y).
const (
	batchCols  = 4
	batchStepX = 120.0
	batchStepY = 110.0
)

func (h *Handler) createDevice(w http.ResponseWriter, r *http.Request) {
	lab, ok := h.labFor(w, r, access.MachAdd)
	if !ok {
		return
	}
	var in struct {
		TemplateID int64   `json:"template_id"`
		ImageID    *string `json:"image_id"`
		Name       string  `json:"name"`
		Count      int     `json:"count"`
		X          float64 `json:"x"`
		Y          float64 `json:"y"`
		// Machine image: standard (icon) or custom (custom_icon - hash of a file uploaded in this lab).
		Icon       string `json:"icon"`
		CustomIcon string `json:"custom_icon"`
		// Machine parameters (CPU, RAM, interfaces, console, etc.). Unset fields are taken from the template.
		paramsPatch
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	if in.Count == 0 {
		in.Count = 1
	}
	if in.Count < 1 || in.Count > maxBatch {
		httpx.ErrKey(w, http.StatusBadRequest, "Quantity: from 1 to {max}", map[string]string{"max": strconv.Itoa(maxBatch)})
		return
	}
	if !okCoord(in.X) || !okCoord(in.Y) {
		httpx.Err(w, http.StatusBadRequest, "Invalid coordinates")
		return
	}
	if in.ImageID != nil && !requirePerm(w, lab, access.DiskAttach) {
		return
	}
	if err := h.checkIcon(in.Icon, in.CustomIcon); err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	tpl, err := getTemplate(h.DB.Catalog, in.TemplateID)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "Template not found")
		return
	}
	if labDeviceCount(h.DB, lab.Name)+in.Count > 500 {
		httpx.Err(w, http.StatusConflict, "Limit reached: 500 devices in a laboratory")
		return
	}
	if in.ImageID != nil {
		if _, ok := h.DB.Catalog.ImageByDirname(*in.ImageID); !ok {
			httpx.Err(w, http.StatusBadRequest, "Image not found")
			return
		}
	}
	names, err := loadNames(h.DB, lab.Name)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	name := strings.TrimSpace(in.Name)
	if in.Count == 1 && name != "" && (!validName(name) || names[name]) {
		httpx.Err(w, http.StatusBadRequest, "The name is invalid or already taken")
		return
	}
	if in.Count > 1 && name != "" && !validName(name) {
		httpx.Err(w, http.StatusBadRequest, "The name is invalid")
		return
	}
	// Names are chosen one by one and recorded in names: they are unique within the batch and relative to the lab.
	base := tpl.Name
	if name != "" {
		base = name
	}
	nameList := make([]string, in.Count)
	for i := range nameList {
		if in.Count == 1 && name != "" {
			nameList[i] = name
		} else {
			nameList[i] = uniqueName(names, base)
		}
	}
	// The disk and default icon come from the template if the user has not chosen them.
	// The template only suggests values: they are stored on the machine as regular values.
	if in.ImageID == nil && tpl.ImageID != 0 {
		if im, ok := h.DB.Catalog.Image(tpl.ImageID); ok {
			id := im.Dirname
			in.ImageID = &id
		}
	}
	if in.Icon == "" && in.CustomIcon == "" {
		in.Icon, in.CustomIcon = tpl.Icon, tpl.CustomIcon
	}
	// The template is only the initial values. User-specified parameters take precedence and are stored
	// on the machine itself (and in its instance rows), and are not overwritten by the template at startup.
	params := tpl.Params
	params.apply(&in.paramsPatch)
	params.Normalize()
	if err := params.Validate(); err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	// The whole batch is written in one file write: either all machines are created or none.
	fds := make([]labfile.Device, 0, in.Count)
	for i := 0; i < in.Count; i++ {
		x, y := in.X, in.Y
		if in.Count > 1 {
			x = in.X + float64(i%batchCols)*batchStepX
			y = in.Y + float64(i/batchCols)*batchStepY
		}
		fd := newDevice(lab.Name, tpl.ID, in.ImageID, nameList[i], x, y, params)
		fd.Icon, fd.CustomIcon = in.Icon, in.CustomIcon
		fds = append(fds, fd)
	}
	if _, err := updateLabGet(h.DB, lab.Name, func(l *labfile.Lab) error {
		l.Devices = append(l.Devices, fds...)
		l.UpdatedAt = time.Now().Unix()
		return nil
	}); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	namesAdded := make([]string, 0, len(fds))
	for _, fd := range fds {
		namesAdded = append(namesAdded, fd.Name)
	}
	// Instance rows (rid, MAC, disks, network interfaces) are created for the whole batch in one pass.
	_ = instances.SyncLab(h.DB, lab.Name)
	// The response is built for the creating user's instance: it contains the machine's rid.
	iid, err := instances.Ensure(h.DB, lab.Name, auth.FromContext(r.Context()).User.ID)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	out := make([]*Device, 0, len(namesAdded))
	for _, name := range namesAdded {
		d, err := getDeviceInst(h.DB, lab.Name, name, iid, h.M)
		if err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		}
		out = append(out, d)
	}
	if len(out) == 1 {
		h.notifyTopo(r, lab, notify.Success, notify.CatMachine, "Machine added", "{name} → «{lab}»", notify.P("name", out[0].Name, "lab", lab.Name))
		httpx.JSON(w, http.StatusCreated, out[0])
		return
	}
	h.notifyTopo(r, lab, notify.Success, notify.CatMachine, "Machines added",
		"{count} pcs.: {first} … {last} → «{lab}»", notify.P("count", strconv.Itoa(len(out)), "first", out[0].Name, "last", out[len(out)-1].Name, "lab", lab.Name))
	httpx.JSON(w, http.StatusCreated, out)
}

type optString struct {
	Set bool
	Val *string
}

func (o *optString) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Val = nil
		return nil
	}
	var v string
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Val = &v
	return nil
}

// rowLab finds the lab by the nested device/network/cable name and checks access.
func (h *Handler) rowLab(w http.ResponseWriter, r *http.Request, kind string) (*Lab, string, bool) {
	lab, ok := h.labFor(w, r, "")
	if !ok {
		return nil, "", false
	}
	name := r.PathValue("device")
	if kind == labfile.KindNetwork {
		name = r.PathValue("network")
	}
	if kind == labfile.KindConnection {
		name = r.PathValue("conn")
	}
	if name == "" {
		httpx.Err(w, http.StatusBadRequest, "Invalid name")
		return nil, "", false
	}
	if _, ok := labOwnerOf(h.DB, kind, lab.Name, name); !ok {
		httpx.Err(w, http.StatusNotFound, "Object not found")
		return nil, "", false
	}
	return lab, name, true
}

// deviceRunning reports whether a machine of the device is running in any instance of the laboratory.
// A failed check counts as running, so that a name is not changed under a machine that may be running.
func (h *Handler) deviceRunning(labName, devName string) bool {
	if h.M == nil {
		return false
	}
	rows, err := h.DB.Query(`SELECT r.id FROM instance_devices r
		JOIN lab_instances i ON i.id = r.instance_id
		WHERE i.lab_id = ? AND r.device_id = ?`, labName, devName)
	if err != nil {
		return true
	}
	defer rows.Close()
	for rows.Next() {
		var rid int64
		if rows.Scan(&rid) == nil && h.M.Active(rid) {
			return true
		}
	}
	return rows.Err() != nil
}

func (h *Handler) updateDevice(w http.ResponseWriter, r *http.Request) {
	lab, rid, instID, ok := h.rowLabRT(w, r)
	if !ok {
		return
	}
	id := rid.devName
	var in struct {
		Name       *string   `json:"name"`
		X          *float64  `json:"x"`
		Y          *float64  `json:"y"`
		ImageID    optString `json:"image_id"`
		Icon       *string   `json:"icon"`
		CustomIcon *string   `json:"custom_icon"`
		IconSize   *int      `json:"icon_size"`
		paramsPatch
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	cur, err := getDeviceInst(h.DB, lab.Name, id, instID, h.M)
	if err != nil {
		httpx.Err(w, http.StatusNotFound, "Device not found")
		return
	}
	p := cur.Params
	changed := p.apply(&in.paramsPatch)
	// Permissions by change type: moving and resizing the icon, parameters, NIC count,
	// attaching and detaching a disk.
	if (in.X != nil || in.Y != nil || in.IconSize != nil) && !requirePerm(w, lab, access.MachMove) {
		return
	}
	if in.IconSize != nil && !validIconSize(*in.IconSize) {
		httpx.Err(w, http.StatusBadRequest, "Icon size: from 24 to 256")
		return
	}
	other := p
	other.NICs = cur.NICs
	if (in.Name != nil || other != cur.Params) && !requirePerm(w, lab, access.MachEdit) {
		return
	}
	if p.NICs != cur.NICs && !requirePerm(w, lab, access.IfaceManage) {
		return
	}
	if (in.Icon != nil || in.CustomIcon != nil) && !requirePerm(w, lab, access.MachEdit) {
		return
	}
	if in.ImageID.Set {
		diskPerm := access.DiskAttach
		if in.ImageID.Val == nil {
			diskPerm = access.DiskDetach
		}
		if !requirePerm(w, lab, diskPerm) {
			return
		}
	}
	if changed {
		if err := p.Validate(); err != nil {
			httpx.Err(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	imgID := cur.ImageID
	if in.ImageID.Set {
		imgID = in.ImageID.Val
		changed = true
		if imgID != nil {
			if _, ok := h.DB.Catalog.ImageByDirname(*imgID); !ok {
				httpx.Err(w, http.StatusBadRequest, "Image not found")
				return
			}
		}
	}
	imgChanged := in.ImageID.Set && !sameString(imgID, cur.ImageID)
	// A running machine keeps its name and icon: its log and working files are named after them.
	// Parameters are saved and applied on restart, and the canvas position is layout only.
	nameChanged := in.Name != nil && strings.TrimSpace(*in.Name) != cur.Name
	iconChanged := (in.Icon != nil && *in.Icon != cur.Icon) || (in.CustomIcon != nil && *in.CustomIcon != cur.CustomIcon)
	if nameChanged && h.deviceRunning(lab.Name, cur.Name) {
		httpx.Err(w, http.StatusConflict, "The machine is running: stop it before renaming")
		return
	}
	if iconChanged && cur.State != "stopped" && cur.State != "error" {
		httpx.Err(w, http.StatusConflict, "The machine is running: stop it before changing its icon")
		return
	}
	// Running machine: parameters are saved and applied on restart (see Pending);
	// the disk image can only be changed for a stopped machine - its working overlay is bound to it.
	if imgChanged && cur.State != "stopped" && cur.State != "error" {
		httpx.Err(w, http.StatusConflict, "The disk image can be changed only on a stopped device")
		return
	}
	// The overlay is bound to the base image: after the first start the disk can only be changed via Wipe.
	if imgChanged && h.HasDisks != nil && h.HasDisks(rid.rid) {
		httpx.Err(w, http.StatusConflict, "The machine already has a working disk: reset the disk (Wipe) to change the image")
		return
	}
	if changed && p.NICs < cur.NICs {
		if cablesFromPort(h.DB, lab.Name, id, p.NICs) > 0 {
			httpx.Err(w, http.StatusConflict, "Remove the cables from the interfaces that will be deleted")
			return
		}
	}
	name, x, y := cur.Name, cur.X, cur.Y
	iconSize := cur.IconSize
	if in.IconSize != nil {
		iconSize = *in.IconSize
	}
	icon, custom := cur.Icon, cur.CustomIcon
	if in.Icon != nil {
		icon = *in.Icon
	}
	if in.CustomIcon != nil {
		custom = *in.CustomIcon
		// A custom icon was deleted and the machine was using it: fall back to the automatic picture.
		if custom == "" && in.Icon == nil && icon == "custom" {
			icon = ""
		}
	}
	if in.Icon != nil || in.CustomIcon != nil {
		if err := h.checkIcon(icon, custom); err != nil {
			httpx.Err(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
		if err := names.Check("Name", name); err != nil {
			httpx.Err(w, http.StatusBadRequest, err.Error())
			return
		}
		names, err := loadNames(h.DB, lab.Name)
		if err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		}
		if name != cur.Name && names[name] {
			httpx.Err(w, http.StatusConflict, "The name is already taken")
			return
		}
	}
	if in.X != nil {
		x = *in.X
	}
	if in.Y != nil {
		y = *in.Y
	}
	if !okCoord(x) || !okCoord(y) {
		httpx.Err(w, http.StatusBadRequest, "Invalid coordinates")
		return
	}
	// The machine's working directories are named after it: they move with the rename and move back on failure.
	undoDirs := func() {}
	if name != cur.Name && h.M != nil {
		undo, err := h.M.RenameDeviceFiles(lab.Name, cur.Name, name)
		if err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Could not move the machine files")
			return
		}
		undoDirs = undo
	}
	// Topology (name, position, icon) is in the lab file.
	beforeLab, _ := labData(h.DB, lab.Name)
	err = updateLab(h.DB, lab.Name, func(l *labfile.Lab) error {
		found := false
		for i := range l.Devices {
			d := &l.Devices[i]
			if d.Name != cur.Name {
				continue
			}
			d.Name, d.X, d.Y, d.Icon, d.CustomIcon, d.IconSize = name, x, y, icon, custom, iconSize
			if imgChanged && lab.Perms[access.LabManageAcc] {
				// The disk chosen by the owner or lab administrator becomes the default disk:
				// it is given to participants who have not made their own choice yet.
				d.ImageID = ""
				if imgID != nil {
					d.ImageID = *imgID
				}
			}
			found = true
			break
		}
		if !found {
			return errNotFound
		}
		// A name is an identifier: renaming a device keeps its cables attached.
		if name != cur.Name {
			for i := range l.Connections {
				if l.Connections[i].A.Type == "device" && l.Connections[i].A.Name == cur.Name {
					l.Connections[i].A.Name = name
				}
				if l.Connections[i].B.Type == "device" && l.Connections[i].B.Name == cur.Name {
					l.Connections[i].B.Name = name
				}
			}
		}
		return nil
	})
	if err == nil && name != cur.Name && h.M != nil {
		// The machine's log is named after the device: the old one is removed.
		h.M.RemoveDeviceLog(lab.Name, cur.Name)
	}
	if err == nil && name != cur.Name {
		// Instance rows and cable keys use the device name.
		_, err = h.DB.Exec(`UPDATE instance_devices SET device_id=?
			WHERE device_id=? AND instance_id IN (SELECT id FROM lab_instances WHERE lab_id=?)`,
			name, cur.Name, lab.Name)
		if err == nil {
			afterLab, _ := labData(h.DB, lab.Name)
			err = syncConnectionKeys(h.DB, lab.Name, beforeLab, afterLab)
		}
	}
	// Instance parameters are in the instance row.
	if err == nil {
		_, err = h.DB.Exec(`UPDATE instance_devices SET machine=?,cpu=?,ram_mb=?,nics=?,nic_model=?,iface_type=?,iface_start=?,disk_bus=?,bios=?,console=?,extra_args=? WHERE id=?`,
			p.Machine, p.CPU, p.RAMMB, p.NICs, p.NICModel, p.IfaceType, p.IfaceStart, p.DiskBus, p.BIOS, p.Console, p.ExtraArgs, rid.rid)
	}
	if err == nil && imgChanged {
		// The disk is selected for this instance: other instances do not change it.
		var imgValue any
		if imgID != nil {
			imgValue = *imgID
		}
		_, err = h.DB.Exec(`UPDATE instance_devices SET image_id=?, image_own=1 WHERE id=?`, imgValue, rid.rid)
	}
	if err != nil {
		undoDirs()
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	if custom != cur.CustomIcon {
		h.dropIcon(cur.CustomIcon) // the previous file is deleted if no one else uses it
	}
	touch(h.DB, lab.Name)
	d, _ := getDeviceInst(h.DB, lab.Name, id, instID, h.M)
	if d != nil {
		h.notifyTopo(r, lab, notify.Info, notify.CatMachine, "Machine changed", "{name} in «{lab}»", notify.P("name", d.Name, "lab", lab.Name))
	}
	httpx.JSON(w, http.StatusOK, d)
}

// ---------- networks ----------

func (h *Handler) createNetwork(w http.ResponseWriter, r *http.Request) {
	lab, ok := h.labFor(w, r, access.NetManage)
	if !ok {
		return
	}
	var in struct {
		Type string  `json:"type"`
		Name string  `json:"name"`
		X    float64 `json:"x"`
		Y    float64 `json:"y"`
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	if in.Type != "isolated" && in.Type != "nat" && in.Type != "cloud" {
		httpx.Err(w, http.StatusBadRequest, "Network type: isolated, nat or cloud")
		return
	}
	if !okCoord(in.X) || !okCoord(in.Y) {
		httpx.Err(w, http.StatusBadRequest, "Invalid coordinates")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	names, err := loadNames(h.DB, lab.Name)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		base := map[string]string{"isolated": "Net", "nat": "NAT", "cloud": "Cloud"}[in.Type]
		name = uniqueName(names, base)
	} else if !validName(name) || names[name] {
		httpx.Err(w, http.StatusBadRequest, "The name is invalid or already taken")
		return
	}
	n := &Network{Name: name, Type: in.Type, X: in.X, Y: in.Y}
	if in.Type == "nat" {
		// Single NAT cloud: subnet, gateway and DHCP are shared by all labs.
		n.Subnet, n.Gateway, n.DHCPStart, n.DHCPEnd = natcloud.Fields()
		n.DHCP = true
	}
	fn := newNetwork(n, in.X, in.Y)
	if _, err := updateLabGet(h.DB, lab.Name, func(l *labfile.Lab) error {
		l.Networks = append(l.Networks, fn)
		l.UpdatedAt = time.Now().Unix()
		return nil
	}); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	h.notifyTopo(r, lab, notify.Success, notify.CatNetwork, "Network created", "{name} in «{lab}»", notify.P("name", n.Name, "lab", lab.Name))
	h.changed()
	out, _ := getNetwork(h.DB, lab.Name, fn.Name)
	httpx.JSON(w, http.StatusCreated, out)
}

func (h *Handler) updateNetwork(w http.ResponseWriter, r *http.Request) {
	lab, netName, ok := h.rowLab(w, r, labfile.KindNetwork)
	if !ok {
		return
	}
	var in struct {
		Name      *string  `json:"name"`
		X         *float64 `json:"x"`
		Y         *float64 `json:"y"`
		Subnet    *string  `json:"subnet"`
		Gateway   *string  `json:"gateway"`
		DHCP      *bool    `json:"dhcp_enabled"`
		DHCPStart *string  `json:"dhcp_start"`
		DHCPEnd   *string  `json:"dhcp_end"`
		HostIface *string  `json:"host_iface"`
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	otherNet := in.Name != nil || in.Subnet != nil || in.Gateway != nil || in.DHCP != nil ||
		in.DHCPStart != nil || in.DHCPEnd != nil || in.HostIface != nil
	if otherNet && !requirePerm(w, lab, access.NetManage) {
		return
	}
	if (in.X != nil || in.Y != nil) && !requirePerm(w, lab, access.MachMove) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	n, err := getNetwork(h.DB, lab.Name, netName)
	if err != nil {
		httpx.Err(w, http.StatusNotFound, "Network not found")
		return
	}
	// The NAT cloud is shared: subnet, gateway and DHCP are set by the server for all labs and do not change.
	if n.Type == "nat" && (in.Subnet != nil || in.Gateway != nil || in.DHCPStart != nil || in.DHCPEnd != nil ||
		(in.DHCP != nil && !*in.DHCP) || in.HostIface != nil) {
		httpx.Err(w, http.StatusBadRequest, "The NAT cloud is shared by all laboratories: the subnet, gateway and DHCP do not change")
		return
	}
	if in.HostIface != nil && netRunning(h.DB, h.M, lab.Name, netName) {
		httpx.Err(w, http.StatusConflict, "Stop the connected devices to change the network settings")
		return
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if err := names.Check("Name", name); err != nil {
			httpx.Err(w, http.StatusBadRequest, err.Error())
			return
		}
		names, err := loadNames(h.DB, lab.Name)
		if err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		}
		if name != n.Name && names[name] {
			httpx.Err(w, http.StatusConflict, "The name is already taken")
			return
		}
		n.Name = name
	}
	if in.X != nil {
		n.X = *in.X
	}
	if in.Y != nil {
		n.Y = *in.Y
	}
	if !okCoord(n.X) || !okCoord(n.Y) {
		httpx.Err(w, http.StatusBadRequest, "Invalid coordinates")
		return
	}
	if n.Type == "cloud" && in.HostIface != nil {
		v := strings.TrimSpace(*in.HostIface)
		if v != "" && !ifaceRe.MatchString(v) {
			httpx.Err(w, http.StatusBadRequest, "Invalid network interface name")
			return
		}
		n.HostIface = v
	}
	beforeLab, _ := labData(h.DB, lab.Name)
	err = updateLab(h.DB, lab.Name, func(l *labfile.Lab) error {
		found := false
		for i := range l.Networks {
			fn := &l.Networks[i]
			if fn.Name != netName {
				continue
			}
			fn.Name, fn.X, fn.Y = n.Name, n.X, n.Y
			fn.Subnet, fn.Gateway, fn.DHCPEnabled = n.Subnet, n.Gateway, n.DHCP
			fn.DHCPStart, fn.DHCPEnd, fn.HostIface = n.DHCPStart, n.DHCPEnd, n.HostIface
			found = true
			break
		}
		if !found {
			return errNotFound
		}
		// Renaming a network keeps its cables attached.
		if n.Name != netName {
			for i := range l.Connections {
				if l.Connections[i].A.Type == "network" && l.Connections[i].A.Name == netName {
					l.Connections[i].A.Name = n.Name
				}
				if l.Connections[i].B.Type == "network" && l.Connections[i].B.Name == netName {
					l.Connections[i].B.Name = n.Name
				}
			}
		}
		return nil
	})
	if err == nil && n.Name != netName {
		_, err = h.DB.Exec(`UPDATE instance_networks SET network_id=?
			WHERE network_id=? AND instance_id IN (SELECT id FROM lab_instances WHERE lab_id=?)`,
			n.Name, netName, lab.Name)
		if err == nil {
			afterLab, _ := labData(h.DB, lab.Name)
			err = syncConnectionKeys(h.DB, lab.Name, beforeLab, afterLab)
		}
	}
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	touch(h.DB, lab.Name)
	h.notifyTopo(r, lab, notify.Info, notify.CatNetwork, "Network changed", "{name} in «{lab}»", notify.P("name", n.Name, "lab", lab.Name))
	h.changed()
	out, _ := getNetwork(h.DB, lab.Name, netName)
	httpx.JSON(w, http.StatusOK, out)
}

// ---------- connections ----------

// setConnState enables or disables a wire in the user's instance (or the administrator's, with ?instance=N).
// Running machines switch immediately (TAP and QMP set_link), stopped ones on startup.
func (h *Handler) setConnState(w http.ResponseWriter, r *http.Request) {
	lab, key, ok := h.rowLab(w, r, labfile.KindConnection)
	if !ok {
		return
	}
	if !requirePerm(w, lab, access.LinkCreate) {
		return
	}
	var in struct {
		Up *bool `json:"up"`
	}
	if !httpx.Decode(w, r, &in, 1<<10) {
		return
	}
	if in.Up == nil {
		httpx.Err(w, http.StatusBadRequest, "Specify up: true or false")
		return
	}
	iid, ok := h.instanceFor(w, r, lab)
	if !ok {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.CableSet == nil {
		httpx.Err(w, http.StatusInternalServerError, "Cable management is not configured")
		return
	}
	if err := h.CableSet(lab.Name, iid, key, *in.Up); err != nil {
		httpx.ErrKey(w, http.StatusInternalServerError, "Cable: {detail}", map[string]string{"detail": err.Error()})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"key": key, "up": *in.Up})
}

// bumpAppliedNICs marks added interfaces as applied on running machines: they are connected
// via QMP hotplug, so no restart is needed to change the NIC count.
func bumpAppliedNICs(db *database.DB, mgr *qemu.Manager, deviceName string, nics int) {
	rows, err := db.Query(`SELECT id, applied FROM instance_devices
		WHERE device_id=? AND applied<>''`, deviceName)
	if err != nil {
		return
	}
	type upd struct {
		id   int64
		snap string
	}
	var ups []upd
	for rows.Next() {
		var u upd
		if rows.Scan(&u.id, &u.snap) != nil {
			continue
		}
		if mgr == nil || !mgr.Active(u.id) {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(u.snap), &m) != nil {
			continue
		}
		m["nics"] = nics
		if b, err := json.Marshal(m); err == nil {
			u.snap = string(b)
			ups = append(ups, u)
		}
	}
	rows.Close()
	for _, u := range ups {
		_, _ = db.Exec(`UPDATE instance_devices SET applied=? WHERE id=?`, u.snap, u.id)
	}
}

func (h *Handler) createConn(w http.ResponseWriter, r *http.Request) {
	lab, ok := h.labFor(w, r, access.LinkCreate)
	if !ok {
		return
	}
	var in struct {
		AType string `json:"a_type"`
		AName string `json:"a_name"`
		AIf   int    `json:"a_if"`
		BType string `json:"b_type"`
		BName string `json:"b_name"`
		BIf   int    `json:"b_if"`
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if in.AType == "network" && in.BType == "device" {
		in.AType, in.AName, in.AIf, in.BType, in.BName, in.BIf = in.BType, in.BName, in.BIf, in.AType, in.AName, in.AIf
	}
	if (in.AType != "device" && in.AType != "network") || (in.BType != "device" && in.BType != "network") {
		httpx.Err(w, http.StatusBadRequest, "Invalid endpoint type")
		return
	}
	if in.AType == "network" && in.BType == "network" {
		httpx.Err(w, http.StatusBadRequest, "Network-to-network connection is not supported")
		return
	}
	if in.AType == in.BType && in.AName == in.BName {
		httpx.Err(w, http.StatusBadRequest, "You cannot connect an object to itself")
		return
	}
	grow := map[string]int{}
	for _, ep := range []struct {
		t    string
		name string
		ifc  *int
	}{{in.AType, in.AName, &in.AIf}, {in.BType, in.BName, &in.BIf}} {
		if ep.t == "network" {
			*ep.ifc = 0
			if owned(h.DB, labfile.KindNetwork, lab.Name, []string{ep.name}) != nil {
				httpx.Err(w, http.StatusBadRequest, "Network not found in this laboratory")
				return
			}
			continue
		}
		nics, ok := deviceNICs(h.DB, lab.Name, ep.name)
		if !ok {
			httpx.Err(w, http.StatusBadRequest, "Device not found in this laboratory")
			return
		}
		// A number equal to the current interface count is a new port: it will be
		// added to a running machine by hotplug (or appear at the next start).
		if *ep.ifc < 0 || *ep.ifc > nics {
			httpx.Err(w, http.StatusBadRequest, "The device has no such interface")
			return
		}
		if *ep.ifc == nics {
			if nics >= 32 {
				httpx.Err(w, http.StatusConflict, "Limit reached: 32 network interfaces")
				return
			}
			*ep.ifc = nics
			grow[ep.name] = nics + 1
		}
		if cablesOnPort(h.DB, lab.Name, ep.name, *ep.ifc) > 0 {
			httpx.Err(w, http.StatusConflict, "The interface is already connected by a cable")
			return
		}
	}
	// The wire and new device interfaces are written in one lab file write.
	saved, err := updateLabGet(h.DB, lab.Name, func(l *labfile.Lab) error {
		for i := range l.Devices {
			if n, ok := grow[l.Devices[i].Name]; ok && l.Devices[i].NICs < n {
				l.Devices[i].NICs = n
			}
		}
		l.Connections = append(l.Connections, labfile.Connection{
			A: labfile.End{Type: in.AType, Name: in.AName, If: in.AIf},
			B: labfile.End{Type: in.BType, Name: in.BName, If: in.BIf},
		})
		l.UpdatedAt = time.Now().Unix()
		return nil
	})
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	key := saved.Connections[len(saved.Connections)-1].Key()
	// Instance rows receive the same number of interfaces.
	for name, n := range grow {
		if _, err := h.DB.Exec(`UPDATE instance_devices SET nics=CASE WHEN nics<? THEN ? ELSE nics END WHERE device_id=?`, n, n, name); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		}
	}
	// Running machines get the wire immediately: the TAP joins the bridge and is brought up, QEMU receives set_link up.
	var warnings []msgs.Message
	if h.CableAttach != nil {
		for _, e := range h.CableAttach(lab.Name, key) {
			warnings = append(warnings, msgs.Of(e))
		}
	}
	if len(warnings) > 0 {
		h.notifyTopo(r, lab, notify.Warning, notify.CatCable, "Cable created, but not fully applied", "{detail}", map[string]string{"detail": msgs.Join(warnings, "; ")})
	} else {
		// New interfaces are added to running machines via QMP: they do not need a restart,
		// so the start snapshot is updated and "changes waiting for restart" is not shown.
		for name, n := range grow {
			bumpAppliedNICs(h.DB, h.M, name, n)
		}
		h.notifyTopo(r, lab, notify.Success, notify.CatCable, "Cable created", "in «{lab}»", notify.P("lab", lab.Name))
	}
	h.changed()
	// The new wire is enabled (up=true): wires are never disabled by default.
	httpx.JSON(w, http.StatusCreated, Conn{ID: key, Key: key, LabID: lab.Name, AType: in.AType, AName: in.AName, AID: in.AName, AIf: in.AIf,
		BType: in.BType, BName: in.BName, BID: in.BName, BIf: in.BIf, Up: true, Warnings: warnings})
}

// ---------- positions, deletion, insertion ----------

func containsName(names []string, v string) bool {
	for _, name := range names {
		if name == v {
			return true
		}
	}
	return false
}

// touchesNames reports whether the cable has an end on the machines or networks being deleted.
func touchesNames(c labfile.Connection, dev, nets []string) bool {
	return (c.A.Type == "device" && containsName(dev, c.A.Name)) || (c.A.Type == "network" && containsName(nets, c.A.Name)) ||
		(c.B.Type == "device" && containsName(dev, c.B.Name)) || (c.B.Type == "network" && containsName(nets, c.B.Name))
}

func dropConnections(list []labfile.Connection, keys []string) []labfile.Connection {
	out := list[:0]
	for _, c := range list {
		if !containsName(keys, c.Key()) {
			out = append(out, c)
		}
	}
	return out
}

func dropDevices(list []labfile.Device, names []string) []labfile.Device {
	out := list[:0]
	for _, d := range list {
		if !containsName(names, d.Name) {
			out = append(out, d)
		}
	}
	return out
}

func dropNetworks(list []labfile.Network, names []string) []labfile.Network {
	out := list[:0]
	for _, n := range list {
		if !containsName(names, n.Name) {
			out = append(out, n)
		}
	}
	return out
}

// ptrOr0 returns the pointer value or 0.
func ptrOr0(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

type pos struct {
	ID string  `json:"id"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
	// IconSize changes only on machines; an empty field leaves the size unchanged.
	IconSize *int `json:"icon_size,omitempty"`
}

func (h *Handler) positions(w http.ResponseWriter, r *http.Request) {
	lab, ok := h.labFor(w, r, access.MachMove)
	if !ok {
		return
	}
	var in struct {
		Devices  []pos `json:"devices"`
		Networks []pos `json:"networks"`
	}
	if !httpx.Decode(w, r, &in, 4<<20) {
		return
	}
	for _, list := range [][]pos{in.Devices, in.Networks} {
		for _, p := range list {
			if !okCoord(p.X) || !okCoord(p.Y) {
				httpx.Err(w, http.StatusBadRequest, "Invalid coordinates")
				return
			}
		}
	}
	for _, p := range in.Devices {
		if p.IconSize != nil && !validIconSize(*p.IconSize) {
			httpx.Err(w, http.StatusBadRequest, "Icon size: from 24 to 256")
			return
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	devPos, netPos := map[string]pos{}, map[string]pos{}
	for _, p := range in.Devices {
		devPos[p.ID] = p
	}
	for _, p := range in.Networks {
		netPos[p.ID] = p
	}
	// Objects of other labs and unknown names are skipped: only own positions are changed.
	if _, err := updateLabGet(h.DB, lab.Name, func(l *labfile.Lab) error {
		for i := range l.Devices {
			if p, ok := devPos[l.Devices[i].Name]; ok {
				l.Devices[i].X, l.Devices[i].Y = p.X, p.Y
				if p.IconSize != nil {
					l.Devices[i].IconSize = *p.IconSize
				}
			}
		}
		for i := range l.Networks {
			if p, ok := netPos[l.Networks[i].Name]; ok {
				l.Networks[i].X, l.Networks[i].Y = p.X, p.Y
			}
		}
		l.UpdatedAt = time.Now().Unix()
		return nil
	}); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) bulkDelete(w http.ResponseWriter, r *http.Request) {
	lab, ok := h.labFor(w, r, access.LabView)
	if !ok {
		return
	}
	var in struct {
		Devices     []string `json:"devices"`
		Networks    []string `json:"networks"`
		Connections []string `json:"connections"`
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	dev, nets, cons := dedupe(in.Devices), dedupe(in.Networks), dedupe(in.Connections)
	if len(dev) > 0 && !requirePerm(w, lab, access.MachDelete) {
		return
	}
	if len(nets) > 0 && !requirePerm(w, lab, access.NetManage) {
		return
	}
	// Deleting a machine or network also deletes their wires, so permission on wires is required.
	if (len(dev) > 0 || len(nets) > 0 || len(cons) > 0) && !requirePerm(w, lab, access.LinkDelete) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range []struct {
		kind  string
		names []string
	}{{labfile.KindDevice, dev}, {labfile.KindNetwork, nets}, {labfile.KindConnection, cons}} {
		if err := owned(h.DB, c.kind, lab.Name, c.names); err != nil {
			httpx.Err(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	for _, d := range dev {
		if devHasRunningPeer(h.DB, h.M, lab.Name, d) {
			httpx.Err(w, http.StatusConflict, "The device is connected to a running device — stop it or remove the cable first")
			return
		}
	}
	for _, n := range nets {
		if netRunning(h.DB, h.M, lab.Name, n) {
			httpx.Err(w, http.StatusConflict, "Stop the devices connected to the network")
			return
		}
	}
	if len(dev) > 0 && anyDeviceRunning(h.DB, h.M, lab.Name, dev) {
		httpx.Err(w, http.StatusConflict, "Stop the running devices first")
		return
	}
	// Wires of running machines are physically disconnected (QMP hot-unplug, TAP) without stopping: remember the ends before deletion.
	var plugs []network.Plug
	if len(cons) > 0 && h.CablePlan != nil {
		plugs = h.CablePlan(lab.Name, cons)
	}
	// Cables that will go away with the machines and networks: their instance rows are deleted too.
	before, _ := labData(h.DB, lab.Name)
	var cascade []string
	for _, c := range before.Connections {
		if containsName(cons, c.Key()) || touchesNames(c, dev, nets) {
			cascade = append(cascade, c.Key())
		}
	}
	// Deletion in the file: cables with deleted ends are removed together with the objects (see labfile).
	if _, err := updateLabGet(h.DB, lab.Name, func(l *labfile.Lab) error {
		l.Connections = dropConnections(l.Connections, cons)
		l.Devices = dropDevices(l.Devices, dev)
		l.Networks = dropNetworks(l.Networks, nets)
		l.UpdatedAt = time.Now().Unix()
		return nil
	}); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	// Instance rows that referenced the deleted objects (formerly a foreign key cascade).
	if err := pruneInstances(h.DB, dev, nets, append(cons, cascade...)); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	var warnings []msgs.Message
	if h.CableDetach != nil && len(plugs) > 0 {
		for _, e := range h.CableDetach(plugs) {
			warnings = append(warnings, msgs.Of(e))
		}
	}
	h.notifyDeleted(r, lab, len(dev), len(nets), len(cons))
	h.changed()
	if h.Cleanup != nil && len(dev) > 0 {
		h.Cleanup(lab.Name, dev)
	}
	out := map[string]any{"ok": true}
	if len(warnings) > 0 {
		out["warnings"] = warnings
	}
	httpx.JSON(w, http.StatusOK, out)
}

// paste duplicates the selected objects together with the cables between them.
func (h *Handler) paste(w http.ResponseWriter, r *http.Request) {
	lab, ok := h.labFor(w, r, access.MachAdd)
	if !ok {
		return
	}
	var in struct {
		Devices  []string `json:"devices"`
		Networks []string `json:"networks"`
		DX       float64  `json:"dx"`
		DY       float64  `json:"dy"`
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	dev, nets := dedupe(in.Devices), dedupe(in.Networks)
	if len(dev)+len(nets) >= 2 && !requirePerm(w, lab, access.LinkCreate) {
		return
	}
	if len(dev)+len(nets) == 0 || len(dev)+len(nets) > 200 || !okCoord(in.DX) || !okCoord(in.DY) {
		httpx.Err(w, http.StatusBadRequest, "Nothing to paste")
		return
	}
	// The inserting user's instance: the copy gets the disk and settings that this user sees.
	iid, err := instances.Ensure(h.DB, lab.Name, auth.FromContext(r.Context()).User.ID)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if owned(h.DB, labfile.KindDevice, lab.Name, dev) != nil || owned(h.DB, labfile.KindNetwork, lab.Name, nets) != nil {
		httpx.Err(w, http.StatusBadRequest, "The objects do not belong to this laboratory")
		return
	}
	if labDeviceCount(h.DB, lab.Name)+len(dev) > 500 {
		httpx.Err(w, http.StatusConflict, "Limit reached: 500 devices in a laboratory")
		return
	}
	t, err := loadTopology(h.DB, lab.Name, iid, h.M)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	names, err := loadNames(h.DB, lab.Name)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	devSel, netSel := map[string]bool{}, map[string]bool{}
	for _, name := range dev {
		devSel[name] = true
	}
	for _, name := range nets {
		netSel[name] = true
	}
	devMap, netMap := map[string]string{}, map[string]string{}
	var addDev []labfile.Device
	var addNet []labfile.Network
	var addConn []labfile.Connection
	for _, d := range t.Devices {
		if !devSel[d.Name] {
			continue
		}
		fd := newDevice(lab.Name, ptrOr0(d.TemplateID), d.ImageID, uniqueName(names, d.Name), d.X+in.DX, d.Y+in.DY, d.Params)
		fd.Icon, fd.CustomIcon = d.Icon, d.CustomIcon
		devMap[d.Name] = fd.Name
		addDev = append(addDev, fd)
	}
	for _, n := range t.Networks {
		if !netSel[n.Name] {
			continue
		}
		c := n
		c.Name = uniqueName(names, n.Name)
		c.X, c.Y = n.X+in.DX, n.Y+in.DY
		if n.Type == "nat" {
			c.Subnet, c.Gateway, c.DHCPStart, c.DHCPEnd = natcloud.Fields()
			c.DHCP = true
		}
		fn := newNetwork(&c, c.X, c.Y)
		netMap[n.Name] = fn.Name
		addNet = append(addNet, fn)
	}
	mapEnd := func(typ, name string) (string, bool) {
		if typ == "device" {
			v, ok := devMap[name]
			return v, ok
		}
		v, ok := netMap[name]
		return v, ok
	}
	for _, c := range t.Connections {
		a, okA := mapEnd(c.AType, c.AName)
		b, okB := mapEnd(c.BType, c.BName)
		if !okA || !okB {
			continue
		}
		addConn = append(addConn, labfile.Connection{
			A: labfile.End{Type: c.AType, Name: a, If: c.AIf},
			B: labfile.End{Type: c.BType, Name: b, If: c.BIf},
		})
	}
	// The whole insertion is one lab file write.
	if _, err := updateLabGet(h.DB, lab.Name, func(l *labfile.Lab) error {
		l.Devices = append(l.Devices, addDev...)
		l.Networks = append(l.Networks, addNet...)
		l.Connections = append(l.Connections, addConn...)
		l.UpdatedAt = time.Now().Unix()
		return nil
	}); err != nil {
		httpx.ErrKey(w, http.StatusInternalServerError, "Insert error: {detail}", map[string]string{"detail": err.Error()})
		return
	}
	h.notifyTopo(r, lab, notify.Success, notify.CatMachine, "Objects inserted", "in «{lab}»", notify.P("lab", lab.Name))
	h.changed()
	_ = instances.SyncLab(h.DB, lab.Name)
	// The response is for the current user's instance: inserted machines have correct rids and disk.
	nt, err := loadTopology(h.DB, lab.Name, iid, h.M)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	newDev, newNet := map[string]bool{}, map[string]bool{}
	for _, v := range devMap {
		newDev[v] = true
	}
	for _, v := range netMap {
		newNet[v] = true
	}
	out := Topology{Devices: []Device{}, Networks: []Network{}, Connections: []Conn{}}
	for _, d := range nt.Devices {
		if newDev[d.Name] {
			out.Devices = append(out.Devices, d)
		}
	}
	for _, n := range nt.Networks {
		if newNet[n.Name] {
			out.Networks = append(out.Networks, n)
		}
	}
	for _, c := range nt.Connections {
		if (c.AType == "device" && newDev[c.AName]) || (c.AType == "network" && newNet[c.AName]) {
			if (c.BType == "device" && newDev[c.BName]) || (c.BType == "network" && newNet[c.BName]) {
				out.Connections = append(out.Connections, c)
			}
		}
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"devices": out.Devices, "networks": out.Networks, "connections": out.Connections})
}

// ridInfo - an instance machine accessed by rid: the instance row and topology.
type ridInfo struct {
	rid     int64
	devName string
}

// rowLabRT checks access to an instance machine by the lab and device name.
func (h *Handler) rowLabRT(w http.ResponseWriter, r *http.Request) (*Lab, ridInfo, int64, bool) {
	lab, ok := h.labFor(w, r, access.LabView)
	if !ok {
		return nil, ridInfo{}, 0, false
	}
	devName := r.PathValue("device")
	if devName == "" {
		httpx.Err(w, http.StatusBadRequest, "Invalid name")
		return nil, ridInfo{}, 0, false
	}
	instID, ok := h.instanceFor(w, r, lab)
	if !ok {
		return nil, ridInfo{}, 0, false
	}
	var rid int64
	if err := h.DB.QueryRow(`SELECT id FROM instance_devices WHERE instance_id=? AND device_id=?`, instID, devName).Scan(&rid); err != nil {
		httpx.Err(w, http.StatusNotFound, "Device not found")
		return nil, ridInfo{}, 0, false
	}
	return lab, ridInfo{rid: rid, devName: devName}, instID, true
}

// instanceFor returns the lab's instance for the request: ?instance=N (administrator or owner) or own.
func (h *Handler) instanceFor(w http.ResponseWriter, r *http.Request, lab *Lab) (int64, bool) {
	ss := auth.FromContext(r.Context())
	if q := r.URL.Query().Get("instance"); q != "" {
		iid, err := strconv.ParseInt(q, 10, 64)
		if err != nil {
			httpx.Err(w, http.StatusBadRequest, "Invalid instance")
			return 0, false
		}
		labName, owner, err := instances.Owner(h.DB, iid)
		if err != nil || labName != lab.Name || !instances.CanUse(owner, ss.User.ID, ss.User.Role == "admin") {
			httpx.Err(w, http.StatusNotFound, "Instance not found")
			return 0, false
		}
		return iid, true
	}
	iid, err := instances.Ensure(h.DB, lab.Name, ss.User.ID)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return 0, false
	}
	return iid, true
}

// instancesList - lab instances of all users (administrator only).
func (h *Handler) instancesList(w http.ResponseWriter, r *http.Request) {
	labName := r.PathValue("name")
	if labName == "" {
		httpx.Err(w, http.StatusNotFound, "Laboratory not found")
		return
	}
	rows, err := h.DB.Query(`SELECT i.id, u.id, u.username,
		(SELECT COUNT(*) FROM instance_devices r WHERE r.instance_id=i.id)
		FROM lab_instances i JOIN users u ON u.id=i.user_id WHERE i.lab_id=? ORDER BY u.username`, labName)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	defer rows.Close()
	type item struct {
		ID       int64  `json:"id"`
		UserID   int64  `json:"user_id"`
		Username string `json:"username"`
		Running  int    `json:"running"`
		Total    int    `json:"total"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		if rows.Scan(&it.ID, &it.UserID, &it.Username, &it.Total) != nil {
			continue
		}
		if h.M != nil {
			devs, _ := h.DB.Query(`SELECT id FROM instance_devices WHERE instance_id=?`, it.ID)
			for devs.Next() {
				var rid int64
				if devs.Scan(&rid) == nil && h.M.Active(rid) {
					it.Running++
				}
			}
			devs.Close()
		}
		out = append(out, it)
	}
	httpx.JSON(w, http.StatusOK, out)
}

// notifyTopo notifies the initiator, the lab owner and administrators about a topology change.
func (h *Handler) notifyTopo(r *http.Request, lab *Lab, level, category, title, body string, params ...map[string]string) {
	var actor int64
	if ss := auth.FromContext(r.Context()); ss != nil {
		actor = ss.User.ID
	}
	h.Notify.Activity(actor, lab.OwnerID, level, category, title, body, labLink(lab.Name), params...)
}

// notifyDeleted notifies about deletion of topology objects (machines, networks, wires).
func (h *Handler) notifyDeleted(r *http.Request, lab *Lab, devices, networks, conns int) {
	if devices == 0 && networks == 0 && conns == 0 {
		return
	}
	cat := notify.CatCable
	if devices > 0 {
		cat = notify.CatMachine
	} else if networks > 0 {
		cat = notify.CatNetwork
	}
	h.notifyTopo(r, lab, notify.Warning, cat, "Objects deleted from «{lab}»",
		"Machines: {devices}, networks: {networks}, cables: {cables}.",
		notify.P("lab", lab.Name, "devices", strconv.Itoa(devices), "networks", strconv.Itoa(networks), "cables", strconv.Itoa(conns)))
}

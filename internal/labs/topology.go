// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"encoding/json"
	"net/http"
	"netrave/internal/labfile"
	"strconv"
	"strings"
	"time"

	"netrave/internal/access"
	"netrave/internal/auth"
	"netrave/internal/database"
	"netrave/internal/httpx"
	"netrave/internal/instances"
	"netrave/internal/natcloud"
	"netrave/internal/network"
	"netrave/internal/notify"
	"netrave/internal/qemu"
)

const maxCoord = 1e6

func okCoord(v float64) bool { return v > -maxCoord && v < maxCoord }

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

// sameID compares optional image ids.
func sameID(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func dedupe(ids []int64) []int64 {
	seen := map[int64]bool{}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func idArgs(first int64, ids []int64) []any {
	a := make([]any, 0, len(ids)+1)
	a = append(a, first)
	for _, id := range ids {
		a = append(a, id)
	}
	return a
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
	t, err := loadTopology(h.DB, lab.ID, iid, h.M)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
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
		states := h.NICState(lab.ID, rids)
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
		httpx.Err(w, http.StatusServiceUnavailable, "диагностика сети недоступна")
		return
	}
	httpx.JSON(w, http.StatusOK, h.NetworkDiag(lab.ID))
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
		ImageID    *int64  `json:"image_id"`
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
		httpx.Err(w, http.StatusBadRequest, "количество: от 1 до "+strconv.Itoa(maxBatch))
		return
	}
	if !okCoord(in.X) || !okCoord(in.Y) {
		httpx.Err(w, http.StatusBadRequest, "некорректные координаты")
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
		httpx.Err(w, http.StatusBadRequest, "шаблон не найден")
		return
	}
	if labDeviceCount(h.DB, lab.ID)+in.Count > 500 {
		httpx.Err(w, http.StatusConflict, "достигнут предел: 500 устройств в лаборатории")
		return
	}
	if in.ImageID != nil {
		if _, ok := h.DB.Catalog.Image(*in.ImageID); !ok {
			httpx.Err(w, http.StatusBadRequest, "образ не найден")
			return
		}
	}
	names, err := loadNames(h.DB, lab.ID)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	name := strings.TrimSpace(in.Name)
	if in.Count == 1 && name != "" && (!nameRe.MatchString(name) || names[name]) {
		httpx.Err(w, http.StatusBadRequest, "имя недопустимо или уже занято")
		return
	}
	if in.Count > 1 && name != "" && !nameRe.MatchString(name) {
		httpx.Err(w, http.StatusBadRequest, "имя недопустимо")
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
	if in.ImageID == nil && tpl.ImageID > 0 {
		id := tpl.ImageID
		in.ImageID = &id
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
		fd := newDevice(lab.ID, tpl.ID, in.ImageID, nameList[i], x, y, params)
		fd.ID = h.DB.Labs.NewID()
		fd.Icon, fd.CustomIcon = in.Icon, in.CustomIcon
		fds = append(fds, fd)
	}
	if _, err := updateLabGet(h.DB, lab.ID, func(l *labfile.Lab) error {
		l.Devices = append(l.Devices, fds...)
		l.UpdatedAt = time.Now().Unix()
		return nil
	}); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	ids := make([]int64, 0, len(fds))
	for _, fd := range fds {
		ids = append(ids, fd.ID)
	}
	// Instance rows (rid, MAC, disks, network interfaces) are created for the whole batch in one pass.
	_ = instances.SyncLab(h.DB, lab.ID)
	// The response is built for the creating user's instance: it contains the machine's rid,
	// which the frontend uses to save settings (PUT /api/devices/{rid}).
	iid, err := instances.Ensure(h.DB, lab.ID, auth.FromContext(r.Context()).User.ID)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	out := make([]*Device, 0, len(ids))
	for _, id := range ids {
		d, err := getDeviceInst(h.DB, id, iid, h.M)
		if err != nil {
			httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
		out = append(out, d)
	}
	if len(out) == 1 {
		h.notifyTopo(r, lab, notify.Success, notify.CatMachine, "Машина добавлена", out[0].Name+" → «"+lab.Name+"»")
		httpx.JSON(w, http.StatusCreated, out[0])
		return
	}
	h.notifyTopo(r, lab, notify.Success, notify.CatMachine, "Машины добавлены",
		strconv.Itoa(len(out))+" шт.: "+out[0].Name+" … "+out[len(out)-1].Name+" → «"+lab.Name+"»")
	httpx.JSON(w, http.StatusCreated, out)
}

type optInt struct {
	Set bool
	Val *int64
}

func (o *optInt) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Val = nil
		return nil
	}
	var v int64
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Val = &v
	return nil
}

// rowLab finds the lab by device/network id and checks access.
func (h *Handler) rowLab(w http.ResponseWriter, r *http.Request, kind string) (*Lab, int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "некорректный id")
		return nil, 0, false
	}
	labID, ok := labOwnerOf(h.DB, kind, id)
	if !ok {
		httpx.Err(w, http.StatusNotFound, "объект не найден")
		return nil, 0, false
	}
	lab, ok := h.labAt(w, auth.FromContext(r.Context()), labID, "")
	return lab, id, ok
}

func (h *Handler) updateDevice(w http.ResponseWriter, r *http.Request) {
	lab, rid, instID, ok := h.rowLabRT(w, r)
	if !ok {
		return
	}
	id := rid.devID
	var in struct {
		Name       *string  `json:"name"`
		X          *float64 `json:"x"`
		Y          *float64 `json:"y"`
		ImageID    optInt   `json:"image_id"`
		Icon       *string  `json:"icon"`
		CustomIcon *string  `json:"custom_icon"`
		IconSize   *int     `json:"icon_size"`
		paramsPatch
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	cur, err := getDeviceInst(h.DB, id, instID, h.M)
	if err != nil {
		httpx.Err(w, http.StatusNotFound, "устройство не найдено")
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
		httpx.Err(w, http.StatusBadRequest, "размер иконки: от 24 до 256")
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
			if _, ok := h.DB.Catalog.Image(*imgID); !ok {
				httpx.Err(w, http.StatusBadRequest, "образ не найден")
				return
			}
		}
	}
	imgChanged := in.ImageID.Set && !sameID(imgID, cur.ImageID)
	// Running machine: parameters are saved and applied on restart (see Pending);
	// the disk image can only be changed for a stopped machine - its working overlay is bound to it.
	if imgChanged && cur.State != "stopped" && cur.State != "error" {
		httpx.Err(w, http.StatusConflict, "образ диска меняется только у остановленного устройства")
		return
	}
	// The overlay is bound to the base image: after the first start the disk can only be changed via Wipe.
	if imgChanged && h.HasDisks != nil && h.HasDisks(lab.ID, rid.rid) {
		httpx.Err(w, http.StatusConflict, "у машины уже есть рабочий диск: сбросьте диск (Wipe), чтобы сменить образ")
		return
	}
	if changed && p.NICs < cur.NICs {
		if cablesFromPort(h.DB, lab.ID, id, p.NICs) > 0 {
			httpx.Err(w, http.StatusConflict, "уберите кабели с интерфейсов, которые будут удалены")
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
		if !nameRe.MatchString(name) {
			httpx.Err(w, http.StatusBadRequest, "имя: 1–32 символа (буквы, цифры, пробел, _ . -)")
			return
		}
		names, err := loadNames(h.DB, lab.ID)
		if err != nil {
			httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
		if name != cur.Name && names[name] {
			httpx.Err(w, http.StatusConflict, "имя уже занято")
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
		httpx.Err(w, http.StatusBadRequest, "некорректные координаты")
		return
	}
	// Topology (name, position, icon) is in the lab file.
	err = setDevice(h.DB, id, func(d *labfile.Device) {
		d.Name, d.X, d.Y, d.Icon, d.CustomIcon, d.IconSize = name, x, y, icon, custom, iconSize
		if imgChanged && lab.Perms[access.LabManageAcc] {
			// The disk chosen by the owner or lab administrator becomes the default disk:
			// it is given to participants who have not made their own choice yet.
			d.ImageID = 0
			if imgID != nil {
				d.ImageID = *imgID
			}
		}
	})
	// Instance parameters are in the instance row.
	if err == nil {
		_, err = h.DB.Exec(`UPDATE instance_devices SET machine=?,cpu=?,ram_mb=?,nics=?,nic_model=?,iface_type=?,iface_start=?,disk_bus=?,bios=?,console=?,extra_args=? WHERE id=?`,
			p.Machine, p.CPU, p.RAMMB, p.NICs, p.NICModel, p.IfaceType, p.IfaceStart, p.DiskBus, p.BIOS, p.Console, p.ExtraArgs, rid.rid)
	}
	if err == nil && imgChanged {
		// The disk is selected for this instance: other instances do not change it.
		_, err = h.DB.Exec(`UPDATE instance_devices SET image_id=?, image_own=1 WHERE id=?`, imgID, rid.rid)
	}
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if custom != cur.CustomIcon {
		h.dropIcon(cur.CustomIcon) // the previous file is deleted if no one else uses it
	}
	touch(h.DB, lab.ID)
	d, _ := getDeviceInst(h.DB, id, instID, h.M)
	if d != nil {
		h.notifyTopo(r, lab, notify.Info, notify.CatMachine, "Машина изменена", d.Name+" в «"+lab.Name+"»")
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
		httpx.Err(w, http.StatusBadRequest, "тип сети: isolated, nat или cloud")
		return
	}
	if !okCoord(in.X) || !okCoord(in.Y) {
		httpx.Err(w, http.StatusBadRequest, "некорректные координаты")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	names, err := loadNames(h.DB, lab.ID)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		base := map[string]string{"isolated": "Net", "nat": "NAT", "cloud": "Cloud"}[in.Type]
		name = uniqueName(names, base)
	} else if !nameRe.MatchString(name) || names[name] {
		httpx.Err(w, http.StatusBadRequest, "имя недопустимо или уже занято")
		return
	}
	n := &Network{Name: name, Type: in.Type, X: in.X, Y: in.Y}
	if in.Type == "nat" {
		// Single NAT cloud: subnet, gateway and DHCP are shared by all labs.
		n.Subnet, n.Gateway, n.DHCPStart, n.DHCPEnd = natcloud.Fields()
		n.DHCP = true
	}
	fn := newNetwork(n, in.X, in.Y)
	fn.ID = h.DB.Labs.NewID()
	fn.BridgeName = bridgeName(fn.ID)
	if _, err := updateLabGet(h.DB, lab.ID, func(l *labfile.Lab) error {
		l.Networks = append(l.Networks, fn)
		l.UpdatedAt = time.Now().Unix()
		return nil
	}); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	id := fn.ID
	h.notifyTopo(r, lab, notify.Success, notify.CatNetwork, "Сеть создана", n.Name+" в «"+lab.Name+"»")
	h.changed()
	out, _ := getNetwork(h.DB, id)
	httpx.JSON(w, http.StatusCreated, out)
}

func (h *Handler) updateNetwork(w http.ResponseWriter, r *http.Request) {
	lab, id, ok := h.rowLab(w, r, labfile.KindNetwork)
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
	n, err := getNetwork(h.DB, id)
	if err != nil {
		httpx.Err(w, http.StatusNotFound, "сеть не найдена")
		return
	}
	// The NAT cloud is shared: subnet, gateway and DHCP are set by the server for all labs and do not change.
	if n.Type == "nat" && (in.Subnet != nil || in.Gateway != nil || in.DHCPStart != nil || in.DHCPEnd != nil ||
		(in.DHCP != nil && !*in.DHCP) || in.HostIface != nil) {
		httpx.Err(w, http.StatusBadRequest, "NAT-облако общее для всех лабораторий: подсеть, шлюз и DHCP не меняются")
		return
	}
	if in.HostIface != nil && netRunning(h.DB, h.M, id) {
		httpx.Err(w, http.StatusConflict, "остановите подключённые устройства, чтобы менять настройки сети")
		return
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if !nameRe.MatchString(name) {
			httpx.Err(w, http.StatusBadRequest, "имя: 1–32 символа (буквы, цифры, пробел, _ . -)")
			return
		}
		names, err := loadNames(h.DB, lab.ID)
		if err != nil {
			httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
		if name != n.Name && names[name] {
			httpx.Err(w, http.StatusConflict, "имя уже занято")
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
		httpx.Err(w, http.StatusBadRequest, "некорректные координаты")
		return
	}
	if n.Type == "cloud" && in.HostIface != nil {
		v := strings.TrimSpace(*in.HostIface)
		if v != "" && !ifaceRe.MatchString(v) {
			httpx.Err(w, http.StatusBadRequest, "недопустимое имя сетевого интерфейса")
			return
		}
		n.HostIface = v
	}
	err = setNetwork(h.DB, id, func(fn *labfile.Network) {
		fn.Name, fn.X, fn.Y = n.Name, n.X, n.Y
		fn.Subnet, fn.Gateway, fn.DHCPEnabled = n.Subnet, n.Gateway, n.DHCP
		fn.DHCPStart, fn.DHCPEnd, fn.HostIface = n.DHCPStart, n.DHCPEnd, n.HostIface
	})
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	touch(h.DB, lab.ID)
	h.notifyTopo(r, lab, notify.Info, notify.CatNetwork, "Сеть изменена", n.Name+" в «"+lab.Name+"»")
	h.changed()
	out, _ := getNetwork(h.DB, id)
	httpx.JSON(w, http.StatusOK, out)
}

// ---------- connections ----------

// setConnState enables or disables a wire in the user's instance (or the administrator's, with ?instance=N).
// Running machines switch immediately (TAP and QMP set_link), stopped ones on startup.
func (h *Handler) setConnState(w http.ResponseWriter, r *http.Request) {
	lab, id, ok := h.rowLab(w, r, labfile.KindConnection)
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
		httpx.Err(w, http.StatusBadRequest, "укажите up: true или false")
		return
	}
	iid, ok := h.instanceFor(w, r, lab)
	if !ok {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.CableSet == nil {
		httpx.Err(w, http.StatusInternalServerError, "управление проводами не настроено")
		return
	}
	if err := h.CableSet(iid, id, *in.Up); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "провод: "+err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"id": id, "up": *in.Up})
}

// bumpAppliedNICs marks added interfaces as applied on running machines: they are connected
// via QMP hotplug, so no restart is needed to change the NIC count.
func bumpAppliedNICs(db *database.DB, mgr *qemu.Manager, deviceID int64, nics int) {
	rows, err := db.Query(`SELECT id, applied FROM instance_devices
		WHERE device_id=? AND applied<>''`, deviceID)
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
		AID   int64  `json:"a_id"`
		AIf   int    `json:"a_if"`
		BType string `json:"b_type"`
		BID   int64  `json:"b_id"`
		BIf   int    `json:"b_if"`
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if in.AType == "network" && in.BType == "device" {
		in.AType, in.AID, in.AIf, in.BType, in.BID, in.BIf = in.BType, in.BID, in.BIf, in.AType, in.AID, in.AIf
	}
	if (in.AType != "device" && in.AType != "network") || (in.BType != "device" && in.BType != "network") {
		httpx.Err(w, http.StatusBadRequest, "недопустимый тип конечной точки")
		return
	}
	if in.AType == "network" && in.BType == "network" {
		httpx.Err(w, http.StatusBadRequest, "соединение сеть—сеть не поддерживается")
		return
	}
	if in.AType == in.BType && in.AID == in.BID {
		httpx.Err(w, http.StatusBadRequest, "нельзя соединить объект с самим собой")
		return
	}
	grow := map[int64]int{}
	for _, ep := range []struct {
		t   string
		id  int64
		ifc *int
	}{{in.AType, in.AID, &in.AIf}, {in.BType, in.BID, &in.BIf}} {
		if ep.t == "network" {
			*ep.ifc = 0
			if owned(h.DB, labfile.KindNetwork, lab.ID, []int64{ep.id}) != nil {
				httpx.Err(w, http.StatusBadRequest, "сеть не найдена в этой лаборатории")
				return
			}
			continue
		}
		nics, ok := deviceNICs(h.DB, lab.ID, ep.id)
		if !ok {
			httpx.Err(w, http.StatusBadRequest, "устройство не найдено в этой лаборатории")
			return
		}
		// A number equal to the current interface count is a new port: it will be
		// added to a running machine by hotplug (or appear at the next start).
		if *ep.ifc < 0 || *ep.ifc > nics {
			httpx.Err(w, http.StatusBadRequest, "несуществующий интерфейс устройства")
			return
		}
		if *ep.ifc == nics {
			if nics >= 32 {
				httpx.Err(w, http.StatusConflict, "достигнут предел: 32 сетевых интерфейса")
				return
			}
			*ep.ifc = nics
			grow[ep.id] = nics + 1
		}
		if cablesOnPort(h.DB, lab.ID, ep.id, *ep.ifc) > 0 {
			httpx.Err(w, http.StatusConflict, "интерфейс уже подключён кабелем")
			return
		}
	}
	// The wire and new device interfaces are written in one lab file write.
	saved, err := updateLabGet(h.DB, lab.ID, func(l *labfile.Lab) error {
		for i := range l.Devices {
			if n, ok := grow[l.Devices[i].ID]; ok && l.Devices[i].NICs < n {
				l.Devices[i].NICs = n
			}
		}
		l.Connections = append(l.Connections, labfile.Connection{
			A: labfile.End{Type: in.AType, ID: in.AID, If: in.AIf},
			B: labfile.End{Type: in.BType, ID: in.BID, If: in.BIf},
		})
		l.UpdatedAt = time.Now().Unix()
		return nil
	})
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	cid := saved.Connections[len(saved.Connections)-1].ID
	// Instance rows receive the same number of interfaces.
	for id, n := range grow {
		if _, err := h.DB.Exec(`UPDATE instance_devices SET nics=CASE WHEN nics<? THEN ? ELSE nics END WHERE device_id=?`, n, n, id); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
	}
	// Running machines get the wire immediately: the TAP joins the bridge and is brought up, QEMU receives set_link up.
	var warnings []string
	if h.CableAttach != nil {
		for _, e := range h.CableAttach(cid) {
			warnings = append(warnings, e.Error())
		}
	}
	if len(warnings) > 0 {
		h.notifyTopo(r, lab, notify.Warning, notify.CatCable, "Провод создан, но применён не полностью", strings.Join(warnings, "; "))
	} else {
		// New interfaces are added to running machines via QMP: they do not need a restart,
		// so the start snapshot is updated and "changes waiting for restart" is not shown.
		for id, n := range grow {
			bumpAppliedNICs(h.DB, h.M, id, n)
		}
		h.notifyTopo(r, lab, notify.Success, notify.CatCable, "Провод создан", "в «"+lab.Name+"»")
	}
	h.changed()
	// The new wire is enabled (up=true): wires are never disabled by default.
	httpx.JSON(w, http.StatusCreated, Conn{ID: cid, LabID: lab.ID, AType: in.AType, AID: in.AID, AIf: in.AIf,
		BType: in.BType, BID: in.BID, BIf: in.BIf, Up: true, Warnings: warnings})
}

// ---------- positions, deletion, insertion ----------

func containsID(ids []int64, v int64) bool {
	for _, id := range ids {
		if id == v {
			return true
		}
	}
	return false
}

// touchesIDs reports whether the wire has an end on the machines or networks being deleted.
func touchesIDs(c labfile.Connection, dev, nets []int64) bool {
	return (c.A.Type == "device" && containsID(dev, c.A.ID)) || (c.A.Type == "network" && containsID(nets, c.A.ID)) ||
		(c.B.Type == "device" && containsID(dev, c.B.ID)) || (c.B.Type == "network" && containsID(nets, c.B.ID))
}

func dropConnections(list []labfile.Connection, ids []int64) []labfile.Connection {
	out := list[:0]
	for _, c := range list {
		if !containsID(ids, c.ID) {
			out = append(out, c)
		}
	}
	return out
}

func dropDevices(list []labfile.Device, ids []int64) []labfile.Device {
	out := list[:0]
	for _, d := range list {
		if !containsID(ids, d.ID) {
			out = append(out, d)
		}
	}
	return out
}

func dropNetworks(list []labfile.Network, ids []int64) []labfile.Network {
	out := list[:0]
	for _, n := range list {
		if !containsID(ids, n.ID) {
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
	ID int64   `json:"id"`
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
				httpx.Err(w, http.StatusBadRequest, "некорректные координаты")
				return
			}
		}
	}
	for _, p := range in.Devices {
		if p.IconSize != nil && !validIconSize(*p.IconSize) {
			httpx.Err(w, http.StatusBadRequest, "размер иконки: от 24 до 256")
			return
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	devPos, netPos := map[int64]pos{}, map[int64]pos{}
	for _, p := range in.Devices {
		devPos[p.ID] = p
	}
	for _, p := range in.Networks {
		netPos[p.ID] = p
	}
	// Objects of other labs and unknown numbers are skipped: only own positions are changed.
	if _, err := updateLabGet(h.DB, lab.ID, func(l *labfile.Lab) error {
		for i := range l.Devices {
			if p, ok := devPos[l.Devices[i].ID]; ok {
				l.Devices[i].X, l.Devices[i].Y = p.X, p.Y
				if p.IconSize != nil {
					l.Devices[i].IconSize = *p.IconSize
				}
			}
		}
		for i := range l.Networks {
			if p, ok := netPos[l.Networks[i].ID]; ok {
				l.Networks[i].X, l.Networks[i].Y = p.X, p.Y
			}
		}
		l.UpdatedAt = time.Now().Unix()
		return nil
	}); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
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
		Devices     []int64 `json:"devices"`
		Networks    []int64 `json:"networks"`
		Connections []int64 `json:"connections"`
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
		kind string
		ids  []int64
	}{{labfile.KindDevice, dev}, {labfile.KindNetwork, nets}, {labfile.KindConnection, cons}} {
		if err := owned(h.DB, c.kind, lab.ID, c.ids); err != nil {
			httpx.Err(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	for _, d := range dev {
		if devHasRunningPeer(h.DB, h.M, d) {
			httpx.Err(w, http.StatusConflict, "устройство соединено с запущенным — остановите его или удалите кабель сначала")
			return
		}
	}
	for _, n := range nets {
		if netRunning(h.DB, h.M, n) {
			httpx.Err(w, http.StatusConflict, "остановите устройства, подключённые к сети")
			return
		}
	}
	if len(dev) > 0 && anyDeviceRunning(h.DB, h.M, dev) {
		httpx.Err(w, http.StatusConflict, "сначала остановите запущенные устройства")
		return
	}
	// Wires of running machines are physically disconnected (QMP hot-unplug, TAP) without stopping: remember the ends before deletion.
	var plugs []network.Plug
	if len(cons) > 0 && h.CablePlan != nil {
		plugs = h.CablePlan(cons)
	}
	// Wires that will go away with the machines and networks: their instance rows are deleted too.
	before, _ := labData(h.DB, lab.ID)
	var cascade []int64
	for _, c := range before.Connections {
		if containsID(cons, c.ID) || touchesIDs(c, dev, nets) {
			cascade = append(cascade, c.ID)
		}
	}
	// Deletion in the file: wires with deleted ends are removed together with the objects (see labfile).
	if _, err := updateLabGet(h.DB, lab.ID, func(l *labfile.Lab) error {
		l.Connections = dropConnections(l.Connections, cons)
		l.Devices = dropDevices(l.Devices, dev)
		l.Networks = dropNetworks(l.Networks, nets)
		l.UpdatedAt = time.Now().Unix()
		return nil
	}); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	// Instance rows that referenced the deleted objects (formerly a foreign key cascade).
	if err := pruneInstances(h.DB, dev, nets, append(cons, cascade...)); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	var warnings []string
	if h.CableDetach != nil && len(plugs) > 0 {
		for _, e := range h.CableDetach(plugs) {
			warnings = append(warnings, e.Error())
		}
	}
	h.notifyDeleted(r, lab, len(dev), len(nets), len(cons))
	h.changed()
	if h.Cleanup != nil && len(dev) > 0 {
		h.Cleanup(lab.ID, dev)
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
		Devices  []int64 `json:"devices"`
		Networks []int64 `json:"networks"`
		DX       float64 `json:"dx"`
		DY       float64 `json:"dy"`
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	dev, nets := dedupe(in.Devices), dedupe(in.Networks)
	if len(dev)+len(nets) >= 2 && !requirePerm(w, lab, access.LinkCreate) {
		return
	}
	if len(dev)+len(nets) == 0 || len(dev)+len(nets) > 200 || !okCoord(in.DX) || !okCoord(in.DY) {
		httpx.Err(w, http.StatusBadRequest, "нечего вставлять")
		return
	}
	// The inserting user's instance: the copy gets the disk and settings that this user sees.
	iid, err := instances.Ensure(h.DB, lab.ID, auth.FromContext(r.Context()).User.ID)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if owned(h.DB, labfile.KindDevice, lab.ID, dev) != nil || owned(h.DB, labfile.KindNetwork, lab.ID, nets) != nil {
		httpx.Err(w, http.StatusBadRequest, "объекты не принадлежат этой лаборатории")
		return
	}
	if labDeviceCount(h.DB, lab.ID)+len(dev) > 500 {
		httpx.Err(w, http.StatusConflict, "достигнут предел: 500 устройств в лаборатории")
		return
	}
	t, err := loadTopology(h.DB, lab.ID, iid, h.M)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	names, err := loadNames(h.DB, lab.ID)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	devSel, netSel := map[int64]bool{}, map[int64]bool{}
	for _, id := range dev {
		devSel[id] = true
	}
	for _, id := range nets {
		netSel[id] = true
	}
	devMap, netMap := map[int64]int64{}, map[int64]int64{}
	var addDev []labfile.Device
	var addNet []labfile.Network
	var addConn []labfile.Connection
	for _, d := range t.Devices {
		if !devSel[d.ID] {
			continue
		}
		fd := newDevice(lab.ID, ptrOr0(d.TemplateID), d.ImageID, uniqueName(names, d.Name), d.X+in.DX, d.Y+in.DY, d.Params)
		fd.ID = h.DB.Labs.NewID()
		fd.Icon, fd.CustomIcon = d.Icon, d.CustomIcon
		devMap[d.ID] = fd.ID
		addDev = append(addDev, fd)
	}
	for _, n := range t.Networks {
		if !netSel[n.ID] {
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
		fn.ID = h.DB.Labs.NewID()
		fn.BridgeName = bridgeName(fn.ID)
		netMap[n.ID] = fn.ID
		addNet = append(addNet, fn)
	}
	mapEnd := func(typ string, id int64) (int64, bool) {
		if typ == "device" {
			v, ok := devMap[id]
			return v, ok
		}
		v, ok := netMap[id]
		return v, ok
	}
	for _, c := range t.Connections {
		a, okA := mapEnd(c.AType, c.AID)
		b, okB := mapEnd(c.BType, c.BID)
		if !okA || !okB {
			continue
		}
		addConn = append(addConn, labfile.Connection{
			A: labfile.End{Type: c.AType, ID: a, If: c.AIf},
			B: labfile.End{Type: c.BType, ID: b, If: c.BIf},
		})
	}
	// The whole insertion is one lab file write.
	if _, err := updateLabGet(h.DB, lab.ID, func(l *labfile.Lab) error {
		l.Devices = append(l.Devices, addDev...)
		l.Networks = append(l.Networks, addNet...)
		l.Connections = append(l.Connections, addConn...)
		l.UpdatedAt = time.Now().Unix()
		return nil
	}); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка вставки: "+err.Error())
		return
	}
	h.notifyTopo(r, lab, notify.Success, notify.CatMachine, "Объекты вставлены", "в «"+lab.Name+"»")
	h.changed()
	_ = instances.SyncLab(h.DB, lab.ID)
	// The response is for the current user's instance: inserted machines have correct rids and disk.
	nt, err := loadTopology(h.DB, lab.ID, iid, h.M)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	newDev, newNet := map[int64]bool{}, map[int64]bool{}
	for _, v := range devMap {
		newDev[v] = true
	}
	for _, v := range netMap {
		newNet[v] = true
	}
	out := Topology{Devices: []Device{}, Networks: []Network{}, Connections: []Conn{}}
	for _, d := range nt.Devices {
		if newDev[d.ID] {
			out.Devices = append(out.Devices, d)
		}
	}
	for _, n := range nt.Networks {
		if newNet[n.ID] {
			out.Networks = append(out.Networks, n)
		}
	}
	for _, c := range nt.Connections {
		if (c.AType == "device" && newDev[c.AID]) || (c.AType == "network" && newNet[c.AID]) {
			if (c.BType == "device" && newDev[c.BID]) || (c.BType == "network" && newNet[c.BID]) {
				out.Connections = append(out.Connections, c)
			}
		}
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"devices": out.Devices, "networks": out.Networks, "connections": out.Connections})
}

// ridInfo - an instance machine accessed by rid: the instance row and topology.
type ridInfo struct {
	rid   int64
	devID int64
}

// rowLabRT checks access to an instance machine by rid: instance owner or administrator, then the lab.
func (h *Handler) rowLabRT(w http.ResponseWriter, r *http.Request) (*Lab, ridInfo, int64, bool) {
	rid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "некорректный id")
		return nil, ridInfo{}, 0, false
	}
	inf, err := instances.Device(h.DB, rid)
	ss := auth.FromContext(r.Context())
	if err != nil || !instances.CanUse(inf.OwnerID, ss.User.ID, ss.User.Role == "admin") {
		httpx.Err(w, http.StatusNotFound, "устройство не найдено")
		return nil, ridInfo{}, 0, false
	}
	lab, ok := h.labAt(w, ss, inf.LabID, access.LabView)
	if !ok {
		return nil, ridInfo{}, 0, false
	}
	return lab, ridInfo{rid: rid, devID: inf.DeviceID}, inf.InstanceID, true
}

// instanceFor returns the lab's instance for the request: ?instance=N (administrator or owner) or own.
func (h *Handler) instanceFor(w http.ResponseWriter, r *http.Request, lab *Lab) (int64, bool) {
	ss := auth.FromContext(r.Context())
	if q := r.URL.Query().Get("instance"); q != "" {
		iid, err := strconv.ParseInt(q, 10, 64)
		if err != nil {
			httpx.Err(w, http.StatusBadRequest, "некорректный экземпляр")
			return 0, false
		}
		lid, owner, err := instances.Owner(h.DB, iid)
		if err != nil || lid != lab.ID || !instances.CanUse(owner, ss.User.ID, ss.User.Role == "admin") {
			httpx.Err(w, http.StatusNotFound, "экземпляр не найден")
			return 0, false
		}
		return iid, true
	}
	iid, err := instances.Ensure(h.DB, lab.ID, ss.User.ID)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return 0, false
	}
	return iid, true
}

// instancesList - lab instances of all users (administrator only).
func (h *Handler) instancesList(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusNotFound, "лаборатория не найдена")
		return
	}
	rows, err := h.DB.Query(`SELECT i.id, u.id, u.username,
		(SELECT COUNT(*) FROM instance_devices r WHERE r.instance_id=i.id)
		FROM lab_instances i JOIN users u ON u.id=i.user_id WHERE i.lab_id=? ORDER BY u.username`, id)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
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
func (h *Handler) notifyTopo(r *http.Request, lab *Lab, level, category, title, body string) {
	var actor int64
	if ss := auth.FromContext(r.Context()); ss != nil {
		actor = ss.User.ID
	}
	h.Notify.Activity(actor, lab.OwnerID, level, category, title, body, "/labs/"+strconv.FormatInt(lab.ID, 10)+"/editor")
}

// notifyDeleted notifies about deletion of topology objects (machines, networks, wires).
func (h *Handler) notifyDeleted(r *http.Request, lab *Lab, devices, networks, conns int) {
	var parts []string
	if devices > 0 {
		parts = append(parts, "машин: "+strconv.Itoa(devices))
	}
	if networks > 0 {
		parts = append(parts, "сетей: "+strconv.Itoa(networks))
	}
	if conns > 0 {
		parts = append(parts, "проводов: "+strconv.Itoa(conns))
	}
	if len(parts) == 0 {
		return
	}
	cat := notify.CatCable
	if devices > 0 {
		cat = notify.CatMachine
	} else if networks > 0 {
		cat = notify.CatNetwork
	}
	h.notifyTopo(r, lab, notify.Warning, cat, "Объекты удалены из «"+lab.Name+"»", strings.Join(parts, ", ")+".")
}

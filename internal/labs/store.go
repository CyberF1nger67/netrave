// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"netrave/internal/catalog"
	"time"

	"netrave/internal/database"
	"netrave/internal/icons"
	"netrave/internal/labfile"
	"netrave/internal/natcloud"
	"netrave/internal/qemu"
)

// errNotFound - topology object not found.
var errNotFound = errors.New("объект не найден")

// The topology (devices, networks, cables, their parameters and layout) is stored in the lab file
// (labfile package). Instance state - running processes, copies of parameters and instance links -
// remains in the database. The functions below join both sources.

// labOwnerOf returns the lab of an object of kind with id id.
func labOwnerOf(db *database.DB, kind string, id int64) (int64, bool) {
	return db.Labs.LabOf(kind, id)
}

// labData returns a copy of the lab from the file.
func labData(db *database.DB, id int64) (labfile.Lab, bool) {
	return db.Labs.Lab(id)
}

// labView assembles a lab for the client: data from the file, owner name - from the database.
func labView(db *database.DB, id int64) (*Lab, bool) {
	fl, ok := labData(db, id)
	if !ok {
		return nil, false
	}
	var owner string
	_ = db.QueryRow(`SELECT username FROM users WHERE id=?`, fl.OwnerID).Scan(&owner)
	canvas := fl.CanvasJSON
	if !validCanvas([]byte(canvas)) {
		canvas = "{}"
	}
	return &Lab{
		ID:          fl.ID,
		OwnerID:     fl.OwnerID,
		Owner:       owner,
		Name:        fl.Name,
		Description: fl.Description,
		Canvas:      json.RawMessage(canvas),
		CreatedAt:   fl.CreatedAt,
		UpdatedAt:   fl.UpdatedAt,
		Devices:     len(fl.Devices),
		Networks:    len(fl.Networks),
		PublicLevel: fl.PublicLevel,
	}, true
}

// runningLabIDs returns the labs in which at least one instance machine is running.
// Machine state lives only in the QEMU manager memory.
func runningLabIDs(db *database.DB, m *qemu.Manager) (map[int64]bool, error) {
	rows, err := db.Query(`SELECT i.lab_id, d.id FROM lab_instances i
		JOIN instance_devices d ON d.instance_id=i.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var labID, rid int64
		if err := rows.Scan(&labID, &rid); err != nil {
			return nil, err
		}
		if m != nil && m.Active(rid) {
			out[labID] = true
		}
	}
	return out, rows.Err()
}

// updateLabGet atomically changes the lab and returns the saved version (with assigned ids).
func updateLabGet(db *database.DB, labID int64, fn func(*labfile.Lab) error) (labfile.Lab, error) {
	return db.Labs.Update(labID, fn)
}

// owned checks that all objects of kind with ids ids belong to lab labID.
func owned(db *database.DB, kind string, labID int64, ids []int64) error {
	for _, id := range ids {
		if owner, ok := labOwnerOf(db, kind, id); !ok || owner != labID {
			return errors.New("объекты не принадлежат этой лаборатории")
		}
	}
	return nil
}

// deviceNICs returns the number of network interfaces of a lab device.
func deviceNICs(db *database.DB, labID, devID int64) (int, bool) {
	l, ok := labData(db, labID)
	if !ok {
		return 0, false
	}
	for _, d := range l.Devices {
		if d.ID == devID {
			return d.NICs, true
		}
	}
	return 0, false
}

// cablesOnPort counts the cables occupying port devID with number ifc in lab labID.
func cablesOnPort(db *database.DB, labID, devID int64, ifc int) int {
	l, ok := labData(db, labID)
	if !ok {
		return 0
	}
	n := 0
	for _, c := range l.Connections {
		if (c.A.Type == "device" && c.A.ID == devID && c.A.If == ifc) || (c.B.Type == "device" && c.B.ID == devID && c.B.If == ifc) {
			n++
		}
	}
	return n
}

// cablesFromPort counts the cables on ports of device devID with a number >= from.
func cablesFromPort(db *database.DB, labID, devID int64, from int) int {
	l, ok := labData(db, labID)
	if !ok {
		return 0
	}
	n := 0
	for _, c := range l.Connections {
		if (c.A.Type == "device" && c.A.ID == devID && c.A.If >= from) || (c.B.Type == "device" && c.B.ID == devID && c.B.If >= from) {
			n++
		}
	}
	return n
}

// updateLab atomically changes the lab in the file.
func updateLab(db *database.DB, labID int64, fn func(*labfile.Lab) error) error {
	_, err := db.Labs.Update(labID, fn)
	return err
}

// touch marks the lab as modified.
func touch(db *database.DB, labID int64) {
	_ = updateLab(db, labID, func(l *labfile.Lab) error {
		l.UpdatedAt = time.Now().Unix()
		return nil
	})
}

// findDevice returns a device from the file and the lab it belongs to.
func findDevice(db *database.DB, id int64) (labfile.Lab, labfile.Device, bool) {
	labID, ok := labOwnerOf(db, labfile.KindDevice, id)
	if !ok {
		return labfile.Lab{}, labfile.Device{}, false
	}
	l, ok := labData(db, labID)
	if !ok {
		return labfile.Lab{}, labfile.Device{}, false
	}
	for _, d := range l.Devices {
		if d.ID == id {
			return l, d, true
		}
	}
	return labfile.Lab{}, labfile.Device{}, false
}

// findNetwork returns a network from the file and its lab.
func findNetwork(db *database.DB, id int64) (labfile.Lab, labfile.Network, bool) {
	labID, ok := labOwnerOf(db, labfile.KindNetwork, id)
	if !ok {
		return labfile.Lab{}, labfile.Network{}, false
	}
	l, ok := labData(db, labID)
	if !ok {
		return labfile.Lab{}, labfile.Network{}, false
	}
	for _, n := range l.Networks {
		if n.ID == id {
			return l, n, true
		}
	}
	return labfile.Lab{}, labfile.Network{}, false
}

// setDevice changes a device in the lab file.
func setDevice(db *database.DB, id int64, fn func(*labfile.Device)) error {
	labID, ok := labOwnerOf(db, labfile.KindDevice, id)
	if !ok {
		return errNotFound
	}
	return updateLab(db, labID, func(l *labfile.Lab) error {
		for i := range l.Devices {
			if l.Devices[i].ID == id {
				fn(&l.Devices[i])
				return nil
			}
		}
		return errNotFound
	})
}

// setNetwork changes a network in the lab file.
func setNetwork(db *database.DB, id int64, fn func(*labfile.Network)) error {
	labID, ok := labOwnerOf(db, labfile.KindNetwork, id)
	if !ok {
		return errNotFound
	}
	return updateLab(db, labID, func(l *labfile.Lab) error {
		for i := range l.Networks {
			if l.Networks[i].ID == id {
				fn(&l.Networks[i])
				return nil
			}
		}
		return errNotFound
	})
}

// labDeviceCount - the number of machines in the lab.
func labDeviceCount(db *database.DB, labID int64) int {
	l, _ := labData(db, labID)
	return len(l.Devices)
}

// loadNames returns the names of the lab's devices and networks.
func loadNames(db *database.DB, labID int64) (map[string]bool, error) {
	l, ok := labData(db, labID)
	if !ok {
		return nil, errNotFound
	}
	names := map[string]bool{}
	for _, d := range l.Devices {
		names[d.Name] = true
	}
	for _, n := range l.Networks {
		names[n.Name] = true
	}
	return names, nil
}

// instRow - an instance machine row (instance_devices) to overlay on top of the lab file.
type instRow struct {
	RID        int64
	ImageID    sql.NullInt64
	Machine    string
	CPU        int
	RAMMB      int
	NICs       int
	NICModel   string
	IfaceType  string
	IfaceStart int
	DiskBus    string
	BIOS       string
	Console    string
	ExtraArgs  string
	Applied    string
}

const instRowCols = `id,device_id,image_id,machine,cpu,ram_mb,nics,nic_model,iface_type,iface_start,disk_bus,bios,console,extra_args,applied`

// loadInstRows reads the machine rows of instance instID, keyed by device id.
func loadInstRows(q querier, instID int64) (map[int64]instRow, error) {
	rows, err := q.Query(`SELECT `+instRowCols+` FROM instance_devices WHERE instance_id=?`, instID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]instRow{}
	for rows.Next() {
		var r instRow
		var devID int64
		if err := rows.Scan(&r.RID, &devID, &r.ImageID, &r.Machine, &r.CPU, &r.RAMMB, &r.NICs, &r.NICModel, &r.IfaceType, &r.IfaceStart,
			&r.DiskBus, &r.BIOS, &r.Console, &r.ExtraArgs, &r.Applied); err != nil {
			return nil, err
		}
		out[devID] = r
	}
	return out, rows.Err()
}

// loadTemplateTypes returns the type of each template.
func loadTemplateTypes(cat *catalog.Store) (map[int64]string, error) {
	out := map[int64]string{}
	for _, t := range cat.Templates() {
		out[t.ID] = t.Type
	}
	return out, nil
}

// deviceView assembles a machine for the client: parameters and image from the file, state and overrides - from
// the instance row (if there is one).
func deviceView(labID int64, fd labfile.Device, r *instRow, tplTypes map[int64]string, rt qemu.Runtime) Device {
	d := Device{
		ID:         fd.ID,
		LabID:      labID,
		Name:       fd.Name,
		X:          fd.X,
		Y:          fd.Y,
		Params:     Params{Arch: fd.Arch, Machine: fd.Machine, CPU: fd.CPU, RAMMB: fd.RAMMB, NICs: fd.NICs, NICModel: fd.NICModel, IfaceType: fd.IfaceType, IfaceStart: fd.IfaceStart, DiskBus: fd.DiskBus, BIOS: fd.BIOS, Console: fd.Console, ExtraArgs: fd.ExtraArgs},
		State:      "stopped",
		Icon:       fd.Icon,
		CustomIcon: fd.CustomIcon,
		IconSize:   fd.IconSize,
	}
	if fd.TemplateID != 0 {
		id := fd.TemplateID
		d.TemplateID = &id
	}
	d.Type = "generic"
	if fd.TemplateID != 0 {
		if t, ok := tplTypes[fd.TemplateID]; ok {
			d.Type = t
		}
	}
	if fd.ImageID != 0 {
		id := fd.ImageID
		d.ImageID = &id
	}
	if r != nil {
		d.RID = r.RID
		d.Machine, d.CPU, d.RAMMB, d.NICs, d.NICModel = r.Machine, r.CPU, r.RAMMB, r.NICs, r.NICModel
		d.IfaceType, d.IfaceStart, d.DiskBus, d.BIOS, d.Console, d.ExtraArgs = r.IfaceType, r.IfaceStart, r.DiskBus, r.BIOS, r.Console, r.ExtraArgs
		d.ImageID = nil
		if r.ImageID.Valid {
			id := r.ImageID.Int64
			d.ImageID = &id
		}
		d.Pending = pendingFields(&d, r.Applied)
	}
	if rt.State == "" {
		rt.State = "stopped"
	}
	d.State, d.PID, d.VNCPort, d.SerialPort, d.LastError = rt.State, rt.PID, rt.VNCPort, rt.SerialPort, rt.LastError
	d.IconURL = icons.URL(labID, d.Icon, d.CustomIcon)
	return d
}

// getDeviceInst - a topology machine with the settings and state of instance instID (0 - no instance).
// Mutable process state is taken from the QEMU manager, not from the database.
func getDeviceInst(db *database.DB, id, instID int64, m *qemu.Manager) (*Device, error) {
	l, fd, ok := findDevice(db, id)
	if !ok {
		return nil, errNotFound
	}
	var r *instRow
	rt := qemu.Runtime{State: "stopped"}
	if instID != 0 {
		rows, err := loadInstRows(db, instID)
		if err != nil {
			return nil, err
		}
		if row, ok := rows[id]; ok {
			r = &row
			if m != nil {
				rt = m.Runtime(r.RID)
			}
		}
	}
	tpl, err := loadTemplateTypes(db.Catalog)
	if err != nil {
		return nil, err
	}
	d := deviceView(l.ID, fd, r, tpl, rt)
	return &d, nil
}

// networkView assembles a network for the client. The single NAT cloud is shared by all labs (see natcloud).
func networkView(fn labfile.Network, labID int64) Network {
	n := Network{
		ID:         fn.ID,
		LabID:      labID,
		Name:       fn.Name,
		Type:       fn.Type,
		Subnet:     fn.Subnet,
		Gateway:    fn.Gateway,
		DHCP:       fn.DHCPEnabled,
		DHCPStart:  fn.DHCPStart,
		DHCPEnd:    fn.DHCPEnd,
		HostIface:  fn.HostIface,
		BridgeName: fn.BridgeName,
		X:          fn.X,
		Y:          fn.Y,
	}
	if n.Type == "nat" {
		n.Subnet, n.Gateway, n.DHCPStart, n.DHCPEnd = natcloud.Fields()
		n.DHCP = true
	}
	return n
}

func getNetwork(db *database.DB, id int64) (*Network, error) {
	l, fn, ok := findNetwork(db, id)
	if !ok {
		return nil, errNotFound
	}
	n := networkView(fn, l.ID)
	return &n, nil
}

// loadTopology assembles the lab topology for instance instID (0 - no instance).
func loadTopology(db *database.DB, labID, instID int64, m *qemu.Manager) (*Topology, error) {
	t := &Topology{Devices: []Device{}, Networks: []Network{}, Connections: []Conn{}}
	l, ok := labData(db, labID)
	if !ok {
		return nil, errNotFound
	}
	rows := map[int64]instRow{}
	up := map[int64]bool{}
	if instID != 0 {
		var err error
		if rows, err = loadInstRows(db, instID); err != nil {
			return nil, err
		}
		if up, err = loadLinkStates(db, instID); err != nil {
			return nil, err
		}
	}
	tpl, err := loadTemplateTypes(db.Catalog)
	if err != nil {
		return nil, err
	}
	for _, fd := range l.Devices {
		var r *instRow
		rt := qemu.Runtime{State: "stopped"}
		if row, ok := rows[fd.ID]; ok {
			r = &row
			if m != nil {
				rt = m.Runtime(r.RID)
			}
		}
		t.Devices = append(t.Devices, deviceView(labID, fd, r, tpl, rt))
	}
	for _, fn := range l.Networks {
		t.Networks = append(t.Networks, networkView(fn, labID))
	}
	for _, c := range l.Connections {
		t.Connections = append(t.Connections, connView(labID, c, up))
	}
	return t, nil
}

// loadLinkStates returns the state of the instance's cables: a cable is enabled by default.
func loadLinkStates(q querier, instID int64) (map[int64]bool, error) {
	rows, err := q.Query(`SELECT connection_id,up FROM instance_links WHERE instance_id=?`, instID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		var u int
		if err := rows.Scan(&id, &u); err != nil {
			return nil, err
		}
		out[id] = u != 0
	}
	return out, rows.Err()
}

func connView(labID int64, c labfile.Connection, up map[int64]bool) Conn {
	state, ok := up[c.ID]
	if !ok {
		state = true
	}
	return Conn{
		ID: c.ID, LabID: labID,
		AType: c.A.Type, AID: c.A.ID, AIf: c.A.If,
		BType: c.B.Type, BID: c.B.ID, BIf: c.B.If,
		Up: state,
	}
}

// deviceAddLabs selects the object written to the file from the creation parameters.
func newDevice(labID int64, tplID int64, imgID *int64, name string, x, y float64, p Params) labfile.Device {
	d := labfile.Device{
		TemplateID: tplID,
		Name:       name,
		X:          x,
		Y:          y,
		Arch:       p.Arch,
		Machine:    p.Machine,
		CPU:        p.CPU,
		RAMMB:      p.RAMMB,
		NICs:       p.NICs,
		NICModel:   p.NICModel,
		IfaceType:  p.IfaceType,
		IfaceStart: p.IfaceStart,
		DiskBus:    p.DiskBus,
		BIOS:       p.BIOS,
		Console:    p.Console,
		ExtraArgs:  p.ExtraArgs,
		CreatedAt:  time.Now().Unix(),
	}
	if imgID != nil {
		d.ImageID = *imgID
	}
	return d
}

func newNetwork(n *Network, x, y float64) labfile.Network {
	return labfile.Network{
		Name:        n.Name,
		Type:        n.Type,
		Subnet:      n.Subnet,
		Gateway:     n.Gateway,
		DHCPEnabled: n.DHCP,
		DHCPStart:   n.DHCPStart,
		DHCPEnd:     n.DHCPEnd,
		HostIface:   n.HostIface,
		X:           x,
		Y:           y,
	}
}

// bridgeName - network bridge name: it depends on the id, so it is assigned after the id is given.
func bridgeName(id int64) string { return fmt.Sprintf("nrn%d", id) }

// pruneInstances deletes instance rows referencing deleted objects (the former foreign key cascade).
func pruneInstances(q execer, devices, networks, connections []int64) error {
	del := func(table, column string, ids []int64) error {
		if len(ids) == 0 {
			return nil
		}
		_, err := q.Exec(`DELETE FROM `+table+` WHERE `+column+` IN (`+placeholders(len(ids))+`)`, idArgs(0, ids)[1:]...)
		return err
	}
	if err := del("instance_devices", "device_id", devices); err != nil {
		return err
	}
	if err := del("instance_networks", "network_id", networks); err != nil {
		return err
	}
	return del("instance_links", "connection_id", connections)
}

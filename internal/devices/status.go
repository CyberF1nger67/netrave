// SPDX-License-Identifier: GPL-3.0-only

package devices

import (
	"net/http"
	"runtime"
	"sort"
	"time"

	"netrave/internal/access"
	"netrave/internal/httpx"
	"netrave/internal/icons"
	"netrave/internal/labfile"
	"netrave/internal/qemu"
)

// machineStatus - metrics of one lab instance machine.
// For stopped machines CPU and RAM are always 0; disk is counted for stopped machines too (overlays are preserved).
type machineStatus struct {
	RID       int64   `json:"rid"`
	DeviceID  int64   `json:"device_id"`
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	IconURL   string  `json:"icon_url"` // the machine's custom icon; empty - standard by type
	Console   string  `json:"console"`  // machine console type: serial or vnc
	State     string  `json:"state"`
	Running   bool    `json:"running"`
	PID       int     `json:"pid"`
	CPUPct    float64 `json:"cpu_pct"`   // actual CPU time consumption, % of one host core
	CPUReady  bool    `json:"cpu_ready"` // false - load has not been measured yet
	RAMBytes  uint64  `json:"ram_bytes"` // resident memory of the QEMU process (VmRSS)
	DiskBytes uint64  `json:"disk_bytes"`
}

// statusTotals - totals across all machines of the lab instance.
type statusTotals struct {
	Machines  int     `json:"machines"`
	Running   int     `json:"running"`
	CPUPct    float64 `json:"cpu_pct"`
	RAMBytes  uint64  `json:"ram_bytes"`
	DiskBytes uint64  `json:"disk_bytes"`
}

// liveState - the QEMU process exists (or is being created/terminated) and its metrics are measured.
func liveState(state string) bool {
	switch state {
	case "running", "paused", "starting", "stopping":
		return true
	}
	return false
}

// status returns the status of machines of the current lab instance (GET /api/labs/{id}/status).
// The selection is limited to the instance (instance_devices.instance_id) and the lab (devices.lab_id):
// machines of other users and other labs do not appear in the response.
func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	labID, ok := h.lab(w, r, access.LabView)
	if !ok {
		return
	}
	iid, ok := h.instanceParam(w, r, labID)
	if !ok {
		return
	}
	rows, err := h.DB.Query(`SELECT r.id, r.device_id, COALESCE(r.console, '')
		FROM instance_devices r WHERE r.instance_id = ?`, iid)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	// Machine details (name, icons) come from the lab topology; type comes from the template.
	fl, _ := h.DB.Labs.Lab(labID)
	devs := map[int64]labfile.Device{}
	for _, d := range fl.Devices {
		devs[d.ID] = d
	}
	types := map[int64]string{}
	for _, tpl := range h.DB.Catalog.Templates() {
		types[tpl.ID] = tpl.Type
	}
	list := []machineStatus{}
	for rows.Next() {
		var m machineStatus
		var console string
		if rows.Scan(&m.RID, &m.DeviceID, &console) != nil {
			continue
		}
		if h.M != nil {
			rt := h.M.Runtime(m.RID)
			m.State, m.PID = rt.State, rt.PID
		} else {
			m.State = "stopped"
		}
		d, ok := devs[m.DeviceID]
		if !ok {
			continue
		}
		m.Name = d.Name
		m.Type = "generic"
		if d.TemplateID != 0 {
			if t, ok := types[d.TemplateID]; ok {
				m.Type = t
			}
		}
		m.Console = console
		if console == "" {
			m.Console = d.Console
		}
		m.IconURL = icons.URL(labID, d.Icon, d.CustomIcon)
		list = append(list, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	// The order is unchanged: by machine name, then by instance row.
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Name != list[j].Name {
			return list[i].Name < list[j].Name
		}
		return list[i].RID < list[j].RID
	})

	// Process metrics come from the QEMU manager: CPU by CPU time delta, RAM by VmRSS.
	var liveIDs []int64
	for _, m := range list {
		if liveState(m.State) && m.PID > 0 {
			liveIDs = append(liveIDs, m.RID)
		}
	}
	var stats map[int64]qemu.Stat
	if h.M != nil {
		stats = h.M.Stats(liveIDs)
	}

	tot := statusTotals{Machines: len(list)}
	for i := range list {
		m := &list[i]
		m.Running = liveState(m.State)
		if m.Running {
			if st, ok := stats[m.RID]; ok {
				m.CPUPct, m.CPUReady, m.RAMBytes = st.CPU, st.Ready, st.RSS
			} else if m.PID > 0 {
				// The process exists but the manager has not adopted it yet: memory only, load not measured yet.
				m.RAMBytes = qemu.ProcessRSS(m.PID)
			}
		} else {
			m.PID = 0
		}
		if h.M != nil {
			m.DiskBytes = h.M.Disk(labID, m.RID)
		}

		if m.Running {
			tot.Running++
			tot.CPUPct += m.CPUPct
			tot.RAMBytes += m.RAMBytes
		}
		tot.DiskBytes += m.DiskBytes
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"lab_id":         labID,
		"instance":       iid,
		"host_cpus":      runtime.NumCPU(),
		"host_ram_bytes": qemu.MemTotal(),
		"time":           time.Now().Unix(),
		"machines":       list,
		"totals":         tot,
	})
}

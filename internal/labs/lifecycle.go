// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"errors"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"netrave/internal/access"
	"netrave/internal/auth"
	"netrave/internal/httpx"
	"netrave/internal/journal"
	"netrave/internal/msgs"
	"netrave/internal/notify"
)

// ---------- running labs ----------

// RunningLab - a "Running labs" row: one lab instance (machines of a specific user),
// which was started and has not been wiped yet. One lab can produce several rows -
// one per user.
type RunningLab struct {
	ID             string  `json:"id"`          // lab name
	Name           string  `json:"name"`        // lab name
	OwnerID        int64   `json:"owner_id"`    // lab owner
	Owner          string  `json:"owner"`       // lab owner name
	OwnerRole      string  `json:"owner_role"`  // owner role (user | admin)
	InstanceID     int64   `json:"instance_id"` // instance the row is built for
	InstanceUserID int64   `json:"instance_user_id"`
	InstanceUser   string  `json:"instance_user"` // user who started the machines
	StartedAt      int64   `json:"started_at"`    // time the instance machine was first started, even if it is already stopped (0 - never started)
	Devices        int     `json:"devices"`       // machines in the instance
	Running        int     `json:"running"`       // running (running or paused)
	Busy           int     `json:"busy"`          // transitioning (starting or stopping)
	Errors         int     `json:"errors"`        // in the error state
	Stale          int     `json:"stale"`         // marked running in the database, but the QEMU process is missing
	WipedAt        int64   `json:"wiped_at"`      // time of the last Wipe of this instance (0 - never wiped)
	WipedAgo       int64   `json:"wiped_ago"`     // seconds since the last Wipe (0 - never wiped)
	Status         string  `json:"status"`        // running | partial | busy | error | stale | stopped | wiped
	CanOpen        bool    `json:"can_open"`      // open the instance
	CanWipe        bool    `json:"can_wipe"`      // wipe the instance
	CanStart       bool    `json:"can_start"`     // start the instance machines
	CanStop        bool    `json:"can_stop"`      // stop the instance machines
	Own            bool    `json:"own"`           // permission to manage the instance (an administrator - any); for permissions only
	Mine           bool    `json:"mine"`          // the user's own instance (independent of the role)
	HostCPUs       int     `json:"host_cpus"`     // host cores (for the load share)
	CPUPct         float64 `json:"cpu_pct"`       // load of the instance machines, sum in % of one core
	CPUReady       bool    `json:"cpu_ready"`     // load measured across all running machines
	RAMBytes       uint64  `json:"ram_bytes"`     // resident memory of the instance's QEMU processes
	PublicLevel    string  `json:"-"`
}

// MachineStat - metrics of one machine's QEMU process (an instance_devices row). Set by the Handler.Stats hook.
type MachineStat struct {
	CPU   float64 // load, % of one core
	RSS   uint64  // resident memory, bytes
	Ready bool    // load measured (two samples are needed)
}

// wipeRetention is how long after a Wipe the instance is considered "wiped" (if it appears
// in the list again). Wipe is the only action that removes a lab from "Running".
const wipeRetention = 10 * 60

// openedEver - the lab was opened by the user: the instance has a machine with a start
// time. This state is stored in the database (instance_devices.started_at) and does not depend on whether
// QEMU processes are running now: stopping machines, closing consoles and restarting the server
// do not remove the lab from "Running".
const openedEver = `EXISTS (SELECT 1 FROM instance_devices r1 WHERE r1.instance_id=i.id AND r1.started_at>0)`

// pidAlive checks whether the process exists (kill with signal 0 does not send a signal).
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// runningLabs returns the instances that were started and not wiped yet (even if
// all their machines are stopped now).
// The administrator sees instances of all labs and all users. A regular user sees
// only instances of their own labs (an owner sees all machines of their lab, but manages only their own instance).
func (h *Handler) runningLabs(ss *auth.Session, q, owner string) ([]RunningLab, error) {
	admin, uid := ss.User.Role == "admin", ss.User.ID
	// Lab data (name, owner, access) is in lab files, so SQL selects only
	// instances, while filters and permissions are applied here. Each instance has its own wipe time (journal).
	// Mutable machine state is not stored in the database: counters are built from QEMU manager data.
	query := `SELECT i.id, i.user_id, iu.username, i.lab_id,
		(SELECT COUNT(*) FROM instance_devices r WHERE r.instance_id=i.id),
		COALESCE((SELECT MIN(r.started_at) FROM instance_devices r WHERE r.instance_id=i.id AND r.started_at>0),0),
		` + openedEver + `
		FROM lab_instances i
		JOIN users iu ON iu.id=i.user_id`
	rows, err := h.DB.Query(query)
	if err != nil {
		return nil, err
	}
	type instRowData struct {
		rl         RunningLab
		labID      string
		instUserID int64
		opened     bool
		anyActive  bool
	}
	var found []instRowData
	for rows.Next() {
		var d instRowData
		if err := rows.Scan(&d.rl.InstanceID, &d.instUserID, &d.rl.InstanceUser, &d.labID,
			&d.rl.Devices, &d.rl.StartedAt, &d.opened); err != nil {
			rows.Close()
			return nil, err
		}
		d.rl.InstanceUserID = d.instUserID
		found = append(found, d)
	}
	rows.Close()

	// Machines of each instance and their state are in the manager's memory.
	devRows, err := h.DB.Query(`SELECT id, instance_id FROM instance_devices`)
	if err != nil {
		return nil, err
	}
	byInst := map[int64][]int64{}
	for devRows.Next() {
		var rid, inst int64
		if devRows.Scan(&rid, &inst) == nil {
			byInst[inst] = append(byInst[inst], rid)
		}
	}
	devRows.Close()
	activeByInst := map[int64][]int64{}
	for i := range found {
		d := &found[i]
		for _, rid := range byInst[d.rl.InstanceID] {
			st := "stopped"
			pid := 0
			if h.M != nil {
				rt := h.M.Runtime(rid)
				st, pid = rt.State, rt.PID
			}
			switch st {
			case "running", "paused":
				d.rl.Running++
				activeByInst[d.rl.InstanceID] = append(activeByInst[d.rl.InstanceID], rid)
				if pid <= 0 || !pidAlive(pid) {
					d.rl.Stale++
				}
			case "starting", "stopping":
				d.rl.Busy++
			case "error":
				d.rl.Errors++
			}
			if st != "stopped" {
				d.anyActive = true
			}
		}
	}

	q = strings.ToLower(strings.TrimSpace(q))
	owner = strings.ToLower(strings.TrimSpace(owner))
	owners := map[int64][2]string{} // owner -> name and role
	type ranked struct {
		rl  RunningLab
		row instRowData
	}
	var kept []ranked
	for _, d := range found {
		fl, ok := labData(h.DB, d.labID)
		if !ok {
			continue
		}
		info, seen := owners[fl.OwnerID]
		if !seen {
			_ = h.DB.QueryRow(`SELECT username, role FROM users WHERE id=?`, fl.OwnerID).Scan(&info[0], &info[1])
			owners[fl.OwnerID] = info
		}
		// All rows go through the same checks: lab visibility and the user's own machines.
		if !admin {
			visible, err := access.LabVisible(h.DB, fl, uid, false)
			if err != nil {
				return nil, err
			}
			// The user sees instances of visible labs and their own running machines in other labs.
			if !visible && d.instUserID != uid {
				continue
			}
		}
		if q != "" && !strings.Contains(strings.ToLower(fl.Name), q) {
			continue
		}
		if owner != "" && !strings.Contains(strings.ToLower(info[0]), owner) {
			continue
		}
		rl := d.rl
		rl.ID, rl.Name = fl.Name, fl.Name
		rl.OwnerID, rl.Owner, rl.OwnerRole = fl.OwnerID, info[0], info[1]
		rl.PublicLevel = fl.PublicLevel
		kept = append(kept, ranked{rl: rl, row: d})
	}
	sort.SliceStable(kept, func(i, j int) bool {
		a, b := kept[i].rl, kept[j].rl
		if na, nb := strings.ToLower(a.Name), strings.ToLower(b.Name); na != nb {
			return na < nb
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		if ua, ub := strings.ToLower(a.InstanceUser), strings.ToLower(b.InstanceUser); ua != ub {
			return ua < ub
		}
		return a.InstanceID < b.InstanceID
	})
	out := []RunningLab{}
	idx := map[int64]int{} // instance -> position in out
	now := time.Now().Unix()
	for _, k := range kept {
		rl := k.rl
		// Wipe is tracked per instance: wiping one user does not change other users' rows.
		rl.WipedAt = h.DB.Journal.LastWipeInstance(rl.InstanceID)
		if rl.WipedAt > 0 {
			rl.WipedAgo = now - rl.WipedAt
		}
		// A lab is shown if something is running in the instance, or the instance machines were started
		// at some point. The start history (instance_devices.started_at) is stored in the database and is not erased
		// by stopping machines; only a Wipe of this instance resets it.
		if !k.row.anyActive && !k.row.opened {
			continue
		}
		idx[rl.InstanceID] = len(out)
		out = append(out, rl)
	}

	// Process metrics come from the QEMU manager; the machine list is already collected from memory.
	var rids []int64
	ridInst := map[int64]int64{} // machine row -> instance
	for inst := range idx {
		for _, rid := range activeByInst[inst] {
			rids = append(rids, rid)
			ridInst[rid] = inst
		}
	}

	// CPU and RAM: sums over the instance machines; load is ready when all running machines are measured.
	var stats map[int64]MachineStat
	if h.Stats != nil && len(rids) > 0 {
		stats = h.Stats(rids)
	}
	counted := make([]int, len(out))
	measured := make([]int, len(out))
	for _, rid := range rids {
		i := idx[ridInst[rid]]
		counted[i]++
		if st, ok := stats[rid]; ok {
			out[i].CPUPct += st.CPU
			out[i].RAMBytes += st.RSS
			if st.Ready {
				measured[i]++
			}
		}
	}

	for i := range out {
		rl := &out[i]
		perms, err := access.Perms(h.DB, rl.Name, uid, admin)
		if err != nil {
			return nil, err
		}
		own := admin || rl.InstanceUserID == uid // only your own instance can be managed; an administrator - any
		rl.Own = own
		rl.Mine = rl.InstanceUserID == uid
		rl.HostCPUs = runtime.NumCPU()
		rl.CPUReady = counted[i] > 0 && measured[i] == counted[i]
		rl.CanOpen = own && perms[access.LabView]
		rl.CanWipe = own && perms[access.LabWipe]
		// Starting and stopping is allowed only with the permission to start machines, and starting -
		// also when the machines are off: this way "Running" can start a wiped lab.
		rl.CanStart = own && perms["machine.start"]
		rl.CanStop = own && perms["machine.stop"] && rl.Running > 0
		// A recent Wipe outranks older states: a wiped instance is neither "error" nor "running".
		// Starting after a Wipe makes it a regular stopped/running one again.
		recentlyWiped := rl.WipedAgo > 0 && rl.WipedAgo < wipeRetention && rl.Running == 0 && rl.Busy == 0 &&
			rl.StartedAt < rl.WipedAt
		switch {
		case recentlyWiped:
			rl.Status = "wiped"
		case rl.Errors > 0:
			rl.Status = "error"
		case rl.Busy > 0:
			rl.Status = "busy"
		case rl.Stale > 0:
			rl.Status = "stale"
		case rl.Running > 0 && rl.Running == rl.Devices:
			rl.Status = "running"
		case rl.Running > 0:
			rl.Status = "partial"
		default:
			rl.Status = "stopped"
		}
	}
	return out, nil
}

func (h *Handler) runningList(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	rows, err := h.runningLabs(ss, r.URL.Query().Get("q"), r.URL.Query().Get("owner"))
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	httpx.JSON(w, http.StatusOK, rows)
}

// ---------- lab wipe ----------

// wipeLab stops all lab machines, terminates QEMU processes, releases TAP interfaces,
// deletes working overlay disks and returns devices to their initial state.
// Source images, uploaded disks, topology, wires and device placement do not change.
func (h *Handler) wipeLab(w http.ResponseWriter, r *http.Request) {
	lab, ok := h.labFor(w, r, access.LabWipe)
	if !ok {
		return
	}
	// Wipe affects only the selected instance (own by default). Other users' machines are not touched.
	iid, ok := h.instanceFor(w, r, lab)
	if !ok {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	rows, err := h.DB.Query(`SELECT id FROM instance_devices WHERE instance_id=? ORDER BY id`, iid)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	failed := []msgs.Message{}
	done := 0
	for _, id := range ids {
		if h.WipeDevice == nil {
			break
		}
		// Wipe stops the machine synchronously itself, so stopping and wiping the disk
		// are performed under the same device lock (no race with starting).
		if err := h.WipeDevice(id); err != nil {
			failed = append(failed, msgs.M("Machine {id}: {detail}", map[string]string{"id": strconv.FormatInt(id, 10), "detail": err.Error()}))
			continue
		}
		done++
	}
	h.changed()
	ss := auth.FromContext(r.Context())
	// The wipe is written to the journal (lab_id and instance fields): the Wipe history is shared by the lab,
	// while the "Running" list uses the wipe time of a specific instance.
	h.DB.AuditKey(ss.User.ID, ss.User.Username, journal.ActionLabWipe, "{lab}: instance {instance}, reset {devices}, errors {failed}",
		map[string]string{"lab": lab.Name, "instance": strconv.FormatInt(iid, 10), "devices": strconv.Itoa(done), "failed": strconv.Itoa(len(failed))},
		map[string]any{"lab_id": lab.Name, "instance": iid, "devices": done, "failed": len(failed)}, auth.ClientIP(r))
	// Wipe is the only action that removes an instance from "Running": the start history of
	// its machines only is reset. Other instances of the lab keep their history and remain in the list.
	_, _ = h.DB.Exec(`UPDATE instance_devices SET started_at=0 WHERE instance_id=?`, iid)
	touch(h.DB, lab.Name)
	wipeLevel, wipeTitle := notify.Success, "Laboratory reset"
	if len(failed) > 0 {
		wipeLevel, wipeTitle = notify.Warning, "Reset completed with errors"
	}
	h.Notify.SendInstance(iid, wipeLevel, "lab", wipeTitle,
		"«{name}»: machines reset — {done}, errors — {failed}.",
		labLink(lab.Name),
		notify.P("name", lab.Name, "done", strconv.Itoa(done), "failed", strconv.Itoa(len(failed))))
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": len(failed) == 0, "devices": done, "failed": failed, "instance": iid})
}

// wipeHistory - the recent Wipe operations of the lab.
func (h *Handler) wipeHistory(w http.ResponseWriter, r *http.Request) {
	lab, ok := h.labFor(w, r, access.LabView)
	if !ok {
		return
	}
	entries, err := h.DB.Journal.LabWipes(lab.Name, 20)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Journal error")
		return
	}
	type wipe struct {
		TS       int64  `json:"ts"`
		Username string `json:"username"`
		Devices  int    `json:"devices"`
		Failed   int    `json:"failed"`
	}
	out := []wipe{}
	for _, e := range entries {
		devices, _ := e.Data["devices"].(float64)
		failed, _ := e.Data["failed"].(float64)
		out = append(out, wipe{TS: e.TS, Username: e.Username, Devices: int(devices), Failed: int(failed)})
	}
	httpx.JSON(w, http.StatusOK, out)
}

// noteAdmin writes an administrator's action in another lab to the journal (not GET).
func (h *Handler) noteAdmin(r *http.Request, lab *Lab, key string, params map[string]string) {
	ss := auth.FromContext(r.Context())
	if ss == nil || ss.User.Role != "admin" || lab.OwnerID == ss.User.ID {
		return
	}
	h.auditKey(r, "admin_lab_action", "{what_key}: laboratory «{lab_name}» of user «{owner}» (id {lab_id})",
		msgs.Merge(params, map[string]string{"what_key": key, "lab_name": lab.Name, "owner": lab.Owner, "lab_id": lab.Name}))
}

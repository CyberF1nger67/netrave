// SPDX-License-Identifier: GPL-3.0-only

package devices

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"netrave/internal/access"
	"netrave/internal/auth"
	"netrave/internal/database"
	"netrave/internal/httpx"
	"netrave/internal/instances"
	"netrave/internal/labfile"
	"netrave/internal/notify"
	"netrave/internal/qemu"
)

// Handler - virtual machine management over REST.
//
// Addressing: {id} in machine routes is a rid, the machine row in a lab instance (instance_devices.id).
// This way each user manages their own QEMU processes; the administrator manages machines of any instance.
// Every operation checks the instance owner and the access level to the lab.
type Handler struct {
	DB     *database.DB
	Auth   *auth.Service
	M      *qemu.Manager
	Notify *notify.Service // notification centre (nil - not stored)
}

func (h *Handler) Register(mux *http.ServeMux) {
	user := func(f http.HandlerFunc) http.Handler { return h.Auth.Require(f) }
	for _, act := range []string{"start", "stop", "restart", "suspend", "resume", "wipe"} {
		a := act
		mux.Handle("POST /api/devices/{id}/"+a, user(func(w http.ResponseWriter, r *http.Request) { h.action(w, r, a) }))
	}
	mux.Handle("POST /api/devices/{id}/clone", user(h.clone))
	mux.Handle("POST /api/devices/{id}/commit", user(h.commit))
	mux.Handle("GET /api/devices/{id}/log", user(h.log))
	mux.Handle("GET /api/devices/{id}/snapshots", user(h.listSnapshots))
	mux.Handle("POST /api/devices/{id}/snapshots", user(h.createSnapshot))
	mux.Handle("POST /api/snapshots/{id}/restore", user(h.restoreSnapshot))
	mux.Handle("DELETE /api/snapshots/{id}", user(h.deleteSnapshot))
	mux.Handle("GET /api/labs/{id}/stats", user(h.stats))
	mux.Handle("GET /api/labs/{id}/status", user(h.status))
	mux.Handle("POST /api/labs/{id}/start-all", user(h.startAll))
	mux.Handle("POST /api/labs/{id}/stop-all", user(h.stopAll))
}

func (h *Handler) audit(r *http.Request, action, detail string) {
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.DB.Audit(ss.User.ID, ss.User.Username, action, detail, auth.ClientIP(r))
	}
}

func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, qemu.ErrBusy):
		httpx.Err(w, http.StatusConflict, err.Error())
	case errors.Is(err, qemu.ErrNotFound):
		httpx.Err(w, http.StatusNotFound, err.Error())
	default:
		httpx.Err(w, http.StatusBadRequest, err.Error())
	}
}

// allow checks the user's need permission on the lab.
// Without view access the lab is hidden (404 with notFound text), without the required permission - 403.
func (h *Handler) allow(w http.ResponseWriter, ss *auth.Session, labID int64, need, notFound string) bool {
	perms, err := access.Perms(h.DB, labID, ss.User.ID, ss.User.Role == "admin")
	if err == sql.ErrNoRows || (err == nil && !perms[access.LabView]) {
		httpx.Err(w, http.StatusNotFound, notFound)
		return false
	}
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return false
	}
	if !perms[need] {
		httpx.Err(w, http.StatusForbidden, "нет права: "+access.Title(need))
		return false
	}
	return true
}

// device authorizes an instance machine rid: instance owner or administrator, then lab permission.
func (h *Handler) device(w http.ResponseWriter, r *http.Request, need string) (rid, labID int64, name string, ok bool) {
	ss := auth.FromContext(r.Context())
	rid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "некорректный id")
		return
	}
	inf, err := instances.Device(h.DB, rid)
	if err != nil || !instances.CanUse(inf.OwnerID, ss.User.ID, ss.User.Role == "admin") {
		httpx.Err(w, http.StatusNotFound, "устройство не найдено")
		return
	}
	if !h.allow(w, ss, inf.LabID, need, "устройство не найдено") {
		return
	}
	return rid, inf.LabID, inf.Name, true
}

// lab authorizes the lab for operations on all instance machines.
func (h *Handler) lab(w http.ResponseWriter, r *http.Request, need string) (int64, bool) {
	ss := auth.FromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusNotFound, "лаборатория не найдена")
		return 0, false
	}
	if !h.allow(w, ss, id, need, "лаборатория не найдена") {
		return 0, false
	}
	return id, true
}

// instanceParam returns the instance for lab operations: ?instance=N or own.
func (h *Handler) instanceParam(w http.ResponseWriter, r *http.Request, labID int64) (int64, bool) {
	ss := auth.FromContext(r.Context())
	if q := r.URL.Query().Get("instance"); q != "" {
		iid, err := strconv.ParseInt(q, 10, 64)
		if err != nil {
			httpx.Err(w, http.StatusBadRequest, "некорректный экземпляр")
			return 0, false
		}
		lid, owner, err := instances.Owner(h.DB, iid)
		if err != nil || lid != labID || !instances.CanUse(owner, ss.User.ID, ss.User.Role == "admin") {
			httpx.Err(w, http.StatusNotFound, "экземпляр не найден")
			return 0, false
		}
		return iid, true
	}
	iid, err := instances.Ensure(h.DB, labID, ss.User.ID)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return 0, false
	}
	return iid, true
}

// actNeed - the permission required for an operation on a machine.
func actNeed(act string) string {
	switch act {
	case "wipe":
		return access.MachWipe
	case "restart":
		return access.MachRestart
	case "stop", "suspend", "resume":
		return access.MachStop
	}
	return access.MachStart
}

func (h *Handler) action(w http.ResponseWriter, r *http.Request, act string) {
	rid, labID, name, ok := h.device(w, r, actNeed(act))
	if !ok {
		return
	}
	if act == "start" || act == "restart" || act == "resume" {
		if _, ok := h.allowStart(w, r, labID); !ok {
			return
		}
	}
	var err error
	switch act {
	case "start":
		err = h.M.Start(rid)
	case "stop":
		err = h.M.Stop(rid, r.URL.Query().Get("force") == "1")
	case "restart":
		err = h.M.Restart(rid)
	case "suspend":
		err = h.M.Suspend(rid)
	case "resume":
		err = h.M.Resume(rid)
	case "wipe":
		err = h.M.Wipe(rid)
	}
	if err != nil {
		fail(w, err)
		return
	}
	h.audit(r, "device_"+act, name)
	lvl, title := actionEvent(act, r.URL.Query().Get("force") == "1")
	h.notifyMachine(r, rid, lvl, title, name)
	h.noteAdmin(r, labID, "машина «"+name+"»: "+act)
	httpx.JSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

// commit performs the "Snapshots" menu mode: Snapshot, New Image or Original Image (see qemu.Commit).
func (h *Handler) commit(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	var in struct {
		Mode      string `json:"mode"`
		Name      string `json:"name"`
		StopFirst bool   `json:"stop_first"`
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	switch in.Mode {
	case qemu.CommitSnapshot, qemu.CommitImage, qemu.CommitOriginal:
	default:
		httpx.Err(w, http.StatusBadRequest, "неизвестный режим фиксации")
		return
	}
	rid, _, devName, ok := h.device(w, r, access.MachSnapshot)
	if !ok {
		return
	}
	if !hasImagesPerm(ss) {
		httpx.Err(w, http.StatusForbidden, "для работы с образами нужно право «Образы»")
		return
	}
	if in.Mode == qemu.CommitOriginal && ss.User.Role != "admin" {
		httpx.Err(w, http.StatusForbidden, "перезаписать исходный образ может только администратор")
		return
	}
	title := strings.TrimSpace(in.Name)
	imgID, err := h.M.Commit(rid, in.Mode, title, in.StopFirst)
	if err != nil {
		fail(w, err)
		return
	}
	h.audit(r, "device_commit", devName+": "+in.Mode+" "+title)
	h.notifyMachine(r, rid, notify.Success, "Образ сохранён", devName+": "+in.Mode)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "image_id": imgID})
}

// hasImagesPerm - whether the user has the "Images" permission (an administrator has all permissions).
func hasImagesPerm(ss *auth.Session) bool {
	return ss.User.Can("images")
}

// clone creates a copy of a topology machine. In the instance, the copy gets the cloned machine's settings and disks;
// in other instances a row with default settings appears.
func (h *Handler) clone(w http.ResponseWriter, r *http.Request) {
	rid, labID, name, ok := h.device(w, r, access.MachClone)
	if !ok {
		return
	}
	if !h.M.IsStopped(rid) {
		httpx.Err(w, http.StatusConflict, "клонировать можно только остановленное устройство")
		return
	}
	var devID, instID int64
	if h.DB.QueryRow(`SELECT device_id, instance_id FROM instance_devices WHERE id=?`, rid).Scan(&devID, &instID) != nil {
		httpx.Err(w, http.StatusNotFound, "устройство не найдено")
		return
	}
	names := map[string]bool{}
	fl, ok := h.DB.Labs.Lab(labID)
	if !ok {
		httpx.Err(w, http.StatusNotFound, "лаборатория не найдена")
		return
	}
	for _, d := range fl.Devices {
		names[d.Name] = true
	}
	for _, n := range fl.Networks {
		names[n.Name] = true
	}
	base := strings.TrimSpace(name)
	if len(base) > 24 {
		base = base[:24]
	}
	newName := base + "-copy"
	for i := 2; names[newName]; i++ {
		newName = base + "-copy" + strconv.Itoa(i)
	}
	// The copy gets the cloned instance's disk (not the default disk, which may not have been chosen):
	// this way all participants get the same machine with a disk, and the copy starts.
	// The copy's disk is the cloned instance's disk (not the default disk, which may not have been chosen).
	var srcImage sql.NullInt64
	_ = h.DB.QueryRow(`SELECT image_id FROM instance_devices WHERE id=?`, rid).Scan(&srcImage)
	src, _, ok := h.DB.Labs.Device(devID)
	if !ok {
		httpx.Err(w, http.StatusNotFound, "устройство не найдено")
		return
	}
	cp := src
	cp.ID = h.DB.Labs.NewID()
	cp.Name = newName
	cp.X, cp.Y = src.X+40, src.Y+40
	cp.ImageID = 0
	if srcImage.Valid {
		cp.ImageID = srcImage.Int64
	}
	if _, err := h.DB.Labs.Update(labID, func(l *labfile.Lab) error {
		l.Devices = append(l.Devices, cp)
		l.UpdatedAt = time.Now().Unix()
		return nil
	}); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	newDev := cp.ID
	if err := instances.SyncLab(h.DB, labID); err != nil {
		removeDevice(h.DB, labID, newDev)
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	var newRID int64
	if h.DB.QueryRow(`SELECT id FROM instance_devices WHERE instance_id=? AND device_id=?`, instID, newDev).Scan(&newRID) != nil {
		removeDevice(h.DB, labID, newDev)
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	// copy settings come from the cloned machine of this instance
	_, _ = h.DB.Exec(`UPDATE instance_devices SET image_id=s.image_id, image_own=1, machine=s.machine, cpu=s.cpu, ram_mb=s.ram_mb, nics=s.nics,
		nic_model=s.nic_model, iface_type=s.iface_type, iface_start=s.iface_start, disk_bus=s.disk_bus, bios=s.bios, console=s.console, extra_args=s.extra_args
		FROM instance_devices s WHERE instance_devices.id=? AND s.id=?`, newRID, rid)
	if err := h.M.CloneFiles(labID, rid, newRID); err != nil {
		removeDevice(h.DB, labID, newDev)
		h.M.RemoveFiles(labID, newRID)
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.audit(r, "device_clone", name)
	h.notifyMachine(r, rid, notify.Success, "Машина клонирована", name)
	var out struct {
		ID   int64   `json:"id"`
		RID  int64   `json:"rid"`
		Name string  `json:"name"`
		X    float64 `json:"x"`
		Y    float64 `json:"y"`
	}
	out.ID, out.RID, out.Name = newDev, newRID, newName
	out.X, out.Y = cp.X, cp.Y
	httpx.JSON(w, http.StatusCreated, out)
}

func (h *Handler) log(w http.ResponseWriter, r *http.Request) {
	rid, _, _, ok := h.device(w, r, access.LabView)
	if !ok {
		return
	}
	s, err := h.M.LogTail(rid, 64<<10)
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(s))
}

// ---------- snapshots ----------

func (h *Handler) listSnapshots(w http.ResponseWriter, r *http.Request) {
	rid, _, _, ok := h.device(w, r, access.LabView)
	if !ok {
		return
	}
	list, err := h.M.ListSnapshots(rid)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

func (h *Handler) createSnapshot(w http.ResponseWriter, r *http.Request) {
	rid, _, name, ok := h.device(w, r, access.MachSnapshot)
	if !ok {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	if err := h.M.CreateSnapshot(rid, strings.TrimSpace(in.Name)); err != nil {
		fail(w, err)
		return
	}
	h.audit(r, "snapshot_create", name+": "+in.Name)
	h.notifyMachine(r, rid, notify.Success, "Снимок создан", name+": "+in.Name)
	httpx.JSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

// snapshotAccess checks access to a snapshot via its instance machine.
func (h *Handler) snapshotAccess(w http.ResponseWriter, r *http.Request, need string) (int64, bool) {
	ss := auth.FromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "некорректный id")
		return 0, false
	}
	sn, ok := h.DB.Catalog.Snapshot(id)
	if !ok {
		httpx.Err(w, http.StatusNotFound, "снимок не найден")
		return 0, false
	}
	devID := sn.DeviceID
	var owner int64
	if h.DB.QueryRow(`SELECT li.user_id FROM instance_devices r
		JOIN lab_instances li ON li.id=r.instance_id WHERE r.id=?`, devID).Scan(&owner) != nil {
		httpx.Err(w, http.StatusNotFound, "снимок не найден")
		return 0, false
	}
	_, labID, ok := h.DB.Labs.Device(devID)
	if !ok || !instances.CanUse(owner, ss.User.ID, ss.User.Role == "admin") {
		httpx.Err(w, http.StatusNotFound, "снимок не найден")
		return 0, false
	}
	if !h.allow(w, ss, labID, need, "снимок не найден") {
		return 0, false
	}
	return id, true
}

func (h *Handler) restoreSnapshot(w http.ResponseWriter, r *http.Request) {
	id, ok := h.snapshotAccess(w, r, access.MachSnapshot)
	if !ok {
		return
	}
	if err := h.M.RestoreSnapshot(id); err != nil {
		fail(w, err)
		return
	}
	h.audit(r, "snapshot_restore", strconv.FormatInt(id, 10))
	if sn, ok := h.DB.Catalog.Snapshot(id); ok {
		h.notifyMachine(r, sn.DeviceID, notify.Success, "Снимок восстановлен", sn.Name)
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) deleteSnapshot(w http.ResponseWriter, r *http.Request) {
	id, ok := h.snapshotAccess(w, r, access.MachSnapshot)
	if !ok {
		return
	}
	if err := h.M.DeleteSnapshot(id); err != nil {
		fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------- whole lab (single instance) ----------

// deviceIDs - rids of instance machines matching the where condition.
func (h *Handler) deviceIDs(instID int64, where string) []int64 {
	var ids []int64
	rows, err := h.DB.Query(`SELECT id FROM instance_devices WHERE instance_id=? `+where+` ORDER BY id`, instID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// deviceIDsInStates returns rids of instance machines in one of the given states.
// The state comes from the QEMU manager's memory, not from the database.
func (h *Handler) deviceIDsInStates(instID int64, states ...string) []int64 {
	want := map[string]bool{}
	for _, s := range states {
		want[s] = true
	}
	var out []int64
	for _, id := range h.deviceIDs(instID, "") {
		st := "stopped"
		if h.M != nil {
			st = h.M.State(id)
		}
		if want[st] {
			out = append(out, id)
		}
	}
	return out
}

func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	labID, ok := h.lab(w, r, access.LabView)
	if !ok {
		return
	}
	iid, ok := h.instanceParam(w, r, labID)
	if !ok {
		return
	}
	// Stats itself returns only live processes; the pid filter is no longer needed.
	httpx.JSON(w, http.StatusOK, h.M.Stats(h.deviceIDs(iid, "")))
}

// startAll starts the instance machines one by one (the memory check sees the real picture).
func (h *Handler) startAll(w http.ResponseWriter, r *http.Request) {
	labID, ok := h.lab(w, r, access.MachStart)
	if !ok {
		return
	}
	iid, ok := h.instanceParam(w, r, labID)
	if !ok {
		return
	}
	if _, ok := h.allowStart(w, r, labID); !ok {
		return
	}
	ids := h.deviceIDsInStates(iid, "stopped", "error")
	if len(ids) == 0 {
		httpx.Err(w, http.StatusBadRequest, "нет машин для запуска в этом экземпляре")
		return
	}
	h.audit(r, "lab_start_all", strconv.FormatInt(labID, 10)+" экземпляр "+strconv.FormatInt(iid, 10))
	go func() {
		for _, id := range ids {
			if err := h.M.Start(id); err == nil {
				h.M.WaitLeftStarting(id, 30*time.Second)
			}
		}
	}()
	httpx.JSON(w, http.StatusAccepted, map[string]int{"devices": len(ids), "instance": int(iid)})
}

func (h *Handler) stopAll(w http.ResponseWriter, r *http.Request) {
	labID, ok := h.lab(w, r, access.MachStop)
	if !ok {
		return
	}
	iid, ok := h.instanceParam(w, r, labID)
	if !ok {
		return
	}
	ids := h.deviceIDsInStates(iid, "running", "paused")
	h.audit(r, "lab_stop_all", strconv.FormatInt(labID, 10)+" экземпляр "+strconv.FormatInt(iid, 10))
	for _, id := range ids {
		_ = h.M.Stop(id, false)
	}
	httpx.JSON(w, http.StatusAccepted, map[string]int{"devices": len(ids), "instance": int(iid)})
}

// allowStart checks the role limit on the number of simultaneously running labs.
// A start of another lab from the same instance that has already begun is allowed: the number
// of labs with running machines is counted, not the attempt itself. On refusal it writes 409 and returns false.
func (h *Handler) allowStart(w http.ResponseWriter, r *http.Request, labID int64) (int64, bool) {
	ss := auth.FromContext(r.Context())
	if ss.User.Role == "admin" {
		return labID, true
	}
	limit, err := access.RunningLabLimit(h.DB, ss.User.ID)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return labID, false
	}
	if limit <= 0 {
		return labID, true
	}
	rows, err := h.DB.Query(`SELECT i.lab_id, d.id FROM instance_devices d
		JOIN lab_instances i ON i.id=d.instance_id
		WHERE i.user_id=? AND i.lab_id<>?`, ss.User.ID, labID)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return labID, false
	}
	activeLabs := map[int64]bool{}
	for rows.Next() {
		var id, rid int64
		if rows.Scan(&id, &rid) != nil {
			continue
		}
		if h.M == nil {
			continue
		}
		switch h.M.State(rid) {
		case "running", "paused", "starting":
			activeLabs[id] = true
		}
	}
	rows.Close()
	running := len(activeLabs)
	if running >= limit {
		httpx.Err(w, http.StatusConflict, "достигнут предел роли: одновременно можно запускать не более "+
			strconv.Itoa(limit)+" лабораторий. Остановите другую лабораторию, чтобы запустить эту")
		return labID, false
	}
	return labID, true
}

// noteAdmin writes an administrator action on a machine in another lab to the journal.
func (h *Handler) noteAdmin(r *http.Request, labID int64, what string) {
	ss := auth.FromContext(r.Context())
	if ss == nil || ss.User.Role != "admin" {
		return
	}
	fl, ok := h.DB.Labs.Lab(labID)
	if !ok || fl.OwnerID == ss.User.ID {
		return
	}
	var owner string
	if h.DB.QueryRow(`SELECT username FROM users WHERE id=?`, fl.OwnerID).Scan(&owner) != nil {
		return
	}
	h.audit(r, "admin_device_action", what+" (лаборатория пользователя «"+owner+"», id "+strconv.FormatInt(labID, 10)+")")
}

// notifyMachine reports an action on a machine to the initiator, instance owner and administrators.
func (h *Handler) notifyMachine(r *http.Request, rid int64, level, title, body string) {
	if h.Notify == nil {
		return
	}
	var iid int64
	_ = h.DB.QueryRow(`SELECT instance_id FROM instance_devices WHERE id=?`, rid).Scan(&iid)
	labID, owner, err := instances.Owner(h.DB, iid)
	if err != nil {
		owner = 0
	}
	var actor int64
	if ss := auth.FromContext(r.Context()); ss != nil {
		actor = ss.User.ID
	}
	h.Notify.Activity(actor, owner, level, notify.CatMachine, title, body, "/labs/"+strconv.FormatInt(labID, 10)+"/editor")
}

// actionEvent - severity and title of the notification for an action on a machine.
func actionEvent(act string, force bool) (string, string) {
	switch act {
	case "start":
		return notify.Success, "Машина запущена"
	case "stop":
		if force {
			return notify.Warning, "Машина остановлена принудительно"
		}
		return notify.Info, "Машина остановлена"
	case "restart":
		return notify.Info, "Машина перезапущена"
	case "suspend":
		return notify.Info, "Машина приостановлена"
	case "resume":
		return notify.Success, "Работа машины продолжена"
	case "wipe":
		return notify.Info, "Диск машины сброшен"
	}
	return notify.Info, "Действие над машиной: " + act
}

// removeDevice removes a machine from the lab topology (rollback of a failed clone).
func removeDevice(db *database.DB, labID, devID int64) {
	_, _ = db.Labs.Update(labID, func(l *labfile.Lab) error {
		out := l.Devices[:0]
		for _, d := range l.Devices {
			if d.ID != devID {
				out = append(out, d)
			}
		}
		l.Devices = out
		return nil
	})
}

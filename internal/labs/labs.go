// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"netrave/internal/access"
	"netrave/internal/auth"
	"netrave/internal/database"
	"netrave/internal/httpx"
	"netrave/internal/icons"
	"netrave/internal/labfile"
	"netrave/internal/names"
	"netrave/internal/network"
	"netrave/internal/notify"
	"netrave/internal/permcatalog"
	"netrave/internal/qemu"
)

// Handler - REST API for labs, topology, templates and the image list.
// Every operation checks its permission (access package) on the server side.
type Handler struct {
	DB   *database.DB
	Auth *auth.Service
	M    *qemu.Manager // mutable machine state lives in the manager's memory
	mu   sync.Mutex    // serializes topology changes

	// Optional hooks: cleanup of files and network objects after deletion.
	Cleanup    func(labID int64, deviceIDs []int64)
	LabRemoved func(labID int64)
	NetChanged func()
	StopLab    func(labID int64) error // stops devices before deletion
	WipeDevice func(devID int64) error // stops one device and wipes its overlay and snapshots
	// Stats provides QEMU process metrics by machine rows (nil - CPU and RAM are not shown in the section).
	Stats func(rids []int64) map[int64]MachineStat
	// CableSet enables or disables a wire in the instance (database, bridge, QMP set_link for running machines).
	CableSet func(instID, connID int64, up bool) error
	// CablePlan remembers the running wire ends BEFORE deleting rows (see network.PlanDetach).
	CablePlan func(connIDs []int64) []network.Plug
	// CableAttach connects a new wire to running machines without a restart (see network.AttachLive).
	CableAttach func(connID int64) []error
	// CableDetach physically disconnects wires from running machines AFTER deletion: the TAP leaves the bridge,
	// QEMU sets the interface to link down (see network.Detach). Returns warnings, but not deletion errors.
	CableDetach func(plugs []network.Plug) []error
	// NetworkDiag provides a snapshot of TAPs, bridges, nftables and br_netfilter to find packet loss.
	NetworkDiag func(labID int64) *network.Diag
	HasDisks    func(labID, rid int64) bool // whether the instance has working disks (overlays)
	// NICState provides the state of the instance's network interfaces: NIC number -> code
	// (0 - link disabled by the administrator or interface without an address, 1 - disabled or without an address,
	// 2 - configured and working). Used by the editor diagram for port and wire colors.
	NICState func(labID int64, rids []int64) map[int64]map[int]int
	// Notify - the notification centre (may be nil: then notifications are not stored).
	Notify *notify.Service
	// Icons - the catalog of user machine icons (nil - upload is unavailable).
	Icons *icons.Store
}

func (h *Handler) Register(mux *http.ServeMux) {
	user := func(f http.HandlerFunc) http.Handler { return h.Auth.Require(f) }
	perm := func(p string, f http.HandlerFunc) http.Handler { return h.Auth.RequirePerm(p, f) }

	// Lab permissions are checked in handlers (labFor / requirePerm), not in routes.
	mux.Handle("GET /api/labs", user(h.list))
	mux.Handle("POST /api/labs", user(h.create))
	mux.Handle("POST /api/labs/import", user(h.importHandler))
	mux.Handle("GET /api/labs/running", user(h.runningList))
	mux.Handle("GET /api/labs/{id}", user(h.get))
	mux.Handle("POST /api/labs/{id}/wipe", user(h.wipeLab))
	mux.Handle("GET /api/labs/{id}/wipes", user(h.wipeHistory))
	mux.Handle("PUT /api/labs/{id}", user(h.update))
	mux.Handle("DELETE /api/labs/{id}", user(h.remove))
	mux.Handle("POST /api/labs/{id}/copy", user(h.copyLab))
	mux.Handle("GET /api/labs/{id}/export", user(h.exportHandler))

	mux.Handle("GET /api/labs/{id}/topology", user(h.topology))
	mux.Handle("GET /api/labs/{id}/network/diagnostics", h.Auth.RequireAdmin(http.HandlerFunc(h.networkDiag)))
	mux.Handle("GET /api/labs/{id}/instances", h.Auth.RequireAdmin(http.HandlerFunc(h.instancesList)))
	mux.Handle("GET /api/labs/{id}/permissions", user(h.permsGet))
	mux.Handle("PUT /api/labs/{id}/permissions/{uid}", user(h.permsSet))
	mux.Handle("PUT /api/labs/{id}/public", user(h.publicSet))
	mux.Handle("PUT /api/labs/{id}/positions", user(h.positions))
	mux.Handle("POST /api/labs/{id}/delete", user(h.bulkDelete))
	mux.Handle("POST /api/labs/{id}/paste", user(h.paste))
	mux.Handle("POST /api/labs/{id}/devices", user(h.createDevice))
	mux.Handle("POST /api/labs/{id}/icons", user(h.iconUpload))
	mux.Handle("GET /api/labs/{id}/icons/{hash}", user(h.iconGet))
	mux.Handle("POST /api/labs/{id}/networks", user(h.createNetwork))
	mux.Handle("POST /api/labs/{id}/connections", user(h.createConn))
	mux.Handle("PUT /api/connections/{id}/state", user(h.setConnState))
	mux.Handle("PUT /api/devices/{id}", user(h.updateDevice))
	mux.Handle("GET /api/devices/{id}/nics", user(h.nicList))
	mux.Handle("PUT /api/networks/{id}", user(h.updateNetwork))

	mux.Handle("GET /api/templates", user(h.listTemplates))
	mux.Handle("POST /api/templates", perm("templates", h.createTemplate))
	mux.Handle("PUT /api/templates/{id}", perm("templates", h.updateTemplate))
	mux.Handle("POST /api/templates/{id}/duplicate", perm("templates", h.duplicateTemplate))
	mux.Handle("DELETE /api/templates/{id}", perm("templates", h.deleteTemplate))
}

func (h *Handler) audit(r *http.Request, action, detail string) {
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.DB.Audit(ss.User.ID, ss.User.Username, action, detail, auth.ClientIP(r))
	}
}

// labAt loads the lab and checks the need permission (empty - view only).
// Without view access the lab is considered nonexistent (404); without need - 403.
func (h *Handler) labAt(w http.ResponseWriter, ss *auth.Session, id int64, need string) (*Lab, bool) {
	perms, err := access.Perms(h.DB, id, ss.User.ID, ss.User.Role == "admin")
	if err == sql.ErrNoRows || (err == nil && !perms[access.LabView]) {
		httpx.Err(w, http.StatusNotFound, "лаборатория не найдена")
		return nil, false
	}
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return nil, false
	}
	if need != "" && !perms[need] {
		httpx.Err(w, http.StatusForbidden, "нет права: "+access.Title(need))
		return nil, false
	}
	l, ok := labView(h.DB, id)
	if !ok {
		httpx.Err(w, http.StatusNotFound, "лаборатория не найдена")
		return nil, false
	}
	l.Perms = perms
	return l, true
}

// labFor takes the lab id from the path and checks the need permission.
func (h *Handler) labFor(w http.ResponseWriter, r *http.Request, need string) (*Lab, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "некорректный id")
		return nil, false
	}
	lab, ok := h.labAt(w, auth.FromContext(r.Context()), id, need)
	if ok && r.Method != http.MethodGet {
		h.noteAdmin(r, lab, r.Method+" "+r.URL.Path)
	}
	return lab, ok
}

// requirePerm checks the permission on an already loaded lab. On refusal it writes 403.
func requirePerm(w http.ResponseWriter, lab *Lab, perm string) bool {
	if lab.Perms[perm] {
		return true
	}
	httpx.Err(w, http.StatusForbidden, "нет права: "+access.Title(perm))
	return false
}

func checkLabText(name, desc string) string {
	if err := names.Check("название лаборатории", name); err != nil {
		return err.Error()
	}
	if utf8.RuneCountInString(desc) > 2000 {
		return "описание: не более 2000 символов"
	}
	return ""
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	admin, uid := ss.User.Role == "admin", ss.User.ID
	grants, err := access.Grants(h.DB, uid)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	roleGrants, err := access.RoleLabGrants(h.DB, uid)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	// Labs in folders that the role allows to see (folder contents are visible to role members).
	inFolders := map[int64]bool{}
	if !admin && roleGrants[access.LabView] {
		if inFolders, err = access.FolderMemberLabs(h.DB, uid); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
	}
	// Labs where one of the users started machines (for the "running" filter).
	var running map[int64]bool
	if r.URL.Query().Get("running") == "1" {
		if running, err = runningLabIDs(h.DB, h.M); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	// Each lab's folder is the shared administrator layout (no row - "No folder").
	placed := h.DB.Folders.Placements()
	// folder: "0" - root only, "N" - folder N only. With a search (q) the folder is ignored: all folders are searched.
	folderFilter := r.URL.Query().Get("folder")
	filterOn := folderFilter != "" && q == ""
	want, _ := strconv.ParseInt(folderFilter, 10, 64)

	files := h.DB.Labs.Labs()
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].UpdatedAt != files[j].UpdatedAt {
			return files[i].UpdatedAt > files[j].UpdatedAt
		}
		return files[i].ID > files[j].ID
	})
	out := []*Lab{}
	for _, fl := range files {
		l, ok := labView(h.DB, fl.ID)
		if !ok {
			continue
		}
		if !admin {
			// Visible: own labs, labs with public access, with an explicit permission and the contents of
			// assigned folders if the role grants lab viewing.
			visible := fl.OwnerID == uid || fl.PublicLevel != "none" || inFolders[fl.ID]
			if !visible {
				for _, p := range fl.Permissions {
					if p.UserID == uid && p.Value {
						visible = true
						break
					}
				}
			}
			if !visible {
				continue
			}
			// And only from assigned folders: a lab without a folder is visible, a lab in someone else's folder is not.
			allowed, err := access.FolderAllowed(h.DB, fl.ID, uid)
			if err != nil {
				httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
				return
			}
			if !allowed {
				continue
			}
		}
		if q != "" && !strings.Contains(strings.ToLower(fl.Name), q) && !strings.Contains(strings.ToLower(fl.Description), q) {
			continue
		}
		if running != nil && !running[fl.ID] {
			continue
		}
		l.FolderID = placed[l.ID]
		if filterOn && l.FolderID != want {
			continue
		}
		grantSet := map[string]bool{}
		for k, v := range grants[l.ID] {
			grantSet[k] = v
		}
		for k := range roleGrants {
			grantSet[k] = true
		}
		l.Perms = access.Compose(admin, uid, l.OwnerID, l.PublicLevel, grantSet)
		if !l.Perms[access.LabView] {
			continue
		}
		out = append(out, l)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	if l, ok := h.labFor(w, r, ""); ok {
		h.noteAdmin(r, l, "открыта лаборатория")
		httpx.JSON(w, http.StatusOK, l)
	}
}

// canCreateLab checks the permission to create a lab. Users without roles keep the previous
// behaviour; once roles are assigned, creation must be explicitly allowed by the role.
func (h *Handler) canCreateLab(u auth.User) bool {
	if u.Role == "admin" {
		return true
	}
	has, err := access.UserHasRoles(h.DB, u.ID)
	if err != nil || !has {
		return err == nil
	}
	ok, err := access.HasRolePermission(h.DB, u.ID, permcatalog.LabCreate)
	return err == nil && ok
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	if !h.canCreateLab(ss.User) {
		httpx.Err(w, http.StatusForbidden, "нет права: создание лабораторий")
		return
	}
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if msg := checkLabText(in.Name, in.Description); msg != "" {
		httpx.Err(w, http.StatusBadRequest, msg)
		return
	}
	if h.DB.NameTakenAtRoot(in.Name, 0) {
		httpx.Err(w, http.StatusConflict, "имя занято папкой или лабораторией в корне")
		return
	}
	now := time.Now().Unix()
	created, err := h.DB.Labs.Save(labfile.Lab{
		OwnerID:     ss.User.ID,
		Name:        in.Name,
		Description: in.Description,
		CanvasJSON:  "{}",
		PublicLevel: access.DefaultPublic(h.DB, ss.User.ID),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	id := created.ID
	h.audit(r, "lab_create", in.Name)
	h.Notify.Activity(ss.User.ID, ss.User.ID, notify.Success, notify.CatLab, "Лаборатория создана", "«"+in.Name+"»", "/labs/"+strconv.FormatInt(id, 10)+"/editor")
	if l, ok := h.labAt(w, ss, id, ""); ok {
		httpx.JSON(w, http.StatusCreated, l)
	}
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	lab, ok := h.labFor(w, r, "")
	if !ok {
		return
	}
	var in struct {
		Name        *string         `json:"name"`
		Description *string         `json:"description"`
		Canvas      json.RawMessage `json:"canvas"`
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	metaChange := in.Name != nil || in.Description != nil
	canvasChange := len(in.Canvas) > 0 && string(in.Canvas) != "null"
	if metaChange && !requirePerm(w, lab, access.LabEditMeta) {
		return
	}
	if canvasChange && !requirePerm(w, lab, access.LabSaveTopo) {
		return
	}
	name, desc := lab.Name, lab.Description
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		desc = *in.Description
	}
	if msg := checkLabText(name, desc); msg != "" {
		httpx.Err(w, http.StatusBadRequest, msg)
		return
	}
	// A lab that is not in a folder occupies the name at the root.
	if in.Name != nil && h.DB.Folders.Placements()[lab.ID] == 0 && h.DB.NameTakenAtRoot(name, lab.ID) {
		httpx.Err(w, http.StatusConflict, "имя занято папкой или лабораторией в корне")
		return
	}
	if metaChange {
		if err := updateLab(h.DB, lab.ID, func(l *labfile.Lab) error {
			l.Name, l.Description, l.UpdatedAt = name, desc, time.Now().Unix()
			return nil
		}); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
	}
	if metaChange {
		if ss := auth.FromContext(r.Context()); ss != nil {
			h.Notify.Activity(ss.User.ID, lab.OwnerID, notify.Info, notify.CatLab, "Параметры лаборатории изменены",
				"«"+name+"»", "/labs/"+strconv.FormatInt(lab.ID, 10)+"/editor")
		}
	}
	if canvasChange {
		if !validCanvas(in.Canvas) {
			httpx.Err(w, http.StatusBadRequest, "некорректные данные вида")
			return
		}
		if err := updateLab(h.DB, lab.ID, func(l *labfile.Lab) error {
			l.CanvasJSON = string(in.Canvas)
			return nil
		}); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	lab, ok := h.labFor(w, r, access.LabDelete)
	if !ok {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.purgeLocked(lab.ID); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	h.audit(r, "lab_delete", lab.Name)
	// The owner is notified only if someone else (an administrator) deleted the lab.
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.Notify.Activity(ss.User.ID, lab.OwnerID, notify.Warning, notify.CatLab, "Лаборатория удалена",
			"«"+lab.Name+"» удалена пользователем «"+ss.User.Username+"». Машины остановлены, данные удалены без возможности восстановления.", "")
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// purgeLocked deletes the lab entirely: machines are stopped, instances, the file, folder placement and
// the directory with disks. Called with h.mu held.
func (h *Handler) purgeLocked(labID int64) error {
	if h.StopLab != nil {
		if err := h.StopLab(labID); err != nil {
			log.Printf("удаление лаборатории %d: остановка устройств: %v", labID, err)
		}
	}
	var devIDs []int64
	if fl, ok := labData(h.DB, labID); ok {
		for _, d := range fl.Devices {
			devIDs = append(devIDs, d.ID)
		}
	}
	// Instances are deleted together with the machines' working state (instance_* foreign keys -> lab_instances).
	if _, err := h.DB.Exec(`DELETE FROM lab_instances WHERE lab_id=?`, labID); err != nil {
		return err
	}
	if err := h.DB.SweepSnapshots(); err != nil {
		return err
	}
	if err := h.DB.Labs.Delete(labID); err != nil {
		return err
	}
	_ = h.DB.Folders.RemoveLab(labID) // lab placement in the folder store (a lab without a folder)
	if h.Cleanup != nil {
		h.Cleanup(labID, devIDs)
	}
	if h.LabRemoved != nil {
		h.LabRemoved(labID)
	}
	h.changed()
	return nil
}

// Purge deletes a lab without permission checks and notifications (for system scenarios).
func (h *Handler) Purge(labID int64) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.purgeLocked(labID)
}

// DropUser removes the user from labs before deleting the account: their own labs
// are deleted entirely, and their personal permissions in other labs are revoked. Previously this was done by foreign keys.
func (h *Handler) DropUser(uid int64) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, l := range h.DB.Labs.Labs() {
		if l.OwnerID == uid {
			if err := h.purgeLocked(l.ID); err != nil {
				return err
			}
			continue
		}
		has := false
		for _, p := range l.Permissions {
			if p.UserID == uid {
				has = true
				break
			}
		}
		if !has {
			continue
		}
		if err := updateLab(h.DB, l.ID, func(fl *labfile.Lab) error {
			var kept []labfile.Permission
			for _, p := range fl.Permissions {
				if p.UserID != uid {
					kept = append(kept, p)
				}
			}
			fl.Permissions = kept
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

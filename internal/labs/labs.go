// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"sort"
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
	"netrave/internal/msgs"
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
	Cleanup    func(labName string, deviceNames []string)
	LabRemoved func(labName string)
	NetChanged func()
	StopLab    func(labName string) error // stops devices before deletion
	WipeDevice func(devID int64) error    // stops one device and wipes its working disk
	// Stats provides QEMU process metrics by machine rows (nil - CPU and RAM are not shown in the section).
	Stats func(rids []int64) map[int64]MachineStat
	// CableSet enables or disables a cable in the instance (database, bridge, QMP set_link for running machines).
	CableSet func(labName string, instID int64, key string, up bool) error
	// CablePlan remembers the running cable ends BEFORE deleting rows (see network.PlanDetach).
	CablePlan func(labName string, keys []string) []network.Plug
	// CableAttach connects a new cable to running machines without a restart (see network.AttachLive).
	CableAttach func(labName, key string) []error
	// CableDetach physically disconnects cables from running machines AFTER deletion: the TAP leaves the bridge,
	// QEMU sets the interface to link down (see network.Detach). Returns warnings, but not deletion errors.
	CableDetach func(plugs []network.Plug) []error
	// NetworkDiag provides a snapshot of TAPs, bridges, nftables and br_netfilter to find packet loss.
	NetworkDiag func(labName string) *network.Diag
	HasDisks    func(rid int64) bool // whether the instance machine has working disks (overlays)
	// NICState provides the state of the instance's network interfaces: NIC number -> code
	// (0 - link disabled by the administrator or interface without an address, 1 - disabled or without an address,
	// 2 - configured and working). Used by the editor diagram for port and wire colors.
	NICState func(labName string, rids []int64) map[int64]map[int]int
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
	mux.Handle("GET /api/labs/{name}", user(h.get))
	mux.Handle("POST /api/labs/{name}/wipe", user(h.wipeLab))
	mux.Handle("GET /api/labs/{name}/wipes", user(h.wipeHistory))
	mux.Handle("PUT /api/labs/{name}", user(h.update))
	mux.Handle("DELETE /api/labs/{name}", user(h.remove))
	mux.Handle("POST /api/labs/{name}/copy", user(h.copyLab))
	mux.Handle("GET /api/labs/{name}/export", user(h.exportHandler))

	mux.Handle("GET /api/labs/{name}/topology", user(h.topology))
	mux.Handle("GET /api/labs/{name}/network/diagnostics", h.Auth.RequireAdmin(http.HandlerFunc(h.networkDiag)))
	mux.Handle("GET /api/labs/{name}/instances", h.Auth.RequireAdmin(http.HandlerFunc(h.instancesList)))
	mux.Handle("GET /api/labs/{name}/permissions", user(h.permsGet))
	mux.Handle("PUT /api/labs/{name}/permissions/{uid}", user(h.permsSet))
	mux.Handle("PUT /api/labs/{name}/public", user(h.publicSet))
	mux.Handle("PUT /api/labs/{name}/positions", user(h.positions))
	mux.Handle("POST /api/labs/{name}/delete", user(h.bulkDelete))
	mux.Handle("POST /api/labs/{name}/paste", user(h.paste))
	mux.Handle("POST /api/labs/{name}/devices", user(h.createDevice))
	mux.Handle("POST /api/labs/{name}/icons", user(h.iconUpload))
	mux.Handle("GET /api/labs/{name}/icons/{hash}", user(h.iconGet))
	mux.Handle("POST /api/labs/{name}/networks", user(h.createNetwork))
	mux.Handle("POST /api/labs/{name}/connections", user(h.createConn))
	mux.Handle("PUT /api/labs/{name}/connections/{conn}/state", user(h.setConnState))
	mux.Handle("PUT /api/labs/{name}/devices/{device}", user(h.updateDevice))
	mux.Handle("GET /api/labs/{name}/devices/{device}/nics", user(h.nicList))
	mux.Handle("PUT /api/labs/{name}/networks/{network}", user(h.updateNetwork))

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

func (h *Handler) auditKey(r *http.Request, action, key string, params map[string]string) {
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.DB.AuditKey(ss.User.ID, ss.User.Username, action, key, params, nil, auth.ClientIP(r))
	}
}

// labAt loads the lab and checks the need permission (empty - view only).
// Without view access the lab is considered nonexistent (404); without need - 403.
func (h *Handler) labAt(w http.ResponseWriter, ss *auth.Session, name string, need string) (*Lab, bool) {
	perms, err := access.Perms(h.DB, name, ss.User.ID, ss.User.Role == "admin")
	if err == sql.ErrNoRows || (err == nil && !perms[access.LabView]) {
		httpx.Err(w, http.StatusNotFound, "Laboratory not found")
		return nil, false
	}
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return nil, false
	}
	if need != "" && !perms[need] {
		httpx.ErrKey(w, http.StatusForbidden, "No permission: {permission_key}", map[string]string{"permission_key": access.Title(need)})
		return nil, false
	}
	l, ok := labView(h.DB, name)
	if !ok {
		httpx.Err(w, http.StatusNotFound, "Laboratory not found")
		return nil, false
	}
	l.Perms = perms
	if running, err := runningLabIDs(h.DB, h.M); err == nil {
		l.Running = running[l.Name]
	}
	return l, true
}

// labFor takes the lab name from the path and checks the need permission.
func (h *Handler) labFor(w http.ResponseWriter, r *http.Request, need string) (*Lab, bool) {
	name := r.PathValue("name")
	if name == "" {
		httpx.Err(w, http.StatusBadRequest, "Invalid name")
		return nil, false
	}
	lab, ok := h.labAt(w, auth.FromContext(r.Context()), name, need)
	if ok && r.Method != http.MethodGet {
		h.noteAdmin(r, lab, "{request}", map[string]string{"request": r.Method + " " + r.URL.Path})
	}
	return lab, ok
}

// requirePerm checks the permission on an already loaded lab. On refusal it writes 403.
func requirePerm(w http.ResponseWriter, lab *Lab, perm string) bool {
	if lab.Perms[perm] {
		return true
	}
	httpx.ErrKey(w, http.StatusForbidden, "No permission: {permission_key}", map[string]string{"permission_key": access.Title(perm)})
	return false
}

// labLink builds the editor URL for notifications and audit records.
func labLink(name string) string { return "/labs/" + url.PathEscape(name) + "/editor" }

func checkLabText(name, desc string) error {
	if err := names.Check("Laboratory name", name); err != nil {
		return err
	}
	if utf8.RuneCountInString(desc) > 2000 {
		return msgs.New("Description: no more than 2000 characters", nil)
	}
	return nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	admin, uid := ss.User.Role == "admin", ss.User.ID
	// Labs where one of the users started machines (for the "running" filter).
	var running map[string]bool
	var err error
	if r.URL.Query().Get("running") == "1" {
		if running, err = runningLabIDs(h.DB, h.M); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		}
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	// folder: "" - root only, "N" - a folder path only. With a search (q) the folder is ignored.
	folderFilter := r.URL.Query().Get("folder")
	filterOn := r.URL.Query().Has("folder") && q == ""

	files := h.DB.Labs.Labs()
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].UpdatedAt != files[j].UpdatedAt {
			return files[i].UpdatedAt > files[j].UpdatedAt
		}
		return files[i].Name < files[j].Name
	})
	out := []*Lab{}
	for _, fl := range files {
		visible, err := access.LabVisible(h.DB, fl, uid, admin)
		if err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		}
		if !visible {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(fl.Name), q) && !strings.Contains(strings.ToLower(fl.Description), q) {
			continue
		}
		if running != nil && !running[fl.Name] {
			continue
		}
		l, ok := labView(h.DB, fl.Name)
		if !ok {
			continue
		}
		l.Folder, _ = h.DB.Labs.Folder(fl.Name)
		if filterOn && l.Folder != folderFilter {
			continue
		}
		perms, err := access.Perms(h.DB, fl.Name, uid, admin)
		if err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		}
		l.Perms = perms
		if !l.Perms[access.LabView] {
			continue
		}
		out = append(out, l)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	if l, ok := h.labFor(w, r, ""); ok {
		h.noteAdmin(r, l, "Laboratory opened", nil)
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
		httpx.Err(w, http.StatusForbidden, "No permission: creating laboratories")
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
	if msg := checkLabText(in.Name, in.Description); msg != nil {
		httpx.ErrFrom(w, http.StatusBadRequest, msg)
		return
	}
	if _, exists := h.DB.Labs.Lab(in.Name); exists {
		httpx.Err(w, http.StatusConflict, "A laboratory with this name already exists")
		return
	}
	if h.DB.NameTakenAtRoot(in.Name, "") {
		httpx.Err(w, http.StatusConflict, "The name is taken by a folder or a laboratory at the root")
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
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	name := created.Name
	h.audit(r, "lab_create", in.Name)
	h.Notify.Activity(ss.User.ID, ss.User.ID, notify.Success, notify.CatLab, "Laboratory created", "«{name}»", labLink(name), notify.P("name", in.Name))
	if l, ok := h.labAt(w, ss, name, ""); ok {
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
	if msg := checkLabText(name, desc); msg != nil {
		httpx.ErrFrom(w, http.StatusBadRequest, msg)
		return
	}
	// A lab that is not in a folder occupies the name at the root.
	folder, _ := h.DB.Labs.Folder(lab.Name)
	renamed := in.Name != nil && name != lab.Name
	if renamed && folder == "" && h.DB.NameTakenAtRoot(name, lab.Name) {
		httpx.Err(w, http.StatusConflict, "The name is taken by a folder or a laboratory at the root")
		return
	}
	curName := lab.Name
	if renamed {
		// A running laboratory keeps its name: its machines write logs under it.
		if running, err := runningLabIDs(h.DB, h.M); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		} else if running[lab.Name] {
			httpx.Err(w, http.StatusConflict, "The laboratory is running: stop it before renaming")
			return
		}
		// The working directories are named after the laboratory: they move with it, and move back if the rename fails.
		undoDirs := func() {}
		if h.M != nil {
			undo, err := h.M.RenameLabFiles(lab.Name, name)
			if err != nil {
				httpx.Err(w, http.StatusInternalServerError, "Could not move the laboratory files")
				return
			}
			undoDirs = undo
		}
		saved, err := h.DB.Labs.Rename(lab.Name, name)
		if err != nil {
			undoDirs()
			httpx.Err(w, http.StatusConflict, "The name is already taken")
			return
		}
		// Instance rows keep the laboratory name: move them to the new name.
		if _, err := h.DB.Exec(`UPDATE lab_instances SET lab_id=? WHERE lab_id=?`, saved.Name, lab.Name); err != nil {
			undoDirs()
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		}
		curName = saved.Name
		// The logs are named after the laboratory: after a rename the old ones are removed.
		if h.M != nil {
			h.M.RemoveLabLogsOfAll(lab.Name)
		}
	}
	if metaChange {
		if err := updateLab(h.DB, curName, func(l *labfile.Lab) error {
			l.Description, l.UpdatedAt = desc, time.Now().Unix()
			return nil
		}); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		}
	}
	if metaChange {
		if ss := auth.FromContext(r.Context()); ss != nil {
			h.Notify.Activity(ss.User.ID, lab.OwnerID, notify.Info, notify.CatLab, "Laboratory settings changed",
				"«{name}»", labLink(curName), notify.P("name", name))
		}
	}
	if canvasChange {
		if !validCanvas(in.Canvas) {
			httpx.Err(w, http.StatusBadRequest, "Invalid view data")
			return
		}
		if err := updateLab(h.DB, curName, func(l *labfile.Lab) error {
			l.CanvasJSON = string(in.Canvas)
			return nil
		}); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
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
	if err := h.purgeLocked(lab.Name); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	h.audit(r, "lab_delete", lab.Name)
	// The owner is notified only if someone else (an administrator) deleted the lab.
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.Notify.Activity(ss.User.ID, lab.OwnerID, notify.Warning, notify.CatLab, "Laboratory deleted",
			"«{name}» was deleted by user «{user}». Machines stopped, data deleted without the possibility of recovery.", "", notify.P("name", lab.Name, "user", ss.User.Username))
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// purgeLocked deletes the lab entirely: machines are stopped, instances, the file, folder placement and
// the directory with disks. Called with h.mu held.
func (h *Handler) purgeLocked(labName string) error {
	if h.StopLab != nil {
		if err := h.StopLab(labName); err != nil {
			log.Printf("lab %s deletion: stopping machines: %v", labName, err)
		}
	}
	var devNames []string
	if fl, ok := labData(h.DB, labName); ok {
		for _, d := range fl.Devices {
			devNames = append(devNames, d.Name)
		}
	}
	// Working directories are found through lab_instances: remove them before the rows disappear.
	// This is also the instance reset: the lab's working copies and logs are deleted.
	if h.Cleanup != nil {
		h.Cleanup(labName, devNames)
	}
	if h.LabRemoved != nil {
		h.LabRemoved(labName)
	}
	// Instances are deleted together with the machines' working state (instance_* foreign keys -> lab_instances).
	if _, err := h.DB.Exec(`DELETE FROM lab_instances WHERE lab_id=?`, labName); err != nil {
		return err
	}
	if err := h.DB.Labs.Delete(labName); err != nil {
		return err
	}
	h.changed()
	return nil
}

// Purge deletes a lab without permission checks and notifications (for system scenarios).
func (h *Handler) Purge(labName string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.purgeLocked(labName)
}

// DropUser removes the user from labs before deleting the account: their own labs
// are deleted entirely, and their personal permissions in other labs are revoked. Previously this was done by foreign keys.
func (h *Handler) DropUser(uid int64) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	// The user's instances are removed with the user: their machines are stopped, and their working files and logs go too.
	// This runs before the user row is deleted, while the instance rows still name the logs.
	if h.M != nil {
		rows, err := h.DB.Query(`SELECT id FROM lab_instances WHERE user_id=?`, uid)
		if err != nil {
			return err
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		for _, id := range ids {
			// A running machine must not lose its disk: it is stopped before its files are removed.
			if err := h.M.StopInstance(id, 60*time.Second); err != nil {
				return err
			}
			h.M.RemoveInstanceFiles(id)
		}
	}
	for _, l := range h.DB.Labs.Labs() {
		if l.OwnerID == uid {
			if err := h.purgeLocked(l.Name); err != nil {
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
		if err := updateLab(h.DB, l.Name, func(fl *labfile.Lab) error {
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

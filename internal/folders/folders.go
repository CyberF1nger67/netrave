// SPDX-License-Identifier: GPL-3.0-only

// Package folders - shared lab folders and access to them.
//
// The folder tree is shared and managed only by an administrator: creation, renaming, moving,
// deletion and lab placement are available only to the admin role. Regular users receive folders
// through assignment (folder store): an assigned folder opens all its subfolders, and labs from
// unassigned folders do not exist for the user (checked in the access package).
//
// The tree, assignments and placement are stored in folders.yaml (folderstore package). A lab without
// placement is "No folder". Deleting a folder does not delete labs: they return to the root (or
// are moved to the folder specified in move_to).
package folders

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"netrave/internal/access"
	"netrave/internal/auth"
	"netrave/internal/database"
	"netrave/internal/folderstore"
	"netrave/internal/httpx"
	"netrave/internal/names"
	"netrave/internal/notify"
	"netrave/internal/permcatalog"
)

const (
	maxDepth   = 16  // maximum folder nesting
	maxFolders = 500 // total number of folders in the system
)

// Handler - REST API for lab folders and folder assignments to users.
type Handler struct {
	DB     *database.DB
	Auth   *auth.Service
	Notify *notify.Service // may be nil: then no notifications are sent
}

// Folder - a folder of the shared tree. LabCount is the number of labs available to the caller directly in this folder.
type Folder struct {
	ID        int64  `json:"id"`
	ParentID  *int64 `json:"parent_id"`
	Name      string `json:"name"`
	LabCount  int    `json:"lab_count"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// requireRolePerms allows an administrator or a user whose role grants at least one of the permissions.
func (h *Handler) requireRolePerms(next http.HandlerFunc, keys ...string) http.Handler {
	return h.Auth.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ss := auth.FromContext(r.Context())
		if ss.User.Role != "admin" {
			granted := false
			for _, key := range keys {
				ok, err := access.HasRolePermission(h.DB, ss.User.ID, key)
				if err != nil {
					httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
					return
				}
				if ok {
					granted = true
					break
				}
			}
			if !granted {
				httpx.Err(w, http.StatusForbidden, "нет права: "+permcatalog.Title(keys[0]))
				return
			}
		}
		next(w, r)
	}))
}

// Register wires up the routes. Reading the tree is available to all authenticated users. Folder management
// is delegated by the folders.manage permission, moving labs by folders.move_labs.
// Assigning folders to users remains administrator-only.
func (h *Handler) Register(mux *http.ServeMux) {
	user := func(f http.HandlerFunc) http.Handler { return h.Auth.Require(f) }
	admin := func(f http.HandlerFunc) http.Handler { return h.Auth.RequireAdmin(f) }
	manage := func(f http.HandlerFunc) http.Handler { return h.requireRolePerms(f, permcatalog.FolderManage) }
	move := func(f http.HandlerFunc) http.Handler {
		return h.requireRolePerms(f, permcatalog.FolderMoveLabs, permcatalog.FolderManage)
	}
	mux.Handle("GET /api/lab-folders", user(h.list))
	mux.Handle("POST /api/lab-folders", manage(h.create))
	mux.Handle("PUT /api/lab-folders/{id}", manage(h.update))
	mux.Handle("DELETE /api/lab-folders/{id}", manage(h.remove))
	mux.Handle("PUT /api/labs/{id}/folder", move(h.placeLab))
	mux.Handle("GET /api/users/{id}/folders", admin(h.userFoldersGet))
	mux.Handle("PUT /api/users/{id}/folders", admin(h.userFoldersSet))
}

func (h *Handler) audit(r *http.Request, action, detail string) {
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.DB.Audit(ss.User.ID, ss.User.Username, action, detail, auth.ClientIP(r))
	}
}

// folderVisible reports whether the folder is within the user's access scope (including roles and subfolders).
func (h *Handler) folderVisible(uid, id int64) (bool, error) {
	roles, err := access.UserRoleIDs(h.DB, uid)
	if err != nil {
		return false, err
	}
	return h.DB.Folders.VisibleFolders(uid, roles)[id], nil
}

// visibleWithAncestors - folders the user sees, with ancestors (to show the path to them).
func (h *Handler) visibleWithAncestors(uid int64) (map[int64]bool, error) {
	roles, err := access.UserRoleIDs(h.DB, uid)
	if err != nil {
		return nil, err
	}
	return h.DB.Folders.WithAncestors(h.DB.Folders.VisibleFolders(uid, roles)), nil
}

func toFolder(f folderstore.Folder) Folder {
	out := Folder{ID: f.ID, Name: f.Name, CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt}
	if f.ParentID != 0 {
		p := f.ParentID
		out.ParentID = &p
	}
	return out
}

// cleanName validates the folder name: only Latin letters, digits, '-' and '_'.
func cleanName(s string) (string, string) {
	s = strings.TrimSpace(s)
	if err := names.Check("название папки", s); err != nil {
		return "", err.Error()
	}
	return s, ""
}

// folder loads a folder. There are no more foreign folders: the tree is shared.
func (h *Handler) folder(w http.ResponseWriter, id int64) (*Folder, bool) {
	f, ok := h.DB.Folders.Folder(id)
	if !ok {
		httpx.Err(w, http.StatusNotFound, "папка не найдена")
		return nil, false
	}
	out := toFolder(f)
	return &out, true
}

// checkParent validates the parent folder and depth. selfID (0 on creation) prevents cycles.
// Returns the error text or an empty string.
func (h *Handler) checkParent(parent *int64, selfID int64) string {
	if parent == nil {
		return ""
	}
	if selfID != 0 && *parent == selfID {
		return "папку нельзя переместить в саму себя"
	}
	chain := h.DB.Folders.Chain(*parent)
	if len(chain) == 0 {
		return "родительская папка не найдена"
	}
	if selfID != 0 {
		for _, c := range chain {
			if c == selfID {
				return "папку нельзя переместить в её же подпапку"
			}
		}
	}
	// chain - the path from the parent to the root. The new folder will be one level deeper.
	if len(chain)+1 > maxDepth {
		return "слишком большая вложенность папок (максимум " + strconv.Itoa(maxDepth) + ")"
	}
	return ""
}

// labCounts counts the labs available to the user by folder (only those placed in folders).
func (h *Handler) labCounts(admin bool, uid int64) (map[int64]int, error) {
	visibleLabs := map[int64]bool{}
	var roleView bool
	var member map[int64]bool
	if !admin {
		roleGrants, err := access.RoleLabGrants(h.DB, uid)
		if err != nil {
			return nil, err
		}
		roleView = roleGrants[access.LabView]
		if roleView {
			if member, err = access.FolderMemberLabs(h.DB, uid); err != nil {
				return nil, err
			}
		}
	}
	for _, l := range h.DB.Labs.Labs() {
		if admin {
			visibleLabs[l.ID] = true
			continue
		}
		allowed, err := access.FolderAllowed(h.DB, l.ID, uid)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}
		direct := l.PublicLevel != "none" || l.OwnerID == uid
		for _, p := range l.Permissions {
			if p.UserID == uid && p.Value {
				direct = true
			}
		}
		if direct || (roleView && member[l.ID]) {
			visibleLabs[l.ID] = true
		}
	}
	out := map[int64]int{}
	for lab, folder := range h.DB.Folders.Placements() {
		if visibleLabs[lab] {
			out[folder]++
		}
	}
	return out, nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	admin := ss.User.Role == "admin"

	visible := map[int64]bool{}
	if !admin {
		var err error
		if visible, err = h.visibleWithAncestors(ss.User.ID); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
	}
	counts, err := h.labCounts(admin, ss.User.ID)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	out := []Folder{}
	for _, f := range h.DB.Folders.Folders() {
		if !admin && !visible[f.ID] {
			continue
		}
		g := toFolder(f)
		g.LabCount = counts[f.ID]
		out = append(out, g)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	nonAdmin := ss.User.Role != "admin"
	var in struct {
		Name     string `json:"name"`
		ParentID *int64 `json:"parent_id"`
	}
	if !httpx.Decode(w, r, &in, 1<<16) {
		return
	}
	name, msg := cleanName(in.Name)
	if msg != "" {
		httpx.Err(w, http.StatusBadRequest, msg)
		return
	}
	if h.DB.NameTakenAtRoot(name, 0) {
		httpx.Err(w, http.StatusConflict, "имя занято лабораторией в корне или папкой")
		return
	}
	if h.DB.Folders.Count() >= maxFolders {
		httpx.Err(w, http.StatusBadRequest, "достигнут лимит папок ("+strconv.Itoa(maxFolders)+")")
		return
	}
	if in.ParentID != nil {
		if _, ok := h.folder(w, *in.ParentID); !ok {
			return
		}
		if nonAdmin {
			ok, err := h.folderVisible(ss.User.ID, *in.ParentID)
			if err != nil {
				httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
				return
			}
			if !ok {
				httpx.Err(w, http.StatusForbidden, "нет доступа к родительской папке")
				return
			}
		}
	}
	if msg := h.checkParent(in.ParentID, 0); msg != "" {
		httpx.Err(w, http.StatusBadRequest, msg)
		return
	}
	created, err := h.DB.Folders.Create(in.ParentID, name, time.Now().Unix())
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "не удалось сохранить папку")
		return
	}
	if nonAdmin {
		// A delegated manager sees the folder they created; an administrator grants access to others.
		if err := h.DB.Folders.GrantUserFolder(created.ID, ss.User.ID, ss.User.ID); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "не удалось сохранить папку")
			return
		}
	}
	h.audit(r, "folder_create", name)
	httpx.JSON(w, http.StatusCreated, toFolder(created))
}

// update renames the folder and/or moves it. parent_id field: null - to the root, a number - to another folder,
// field absent - unchanged.
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	nonAdmin := ss.User.Role != "admin"
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "некорректный id")
		return
	}
	cur, ok := h.folder(w, id)
	if !ok {
		return
	}
	if nonAdmin {
		visible, err := h.folderVisible(ss.User.ID, id)
		if err != nil {
			httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
		if !visible {
			httpx.Err(w, http.StatusNotFound, "папка не найдена")
			return
		}
	}
	var in struct {
		Name     *string         `json:"name"`
		ParentID json.RawMessage `json:"parent_id"`
	}
	if !httpx.Decode(w, r, &in, 1<<16) {
		return
	}
	name := cur.Name
	if in.Name != nil {
		n, msg := cleanName(*in.Name)
		if msg != "" {
			httpx.Err(w, http.StatusBadRequest, msg)
			return
		}
		if h.DB.NameTakenAtRoot(n, 0) && n != cur.Name {
			httpx.Err(w, http.StatusConflict, "имя занято лабораторией в корне или папкой")
			return
		}
		name = n
	}
	parent := cur.ParentID
	moved := false
	if len(in.ParentID) > 0 {
		moved = true
		parent = nil
		if s := strings.TrimSpace(string(in.ParentID)); s != "null" {
			p, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				httpx.Err(w, http.StatusBadRequest, "некорректная родительская папка")
				return
			}
			if _, ok := h.folder(w, p); !ok {
				return
			}
			if nonAdmin {
				visible, err := h.folderVisible(ss.User.ID, p)
				if err != nil {
					httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
					return
				}
				if !visible {
					httpx.Err(w, http.StatusForbidden, "нет доступа к папке назначения")
					return
				}
			}
			parent = &p
		}
	}
	if moved {
		if msg := h.checkParent(parent, id); msg != "" {
			httpx.Err(w, http.StatusBadRequest, msg)
			return
		}
		// The subtree height after the move must not exceed the limit.
		base := 0
		if parent != nil {
			base = len(h.DB.Folders.Chain(*parent))
		}
		if base+h.DB.Folders.Height(id) > maxDepth {
			httpx.Err(w, http.StatusBadRequest, "слишком большая вложенность папок (максимум "+strconv.Itoa(maxDepth)+")")
			return
		}
	}
	if err := h.DB.Folders.Update(id, name, parent, time.Now().Unix()); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "не удалось сохранить папку")
		return
	}
	h.audit(r, "folder_update", name)
	if f, ok := h.DB.Folders.Folder(id); ok {
		httpx.JSON(w, http.StatusOK, toFolder(f))
	}
}

// remove deletes the folder together with nested folders. Labs are not deleted: by default they
// return to the root; with move_to=N they are moved to the specified folder outside the deleted subtree.
func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	nonAdmin := ss.User.Role != "admin"
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "некорректный id")
		return
	}
	f, ok := h.folder(w, id)
	if !ok {
		return
	}
	if nonAdmin {
		visible, err := h.folderVisible(ss.User.ID, id)
		if err != nil {
			httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
		if !visible {
			httpx.Err(w, http.StatusNotFound, "папка не найдена")
			return
		}
	}
	inside := map[int64]bool{}
	for _, v := range h.DB.Folders.Subtree(id) {
		inside[v] = true
	}
	var target int64
	if raw := strings.TrimSpace(r.URL.Query().Get("move_to")); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			httpx.Err(w, http.StatusBadRequest, "некорректная папка назначения")
			return
		}
		if inside[v] {
			httpx.Err(w, http.StatusBadRequest, "нельзя переносить лаборатории внутрь удаляемой папки")
			return
		}
		if _, ok := h.folder(w, v); !ok {
			return
		}
		if nonAdmin {
			visible, err := h.folderVisible(ss.User.ID, v)
			if err != nil {
				httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
				return
			}
			if !visible {
				httpx.Err(w, http.StatusForbidden, "нет доступа к папке назначения")
				return
			}
		}
		target = v
	}
	labs, moved, err := h.DB.Folders.Remove(id, target)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "не удалось сохранить папки")
		return
	}
	h.audit(r, "folder_delete", f.Name)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "labs": labs, "moved_labs": moved})
}

// placeLab puts a lab into a folder: body {"folder_id": N} or {"folder_id": null} - "No folder".
// Placement is shared and changed only by an administrator.
func (h *Handler) placeLab(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	nonAdmin := ss.User.Role != "admin"
	labID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "некорректный id")
		return
	}
	if nonAdmin {
		perms, err := access.Perms(h.DB, labID, ss.User.ID, false)
		if err != nil || !perms[access.LabView] {
			httpx.Err(w, http.StatusNotFound, "лаборатория не найдена")
			return
		}
	}
	fl, ok := h.DB.Labs.Lab(labID)
	if !ok {
		httpx.Err(w, http.StatusNotFound, "лаборатория не найдена")
		return
	}
	name := fl.Name
	var in struct {
		FolderID *int64 `json:"folder_id"`
	}
	if !httpx.Decode(w, r, &in, 1<<16) {
		return
	}
	if in.FolderID == nil {
		if err := h.DB.Folders.Place(labID, 0); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "не удалось сохранить раскладку")
			return
		}
		h.audit(r, "lab_folder", name+": без папки")
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "folder_id": 0})
		return
	}
	if _, ok := h.folder(w, *in.FolderID); !ok {
		return
	}
	if nonAdmin {
		visible, err := h.folderVisible(ss.User.ID, *in.FolderID)
		if err != nil {
			httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
		if !visible {
			httpx.Err(w, http.StatusForbidden, "нет доступа к папке назначения")
			return
		}
	}
	if err := h.DB.Folders.Place(labID, *in.FolderID); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "не удалось сохранить раскладку")
		return
	}
	h.audit(r, "lab_folder", name+": папка "+strconv.FormatInt(*in.FolderID, 10))
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "folder_id": *in.FolderID})
}

// userFoldersGet returns folders assigned to the user (direct assignments, without subfolders).
func (h *Handler) userFoldersGet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "некорректный id")
		return
	}
	var name string
	if err := h.DB.QueryRow(`SELECT username FROM users WHERE id=?`, id).Scan(&name); err == sql.ErrNoRows {
		httpx.Err(w, http.StatusNotFound, "пользователь не найден")
		return
	} else if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"user_id": id, "username": name, "folder_ids": h.DB.Folders.UserFolderIDs(id)})
}

// userFoldersSet replaces the set of assigned folders. Body: {"folder_ids": [1,2,3]} (an empty list revokes everything).
func (h *Handler) userFoldersSet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "некорректный id")
		return
	}
	var name string
	if err := h.DB.QueryRow(`SELECT username FROM users WHERE id=?`, id).Scan(&name); err == sql.ErrNoRows {
		httpx.Err(w, http.StatusNotFound, "пользователь не найден")
		return
	} else if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	var in struct {
		FolderIDs []int64 `json:"folder_ids"`
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	seen := map[int64]bool{}
	ids := make([]int64, 0, len(in.FolderIDs))
	for _, fid := range in.FolderIDs {
		if seen[fid] {
			continue
		}
		seen[fid] = true
		if _, ok := h.folder(w, fid); !ok {
			return
		}
		ids = append(ids, fid)
	}
	ss := auth.FromContext(r.Context())
	if err := h.DB.Folders.SetUserFolders(id, ids, ss.User.ID); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "не удалось сохранить назначения")
		return
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	h.audit(r, "user_folders", name)
	if id != ss.User.ID {
		h.Notify.Send(id, notify.Info, notify.CatAccount, "Доступ к папкам изменён",
			"Назначено папок: "+strconv.Itoa(len(ids))+". Инициатор: «"+ss.User.Username+"».",
			"/labs")
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "folder_ids": ids})
}

// SPDX-License-Identifier: GPL-3.0-only

// Package folders - shared lab folders and access to them.
//
// A folder is a directory of the laboratory tree (see package fstree); a lab is a file inside
// a folder. The tree is shared: creation, renaming, moving, deletion and lab placement are
// available to an administrator or to a role with the folder permissions. Users see the folders
// that contain a lab they can access; there are no folder-level grants.
//
// Deleting a folder does not delete labs: they return to the root (or are moved to the folder
// specified in move_to), and then the empty folders are removed.
package folders

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"netrave/internal/access"
	"netrave/internal/auth"
	"netrave/internal/database"
	"netrave/internal/fstree"
	"netrave/internal/httpx"
	"netrave/internal/names"
	"netrave/internal/notify"
	"netrave/internal/permcatalog"
)

const (
	maxDepth   = 16  // maximum folder nesting
	maxFolders = 500 // total number of folders in the system
)

// Handler - REST API for lab folders.
type Handler struct {
	DB     *database.DB
	Auth   *auth.Service
	Notify *notify.Service // may be nil: then no notifications are sent
}

// Folder - a folder of the shared tree. LabCount is the number of labs available to the
// caller directly in this folder.
type Folder struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Parent    string `json:"parent"`
	LabCount  int    `json:"lab_count"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// folderView builds the API view of a folder.
func folderView(path, name string, labCount int) Folder {
	return Folder{Path: path, Name: name, Parent: parentPath(path), LabCount: labCount}
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
					httpx.Err(w, http.StatusInternalServerError, "Database error")
					return
				}
				if ok {
					granted = true
					break
				}
			}
			if !granted {
				httpx.ErrKey(w, http.StatusForbidden, "No permission: {permission_key}", map[string]string{"permission_key": permcatalog.Title(keys[0])})
				return
			}
		}
		next(w, r)
	}))
}

// Register wires up the routes. Reading the tree is available to all authenticated users.
func (h *Handler) Register(mux *http.ServeMux) {
	user := func(f http.HandlerFunc) http.Handler { return h.Auth.Require(f) }
	manage := func(f http.HandlerFunc) http.Handler { return h.requireRolePerms(f, permcatalog.FolderManage) }
	move := func(f http.HandlerFunc) http.Handler {
		return h.requireRolePerms(f, permcatalog.FolderMoveLabs, permcatalog.FolderManage)
	}
	mux.Handle("GET /api/lab-folders", user(h.list))
	mux.Handle("POST /api/lab-folders", manage(h.create))
	mux.Handle("PUT /api/lab-folders", manage(h.update))
	mux.Handle("DELETE /api/lab-folders", manage(h.remove))
	mux.Handle("PUT /api/labs/{name}/folder", move(h.placeLab))
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

func parentPath(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i]
	}
	return ""
}

func baseName(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func joinPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "/" + name
}

func depth(path string) int {
	if path == "" {
		return 0
	}
	return strings.Count(path, "/") + 1
}

// cleanName validates the folder name: any name Linux allows in a file name (see the names package).
func cleanName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if err := names.Check("Folder name", s); err != nil {
		return "", err
	}
	return s, nil
}

// errKey translates file tree errors into user-facing messages.
func errKey(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fstree.ErrExists):
		httpx.Err(w, http.StatusConflict, "The name is already taken")
	case errors.Is(err, fstree.ErrNotFound):
		httpx.Err(w, http.StatusNotFound, "Folder not found")
	case errors.Is(err, fstree.ErrNotEmpty):
		httpx.Err(w, http.StatusConflict, "The folder is not empty")
	case errors.Is(err, fstree.ErrInside):
		httpx.Err(w, http.StatusBadRequest, "A folder cannot be moved into itself")
	case errors.Is(err, fstree.ErrInvalidPath), errors.Is(err, fstree.ErrInvalidName):
		httpx.Err(w, http.StatusBadRequest, "Invalid folder name")
	default:
		httpx.Err(w, http.StatusInternalServerError, "Could not save the folder")
	}
}

// tree returns the current tree and checks the folder exists.
func (h *Handler) tree() (fstree.Tree, error) { return h.DB.Labs.Tree() }

func (h *Handler) exists(tree fstree.Tree, path string) bool {
	if path == "" {
		return true
	}
	for _, f := range tree.Folders {
		if f.Path == path {
			return true
		}
	}
	return false
}

// subtree returns the folder and all nested folders.
func subtree(tree fstree.Tree, path string) []string {
	out := []string{path}
	for _, f := range tree.Folders {
		if strings.HasPrefix(f.Path, path+"/") {
			out = append(out, f.Path)
		}
	}
	return out
}

func (h *Handler) visibleFolders(ss *auth.Session) (map[string]bool, error) {
	return access.VisibleFolders(h.DB, ss.User.ID, ss.User.Role == "admin")
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	admin := ss.User.Role == "admin"
	tree, err := h.tree()
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	visible := map[string]bool{}
	if !admin {
		if visible, err = h.visibleFolders(ss); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		}
	}
	counts := map[string]int{}
	for _, entry := range tree.Labs {
		l, ok := h.DB.Labs.Lab(entry.Name)
		if !ok {
			continue
		}
		ok, err = access.LabVisible(h.DB, l, ss.User.ID, admin)
		if err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		}
		if ok {
			counts[entry.Folder]++
		}
	}
	out := []Folder{}
	for _, f := range tree.Folders {
		if !admin && !visible[f.Path] {
			continue
		}
		out = append(out, folderView(f.Path, f.Name, counts[f.Path]))
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	nonAdmin := ss.User.Role != "admin"
	var in struct {
		Name   string `json:"name"`
		Parent string `json:"parent"`
	}
	if !httpx.Decode(w, r, &in, 1<<16) {
		return
	}
	name, msg := cleanName(in.Name)
	if msg != nil {
		httpx.ErrFrom(w, http.StatusBadRequest, msg)
		return
	}
	tree, err := h.tree()
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	if !h.exists(tree, in.Parent) {
		httpx.Err(w, http.StatusNotFound, "Parent folder not found")
		return
	}
	if nonAdmin {
		ok, err := access.FolderVisible(h.DB, ss.User.ID, false, in.Parent)
		if err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		}
		if !ok {
			httpx.Err(w, http.StatusForbidden, "No access to the parent folder")
			return
		}
	}
	if depth(in.Parent)+1 > maxDepth {
		httpx.ErrKey(w, http.StatusBadRequest, "Folder nesting is too deep (maximum {max})", map[string]string{"max": strconv.Itoa(maxDepth)})
		return
	}
	if len(tree.Folders) >= maxFolders {
		httpx.ErrKey(w, http.StatusBadRequest, "Folder limit reached ({limit})", map[string]string{"limit": strconv.Itoa(maxFolders)})
		return
	}
	if in.Parent == "" && h.DB.NameTakenAtRoot(name, "") {
		httpx.Err(w, http.StatusConflict, "The name is taken by a laboratory or a folder at the root")
		return
	}
	if err := fstree.Mkdir(h.DB.Labs.Root(), in.Parent, name); err != nil {
		errKey(w, err)
		return
	}
	h.audit(r, "folder_create", joinPath(in.Parent, name))
	httpx.JSON(w, http.StatusCreated, folderView(joinPath(in.Parent, name), name, 0))
}

// update renames the folder and/or moves it. The parent field: absent - unchanged, "" or null - the root.
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	nonAdmin := ss.User.Role != "admin"
	var in struct {
		Path   string          `json:"path"`
		Name   *string         `json:"name"`
		Parent json.RawMessage `json:"parent"`
	}
	if !httpx.Decode(w, r, &in, 1<<16) {
		return
	}
	tree, err := h.tree()
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	if !h.exists(tree, in.Path) || in.Path == "" {
		httpx.Err(w, http.StatusNotFound, "Folder not found")
		return
	}
	if nonAdmin {
		ok, err := access.FolderVisible(h.DB, ss.User.ID, false, in.Path)
		if err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		}
		if !ok {
			httpx.Err(w, http.StatusNotFound, "Folder not found")
			return
		}
	}
	name := baseName(in.Path)
	if in.Name != nil {
		if name, err = cleanName(*in.Name); err != nil {
			httpx.ErrFrom(w, http.StatusBadRequest, err)
			return
		}
	}
	parent := parentPath(in.Path)
	moved := len(in.Parent) > 0
	if moved {
		parent = ""
		if s := strings.TrimSpace(string(in.Parent)); s != "null" {
			if err := json.Unmarshal(in.Parent, &parent); err != nil {
				httpx.Err(w, http.StatusBadRequest, "Invalid parent folder")
				return
			}
		}
		if !h.exists(tree, parent) {
			httpx.Err(w, http.StatusNotFound, "Parent folder not found")
			return
		}
		if nonAdmin {
			ok, err := access.FolderVisible(h.DB, ss.User.ID, false, parent)
			if err != nil {
				httpx.Err(w, http.StatusInternalServerError, "Database error")
				return
			}
			if !ok {
				httpx.Err(w, http.StatusForbidden, "No access to the target folder")
				return
			}
		}
	}
	if moved {
		if parent == in.Path || strings.HasPrefix(parent, in.Path+"/") {
			httpx.Err(w, http.StatusBadRequest, "A folder cannot be moved into itself")
			return
		}
		maxChild := 0
		for _, f := range tree.Folders {
			if strings.HasPrefix(f.Path, in.Path+"/") {
				d := depth(f.Path) - depth(in.Path)
				if d > maxChild {
					maxChild = d
				}
			}
		}
		if depth(parent)+1+maxChild > maxDepth {
			httpx.ErrKey(w, http.StatusBadRequest, "Folder nesting is too deep (maximum {max})", map[string]string{"max": strconv.Itoa(maxDepth)})
			return
		}
	}
	if parent == "" && h.DB.NameTakenAtRoot(name, "") {
		httpx.Err(w, http.StatusConflict, "The name is taken by a laboratory or a folder at the root")
		return
	}
	if name != baseName(in.Path) {
		if parentPath(in.Path) == "" && h.DB.NameTakenAtRoot(name, "") {
			httpx.Err(w, http.StatusConflict, "The name is taken by a laboratory or a folder at the root")
			return
		}
		if err := fstree.RenameFolder(h.DB.Labs.Root(), in.Path, name); err != nil {
			errKey(w, err)
			return
		}
	}
	newPath := joinPath(parentPath(in.Path), name)
	if moved {
		if err := fstree.MoveFolder(h.DB.Labs.Root(), newPath, parent); err != nil {
			errKey(w, err)
			return
		}
		newPath = joinPath(parent, name)
	}
	h.audit(r, "folder_update", newPath)
	httpx.JSON(w, http.StatusOK, folderView(newPath, name, 0))
}

// remove deletes the folder together with nested folders. Labs are not deleted: by default they
// return to the root; with move_to=PATH they are moved to the folder outside the deleted subtree.
func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	nonAdmin := ss.User.Role != "admin"
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		httpx.Err(w, http.StatusBadRequest, "Folder not found")
		return
	}
	tree, err := h.tree()
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	if !h.exists(tree, path) {
		httpx.Err(w, http.StatusNotFound, "Folder not found")
		return
	}
	if nonAdmin {
		ok, err := access.FolderVisible(h.DB, ss.User.ID, false, path)
		if err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		}
		if !ok {
			httpx.Err(w, http.StatusNotFound, "Folder not found")
			return
		}
	}
	inside := map[string]bool{}
	for _, p := range subtree(tree, path) {
		inside[p] = true
	}
	target := ""
	if raw := strings.TrimSpace(r.URL.Query().Get("move_to")); raw != "" {
		target = raw
		if inside[target] {
			httpx.Err(w, http.StatusBadRequest, "You cannot move laboratories into a folder being deleted")
			return
		}
		if !h.exists(tree, target) {
			httpx.Err(w, http.StatusNotFound, "Target folder not found")
			return
		}
		if nonAdmin {
			ok, err := access.FolderVisible(h.DB, ss.User.ID, false, target)
			if err != nil {
				httpx.Err(w, http.StatusInternalServerError, "Database error")
				return
			}
			if !ok {
				httpx.Err(w, http.StatusForbidden, "No access to the target folder")
				return
			}
		}
	}
	moved := 0
	for _, entry := range tree.Labs {
		if entry.Folder != path && !strings.HasPrefix(entry.Folder, path+"/") {
			continue
		}
		if err := h.DB.Labs.Move(entry.Name, target); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Could not save the layout")
			return
		}
		moved++
	}
	paths := subtree(tree, path)
	sort.Slice(paths, func(i, j int) bool { return depth(paths[i]) > depth(paths[j]) })
	for _, p := range paths {
		if err := fstree.RemoveFolder(h.DB.Labs.Root(), p); err != nil {
			errKey(w, err)
			return
		}
	}
	h.audit(r, "folder_delete", path)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "labs": moved, "moved_labs": moved})
}

// placeLab puts a lab into a folder: body {"folder": "path"} or {"folder": null} - the root.
func (h *Handler) placeLab(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	nonAdmin := ss.User.Role != "admin"
	labName := r.PathValue("name")
	lab, ok := h.DB.Labs.Lab(labName)
	if !ok {
		httpx.Err(w, http.StatusNotFound, "Laboratory not found")
		return
	}
	if nonAdmin {
		perms, err := access.Perms(h.DB, lab.Name, ss.User.ID, false)
		if err != nil || !perms[access.LabView] {
			httpx.Err(w, http.StatusNotFound, "Laboratory not found")
			return
		}
	}
	var in struct {
		Folder *string `json:"folder"`
	}
	if !httpx.Decode(w, r, &in, 1<<16) {
		return
	}
	target := ""
	if in.Folder != nil {
		target = strings.TrimSpace(*in.Folder)
	}
	tree, err := h.tree()
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	if !h.exists(tree, target) {
		httpx.Err(w, http.StatusNotFound, "Folder not found")
		return
	}
	if nonAdmin {
		ok, err := access.FolderVisible(h.DB, ss.User.ID, false, target)
		if err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		}
		if !ok {
			httpx.Err(w, http.StatusForbidden, "No access to the target folder")
			return
		}
	}
	if err := h.DB.Labs.Move(lab.Name, target); err != nil {
		errKey(w, err)
		return
	}
	if target == "" {
		h.auditKey(r, "lab_folder", "{name}: no folder", map[string]string{"name": lab.Name})
	} else {
		h.auditKey(r, "lab_folder", "{name}: folder {folder}", map[string]string{"name": lab.Name, "folder": target})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "folder": target})
}

// SPDX-License-Identifier: GPL-3.0-only

// Package roles - user roles (RBAC): permissions and members.
//
// Role management is available only to a real administrator (users.role='admin').
// A role does not change users.role and cannot make a member an administrator: it only
// adds catalog permissions. Access to a lab is granted to a role in the lab file itself.
package roles

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"netrave/internal/access"
	"netrave/internal/auth"
	"netrave/internal/config"
	"netrave/internal/database"
	"netrave/internal/httpx"
	"netrave/internal/labfile"
	"netrave/internal/msgs"
	"netrave/internal/names"
	"netrave/internal/permcatalog"
)

// Built-in administrator role. It is not stored in the roles table: an administrator
// always has all permissions, so the role is shown in the list, its description
// and settings can be changed, but it cannot be deleted.
const AdminRoleID int64 = -1

// AdminRoleName - the name of the built-in administrator role.
const AdminRoleName = "Administrator"

// adminRole returns the built-in role with all catalog permissions.
func adminRole(description string) Role {
	perms := make([]string, 0, len(permcatalog.Catalog))
	for _, p := range permcatalog.Catalog {
		perms = append(perms, p.Key)
	}
	if description == "" {
		description = "Full access to the whole system"
	}
	return Role{ID: AdminRoleID, Name: AdminRoleName, Description: description,
		Permissions: perms, System: true, MaxRunningLabs: 0}
}

// ErrSystemRole - the built-in role cannot be changed or deleted.
var ErrSystemRole = "The built-in role cannot be changed"

// Handler - role REST API. All routes require an administrator.
type Handler struct {
	DB   *database.DB
	Auth *auth.Service
	Cfg  *config.Config // the administrator role description is stored in the configuration settings
}

// Role - a role with permissions and member count.
type Role struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	MemberCount int      `json:"member_count"`
	// MaxRunningLabs - how many labs the role allows to run simultaneously (0 - no limit).
	MaxRunningLabs int   `json:"max_running_labs"`
	CreatedAt      int64 `json:"created_at"`
	UpdatedAt      int64 `json:"updated_at"`
	// System - the built-in administrator role: read-only.
	System bool `json:"system"`
}

// Member - a role member.
type Member struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	Disabled  bool   `json:"disabled"`
	CreatedAt int64  `json:"created_at"`
}

// Register wires up the routes.
func (h *Handler) Register(mux *http.ServeMux) {
	admin := func(f http.HandlerFunc) http.Handler { return h.Auth.RequireAdmin(f) }
	mux.Handle("GET /api/roles", admin(h.list))
	mux.Handle("GET /api/roles/permissions", admin(h.catalog))
	mux.Handle("POST /api/roles", admin(h.create))
	mux.Handle("GET /api/roles/{id}", admin(h.get))
	mux.Handle("PUT /api/roles/{id}", admin(h.update))
	mux.Handle("DELETE /api/roles/{id}", admin(h.remove))
	mux.Handle("PUT /api/roles/{id}/permissions", admin(h.setPermissions))
	mux.Handle("GET /api/roles/{id}/members", admin(h.members))
	mux.Handle("POST /api/roles/{id}/members", admin(h.addMembers))
	mux.Handle("DELETE /api/roles/{id}/members/{uid}", admin(h.removeMember))
}

func (h *Handler) audit(r *http.Request, action, detail string) {
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.DB.Audit(ss.User.ID, ss.User.Username, action, detail, auth.ClientIP(r))
	}
}

// cleanName validates the role name: Latin letters, digits, '-', '_' and single spaces between words.
func cleanName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if err := names.CheckLogin("Role name", s); err != nil {
		return "", err
	}
	return s, nil
}

func cleanDesc(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > 2000 {
		return "", msgs.New("Role description: no more than 2000 characters", nil)
	}
	return s, nil
}

func uniqueStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func uniqueIDs(in []int64) []int64 {
	out := make([]int64, 0, len(in))
	seen := map[int64]bool{}
	for _, v := range in {
		if v == 0 || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func (h *Handler) role(w http.ResponseWriter, id int64) (*Role, bool) {
	if id == AdminRoleID {
		r := adminRole(h.Cfg.SettingsSnapshot().RoleAdminDescription)
		err := h.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE role='admin'`).Scan(&r.MemberCount)
		if err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return nil, false
		}
		return &r, true
	}
	r := &Role{ID: id, Permissions: []string{}}
	err := h.DB.QueryRow(`SELECT id,name,description,max_running_labs,created_at,updated_at FROM roles WHERE id=?`, id).
		Scan(&r.ID, &r.Name, &r.Description, &r.MaxRunningLabs, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		httpx.Err(w, http.StatusNotFound, "Role not found")
		return nil, false
	}
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return nil, false
	}
	if err := h.fill(r); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return nil, false
	}
	return r, true
}

func (h *Handler) fill(r *Role) error {
	rows, err := h.DB.Query(`SELECT permission FROM role_permissions WHERE role_id=? ORDER BY permission`, r.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return err
		}
		if permcatalog.Known(p) {
			r.Permissions = append(r.Permissions, p)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	return h.DB.QueryRow(`SELECT COUNT(*) FROM user_roles WHERE role_id=?`, r.ID).Scan(&r.MemberCount)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`SELECT id FROM roles ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	// The built-in administrator role is shown first and is read-only.
	out := make([]Role, 0, len(ids)+1)
	if adm, ok := h.role(w, AdminRoleID); ok {
		out = append(out, *adm)
	} else {
		return
	}
	for _, id := range ids {
		role, ok := h.role(w, id)
		if !ok {
			return
		}
		out = append(out, *role)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) catalog(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, permcatalog.Catalog)
}

// isSystemRole reports that id is the built-in administrator role.
func isSystemRole(id int64) bool { return id == AdminRoleID }

// setAdminRole enables or disables administrator status for a user.
// The administrator role is the users.role field: all permission checks depend on it, so
// assigning the built-in role from "Change role" changes exactly that field. The last active
// administrator cannot be demoted, so as not to lose access to management.
func setAdminRole(db *database.DB, uid int64, admin bool) (string, int) {
	var name, role string
	var disabled int
	err := db.QueryRow(`SELECT username, role, disabled FROM users WHERE id=?`, uid).Scan(&name, &role, &disabled)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", http.StatusNotFound
		}
		return "", http.StatusInternalServerError
	}
	if (role == "admin") == admin {
		return name, 0
	}
	if !admin {
		var others int
		if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE role='admin' AND disabled=0 AND id<>?`, uid).Scan(&others); err != nil {
			return name, http.StatusInternalServerError
		}
		if others == 0 {
			return name, http.StatusConflict
		}
	}
	newRole := "user"
	if admin {
		newRole = "admin"
	}
	if _, err := db.Exec(`UPDATE users SET role=? WHERE id=?`, newRole, uid); err != nil {
		return name, http.StatusInternalServerError
	}
	access.InvalidateUserPermissions(db, uid)
	return name, 0
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name           string   `json:"name"`
		Description    string   `json:"description"`
		Permissions    []string `json:"permissions"`
		MaxRunningLabs int      `json:"max_running_labs"`
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	name, msg := cleanName(in.Name)
	if msg != nil {
		httpx.ErrFrom(w, http.StatusBadRequest, msg)
		return
	}
	if strings.EqualFold(name, AdminRoleName) {
		httpx.ErrKey(w, http.StatusConflict, "The name «{name}» is taken by a built-in role", map[string]string{"name": AdminRoleName})
		return
	}
	desc, msg := cleanDesc(in.Description)
	if msg != nil {
		httpx.ErrFrom(w, http.StatusBadRequest, msg)
		return
	}
	perms, msg := h.checkPermissions(in.Permissions)
	if msg != nil {
		httpx.ErrFrom(w, http.StatusBadRequest, msg)
		return
	}
	if in.MaxRunningLabs < 0 || in.MaxRunningLabs > 1000 {
		httpx.Err(w, http.StatusBadRequest, "Limit of simultaneously running laboratories: from 0 to 1000")
		return
	}
	tx, err := h.DB.Begin()
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO roles(name,description,max_running_labs) VALUES(?,?,?)`, name, desc, in.MaxRunningLabs)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			httpx.Err(w, http.StatusConflict, "A role with this name already exists")
			return
		}
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	id, _ := res.LastInsertId()
	if err := insertPermissions(tx, id, perms); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	h.audit(r, "role_create", name)
	role, ok := h.role(w, id)
	if ok {
		httpx.JSON(w, http.StatusCreated, role)
	}
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := h.roleID(w, r)
	if !ok {
		return
	}
	role, ok := h.role(w, id)
	if ok {
		httpx.JSON(w, http.StatusOK, role)
	}
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.roleID(w, r)
	if !ok {
		return
	}
	cur, ok := h.role(w, id)
	if !ok {
		return
	}
	var in struct {
		Name           *string   `json:"name"`
		Description    *string   `json:"description"`
		Permissions    *[]string `json:"permissions"`
		MaxRunningLabs *int      `json:"max_running_labs"`
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	name, desc := cur.Name, cur.Description
	if in.Name != nil {
		var msg error
		if name, msg = cleanName(*in.Name); msg != nil {
			httpx.ErrFrom(w, http.StatusBadRequest, msg)
			return
		}
	}
	if in.Description != nil {
		var msg error
		if desc, msg = cleanDesc(*in.Description); msg != nil {
			httpx.ErrFrom(w, http.StatusBadRequest, msg)
			return
		}
	}
	var perms []string
	if in.Permissions != nil {
		var msg error
		if perms, msg = h.checkPermissions(*in.Permissions); msg != nil {
			httpx.ErrFrom(w, http.StatusBadRequest, msg)
			return
		}
	}
	if isSystemRole(id) {
		h.updateAdminRole(w, r, in.Description)
		return
	}
	limit := cur.MaxRunningLabs
	if in.MaxRunningLabs != nil {
		if *in.MaxRunningLabs < 0 || *in.MaxRunningLabs > 1000 {
			httpx.Err(w, http.StatusBadRequest, "Limit of simultaneously running laboratories: from 0 to 1000")
			return
		}
		limit = *in.MaxRunningLabs
	}
	tx, err := h.DB.Begin()
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE roles SET name=?,description=?,max_running_labs=?,updated_at=strftime('%s','now') WHERE id=?`,
		name, desc, limit, id); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			httpx.Err(w, http.StatusConflict, "A role with this name already exists")
			return
		}
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	if in.Permissions != nil {
		if _, err := tx.Exec(`DELETE FROM role_permissions WHERE role_id=?`, id); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		}
		if err := insertPermissions(tx, id, perms); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	access.InvalidateAllPermissions(h.DB)
	h.audit(r, "role_update", name)
	role, ok := h.role(w, id)
	if ok {
		httpx.JSON(w, http.StatusOK, role)
	}
}

// updateAdminRole changes the description of the built-in administrator role. Its permissions and members
// are defined by the very notion of an administrator and are not editable.
func (h *Handler) updateAdminRole(w http.ResponseWriter, r *http.Request, description *string) {
	if description != nil {
		desc, msg := cleanDesc(*description)
		if msg != nil {
			httpx.ErrFrom(w, http.StatusBadRequest, msg)
			return
		}
		if err := h.Cfg.SetRoleAdminDescription(desc); err != nil {
			httpx.ErrKey(w, http.StatusInternalServerError, "Description was not saved: {detail}", map[string]string{"detail": err.Error()})
			return
		}
		h.audit(r, "role_update", AdminRoleName)
	}
	role, ok := h.role(w, AdminRoleID)
	if ok {
		httpx.JSON(w, http.StatusOK, role)
	}
}

func (h *Handler) setPermissions(w http.ResponseWriter, r *http.Request) {
	id, ok := h.roleID(w, r)
	if !ok {
		return
	}
	if isSystemRole(id) {
		httpx.Err(w, http.StatusBadRequest, ErrSystemRole)
		return
	}
	if _, ok := h.role(w, id); !ok {
		return
	}
	var in struct {
		Permissions []string `json:"permissions"`
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	perms, msg := h.checkPermissions(in.Permissions)
	if msg != nil {
		httpx.ErrFrom(w, http.StatusBadRequest, msg)
		return
	}
	tx, err := h.DB.Begin()
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM role_permissions WHERE role_id=?`, id); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	if err := insertPermissions(tx, id, perms); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	if _, err := tx.Exec(`UPDATE roles SET updated_at=strftime('%s','now') WHERE id=?`, id); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	access.InvalidateAllPermissions(h.DB)
	h.audit(r, "role_permissions", strconv.FormatInt(id, 10))
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "permissions": perms})
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := h.roleID(w, r)
	if !ok {
		return
	}
	if isSystemRole(id) {
		httpx.Err(w, http.StatusBadRequest, ErrSystemRole)
		return
	}
	role, ok := h.role(w, id)
	if !ok {
		return
	}
	// The role may be listed in lab permissions: remove those entries before deleting the row.
	for _, l := range h.DB.Labs.Labs() {
		has := false
		for _, p := range l.Permissions {
			if p.RoleID == id {
				has = true
				break
			}
		}
		if !has {
			continue
		}
		if _, err := h.DB.Labs.Update(l.Name, func(fl *labfile.Lab) error {
			kept := fl.Permissions[:0]
			for _, p := range fl.Permissions {
				if p.RoleID != id {
					kept = append(kept, p)
				}
			}
			fl.Permissions = kept
			return nil
		}); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Could not update the laboratories")
			return
		}
	}
	if _, err := h.DB.Exec(`DELETE FROM roles WHERE id=?`, id); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	access.InvalidateAllPermissions(h.DB)
	h.audit(r, "role_delete", role.Name)
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) members(w http.ResponseWriter, r *http.Request) {
	id, ok := h.roleID(w, r)
	if !ok {
		return
	}
	if _, ok := h.role(w, id); !ok {
		return
	}
	rows, err := h.DB.Query(`SELECT u.id,u.username,u.role,u.disabled,u.created_at
		FROM user_roles ur JOIN users u ON u.id=ur.user_id
		WHERE ur.role_id=? ORDER BY u.username COLLATE NOCASE, u.id`, id)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		var dis int
		if err := rows.Scan(&m.ID, &m.Username, &m.Role, &dis, &m.CreatedAt); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		}
		m.Disabled = dis != 0
		out = append(out, m)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) addMembers(w http.ResponseWriter, r *http.Request) {
	id, ok := h.roleID(w, r)
	if !ok {
		return
	}
	if _, ok := h.role(w, id); !ok {
		return
	}
	var in struct {
		UserIDs []int64 `json:"user_ids"`
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	ids := uniqueIDs(in.UserIDs)
	for _, uid := range ids {
		var exists int
		if err := h.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE id=?`, uid).Scan(&exists); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		}
		if exists == 0 {
			httpx.ErrKey(w, http.StatusBadRequest, "User not found: {id}", map[string]string{"id": strconv.FormatInt(uid, 10)})
			return
		}
	}
	if isSystemRole(id) {
		// The built-in administrator role is users.role: enable it for the selected users.
		for _, uid := range ids {
			name, code := setAdminRole(h.DB, uid, true)
			switch code {
			case 0:
				h.audit(r, "role_members_add", AdminRoleName+": "+name)
			case http.StatusNotFound:
				httpx.ErrKey(w, http.StatusBadRequest, "User not found: {id}", map[string]string{"id": strconv.FormatInt(uid, 10)})
				return
			default:
				httpx.Err(w, http.StatusInternalServerError, "Database error")
				return
			}
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "user_ids": ids})
		return
	}
	tx, err := h.DB.Begin()
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	defer tx.Rollback()
	for _, uid := range ids {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO user_roles(user_id,role_id) VALUES(?,?)`, uid, id); err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	for _, uid := range ids {
		access.InvalidateUserPermissions(h.DB, uid)
	}
	h.audit(r, "role_members_add", strconv.FormatInt(id, 10))
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "user_ids": ids})
}

func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	id, ok := h.roleID(w, r)
	if !ok {
		return
	}
	uid, err := strconv.ParseInt(r.PathValue("uid"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid user id")
		return
	}
	if isSystemRole(id) {
		// Removing the built-in administrator role means demoting the user to a regular one.
		name, code := setAdminRole(h.DB, uid, false)
		switch code {
		case 0:
			h.audit(r, "role_member_remove", AdminRoleName+": "+name)
			httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
		case http.StatusNotFound:
			httpx.Err(w, http.StatusNotFound, "User not found")
		case http.StatusConflict:
			httpx.Err(w, http.StatusConflict, "At least one active administrator must remain in the system")
		default:
			httpx.Err(w, http.StatusInternalServerError, "Database error")
		}
		return
	}
	if _, err := h.DB.Exec(`DELETE FROM user_roles WHERE role_id=? AND user_id=?`, id, uid); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	access.InvalidateUserPermissions(h.DB, uid)
	h.audit(r, "role_member_remove", strconv.FormatInt(id, 10)+"/"+strconv.FormatInt(uid, 10))
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) roleID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid id")
		return 0, false
	}
	return id, true
}

func (h *Handler) checkPermissions(in []string) ([]string, error) {
	out := uniqueStrings(in)
	for _, p := range out {
		if !permcatalog.Known(p) {
			return nil, msgs.New("Unknown permission: {permission_key}", map[string]string{"permission_key": p})
		}
	}
	return out, nil
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func insertPermissions(tx execer, roleID int64, perms []string) error {
	for _, p := range perms {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO role_permissions(role_id,permission) VALUES(?,?)`, roleID, p); err != nil {
			return err
		}
	}
	return nil
}

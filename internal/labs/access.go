// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"database/sql"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"netrave/internal/access"
	"netrave/internal/auth"
	"netrave/internal/httpx"
	"netrave/internal/labfile"
	"netrave/internal/notify"
)

// Permission management. Requires the lab.manage_access permission (owner and administrator - always).
// A user with lab.manage_access cannot change their own permissions and cannot grant or revoke
// permissions they do not have themselves - so permissions cannot be escalated through this API.

type permUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Disabled bool   `json:"disabled"`
	// Perms - effective permissions on the lab: own permissions paired with those
	// the user gets from an assigned role.
	Perms map[string]bool `json:"perms"`
	// OwnPerms - only permissions set for this lab directly.
	OwnPerms map[string]bool `json:"own_perms"`
	// RolePerms - permissions inherited from assigned roles.
	RolePerms map[string]bool `json:"role_perms"`
}

// permsGet returns the permission catalog and each user's permissions on the lab.
func (h *Handler) permsGet(w http.ResponseWriter, r *http.Request) {
	lab, ok := h.labFor(w, r, access.LabManageAcc)
	if !ok {
		return
	}
	rows, err := h.DB.Query(`SELECT id,username,role,disabled FROM users WHERE id<>? ORDER BY username COLLATE NOCASE`, lab.OwnerID)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	defer rows.Close()
	users := []*permUser{}
	byID := map[int64]*permUser{}
	for rows.Next() {
		var id int64
		var username, role string
		var dis int
		if rows.Scan(&id, &username, &role, &dis) != nil {
			continue
		}
		u := &permUser{ID: id, Username: username, Role: role, Disabled: dis != 0,
			Perms: map[string]bool{}, OwnPerms: map[string]bool{}, RolePerms: map[string]bool{}}
		byID[id] = u
		users = append(users, u)
	}
	// Users' own permissions and their roles' permissions on this lab - from the lab file.
	if fl, ok := labData(h.DB, lab.Name); ok {
		rolesOf := map[int64]map[int64]bool{}
		for _, u := range users {
			ids, err := access.UserRoleIDs(h.DB, u.ID)
			if err != nil {
				httpx.Err(w, http.StatusInternalServerError, "Database error")
				return
			}
			set := map[int64]bool{}
			for _, roleID := range ids {
				set[roleID] = true
			}
			rolesOf[u.ID] = set
		}
		for _, p := range fl.Permissions {
			if u := byID[p.UserID]; u != nil && p.Perm != "" {
				u.OwnPerms[p.Perm] = p.Value
			}
			for _, u := range users {
				if p.RoleID != 0 && rolesOf[u.ID][p.RoleID] && p.Perm != "" {
					u.RolePerms[p.Perm] = p.Value
				}
			}
		}
	}
	for _, u := range users {
		// Lab permissions are also inherited from the user's global roles (capability mapping).
		inherited, err := access.RoleLabGrants(h.DB, u.ID)
		if err != nil {
			httpx.Err(w, http.StatusInternalServerError, "Database error")
			return
		}
		for k := range inherited {
			u.RolePerms[k] = true
		}
		for _, p := range access.Catalog {
			if own, set := u.OwnPerms[p.Key]; set {
				u.Perms[p.Key] = own
			} else if inherited[p.Key] {
				u.Perms[p.Key] = true
			}
		}
		// Entries of the user's roles written in this lab file override global role capabilities.
		for k, v := range u.RolePerms {
			if _, set := u.OwnPerms[k]; !set {
				u.Perms[k] = v
			}
		}
		if u.Role == "admin" {
			for _, p := range access.Catalog {
				u.Perms[p.Key] = true
			}
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"owner_id":     lab.OwnerID,
		"owner":        lab.Owner,
		"public_level": lab.PublicLevel,
		"catalog":      access.Catalog,
		"users":        users,
	})
}

// permsSet applies permission changes for one user. Other permissions are not affected.
// Body: {"changes": {"machine.start": true, "machine.wipe": false}}
func (h *Handler) permsSet(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	lab, ok := h.labFor(w, r, access.LabManageAcc)
	if !ok {
		return
	}
	uid, err := strconv.ParseInt(r.PathValue("uid"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid user id")
		return
	}
	var in struct {
		Changes map[string]bool `json:"changes"`
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	if len(in.Changes) == 0 {
		httpx.Err(w, http.StatusBadRequest, "No changes")
		return
	}
	isManager := ss.User.Role == "admin" || ss.User.ID == lab.OwnerID
	if !isManager && uid == ss.User.ID {
		httpx.Err(w, http.StatusForbidden, "You cannot change your own permissions")
		return
	}
	if uid == lab.OwnerID {
		httpx.Err(w, http.StatusBadRequest, "The owner has all permissions")
		return
	}
	keys := make([]string, 0, len(in.Changes))
	for k := range in.Changes {
		if !access.Known(k) {
			httpx.ErrKey(w, http.StatusBadRequest, "Unknown permission: {permission_key}", map[string]string{"permission_key": k})
			return
		}
		if !isManager && !lab.Perms[k] {
			httpx.ErrKey(w, http.StatusForbidden, "You cannot grant or revoke a permission you do not have: {permission_key}", map[string]string{"permission_key": access.Title(k)})
			return
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var username string
	if err := h.DB.QueryRow(`SELECT username FROM users WHERE id=?`, uid).Scan(&username); err == sql.ErrNoRows {
		httpx.Err(w, http.StatusBadRequest, "User not found")
		return
	} else if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	// All permission changes - in one lab file write.
	now := time.Now().Unix()
	if err := updateLab(h.DB, lab.Name, func(l *labfile.Lab) error {
		for _, k := range keys {
			set := false
			for i := range l.Permissions {
				if l.Permissions[i].UserID == uid && l.Permissions[i].Perm == k {
					l.Permissions[i].Value = in.Changes[k]
					l.Permissions[i].UpdatedAt = now
					l.Permissions[i].UpdatedBy = ss.User.ID
					set = true
					break
				}
			}
			if !set {
				l.Permissions = append(l.Permissions, labfile.Permission{
					UserID: uid, Perm: k, Value: in.Changes[k], UpdatedAt: now, UpdatedBy: ss.User.ID,
				})
			}
		}
		return nil
	}); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	h.audit(r, "lab_perms", lab.Name+": "+username+": "+strings.Join(keys, ", "))
	// The permission change notification is addressed only to the user whose permissions changed.
	// The initiator is mentioned in the text but does not become a recipient (otherwise an administrator
	// would receive a false message about their own permissions).
	body := "Your permissions on «{lab}» changed. Changed permissions: {count}. {by_key}"
	if uid == ss.User.ID {
		body = "You changed your own permissions. Changed permissions: {count}. {by_key}"
	}
	h.Notify.Send(uid, notify.Info, notify.CatAccount, "Lab access changed", body,
		labLink(lab.Name),
		notify.P("count", strconv.Itoa(len(keys)), "lab", lab.Name, "by_key", "Initiator: «{user}».", "user", ss.User.Username))
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "saved": len(keys)})
}

// publicSet sets public access: none - only the owner and granted permissions, view - visible to everyone.
func (h *Handler) publicSet(w http.ResponseWriter, r *http.Request) {
	lab, ok := h.labFor(w, r, access.LabManageAcc)
	if !ok {
		return
	}
	var in struct {
		Level string `json:"level"`
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	if !access.ValidPublic(in.Level) {
		httpx.Err(w, http.StatusBadRequest, "Shared access: none or view")
		return
	}
	if err := updateLab(h.DB, lab.Name, func(l *labfile.Lab) error {
		l.PublicLevel = in.Level
		return nil
	}); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	h.audit(r, "lab_public", lab.Name+": "+in.Level)
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// SPDX-License-Identifier: GPL-3.0-only

// Package access - user permissions for each operation in each lab.
//
// A permission is a catalog string (a key like "machine.start"). The user's effective permission set:
//   - administrator and lab owner - all catalog permissions;
//   - others - permissions from the lab file (value=true) and "lab.view" if the lab is visible to everyone.
package access

import (
	"database/sql"
	"sync"

	"netrave/internal/database"
	"netrave/internal/permcatalog"
)

// Permission keys. All server checks use these constants.
const (
	LabView       = "lab.view"
	LabExport     = "lab.export"
	LabEditMeta   = "lab.edit_meta"
	LabSaveTopo   = "lab.save_topology"
	LabDelete     = "lab.delete"
	LabManageAcc  = "lab.manage_access"
	LabWipe       = "lab.wipe"
	MachAdd       = "machine.add"
	MachDelete    = "machine.delete"
	MachEdit      = "machine.edit"
	MachMove      = "machine.move"
	MachClone     = "machine.clone"
	MachStart     = "machine.start"
	MachStop      = "machine.stop"
	MachRestart   = "machine.restart"
	MachWipe      = "machine.wipe"
	MachSnapshot  = "machine.snapshot"
	LinkCreate    = "link.create"
	LinkDelete    = "link.delete"
	NetManage     = "net.manage"
	IfaceManage   = "iface.manage"
	DiskAttach    = "disk.attach"
	DiskDetach    = "disk.detach"
	ConsoleSerial = "console.serial"
	ConsoleVNC    = "console.vnc"
	// ConsoleClipboard allows the copy/paste button in the console: the clipboard panel,
	// pasting text into the machine and receiving text from it. The administrator and lab
	// owner get the permission automatically; for others an administrator grants it.
	ConsoleClipboard = "console.clipboard"
)

// Perm - a permission description for the management interface.
type Perm struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Group string `json:"group"`
}

// Catalog - all permissions in display order (groups are consecutive).
var Catalog = []Perm{
	{LabView, "Просмотр лаборатории и топологии", "Лаборатория"},
	{LabEditMeta, "Изменение названия и описания", "Лаборатория"},
	{LabSaveTopo, "Сохранение вида топологии", "Лаборатория"},
	{LabExport, "Экспорт и копирование лаборатории", "Лаборатория"},
	{LabDelete, "Удаление лаборатории", "Лаборатория"},
	{LabManageAcc, "Управление правами других пользователей", "Лаборатория"},
	{LabWipe, "Wipe лаборатории (сброс всех машин)", "Лаборатория"},
	{MachAdd, "Добавление машин", "Машины"},
	{MachEdit, "Изменение параметров машин", "Машины"},
	{MachMove, "Перемещение по рабочему полю", "Машины"},
	{MachClone, "Клонирование машин", "Машины"},
	{MachDelete, "Удаление машин", "Машины"},
	{MachStart, "Запуск машин", "Машины"},
	{MachStop, "Остановка и пауза", "Машины"},
	{MachRestart, "Перезагрузка машин", "Машины"},
	{MachWipe, "Wipe (сброс состояния машины)", "Машины"},
	{MachSnapshot, "Снимки состояния", "Машины"},
	{LinkCreate, "Создание проводов", "Провода и сети"},
	{LinkDelete, "Удаление проводов", "Провода и сети"},
	{IfaceManage, "Сетевые интерфейсы машины (число NIC)", "Провода и сети"},
	{NetManage, "Сети: создание, настройка, удаление", "Провода и сети"},
	{DiskAttach, "Подключение дисков и образов", "Диски и консоли"},
	{DiskDetach, "Отключение дисков", "Диски и консоли"},
	{ConsoleSerial, "Консоль Serial (терминал)", "Диски и консоли"},
	{ConsoleVNC, "Консоль VNC (экран)", "Диски и консоли"},
	{ConsoleClipboard, "Копирование и вставка в консоли", "Диски и консоли"},
}

var known = func() map[string]string {
	m := map[string]string{}
	for _, p := range Catalog {
		m[p.Key] = p.Title
	}
	return m
}()

// Known reports whether the permission is in the catalog.
func Known(key string) bool { _, ok := known[key]; return ok }

// Title returns the permission label for error messages.
func Title(key string) string {
	if t, ok := known[key]; ok {
		return t
	}
	return key
}

// Querier - minimal query interface (*database.DB and transactions fit).
type Querier interface {
	QueryRow(query string, args ...any) *sql.Row
}

// ValidPublic - public access: none or view for all system users.
func ValidPublic(level string) bool { return level == "none" || level == "view" }

// DefaultPublic - public access for a new lab: administrator labs are visible to everyone.
func DefaultPublic(q Querier, ownerID int64) string {
	var role string
	if q.QueryRow(`SELECT role FROM users WHERE id=?`, ownerID).Scan(&role) == nil && role == "admin" {
		return "view"
	}
	return "none"
}

// roleLabGrants translates role permissions into lab access keys.
// A role defines capabilities, while lab visibility is determined by the role's folders and public access.
var roleLabGrants = map[string][]string{
	permcatalog.LabView:       {LabView},
	permcatalog.LabEdit:       {LabEditMeta, LabSaveTopo, MachEdit, MachMove, LinkCreate, LinkDelete, IfaceManage, DiskAttach, DiskDetach, MachSnapshot},
	permcatalog.LabDelete:     {LabDelete, MachDelete},
	permcatalog.LabCopy:       {LabExport, MachClone},
	permcatalog.LabStartStop:  {MachStart, MachStop},
	permcatalog.LabWipe:       {LabWipe, MachWipe},
	permcatalog.LabTopology:   {LabSaveTopo, MachAdd, MachEdit, MachMove, MachClone, LinkCreate, LinkDelete, IfaceManage, NetManage, DiskAttach, DiskDetach, MachSnapshot},
	permcatalog.MachStartStop: {MachStart, MachStop},
	permcatalog.MachRestart:   {MachRestart},
	permcatalog.MachWipe:      {MachWipe},
	permcatalog.MachSerial:    {ConsoleSerial},
	permcatalog.MachVNC:       {ConsoleVNC},
	permcatalog.MachClipboard: {ConsoleClipboard},
}

// UserRoleInfo - a user's role in the permission cache.
type UserRoleInfo struct {
	ID   int64
	Name string
}

// PermissionSnapshot - data derived from users/roles/role_permissions/user_roles for one
// user. The database remains the source of truth: the snapshot only caches query results.
type PermissionSnapshot struct {
	Roles          []UserRoleInfo
	RoleIDs        []int64
	Permissions    []string
	LabGrants      map[string]bool
	MaxRunningLabs int
}

var (
	permCacheMu sync.RWMutex
	// The key is the unique database instance number: the *database.DB address may be reused by a new database.
	permCaches = map[uint64]map[int64]*PermissionSnapshot{}
	// permEpoch grows on every reset: a snapshot read before the reset will not enter the cache after it.
	permEpoch = map[uint64]uint64{}
)

// UserPermissions returns cached roles, combined permissions, lab permissions
// by role and the user's simultaneous lab limit. On a miss, data is read from the database.
func UserPermissions(db *database.DB, userID int64) (*PermissionSnapshot, error) {
	key := db.Seq()
	permCacheMu.RLock()
	snap := permCaches[key][userID]
	epoch := permEpoch[key]
	permCacheMu.RUnlock()
	if snap != nil {
		return snap, nil
	}

	snap, err := loadPermissions(db, userID)
	if err != nil {
		return nil, err
	}
	permCacheMu.Lock()
	// If the cache was reset during the read, the snapshot may be stale: return it to this request,
	// but do not store it - the next request will read the database again.
	if permEpoch[key] == epoch {
		if permCaches[key] == nil {
			permCaches[key] = map[int64]*PermissionSnapshot{}
		}
		permCaches[key][userID] = snap
	}
	permCacheMu.Unlock()
	return snap, nil
}

func loadPermissions(db *database.DB, userID int64) (*PermissionSnapshot, error) {
	rows, err := db.Query(`SELECT r.id, r.name, r.max_running_labs, COALESCE(rp.permission, '')
		FROM user_roles ur
		JOIN roles r ON r.id = ur.role_id
		LEFT JOIN role_permissions rp ON rp.role_id = r.id
		WHERE ur.user_id = ?
		ORDER BY r.name COLLATE NOCASE, r.id, rp.permission`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	snap := &PermissionSnapshot{LabGrants: map[string]bool{}}
	seenRole := map[int64]bool{}
	seenPerm := map[string]bool{}
	for rows.Next() {
		var id int64
		var name, perm string
		var limit int
		if err := rows.Scan(&id, &name, &limit, &perm); err != nil {
			return nil, err
		}
		if !seenRole[id] {
			seenRole[id] = true
			snap.Roles = append(snap.Roles, UserRoleInfo{ID: id, Name: name})
			snap.RoleIDs = append(snap.RoleIDs, id)
			if limit > 0 && (snap.MaxRunningLabs == 0 || limit < snap.MaxRunningLabs) {
				snap.MaxRunningLabs = limit
			}
		}
		if perm != "" && !seenPerm[perm] {
			seenPerm[perm] = true
			snap.Permissions = append(snap.Permissions, perm)
			for _, key := range roleLabGrants[perm] {
				snap.LabGrants[key] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return snap, nil
}

// InvalidateUserPermissions deletes the user's permission cache. Called immediately after changing
// roles, role assignments or user activity.
func InvalidateUserPermissions(db *database.DB, userID int64) {
	key := db.Seq()
	permCacheMu.Lock()
	permEpoch[key]++
	if c := permCaches[key]; c != nil {
		delete(c, userID)
	}
	permCacheMu.Unlock()
}

// InvalidateAllPermissions deletes the permission cache of all users (e.g. after a role change).
func InvalidateAllPermissions(db *database.DB) {
	key := db.Seq()
	permCacheMu.Lock()
	permEpoch[key]++
	delete(permCaches, key)
	permCacheMu.Unlock()
}

// RoleLabGrants returns the combined lab permissions granted by the user's roles.
func RoleLabGrants(db *database.DB, userID int64) (map[string]bool, error) {
	snap, err := UserPermissions(db, userID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(snap.LabGrants))
	for k, v := range snap.LabGrants {
		out[k] = v
	}
	return out, nil
}

// RunningLabLimit returns the limit on simultaneously running labs from the user's roles:
// 0 - no limit. If several roles set a limit, the strictest (smallest) applies.
func RunningLabLimit(db *database.DB, userID int64) (int, error) {
	snap, err := UserPermissions(db, userID)
	if err != nil {
		return 0, err
	}
	return snap.MaxRunningLabs, nil
}

// HasRolePermission reports whether the catalog permission is granted by at least one of the user's roles.
func HasRolePermission(db *database.DB, userID int64, key string) (bool, error) {
	snap, err := UserPermissions(db, userID)
	if err != nil {
		return false, err
	}
	for _, p := range snap.Permissions {
		if p == key {
			return true, nil
		}
	}
	return false, nil
}

// UserHasRoles reports whether the user has at least one role assigned.
func UserHasRoles(db *database.DB, userID int64) (bool, error) {
	snap, err := UserPermissions(db, userID)
	if err != nil {
		return false, err
	}
	return len(snap.RoleIDs) > 0, nil
}

// Compose computes the user's effective permissions from lab data and their explicit permissions.
func Compose(admin bool, userID, ownerID int64, publicLevel string, grants map[string]bool) map[string]bool {
	out := map[string]bool{}
	if admin || userID == ownerID {
		for _, p := range Catalog {
			out[p.Key] = true
		}
		return out
	}
	if publicLevel == "view" {
		out[LabView] = true
	}
	for k, v := range grants {
		if v && Known(k) {
			out[k] = true
		}
	}
	return out
}

// Grants returns the user's explicit permissions across all labs (lab_id -> permission -> value).
func Grants(db *database.DB, userID int64) (map[int64]map[string]bool, error) {
	out := map[int64]map[string]bool{}
	for _, l := range db.Labs.Labs() {
		for _, p := range l.Permissions {
			if p.UserID != userID || !p.Value {
				continue
			}
			if out[l.ID] == nil {
				out[l.ID] = map[string]bool{}
			}
			out[l.ID][p.Perm] = true
		}
	}
	return out, nil
}

// UserRoleIDs returns the user's roles.
func UserRoleIDs(db *database.DB, uid int64) ([]int64, error) {
	snap, err := UserPermissions(db, uid)
	if err != nil {
		return nil, err
	}
	out := make([]int64, len(snap.RoleIDs))
	copy(out, snap.RoleIDs)
	return out, nil
}

// FolderAllowed reports whether the lab is available to the user by folder placement: a lab without
// a folder is available, a lab in a folder - only if the folder (or its ancestor) is assigned to the user directly
// or through roles. The caller checks that the lab exists.
func FolderAllowed(db *database.DB, labID, userID int64) (bool, error) {
	folder, placed := db.Folders.Placement(labID)
	if !placed {
		return true, nil
	}
	roles, err := UserRoleIDs(db, userID)
	if err != nil {
		return false, err
	}
	return db.Folders.VisibleFolders(userID, roles)[folder], nil
}

// FolderMemberLabs returns labs located in folders available to the user directly or through
// roles. The contents of an assigned folder are visible to role members, even if the lab has no public access.
func FolderMemberLabs(db *database.DB, userID int64) (map[int64]bool, error) {
	roles, err := UserRoleIDs(db, userID)
	if err != nil {
		return nil, err
	}
	visible := db.Folders.VisibleFolders(userID, roles)
	out := map[int64]bool{}
	for lab, folder := range db.Folders.Placements() {
		if visible[folder] {
			out[lab] = true
		}
	}
	return out, nil
}

// Perms returns the user's effective permissions on the lab. If the lab does not exist - sql.ErrNoRows.
func Perms(db *database.DB, labID, userID int64, admin bool) (map[string]bool, error) {
	l, ok := db.Labs.Lab(labID)
	if !ok {
		return nil, sql.ErrNoRows
	}
	// Folders are assigned by an administrator: a lab in someone else's (unassigned) folder does not exist for the user.
	if !admin {
		allowed, err := FolderAllowed(db, labID, userID)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, sql.ErrNoRows
		}
	}
	grants := map[string]bool{}
	if !admin && userID != l.OwnerID {
		for _, p := range l.Permissions {
			if p.UserID == userID && p.Value {
				grants[p.Perm] = true
			}
		}
	}
	if !admin {
		roleGrants, err := RoleLabGrants(db, userID)
		if err != nil {
			return nil, err
		}
		for p := range roleGrants {
			grants[p] = true
		}
	}
	return Compose(admin, userID, l.OwnerID, l.PublicLevel, grants), nil
}

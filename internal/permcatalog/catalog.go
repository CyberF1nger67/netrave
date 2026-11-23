// SPDX-License-Identifier: GPL-3.0-only

// Package permcatalog - the catalog of user role permissions.
//
// The catalog does not depend on the database: it is used by seed and the roles package.
// Permission keys are stable and stored in role_permissions. Lab permissions
// are additionally translated into access package keys (see access.RoleLabGrants).
package permcatalog

// Catalog groups in display order.
const (
	GroupLabs     = "labs"
	GroupFolders  = "folders"
	GroupMachines = "machines"
	GroupUsers    = "users"
	GroupExtra    = "extra"
)

// Role permissions.
const (
	LabView      = "laboratories.view"
	LabCreate    = "laboratories.create"
	LabEdit      = "laboratories.edit"
	LabDelete    = "laboratories.delete"
	LabCopy      = "laboratories.copy"
	LabStartStop = "laboratories.start_stop"
	LabWipe      = "laboratories.wipe"
	LabTopology  = "laboratories.topology"

	FolderView     = "folders.view"
	FolderAccess   = "folders.access"
	FolderMoveLabs = "folders.move_labs"
	FolderManage   = "folders.manage"

	MachStartStop = "machines.start_stop"
	MachRestart   = "machines.restart"
	MachWipe      = "machines.wipe"
	MachSerial    = "machines.serial"
	MachVNC       = "machines.vnc"
	MachClipboard = "machines.clipboard"

	UsersView     = "users.view"
	UsersSettings = "users.settings"
	UsersPassword = "users.password"
	UsersManage   = "users.manage"

	NotificationsView = "notifications.view"
	SettingsOwn       = "settings.own"
	ThemesCreate      = "themes.create"
	AdminAccess       = "admin.access"

	// Sections that were previously granted directly to a user (users.perms). Now - only through roles.
	SectionDashboard = "dashboard"
	SectionLogs      = "logs"
	SectionTemplates = "templates"
	SectionImages    = "images"
)

// Perm - a permission description for the UI.
type Perm struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Group    string `json:"group"`
	GroupKey string `json:"group_key"`
}

// Catalog - all role permissions in display order.
var Catalog = []Perm{
	{LabView, "View laboratories", "Laboratories", GroupLabs},
	{LabCreate, "Create laboratories", "Laboratories", GroupLabs},
	{LabEdit, "Edit laboratories", "Laboratories", GroupLabs},
	{LabDelete, "Delete laboratories", "Laboratories", GroupLabs},
	{LabCopy, "Copy laboratories", "Laboratories", GroupLabs},
	{LabStartStop, "Start and stop machines", "Laboratories", GroupLabs},
	{LabWipe, "Wipe machines", "Laboratories", GroupLabs},
	{LabTopology, "Manage topology", "Laboratories", GroupLabs},

	{FolderView, "View assigned folders", "Folders", GroupFolders},
	{FolderAccess, "Access folder contents", "Folders", GroupFolders},
	{FolderMoveLabs, "Move laboratories between folders", "Folders", GroupFolders},
	{FolderManage, "Manage folders", "Folders", GroupFolders},

	{MachStartStop, "Start and stop", "Virtual machines", GroupMachines},
	{MachRestart, "Restart", "Virtual machines", GroupMachines},
	{MachWipe, "Wipe", "Virtual machines", GroupMachines},
	{MachSerial, "Serial console", "Virtual machines", GroupMachines},
	{MachVNC, "VNC console", "Virtual machines", GroupMachines},
	{MachClipboard, "Console clipboard (copy and paste)", "Virtual machines", GroupMachines},

	{UsersView, "View users", "Users", GroupUsers},
	{UsersSettings, "Change user settings", "Users", GroupUsers},
	{UsersPassword, "Change own password", "Users", GroupUsers},
	{UsersManage, "Manage users", "Users", GroupUsers},

	{NotificationsView, "View notifications", "Additional capabilities", GroupExtra},
	{SettingsOwn, "Manage own settings", "Additional capabilities", GroupExtra},
	{ThemesCreate, "Create own themes", "Additional capabilities", GroupExtra},
	{AdminAccess, "Access administrative sections", "Additional capabilities", GroupExtra},
	{SectionDashboard, "Dashboard", "Sections", GroupExtra},
	{SectionLogs, "Event log", "Sections", GroupExtra},
	{SectionTemplates, "Machine templates", "Sections", GroupExtra},
	{SectionImages, "Disk images", "Sections", GroupExtra},
}

var known = func() map[string]Perm {
	m := make(map[string]Perm, len(Catalog))
	for _, p := range Catalog {
		m[p.Key] = p
	}
	return m
}()

// Known reports whether the permission exists.
func Known(key string) bool { _, ok := known[key]; return ok }

// Title returns the permission label.
func Title(key string) string {
	if p, ok := known[key]; ok {
		return p.Title
	}
	return key
}

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
	{LabView, "Просмотр лабораторий", "Лаборатории", GroupLabs},
	{LabCreate, "Создание лабораторий", "Лаборатории", GroupLabs},
	{LabEdit, "Редактирование лабораторий", "Лаборатории", GroupLabs},
	{LabDelete, "Удаление лабораторий", "Лаборатории", GroupLabs},
	{LabCopy, "Копирование лабораторий", "Лаборатории", GroupLabs},
	{LabStartStop, "Запуск и остановка машин", "Лаборатории", GroupLabs},
	{LabWipe, "Wipe машин", "Лаборатории", GroupLabs},
	{LabTopology, "Управление топологией", "Лаборатории", GroupLabs},

	{FolderView, "Просмотр назначенных папок", "Папки", GroupFolders},
	{FolderAccess, "Доступ к содержимому папок", "Папки", GroupFolders},
	{FolderMoveLabs, "Перемещение лабораторий между папками", "Папки", GroupFolders},
	{FolderManage, "Управление папками", "Папки", GroupFolders},

	{MachStartStop, "Запуск и остановка", "Виртуальные машины", GroupMachines},
	{MachRestart, "Перезагрузка", "Виртуальные машины", GroupMachines},
	{MachWipe, "Wipe", "Виртуальные машины", GroupMachines},
	{MachSerial, "Serial Console", "Виртуальные машины", GroupMachines},
	{MachVNC, "VNC Console", "Виртуальные машины", GroupMachines},
	{MachClipboard, "Буфер обмена в консоли", "Виртуальные машины", GroupMachines},

	{UsersView, "Просмотр пользователей", "Пользователи", GroupUsers},
	{UsersSettings, "Изменение пользовательских настроек", "Пользователи", GroupUsers},
	{UsersPassword, "Смена собственного пароля", "Пользователи", GroupUsers},
	{UsersManage, "Управление пользователями", "Пользователи", GroupUsers},

	{NotificationsView, "Просмотр уведомлений", "Дополнительные возможности", GroupExtra},
	{SettingsOwn, "Управление собственными настройками", "Дополнительные возможности", GroupExtra},
	{ThemesCreate, "Создание собственных тем оформления", "Дополнительные возможности", GroupExtra},
	{AdminAccess, "Доступ к административным разделам", "Дополнительные возможности", GroupExtra},
	{SectionDashboard, "Дашборд", "Разделы", GroupExtra},
	{SectionLogs, "Журнал событий", "Разделы", GroupExtra},
	{SectionTemplates, "Шаблоны машин", "Разделы", GroupExtra},
	{SectionImages, "Образы дисков", "Разделы", GroupExtra},
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

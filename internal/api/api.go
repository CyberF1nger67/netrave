// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"netrave/internal/access"
	"netrave/internal/auth"
	"netrave/internal/config"
	"netrave/internal/database"
	"netrave/internal/hoststat"
	"netrave/internal/iconlib"
	"netrave/internal/icons"
	"netrave/internal/ksm"
	"netrave/internal/notify"
	"netrave/internal/permcatalog"
	"netrave/internal/qemu"
)

// Handler — REST API.
type Handler struct {
	DB     *database.DB
	Auth   *auth.Service
	Cfg    *config.Config // system settings are stored in the configuration file
	Status func() map[string]any
	CPU    *hoststat.CPU   // server CPU load (for live dashboard cards)
	Notify *notify.Service // notification centre (nil - notifications are not stored)
	KSM    ksm.Service     // page merging: KSM and UKSM (nil - directly from the host's sysfs)
	M      *qemu.Manager   // running machine state - in memory
	// DropUser removes a user from labs (their ownership in labs is removed); set by the server.
	DropUser func(uid int64) error
}

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,32}$`)

// validPassword checks password length in characters, not bytes:
// Cyrillic takes two bytes in UTF-8, so len() would reject
// valid passwords longer than 64 characters.
func validPassword(p string) bool {
	n := utf8.RuneCountInString(p)
	return n >= 8 && n <= 128
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return false
	}
	return true
}

func (h *Handler) audit(r *http.Request, action, detail string) {
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.DB.Audit(ss.User.ID, ss.User.Username, action, detail, auth.ClientIP(r))
		return
	}
	h.DB.Audit(0, "", action, detail, auth.ClientIP(r))
}

// Register wires routes into mux.
func (h *Handler) Register(mux *http.ServeMux) {
	user := func(f http.HandlerFunc) http.Handler { return h.Auth.Require(f) }
	admin := func(f http.HandlerFunc) http.Handler { return h.Auth.RequireAdmin(f) }
	anyPerm := func(f http.HandlerFunc, perms ...string) http.Handler {
		return user(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := auth.FromContext(r.Context()).User
			for _, p := range perms {
				if u.Can(p) {
					f(w, r)
					return
				}
			}
			writeErr(w, http.StatusForbidden, "нет права: "+strings.Join(perms, " / "))
		}))
	}

	mux.HandleFunc("POST /api/login", h.login)
	mux.Handle("POST /api/logout", user(h.logout))
	mux.Handle("GET /api/me", user(h.me))
	mux.Handle("PUT /api/me/password", user(h.changePassword))
	mux.Handle("GET /api/me/theme", user(h.getTheme))
	mux.Handle("GET /api/me/theme/presets", user(h.themePresets))
	mux.Handle("PUT /api/me/theme", user(h.putTheme))
	mux.Handle("GET /api/system", h.Auth.RequirePerm("dashboard", http.HandlerFunc(h.system)))
	mux.Handle("GET /api/dashboard/live", h.Auth.RequirePerm("dashboard", http.HandlerFunc(h.live)))

	mux.Handle("GET /api/users", anyPerm(h.listUsers, "users.view", "users.settings", "users.manage"))
	mux.Handle("POST /api/users", anyPerm(h.createUser, "users.manage"))
	mux.Handle("PUT /api/users/{id}", anyPerm(h.updateUser, "users.settings", "users.manage"))
	mux.Handle("DELETE /api/users/{id}", anyPerm(h.deleteUser, "users.manage"))

	mux.Handle("GET /api/settings", admin(h.getSettings))
	mux.Handle("PUT /api/settings", admin(h.putSettings))
	// Page merging affects the whole host, so only an administrator can enable it.
	mux.Handle("GET /api/ksm", admin(h.ksmStatus))
	mux.Handle("PUT /api/ksm", admin(h.ksmSet))
	mux.Handle("GET /api/logs", h.Auth.RequirePerm("logs", http.HandlerFunc(h.logs)))
	mux.Handle("DELETE /api/logs", h.Auth.RequirePerm("logs", http.HandlerFunc(h.clearLogs)))
}

// ---------- authorization ----------

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	ip := auth.ClientIP(r)
	if !auth.SameOrigin(r) {
		writeErr(w, http.StatusForbidden, "недопустимый Origin")
		return
	}
	if !h.Auth.Allow(ip) {
		writeErr(w, http.StatusTooManyRequests, "слишком много попыток, попробуйте позже")
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	u, hash, err := h.Auth.FindByUsername(strings.TrimSpace(in.Username))
	ok := false
	if err != nil {
		auth.VerifyPassword(in.Password, auth.DummyHash)
	} else {
		ok = auth.VerifyPassword(in.Password, hash) && !u.Disabled
	}
	if !ok {
		h.Auth.Fail(ip)
		h.DB.Audit(0, in.Username, "login_failed", "", ip)
		h.Notify.SendAdmins(notify.Warning, notify.CatSecurity, "Неудачная попытка входа",
			"имя «"+in.Username+"», адрес "+ip, "/journal")
		writeErr(w, http.StatusUnauthorized, "неверное имя пользователя или пароль")
		return
	}
	h.Auth.Reset(ip)
	token, csrf, exp, err := h.Auth.CreateSession(u.ID, ip)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "не удалось создать сессию")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: token, Path: "/", Expires: exp,
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
	h.DB.Audit(u.ID, u.Username, "login", "", ip)
	h.Notify.Activity(u.ID, 0, notify.Info, notify.CatSecurity, "Вход в систему", "адрес "+ip, "")
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "csrf": csrf})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	h.audit(r, "logout", "")
	h.Auth.DeleteSession(ss.ID)
	auth.ClearCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"user": ss.User, "csrf": ss.CSRF})
}

// canChangeOwnPassword: an administrator can always; users without roles
// keep the previous behaviour; once roles are assigned, password changes must be
// explicitly allowed by the role (users.password).
func canChangeOwnPassword(u auth.User) bool {
	if u.Role == "admin" {
		return true
	}
	if len(u.Roles) == 0 {
		return true
	}
	return u.HasRolePerm(permcatalog.UsersPassword)
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	if !canChangeOwnPassword(ss.User) {
		writeErr(w, http.StatusForbidden, "нет права: "+permcatalog.Title(permcatalog.UsersPassword))
		return
	}
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
		Confirm string `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !validPassword(in.New) {
		writeErr(w, http.StatusBadRequest, "новый пароль должен содержать от 8 до 128 символов")
		return
	}
	if in.Confirm != in.New {
		writeErr(w, http.StatusBadRequest, "пароли не совпадают")
		return
	}
	_, hash, err := h.Auth.FindByUsername(ss.User.Username)
	if err != nil || !auth.VerifyPassword(in.Current, hash) {
		writeErr(w, http.StatusForbidden, "текущий пароль указан неверно")
		return
	}
	nh, err := auth.HashPassword(in.New)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка хеширования")
		return
	}
	if _, err := h.DB.Exec(`UPDATE users SET password_hash=? WHERE id=?`, nh, ss.User.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	h.Auth.DeleteUserSessions(ss.User.ID, ss.ID)
	h.audit(r, "password_change", "")
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.Notify.Activity(ss.User.ID, 0, notify.Info, notify.CatSecurity, "Пароль изменён", "«"+ss.User.Username+"»", "/account")
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------- system ----------

func (h *Handler) system(w http.ResponseWriter, r *http.Request) {
	out := h.Status()
	count := func(q string) int {
		var n int
		_ = h.DB.QueryRow(q).Scan(&n)
		return n
	}
	running := 0
	if h.M != nil {
		running = h.M.RunningCount()
	}
	out["counts"] = map[string]int{
		"labs":            len(h.DB.Labs.Labs()),
		"devices":         len(h.DB.Labs.AllDevices()),
		"devices_running": running,
		"images":          len(h.DB.Catalog.Images()),
		"users":           count(`SELECT COUNT(*) FROM users`),
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------- users ----------

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`SELECT id,username,role,disabled,created_at FROM users ORDER BY id`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	defer rows.Close()
	list := []auth.User{}
	for rows.Next() {
		var u auth.User
		var dis int
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &dis, &u.CreatedAt); err != nil {
			continue
		}
		u.Disabled = dis != 0
		if err := auth.FillRoles(h.DB, &u); err != nil {
			continue
		}
		list = append(list, u)
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Confirm  string `json:"confirm"`
		Role     string `json:"role"`
		// RoleID is an additional role from the "Roles" section: assigned right after creation.
		RoleID int64 `json:"role_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !usernameRe.MatchString(in.Username) {
		writeErr(w, http.StatusBadRequest, "имя: 3–32 символа (буквы, цифры, _ . -)")
		return
	}
	if !validPassword(in.Password) {
		writeErr(w, http.StatusBadRequest, "пароль должен содержать от 8 до 128 символов")
		return
	}
	if in.Confirm != in.Password {
		writeErr(w, http.StatusBadRequest, "пароли не совпадают")
		return
	}
	if in.Role == "" {
		in.Role = "user"
	}
	if in.Role != "user" && in.Role != "admin" {
		writeErr(w, http.StatusBadRequest, "недопустимая роль")
		return
	}
	if ss.User.Role != "admin" {
		if in.Role != "user" || in.RoleID != 0 {
			writeErr(w, http.StatusForbidden, "только администратор может назначать роли")
			return
		}
		if !ss.User.Can("users.manage") {
			writeErr(w, http.StatusForbidden, "нет права: управление пользователями")
			return
		}
	}
	if in.RoleID != 0 {
		var n int
		if err := h.DB.QueryRow(`SELECT COUNT(*) FROM roles WHERE id=?`, in.RoleID).Scan(&n); err != nil {
			writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
		if n == 0 {
			writeErr(w, http.StatusBadRequest, "роль не найдена")
			return
		}
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка хеширования")
		return
	}
	res, err := h.DB.Exec(`INSERT INTO users(username,password_hash,role) VALUES(?,?,?)`, in.Username, hash, in.Role)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, "пользователь с таким именем уже существует")
			return
		}
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	id, _ := res.LastInsertId()
	if in.RoleID != 0 {
		if _, err := h.DB.Exec(`INSERT OR IGNORE INTO user_roles(user_id,role_id) VALUES(?,?)`, id, in.RoleID); err != nil {
			writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
	}
	h.audit(r, "user_create", in.Username+" ("+in.Role+")")
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.Notify.Activity(ss.User.ID, 0, notify.Success, notify.CatAccount, "Пользователь создан", in.Username+" ("+in.Role+")", "/users")
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// otherAdmins is the number of active administrators other than the specified user.
func (h *Handler) otherAdmins(exceptID int64) int {
	var n int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE role='admin' AND disabled=0 AND id<>?`, exceptID).Scan(&n)
	return n
}

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный id")
		return
	}
	var in struct {
		Role     *string `json:"role"`
		Disabled *bool   `json:"disabled"`
		Password *string `json:"password"`
		Confirm  *string `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	var curRole, name string
	var curDis int
	err = h.DB.QueryRow(`SELECT role,disabled,username FROM users WHERE id=?`, id).Scan(&curRole, &curDis, &name)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "пользователь не найден")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	newRole, newDis := curRole, curDis != 0
	if ss.User.Role != "admin" {
		if curRole == "admin" {
			writeErr(w, http.StatusForbidden, "нельзя изменять администратора")
			return
		}
		if in.Role != nil {
			writeErr(w, http.StatusForbidden, "только администратор может менять роль пользователя")
			return
		}
	}
	if in.Role != nil {
		if *in.Role != "user" && *in.Role != "admin" {
			writeErr(w, http.StatusBadRequest, "недопустимая роль")
			return
		}
		newRole = *in.Role
	}
	if in.Disabled != nil {
		newDis = *in.Disabled
	}
	if id == ss.User.ID {
		if ss.User.Role == "admin" && (newRole != "admin" || newDis) {
			writeErr(w, http.StatusBadRequest, "нельзя понизить или отключить самого себя")
			return
		}
		if ss.User.Role != "admin" && newDis {
			writeErr(w, http.StatusBadRequest, "нельзя отключить самого себя")
			return
		}
	}
	if curRole == "admin" && (newRole != "admin" || newDis) && h.otherAdmins(id) == 0 {
		writeErr(w, http.StatusBadRequest, "в системе должен остаться хотя бы один активный администратор")
		return
	}
	var hash string
	if in.Password != nil {
		if !validPassword(*in.Password) {
			writeErr(w, http.StatusBadRequest, "пароль должен содержать от 8 до 128 символов")
			return
		}
		if in.Confirm == nil || *in.Confirm != *in.Password {
			writeErr(w, http.StatusBadRequest, "пароли не совпадают")
			return
		}
		if hash, err = auth.HashPassword(*in.Password); err != nil {
			writeErr(w, http.StatusInternalServerError, "ошибка хеширования")
			return
		}
	}
	tx, err := h.DB.Begin()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	defer tx.Rollback()
	dis := 0
	if newDis {
		dis = 1
	}
	if _, err = tx.Exec(`UPDATE users SET role=?, disabled=? WHERE id=?`, newRole, dis, id); err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if hash != "" {
		if _, err = tx.Exec(`UPDATE users SET password_hash=? WHERE id=?`, hash, id); err != nil {
			writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if newDis || hash != "" {
		h.Auth.DeleteUserSessions(id, "")
	}
	access.InvalidateUserPermissions(h.DB, id)
	h.audit(r, "user_update", name)
	h.notifyUserChanged(ss, id, curRole, newRole, curDis != 0, newDis, hash != "" && id == ss.User.ID)
	if hash != "" && id != ss.User.ID {
		h.Notify.Send(id, notify.Warning, notify.CatSecurity, "Администратор изменил ваш пароль",
			"Дата и время: "+time.Now().Format("02.01.2006 15:04")+". Вам нужно войти заново.", "/account")
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный id")
		return
	}
	if id == ss.User.ID {
		writeErr(w, http.StatusBadRequest, "нельзя удалить самого себя")
		return
	}
	var role, name string
	err = h.DB.QueryRow(`SELECT role,username FROM users WHERE id=?`, id).Scan(&role, &name)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "пользователь не найден")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if role == "admin" && ss.User.Role != "admin" {
		writeErr(w, http.StatusForbidden, "нельзя удалить администратора")
		return
	}
	if role == "admin" && h.otherAdmins(id) == 0 {
		writeErr(w, http.StatusBadRequest, "нельзя удалить последнего администратора")
		return
	}
	if h.DropUser != nil {
		if err := h.DropUser(id); err != nil {
			writeErr(w, http.StatusInternalServerError, "не удалось удалить лаборатории пользователя")
			return
		}
	}
	if _, err := h.DB.Exec(`DELETE FROM users WHERE id=?`, id); err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	access.InvalidateUserPermissions(h.DB, id)
	if err := h.DB.SweepSnapshots(); err != nil {
		writeErr(w, http.StatusInternalServerError, "не удалось удалить снимки пользователя")
		return
	}
	_ = h.DB.Folders.RemoveUser(id) // folder assignments are stored in the folder file
	if h.Cfg != nil {
		_ = RemoveTheme(h.Cfg.ThemesDir, id) // theme is a file; an error does not cancel user deletion
		_ = iconlib.RemoveOwner(icons.New(h.Cfg.IconsDir), id)
	}
	h.audit(r, "user_delete", name)
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.Notify.Activity(ss.User.ID, 0, notify.Warning, notify.CatAccount, "Пользователь удалён", name, "/users")
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------- settings ----------

func (h *Handler) getSettings(w http.ResponseWriter, r *http.Request) {
	s := h.Cfg.SettingsSnapshot()
	writeJSON(w, http.StatusOK, map[string]string{
		"session_ttl_hours": strconv.Itoa(s.SessionTTLHours),
		"max_upload_mb":     strconv.Itoa(s.MaxUploadMB),
	})
}

func (h *Handler) putSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if !decode(w, r, &in) {
		return
	}
	for k, v := range in {
		known, ok := config.SettingValid(k, v)
		if !known {
			writeErr(w, http.StatusBadRequest, "неизвестная настройка: "+k)
			return
		}
		if !ok {
			writeErr(w, http.StatusBadRequest, "недопустимое значение для "+k)
			return
		}
	}
	if err := h.Cfg.UpdateSettings(in); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	h.audit(r, "settings_update", "")
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.Notify.Activity(ss.User.ID, 0, notify.Info, notify.CatSettings, "Системные настройки изменены", "«"+ss.User.Username+"»", "/settings")
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------- page merging (ksm/uksm) ----------

// ksmService returns page-merging control: the passed service or the host's sysfs.
func (h *Handler) ksmService() ksm.Service {
	if h.KSM != nil {
		return h.KSM
	}
	return ksm.SYSFS
}

func (h *Handler) ksmStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.ksmService().Status())
}

// ksmSet enables or disables one mechanism: {"mechanism": "ksm"|"uksm", "enabled": true|false}.
// Enabling one mechanism always disables the other, so KSM and UKSM never run at the same time.
func (h *Handler) ksmSet(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Mechanism string `json:"mechanism"`
		Enabled   *bool  `json:"enabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Mechanism != ksm.KindKSM && in.Mechanism != ksm.KindUKSM {
		writeErr(w, http.StatusBadRequest, "укажите mechanism: ksm или uksm")
		return
	}
	if in.Enabled == nil {
		writeErr(w, http.StatusBadRequest, "укажите enabled: true или false")
		return
	}
	if err := h.ksmService().Set(in.Mechanism, *in.Enabled); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	title := map[string]string{ksm.KindKSM: "KSM", ksm.KindUKSM: "UKSM"}[in.Mechanism]
	state := map[bool]string{true: "включён", false: "выключен"}[*in.Enabled]
	h.audit(r, "ksm_"+in.Mechanism, state)
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.Notify.Activity(ss.User.ID, 0, notify.Info, notify.CatSettings, "Слияние страниц памяти: "+title, state, "/settings")
	}
	writeJSON(w, http.StatusOK, h.ksmService().Status())
}

// ---------- journal ----------

func (h *Handler) logs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	list, err := h.DB.Journal.Tail(limit, nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка чтения журнала")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// clearLogs clears the journal. Permission is checked in the route, as for reading.
func (h *Handler) clearLogs(w http.ResponseWriter, r *http.Request) {
	if err := h.DB.Journal.Clear(); err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка записи журнала")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// notifyUserChanged tells the user which of their parameters an administrator changed.
// Only the changed user receives the notification, and only if the change was made by someone else.
func (h *Handler) notifyUserChanged(ss *auth.Session, id int64, oldRole, newRole string, oldDis, newDis, pass bool) {
	if ss == nil {
		return
	}
	var parts []string
	level := notify.Info
	if oldRole != newRole {
		if newRole == "admin" {
			parts = append(parts, "роль: администратор")
		} else {
			parts = append(parts, "роль: пользователь")
		}
		level = notify.Warning
	}
	if oldDis != newDis {
		if newDis {
			parts = append(parts, "учётная запись отключена (активные сессии завершены)")
			level = notify.Error
		} else {
			parts = append(parts, "учётная запись снова активна")
			level = notify.Success
		}
	}
	if pass {
		parts = append(parts, "пароль (вам нужно войти заново)")
		level = notify.Warning
	}
	if len(parts) == 0 {
		return
	}
	// The recipient is strictly the account being changed. The initiator is mentioned in the text: otherwise
	// an administrator changing another user's permissions would receive a notification about themselves.
	title := "Ваш аккаунт изменён"
	by := "Инициатор: «" + ss.User.Username + "»."
	if id == ss.User.ID {
		title = "Вы изменили свой аккаунт"
		by = "Изменения внесены в ваш собственный аккаунт."
	}
	h.Notify.Send(id, level, notify.CatAccount, title,
		"Изменено: "+strings.Join(parts, "; ")+". "+by, "/account")
}

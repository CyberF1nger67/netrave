// SPDX-License-Identifier: GPL-3.0-only

// Package notify is NetRave's unified notification centre.
//
// Notifications are stored in process memory: a separate list per user. The list lives while
// the user is active: if they are inactive longer than IdleTTL and not connected via WebSocket, their list
// is released. After a server restart notifications are not preserved.
//
// Each notification belongs to one user. New notifications are immediately delivered to that
// user over WebSocket (/ws/notifications) if they are connected.
//
// Addressing: the initiator of an action and the object owner see the event themselves; administrators see
// all users' events (method Activity). Sending never breaks the operation that triggered it.
package notify

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"netrave/internal/auth"
	"netrave/internal/console"
	"netrave/internal/database"
	"netrave/internal/httpx"
)

// KeepPerUser is how many recent notifications are kept for one user.
const KeepPerUser = 300

// IdleTTL is how long an inactive user (no requests and no WebSocket) keeps their notifications in memory.
const IdleTTL = time.Hour

// Severity levels.
const (
	Info    = "info"
	Success = "success"
	Warning = "warning"
	Error   = "error"
)

// Event categories. Keys are used by the UI filter.
const (
	CatMachine  = "machine"  // start, stop, create, update, delete of machines, snapshots, disks
	CatLab      = "lab"      // labs: creation, parameters, deletion, wipe
	CatCable    = "cable"    // network cables (wires)
	CatNetwork  = "network"  // networks: NAT, isolated, Cloud; network connection errors
	CatImage    = "image"    // disk images
	CatTemplate = "template" // device templates
	CatSettings = "settings" // system settings
	CatAccount  = "account"  // users: creation, changes, passwords, permissions
	CatSecurity = "security" // logins and logouts
	CatBackup   = "backup"   // backups and lab restoration
	CatSystem   = "system"   // platform errors and warnings
)

// Categories - all categories in display order, with labels for the UI.
var Categories = []struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}{
	{CatMachine, "Машины"}, {CatLab, "Лаборатории"}, {CatCable, "Кабели"}, {CatNetwork, "Сети"},
	{CatImage, "Образы"}, {CatTemplate, "Шаблоны"}, {CatBackup, "Резервные копии"}, {CatSettings, "Настройки"},
	{CatAccount, "Пользователи"}, {CatSecurity, "Вход и безопасность"}, {CatSystem, "Система"},
}

// Service sends notifications, broadcasts them over WebSocket and serves them via the HTTP API.
// Methods are safe to call on a nil receiver: notifications are then simply not stored.
type Service struct {
	DB  *database.DB // needed only for the recipient list (users and administrators)
	hub *hub
	mem *store
}

// New creates a notification service on top of the database.
func New(db *database.DB) *Service {
	return &Service{DB: db, hub: newHub(), mem: newStore()}
}

// Item is one notification in the API response and in a WebSocket message.
type Item struct {
	ID        int64  `json:"id"`
	Level     string `json:"level"`
	Category  string `json:"category"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Link      string `json:"link"`
	Read      bool   `json:"read"`
	CreatedAt int64  `json:"created_at"`
}

func normLevel(l string) string {
	switch l {
	case Info, Success, Warning, Error:
		return l
	}
	return Info
}

func normCategory(c string) string {
	for _, k := range Categories {
		if k.Key == c {
			return c
		}
	}
	return CatSystem
}

// cut truncates a string to n characters (in runes), keeping valid UTF-8.
func cut(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// ---------- in-memory storage ----------

// store - users' notifications in process memory.
type store struct {
	mu        sync.Mutex
	users     map[int64]*userBox
	lastPrune time.Time
}

// userBox - one user's notifications (oldest to newest) and their last activity time.
type userBox struct {
	items    []Item
	lastSeen time.Time
}

// lastID - shared notification number counter: a number is not reused even across users.
var lastID atomic.Int64

func newStore() *store { return &store{users: map[int64]*userBox{}} }

// box returns the user's list and marks them as active. Call under st.mu.
func (st *store) box(uid int64, now time.Time) *userBox {
	b := st.users[uid]
	if b == nil {
		b = &userBox{}
		st.users[uid] = b
	}
	b.lastSeen = now
	return b
}

// prune releases lists of inactive users who are not connected via WebSocket. Call under st.mu.
func (st *store) prune(now time.Time, connected func(int64) bool) {
	if now.Sub(st.lastPrune) < time.Minute {
		return
	}
	st.lastPrune = now
	for uid, b := range st.users {
		if now.Sub(b.lastSeen) > IdleTTL && !connected(uid) {
			delete(st.users, uid)
		}
	}
}

// add adds a notification. An identical unread notification is not duplicated (warnings do not pile up).
// Returns the stored notification and false if it is a duplicate.
func (st *store) add(uid int64, it Item, now time.Time, connected func(int64) bool) (Item, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.prune(now, connected)
	b := st.box(uid, now)
	for _, x := range b.items {
		if !x.Read && x.Level == it.Level && x.Category == it.Category && x.Title == it.Title && x.Body == it.Body && x.Link == it.Link {
			return Item{}, false
		}
	}
	it.ID = lastID.Add(1)
	it.CreatedAt = now.Unix()
	b.items = append(b.items, it)
	if over := len(b.items) - KeepPerUser; over > 0 {
		b.items = append([]Item(nil), b.items[over:]...)
	}
	return it, true
}

// list returns the user's notifications, newest first.
func (st *store) list(uid int64, now time.Time, connected func(int64) bool, unreadOnly bool, category string, limit int) []Item {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.prune(now, connected)
	b := st.box(uid, now)
	out := []Item{}
	for i := len(b.items) - 1; i >= 0 && len(out) < limit; i-- {
		it := b.items[i]
		if unreadOnly && it.Read {
			continue
		}
		if category != "" && it.Category != category {
			continue
		}
		out = append(out, it)
	}
	return out
}

// find returns the user's notification by number. Call under st.mu.
func (b *userBox) find(id int64) int {
	for i := range b.items {
		if b.items[i].ID == id {
			return i
		}
	}
	return -1
}

// setRead marks a notification; false - the user has no such notification.
func (st *store) setRead(uid, id int64, read bool, now time.Time) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	b := st.box(uid, now)
	i := b.find(id)
	if i < 0 {
		return false
	}
	b.items[i].Read = read
	return true
}

// readAll marks all of the user's notifications as read.
func (st *store) readAll(uid int64, now time.Time) {
	st.mu.Lock()
	defer st.mu.Unlock()
	b := st.box(uid, now)
	for i := range b.items {
		b.items[i].Read = true
	}
}

// remove deletes one notification; false - the user has no such notification.
func (st *store) remove(uid, id int64, now time.Time) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	b := st.box(uid, now)
	i := b.find(id)
	if i < 0 {
		return false
	}
	b.items = append(b.items[:i], b.items[i+1:]...)
	return true
}

// clearRead deletes read notifications and returns their count.
func (st *store) clearRead(uid int64, now time.Time) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	b := st.box(uid, now)
	kept := b.items[:0]
	removed := 0
	for _, it := range b.items {
		if it.Read {
			removed++
			continue
		}
		kept = append(kept, it)
	}
	b.items = kept
	return removed
}

// unread returns the number of unread notifications.
func (st *store) unread(uid int64, now time.Time) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	n := 0
	for _, it := range st.box(uid, now).items {
		if !it.Read {
			n++
		}
	}
	return n
}

// Snapshot returns all of the user's notifications currently held in memory, oldest to newest.
// Used for checks and diagnostics.
func (s *Service) Snapshot(uid int64) []Item {
	if s == nil || s.mem == nil {
		return nil
	}
	s.mem.mu.Lock()
	defer s.mem.mu.Unlock()
	b := s.mem.users[uid]
	if b == nil {
		return nil
	}
	return append([]Item(nil), b.items...)
}

// ---------- sending ----------

// Send stores a notification for one user and broadcasts it over WebSocket.
func (s *Service) Send(userID int64, level, category, title, body, link string) {
	if s == nil || userID == 0 {
		return
	}
	it := Item{
		Level: normLevel(level), Category: normCategory(category),
		Title: cut(title, 200), Body: cut(body, 2000), Link: cut(link, 200),
	}
	saved, ok := s.mem.add(userID, it, time.Now(), s.hub.connected)
	if !ok {
		return
	}
	s.publish(userID, saved)
}

// Activity reports an event to the initiator and the object owner, as well as to all administrators.
// actorID or ownerID may be 0 (unknown); duplicate recipients are filtered out.
func (s *Service) Activity(actorID, ownerID int64, level, category, title, body, link string) {
	if s == nil || s.DB == nil {
		return
	}
	seen := map[int64]bool{}
	targets := append([]int64{actorID, ownerID}, s.ids(`SELECT id FROM users WHERE role='admin' AND disabled=0`)...)
	for _, uid := range targets {
		if uid == 0 || seen[uid] {
			continue
		}
		seen[uid] = true
		s.Send(uid, level, category, title, body, link)
	}
}

// SendAdmins sends a notification to all active administrators.
func (s *Service) SendAdmins(level, category, title, body, link string) {
	for _, uid := range s.ids(`SELECT id FROM users WHERE role='admin' AND disabled=0`) {
		s.Send(uid, level, category, title, body, link)
	}
}

// SendActive sends a notification to all active users except exceptUserID (0 - exclude no one).
func (s *Service) SendActive(exceptUserID int64, level, category, title, body, link string) {
	for _, uid := range s.ids(`SELECT id FROM users WHERE disabled=0 AND id<>?`, exceptUserID) {
		s.Send(uid, level, category, title, body, link)
	}
}

// SendInstance sends a notification to the lab instance owner (lab_instances.user_id).
func (s *Service) SendInstance(instanceID int64, level, category, title, body, link string) {
	if s == nil || s.DB == nil {
		return
	}
	var uid int64
	if err := s.DB.QueryRow(`SELECT user_id FROM lab_instances WHERE id=?`, instanceID).Scan(&uid); err != nil {
		return
	}
	s.Send(uid, level, category, title, body, link)
}

// ids reads a list of identifiers from the query (errors are logged, the list is empty).
func (s *Service) ids(query string, args ...any) []int64 {
	if s == nil || s.DB == nil {
		return nil
	}
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		log.Printf("уведомления: выборка получателей: %v", err)
		return nil
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

// ---------- WebSocket: real-time delivery ----------

// WebSocket messages: "new" - a new notification, "sync" - the list changed (read/deleted) in another tab.
type wsMsg struct {
	Type   string `json:"type"`
	Unread int    `json:"unread"`
	Item   *Item  `json:"item,omitempty"`
}

type client struct {
	conn *console.Conn
	out  chan []byte
}

type hub struct {
	mu      sync.Mutex
	clients map[int64]map[*client]struct{}
}

func newHub() *hub { return &hub{clients: map[int64]map[*client]struct{}{}} }

func (hb *hub) add(uid int64, c *client) {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	if hb.clients[uid] == nil {
		hb.clients[uid] = map[*client]struct{}{}
	}
	hb.clients[uid][c] = struct{}{}
}

func (hb *hub) remove(uid int64, c *client) {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	if set := hb.clients[uid]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(hb.clients, uid)
		}
	}
}

// deliver queues a message for each of the user's connections. A slow connection skips
// the message (its client will reconcile the list the next time the panel opens).
func (hb *hub) deliver(uid int64, msg []byte) {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	for c := range hb.clients[uid] {
		select {
		case c.out <- msg:
		default:
		}
	}
}

func (hb *hub) connected(uid int64) bool {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	return len(hb.clients[uid]) > 0
}

// publish broadcasts a new notification and the current unread count to the user's connected tabs.
func (s *Service) publish(uid int64, it Item) {
	if s == nil || s.hub == nil || !s.hub.connected(uid) {
		return
	}
	s.send(uid, wsMsg{Type: "new", Item: &it, Unread: s.unread(uid)})
}

// publishSync tells the user's tabs that the list has changed (e.g. marked as read).
func (s *Service) publishSync(uid int64) {
	if s == nil || s.hub == nil || !s.hub.connected(uid) {
		return
	}
	s.send(uid, wsMsg{Type: "sync", Unread: s.unread(uid)})
}

func (s *Service) send(uid int64, m wsMsg) {
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	s.hub.deliver(uid, b)
}

func (s *Service) unread(uid int64) int {
	return s.mem.unread(uid, time.Now())
}

// wsHandler opens a WebSocket for the current user. Access is by session cookie and Origin, like the rest of the API.
func (s *Service) wsHandler(as *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !auth.SameOrigin(r) {
			httpx.Err(w, http.StatusForbidden, "недопустимый Origin")
			return
		}
		c, err := r.Cookie(auth.CookieName)
		if err != nil || c.Value == "" {
			httpx.Err(w, http.StatusUnauthorized, "требуется авторизация")
			return
		}
		ss, err := as.Lookup(c.Value)
		if err != nil {
			httpx.Err(w, http.StatusUnauthorized, "сессия недействительна")
			return
		}
		conn, err := console.Upgrade(w, r)
		if err != nil {
			httpx.Err(w, http.StatusBadRequest, err.Error())
			return
		}
		cl := &client{conn: conn, out: make(chan []byte, 64)}
		uid := ss.User.ID
		s.hub.add(uid, cl)
		s.mem.unread(uid, time.Now()) // connection marks the user as active
		done := make(chan struct{})
		// Writer: messages from the queue and a periodic ping.
		go func() {
			t := time.NewTicker(25 * time.Second)
			defer t.Stop()
			for {
				select {
				case m := <-cl.out:
					if conn.WriteText(m) != nil {
						conn.Close()
						return
					}
				case <-t.C:
					if conn.Ping() != nil {
						conn.Close()
						return
					}
				case <-done:
					return
				}
			}
		}()
		// Reader: needed to see connection closure and respond to control frames.
		for {
			if err := conn.ReadMessage(); err != nil {
				break
			}
		}
		close(done)
		s.hub.remove(uid, cl)
		conn.Close()
	}
}

// ---------- HTTP API (only the user's own notifications) ----------

// Register wires up notification routes and the WebSocket.
func (s *Service) Register(mux *http.ServeMux, as *auth.Service) {
	user := func(f http.HandlerFunc) http.Handler { return as.Require(f) }
	mux.Handle("GET /api/notifications", user(s.list))
	mux.Handle("POST /api/notifications/read-all", user(s.readAll))
	mux.Handle("POST /api/notifications/clear-read", user(s.clearRead))
	mux.Handle("PUT /api/notifications/{id}", user(s.setRead))
	mux.Handle("DELETE /api/notifications/{id}", user(s.remove))
	mux.HandleFunc("GET /ws/notifications", s.wsHandler(as))
}

// list returns the user's notifications, the unread count and the category catalog.
// Parameters: limit (up to 300), unread=1 (unread only), category=<key> (category filter).
func (s *Service) list(w http.ResponseWriter, r *http.Request) {
	uid := auth.FromContext(r.Context()).User.ID
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > KeepPerUser {
		limit = 100
	}
	category := ""
	if c := r.URL.Query().Get("category"); c != "" {
		category = normCategory(c)
	}
	now := time.Now()
	items := s.mem.list(uid, now, s.hub.connected, r.URL.Query().Get("unread") == "1", category, limit)
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "unread": s.mem.unread(uid, now), "categories": Categories})
}

// setRead marks a notification as read or unread: body {"read": true|false}.
func (s *Service) setRead(w http.ResponseWriter, r *http.Request) {
	uid := auth.FromContext(r.Context()).User.ID
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "некорректный id")
		return
	}
	var in struct {
		Read *bool `json:"read"`
	}
	if !httpx.Decode(w, r, &in, 1<<10) {
		return
	}
	if in.Read == nil {
		httpx.Err(w, http.StatusBadRequest, "укажите поле read")
		return
	}
	if !s.mem.setRead(uid, id, *in.Read, time.Now()) {
		httpx.Err(w, http.StatusNotFound, "уведомление не найдено")
		return
	}
	s.publishSync(uid)
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// readAll marks all of the user's notifications as read.
func (s *Service) readAll(w http.ResponseWriter, r *http.Request) {
	uid := auth.FromContext(r.Context()).User.ID
	s.mem.readAll(uid, time.Now())
	s.publishSync(uid)
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// remove deletes one of the user's notifications.
func (s *Service) remove(w http.ResponseWriter, r *http.Request) {
	uid := auth.FromContext(r.Context()).User.ID
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "некорректный id")
		return
	}
	if !s.mem.remove(uid, id, time.Now()) {
		httpx.Err(w, http.StatusNotFound, "уведомление не найдено")
		return
	}
	s.publishSync(uid)
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// clearRead deletes all of the user's already read notifications.
func (s *Service) clearRead(w http.ResponseWriter, r *http.Request) {
	uid := auth.FromContext(r.Context()).User.ID
	n := s.mem.clearRead(uid, time.Now())
	s.publishSync(uid)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "removed": n})
}

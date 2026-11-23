// SPDX-License-Identifier: GPL-3.0-only

package console

import (
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"netrave/internal/access"
	"netrave/internal/auth"
	"netrave/internal/database"
	"netrave/internal/httpx"
	"netrave/internal/msgs"
	"netrave/internal/qemu"
)

// Handler opens device consoles: serial (terminal) and VNC (graphics).
// The browser connects to a WebSocket, the server checks access and connects it to the port of exactly this device
// on 127.0.0.1. The QEMU port is never exposed externally.
type Handler struct {
	DB      *database.DB
	Auth    *auth.Service
	M       *qemu.Manager // machine state, console ports - memory only
	mu      sync.Mutex
	hubs    map[string]*serialHub // serial: one QEMU connection per machine, shared by all connected clients
	quality *qualityStore         // VNC: current image settings by machine (in memory)
}

func New(db *database.DB, as *auth.Service, m ...*qemu.Manager) *Handler {
	h := &Handler{DB: db, Auth: as, hubs: map[string]*serialHub{}, quality: newQualityStore()}
	if len(m) > 0 {
		h.M = m[0]
	}
	return h
}

func (h *Handler) Register(mux *http.ServeMux) {
	user := func(f http.HandlerFunc) http.Handler { return h.Auth.Require(f) }
	mux.Handle("GET /ws/serial/{id}", user(func(w http.ResponseWriter, r *http.Request) { h.serve(w, r, "serial") }))
	mux.Handle("GET /ws/vnc/{id}", user(func(w http.ResponseWriter, r *http.Request) { h.serve(w, r, "vnc") }))
	mux.Handle("GET /api/consoles/{id}/quality", user(h.getQuality))
	mux.Handle("PUT /api/consoles/{id}/quality", user(h.putQuality))
}

// deviceInfo - the instance machine the console belongs to. Access is checked once
// both for the WebSocket connection and for quality settings.
type deviceInfo struct {
	name       string
	state      string
	labID      string
	ownerID    int64
	templateID int64
	serialPort int
	vncPort    int
}

// deviceAccess checks the machine id and the current user's permissions. Returns the HTTP code and error
// text so the calling handler writes them in its own format.
func (h *Handler) deviceAccess(r *http.Request, id int64, kind string) (deviceInfo, int, error) {
	ss := auth.FromContext(r.Context())
	var info deviceInfo
	// id - the machine row in the instance: a console opens only for a machine of your own instance (or an administrator's).
	var devID string
	err := h.DB.QueryRow(`SELECT r.device_id,li.user_id,li.lab_id
		FROM instance_devices r JOIN lab_instances li ON li.id=r.instance_id
		WHERE r.id=?`, id).
		Scan(&devID, &info.ownerID, &info.labID)
	info.state = "stopped"
	if h.M != nil {
		rt := h.M.Runtime(id)
		info.state, info.serialPort, info.vncPort = rt.State, rt.SerialPort, rt.VNCPort
	}
	if err == nil {
		fd, _, ok := h.DB.Labs.Device(info.labID, devID)
		if !ok {
			err = sql.ErrNoRows
		} else {
			info.name, info.templateID = fd.Name, fd.TemplateID
		}
	}
	if err == nil && ss.User.Role != "admin" && info.ownerID != ss.User.ID {
		err = sql.ErrNoRows
	}
	if err != nil {
		return info, http.StatusNotFound, msgs.New("Device not found", nil)
	}
	// A console is machine control, so the "start" level is required. Without view access the lab is hidden.
	perms, lerr := access.Perms(h.DB, info.labID, ss.User.ID, ss.User.Role == "admin")
	if lerr != nil || !perms[access.LabView] {
		return info, http.StatusNotFound, msgs.New("Device not found", nil)
	}
	need := access.ConsoleSerial
	if kind == "vnc" {
		need = access.ConsoleVNC
	}
	if !perms[need] {
		return info, http.StatusForbidden, msgs.New("No permission: {permission_key}", map[string]string{"permission_key": access.Title(need)})
	}
	return info, 0, nil
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request, kind string) {
	ss := auth.FromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return
	}
	info, code, message := h.deviceAccess(r, id, kind)
	if code != 0 {
		http.Error(w, message.Error(), code)
		return
	}
	name, state, labID := info.name, info.state, info.labID
	serialPort, vncPort := info.serialPort, info.vncPort
	if !auth.SameOrigin(r) {
		http.Error(w, "Invalid Origin", http.StatusForbidden)
		return
	}
	if err := checkUpgrade(r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if state != "running" && state != "paused" {
		http.Error(w, "The device is not running", http.StatusConflict)
		return
	}
	port := serialPort
	if kind == "vnc" {
		port = vncPort
	}
	if port <= 0 {
		http.Error(w, "The console port is not assigned", http.StatusConflict)
		return
	}

	// Serial: QEMU accepts one TCP connection, so all clients (owner, administrators, other windows)
	// work through a shared hub (hub.go): everyone receives output, input does not block.
	// VNC - a shared screen (QEMU share=force-shared): one connection does not drop another.
	var ws *wsConn
	var kill func()
	var run func()
	if kind == "serial" {
		var after int64
		resume := false
		if raw := r.URL.Query().Get("after"); raw != "" {
			if value, perr := strconv.ParseInt(raw, 10, 64); perr == nil && value >= 0 {
				after, resume = value, true
			}
		}
		cl, err := h.attach(fmt.Sprintf("serial:%d:%d", id, port), port, after, resume)
		if err != nil {
			httpx.ErrKey(w, http.StatusBadGateway, "Could not connect to QEMU: {detail}", map[string]string{"detail": err.Error()})
			return
		}
		ws, err = upgrade(w, r)
		if err != nil {
			h.detach(cl)
			return
		}
		cl.setWS(ws)
		kill = cl.kill
		run = func() { h.runSerial(cl) }
	} else {
		back, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 3*time.Second)
		if err != nil {
			httpx.ErrKey(w, http.StatusBadGateway, "Could not connect to QEMU: {detail}", map[string]string{"detail": err.Error()})
			return
		}
		ws, err = upgrade(w, r)
		if err != nil {
			back.Close()
			return
		}
		kill = func() { ws.closeConn(); back.Close() }
		run = func() { bridge(ws, back) }
	}
	ip := auth.ClientIP(r)
	h.DB.Audit(ss.User.ID, ss.User.Username, "console_open", kind+" "+name, ip)
	if ss.User.Role == "admin" {
		var owner int64
		var ownerName string
		if fl, ok := h.DB.Labs.Lab(labID); ok && h.DB.QueryRow(`SELECT username FROM users WHERE id=?`, fl.OwnerID).Scan(&ownerName) == nil {
			owner = fl.OwnerID
		}
		if owner != 0 && owner != ss.User.ID {
			h.DB.AuditKey(ss.User.ID, ss.User.Username, "admin_console_open", "{kind} {name} (laboratory of user «{owner}»)",
				map[string]string{"kind": kind, "name": name, "owner": ownerName}, nil, ip)
		}
	}
	need := access.ConsoleSerial
	if kind == "vnc" {
		need = access.ConsoleVNC
	}
	stop := make(chan struct{})
	go h.watch(stop, kill, labID, ss.User.ID, ss.User.Role == "admin", need)
	heartbeatStop := make(chan struct{})
	go heartbeat(ws, heartbeatStop)
	run()
	close(heartbeatStop)
	close(stop)
	h.DB.Audit(ss.User.ID, ss.User.Username, "console_close", kind+" "+name, ip)
}

// watch closes an open console if the permission is revoked or the user is disabled during the session.
func (h *Handler) watch(stop <-chan struct{}, kill func(), labName string, userID int64, admin bool, need string) {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			var dis int
			perms, err := access.Perms(h.DB, labName, userID, admin)
			derr := h.DB.QueryRow(`SELECT disabled FROM users WHERE id=?`, userID).Scan(&dis)
			if err != nil || derr != nil || dis != 0 || !perms[need] {
				kill()
				return
			}
		}
	}
}

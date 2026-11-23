// SPDX-License-Identifier: GPL-3.0-only

package console

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"netrave/internal/auth"
	"netrave/internal/catalog"
	"netrave/internal/config"
	"netrave/internal/database"
	"netrave/internal/labfile"
	"netrave/internal/labtest"
	"netrave/internal/qemu"
)

type qualityEnv struct {
	db  *database.DB
	as  *auth.Service
	srv http.Handler
	m   *qemu.Manager
}

type qualityUser struct {
	id   int64
	tok  string
	csrf string
}

func newQualityEnv(t *testing.T) *qualityEnv {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	as := auth.New(db)
	m := qemu.New(db, &config.Config{LabsDir: filepath.Join(t.TempDir(), "labs"), RunDir: filepath.Join(t.TempDir(), "run")}, nil)
	mux := http.NewServeMux()
	New(db, as, m).Register(mux)
	return &qualityEnv{db: db, as: as, srv: mux, m: m}
}

func (e *qualityEnv) user(t *testing.T, name, role string) qualityUser {
	t.Helper()
	res, err := e.db.Exec(`INSERT INTO users(username,password_hash,role) VALUES(?,?,?)`, name, "x", role)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	tok, csrf, _, err := e.as.CreateSession(id, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	return qualityUser{id: id, tok: tok, csrf: csrf}
}

// template creates a VNC machine template with the given image settings.
func (e *qualityEnv) template(t *testing.T, vnc catalog.VNCQuality) int64 {
	t.Helper()
	id, err := e.db.Catalog.AddTemplate(catalog.Template{
		Name: "vnctpl", Type: "linux", Arch: "x86_64", Machine: "pc", CPU: 1, RAMMB: 1024,
		NICs: 1, NICModel: "virtio-net-pci", DiskBus: "virtio", BIOS: "seabios", Console: "vnc",
		IfaceType: "ens", IfaceStart: 0, VNC: vnc,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// machine creates a user lab, a machine from template tplID (0 - without a template) and an instance row.
// Returns the instance_devices row id - it is what addresses the console and the settings.
func (e *qualityEnv) machine(t *testing.T, owner int64, tplID int64) int64 {
	t.Helper()
	labID := labtest.Lab(t, e.db, owner, "lab", "")
	deviceID := labtest.Device(t, e.db, labID, labfile.Device{Name: "vm", Console: "vnc", TemplateID: tplID})
	res, err := e.db.Exec(`INSERT INTO lab_instances(lab_id,user_id) VALUES(?,?)`, labID, owner)
	if err != nil {
		t.Fatal(err)
	}
	instanceID, _ := res.LastInsertId()
	res, err = e.db.Exec(`INSERT INTO instance_devices(instance_id,device_id,console) VALUES(?,?,?)`,
		instanceID, deviceID, "vnc")
	if err != nil {
		t.Fatal(err)
	}
	rid, _ := res.LastInsertId()
	e.m.SetRuntime(rid, qemu.Runtime{State: "running", VNCPort: 5901})
	return rid
}

func (e *qualityEnv) call(u qualityUser, method, path string, body any) *httptest.ResponseRecorder {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: u.tok})
	if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", u.csrf)
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	return rec
}

func qualityPath(rid int64) string {
	return "/api/consoles/" + strconv.FormatInt(rid, 10) + "/quality"
}

func decodeQuality(t *testing.T, rec *httptest.ResponseRecorder) qualityResponse {
	t.Helper()
	var response qualityResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, rec.Body)
	}
	return response
}

func intp(v int) *int { return &v }

func TestQualityDefaultsWithoutTemplate(t *testing.T) {
	e := newQualityEnv(t)
	owner := e.user(t, "alice", "user")
	rid := e.machine(t, owner.id, 0)

	rec := e.call(owner, http.MethodGet, qualityPath(rid), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body)
	}
	response := decodeQuality(t, rec)
	if response.Settings.Resolution != "auto" || response.Settings.Encoding != "auto" {
		t.Fatalf("defaults: %+v", response.Settings)
	}
	if response.Settings.Bitrate != nil || response.Settings.Quality != nil || response.Settings.Compression != nil || response.Settings.FPS != nil {
		t.Fatalf("numeric fields must be null: %+v", response.Settings)
	}
}

func TestQualityTakenFromTemplate(t *testing.T) {
	e := newQualityEnv(t)
	owner := e.user(t, "alice", "user")
	tpl := e.template(t, catalog.VNCQuality{Resolution: "1280x720", Bitrate: intp(25), Encoding: "tight"})
	rid := e.machine(t, owner.id, tpl)

	response := decodeQuality(t, e.call(owner, http.MethodGet, qualityPath(rid), nil))
	if response.Settings.Resolution != "1280x720" || response.Settings.Bitrate == nil || *response.Settings.Bitrate != 25 {
		t.Fatalf("settings must come from the template: %+v", response.Settings)
	}
	if response.Defaults.Resolution != "1280x720" {
		t.Fatalf("defaults must match the template: %+v", response.Defaults)
	}
}

func TestQualityPutOverridesTemplateInMemory(t *testing.T) {
	e := newQualityEnv(t)
	owner := e.user(t, "alice", "user")
	tpl := e.template(t, catalog.VNCQuality{Resolution: "auto", Encoding: "auto"})
	rid := e.machine(t, owner.id, tpl)

	saved := map[string]any{"resolution": "1024x768", "bitrate": 10, "quality": 7, "compression": 4, "fps": 30, "encoding": "tight"}
	rec := e.call(owner, http.MethodPut, qualityPath(rid), saved)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body)
	}
	response := decodeQuality(t, e.call(owner, http.MethodGet, qualityPath(rid), nil))
	if response.Settings.Resolution != "1024x768" || response.Settings.FPS == nil || *response.Settings.FPS != 30 {
		t.Fatalf("after PUT the new settings must apply: %+v", response.Settings)
	}
}

func TestQualityValidation(t *testing.T) {
	e := newQualityEnv(t)
	owner := e.user(t, "alice", "user")
	rid := e.machine(t, owner.id, 0)
	bad := []map[string]any{
		{"resolution": "999x999"},
		{"bitrate": 0},
		{"quality": 10},
		{"fps": 61},
		{"encoding": "zlib"},
	}
	for _, body := range bad {
		if rec := e.call(owner, http.MethodPut, qualityPath(rid), body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%v must be rejected: %d %s", body, rec.Code, rec.Body)
		}
	}
}

func TestQualityRequiresDeviceAccess(t *testing.T) {
	e := newQualityEnv(t)
	owner := e.user(t, "alice", "user")
	stranger := e.user(t, "bob", "user")
	rid := e.machine(t, owner.id, 0)

	if rec := e.call(stranger, http.MethodGet, qualityPath(rid), nil); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign machine: %d %s", rec.Code, rec.Body)
	}
	if rec := e.call(stranger, http.MethodPut, qualityPath(rid), map[string]any{"fps": 10}); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign machine PUT: %d %s", rec.Code, rec.Body)
	}
}

func TestQualityDefaultsOnInvalidID(t *testing.T) {
	e := newQualityEnv(t)
	owner := e.user(t, "alice", "user")
	if rec := e.call(owner, http.MethodGet, "/api/consoles/abc/quality", nil); rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid id: %d %s", rec.Code, rec.Body)
	}
}

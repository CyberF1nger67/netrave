// SPDX-License-Identifier: GPL-3.0-only

// Package iconlib - the user's icon library (PNG, JPEG, WEBP, SVG).
//
// Library data lives in files: <icons directory>/library/<owner id>.yaml. Each file holds the owner's icon list:
// id, content hash, name, dimensions and the path to the picture (file, relative to the icons directory).
// The pictures themselves are stored by the icons package under <hash>.png. One picture per owner is one entry:
// uploading it again returns the already saved icon and does not create a duplicate.
//
// The library and machines are independent: a machine references an icon via devices.custom_icon (the same hash), so
// deleting a library entry does not change machines. The picture file is deleted only when neither
// a machine nor a library references it (see icons.Store.Remove).
//
// Each user sees and changes only their own library; other users' entries are indistinguishable from nonexistent ones.
// The exception is administrators: they share one common library, so icons added by one administrator
// are visible and available to the other administrators.
package iconlib

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"netrave/internal/auth"
	"netrave/internal/database"
	"netrave/internal/httpx"
	"netrave/internal/icons"
	"netrave/internal/names"
)

const (
	// MaxPerUser is the maximum number of icons in one user's library.
	MaxPerUser = 200
)

var (
	// ErrNotFound - the entry does not exist or belongs to another user.
	ErrNotFound = errors.New("иконка не найдена")
	// ErrLimit - the user's library is full.
	ErrLimit = fmt.Errorf("достигнут лимит библиотеки иконок (%d)", MaxPerUser)
)

// mu protects reading and writing library files in this process.
var mu sync.Mutex

// Item - a library icon. URL - the picture address for preview (only for the owner).
// UsedBy - how many machines in the owner's labs use this picture; filled in the list.
type Item struct {
	ID        int64  `json:"id"`
	Hash      string `json:"hash"`
	Name      string `json:"name"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	SizeBytes int64  `json:"size_bytes"`
	HasAlpha  bool   `json:"has_alpha"`
	URL       string `json:"url"`
	UsedBy    int    `json:"used_by"`
	CreatedAt int64  `json:"created_at"`
}

// uploadResult - the upload response: the icon itself and whether it was just added (false - such a picture already existed).
type uploadResult struct {
	Item
	Created bool `json:"created"`
}

// CleanName validates the icon name: only Latin letters, digits, '-' and '_'.
func CleanName(s string) (string, string) {
	s = strings.TrimSpace(s)
	if err := names.Check("название иконки", s); err != nil {
		return "", err.Error()
	}
	return s, ""
}

func imageURL(id int64) string {
	return fmt.Sprintf("/api/icon-library/%d/image", id)
}

// owned - an icon together with the owner of its library file.
type owned struct {
	owner int64
	icons.LibraryEntry
}

// owners returns the owners whose libraries are available to user uid: the user themselves and, for
// an administrator - all administrators (the shared library).
func owners(db *database.DB, uid int64) ([]int64, error) {
	var role string
	err := db.QueryRow(`SELECT role FROM users WHERE id=?`, uid).Scan(&role)
	if err == sql.ErrNoRows {
		return []int64{uid}, nil
	}
	if err != nil {
		return nil, err
	}
	if role != "admin" {
		return []int64{uid}, nil
	}
	rows, err := db.Query(`SELECT id FROM users WHERE role='admin' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// scope reads the icons available to user uid.
func scope(db *database.DB, st *icons.Store, uid int64) ([]owned, error) {
	ids, err := owners(db, uid)
	if err != nil {
		return nil, err
	}
	var out []owned
	for _, o := range ids {
		f, err := st.ReadLibrary(o)
		if err != nil {
			return nil, err
		}
		for _, e := range f.Items {
			out = append(out, owned{owner: o, LibraryEntry: e})
		}
	}
	return out, nil
}

// find looks up an icon by id among those available to the user.
func find(db *database.DB, st *icons.Store, uid, id int64) (owned, error) {
	items, err := scope(db, st, uid)
	if err != nil {
		return owned{}, err
	}
	for _, it := range items {
		if it.ID == id {
			return it, nil
		}
	}
	return owned{}, ErrNotFound
}

// usedBy counts machines in the owner's labs that use the picture.
func usedBy(db *database.DB, o owned) (int, error) {
	n := 0
	for _, l := range db.Labs.Labs() {
		if l.OwnerID != o.owner {
			continue
		}
		for _, d := range l.Devices {
			if d.CustomIcon == o.Hash {
				n++
			}
		}
	}
	return n, nil
}

func toItem(db *database.DB, o owned) (Item, error) {
	n, err := usedBy(db, o)
	if err != nil {
		return Item{}, err
	}
	return Item{
		ID: o.ID, Hash: o.Hash, Name: o.Name, Width: o.Width, Height: o.Height, SizeBytes: o.SizeBytes,
		HasAlpha: o.HasAlpha, URL: imageURL(o.ID), UsedBy: n, CreatedAt: o.CreatedAt,
	}, nil
}

// List returns the library available to the user, newest first.
func List(db *database.DB, st *icons.Store, uid int64) ([]Item, error) {
	mu.Lock()
	defer mu.Unlock()
	items, err := scope(db, st, uid)
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	out := make([]Item, 0, len(items))
	for _, o := range items {
		it, err := toItem(db, o)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, nil
}

// get returns the entry available to user uid by id.
func get(db *database.DB, st *icons.Store, uid, id int64) (Item, error) {
	o, err := find(db, st, uid, id)
	if err != nil {
		return Item{}, err
	}
	return toItem(db, o)
}

// nextID - the next free number: greater than the maximum across all libraries so numbers are not reused.
func nextID(st *icons.Store) (int64, error) {
	owners, err := st.LibraryOwners()
	if err != nil {
		return 0, err
	}
	var max int64
	for _, o := range owners {
		f, err := st.ReadLibrary(o)
		if err != nil {
			return 0, err
		}
		for _, e := range f.Items {
			if e.ID > max {
				max = e.ID
			}
		}
	}
	return max + 1, nil
}

// Remember records the accepted icon in uid's library. By this point the picture file is already saved (icons.Store.Save).
// A known picture is not duplicated: the existing entry is returned with created=false.
// An empty name - a name based on the picture size. The MaxPerUser limit is checked only for new entries.
func Remember(db *database.DB, st *icons.Store, uid int64, info icons.Info, name string) (Item, bool, error) {
	if name == "" {
		name = fmt.Sprintf("icon-%dx%d", info.Width, info.Height)
	}
	mu.Lock()
	defer mu.Unlock()
	items, err := scope(db, st, uid)
	if err != nil {
		return Item{}, false, err
	}
	for _, o := range items {
		if o.Hash == info.Hash {
			it, err := toItem(db, o)
			return it, false, err
		}
	}
	own, err := st.ReadLibrary(uid)
	if err != nil {
		return Item{}, false, err
	}
	if len(own.Items) >= MaxPerUser {
		return Item{}, false, ErrLimit
	}
	id, err := nextID(st)
	if err != nil {
		return Item{}, false, err
	}
	e := icons.LibraryEntry{
		ID: id, Hash: info.Hash, Name: name, Width: info.Width, Height: info.Height,
		SizeBytes: int64(info.Size), HasAlpha: info.Alpha, CreatedAt: time.Now().Unix(), File: info.FileName(),
	}
	own.Items = append(own.Items, e)
	if err := st.WriteLibrary(uid, own); err != nil {
		return Item{}, false, err
	}
	it, err := toItem(db, owned{owner: uid, LibraryEntry: e})
	return it, true, err
}

// Rename changes the name of an entry available to user uid.
func Rename(db *database.DB, st *icons.Store, uid, id int64, name string) (Item, error) {
	mu.Lock()
	defer mu.Unlock()
	o, err := find(db, st, uid, id)
	if err != nil {
		return Item{}, err
	}
	f, err := st.ReadLibrary(o.owner)
	if err != nil {
		return Item{}, err
	}
	for i := range f.Items {
		if f.Items[i].ID == id {
			f.Items[i].Name = name
		}
	}
	if err := st.WriteLibrary(o.owner, f); err != nil {
		return Item{}, err
	}
	return get(db, st, uid, id)
}

// Delete deletes an entry available to user uid and returns the hash of its picture. The picture file is deleted by
// the caller (icons.Store.Remove) so a picture still used by a machine is not removed.
func Delete(db *database.DB, st *icons.Store, uid, id int64) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	o, err := find(db, st, uid, id)
	if err != nil {
		return "", err
	}
	f, err := st.ReadLibrary(o.owner)
	if err != nil {
		return "", err
	}
	kept := f.Items[:0]
	for _, e := range f.Items {
		if e.ID != id {
			kept = append(kept, e)
		}
	}
	f.Items = kept
	if err := st.WriteLibrary(o.owner, f); err != nil {
		return "", err
	}
	return o.Hash, nil
}

// Image returns the picture of an entry available to user uid.
func Image(db *database.DB, st *icons.Store, uid, id int64) ([]byte, error) {
	data, _, err := ImageData(db, st, uid, id)
	return data, err
}

// ImageData returns the picture and its MIME type.
func ImageData(db *database.DB, st *icons.Store, uid, id int64) ([]byte, string, error) {
	mu.Lock()
	o, err := find(db, st, uid, id)
	mu.Unlock()
	if err != nil {
		return nil, "", err
	}
	data, err := st.ReadFile(o.File)
	if err != nil {
		return nil, "", err
	}
	return data, icons.ContentType(o.File), nil
}

// RemoveOwner deletes the user's library file (when a user is deleted).
func RemoveOwner(st *icons.Store, uid int64) error {
	mu.Lock()
	defer mu.Unlock()
	return st.RemoveLibrary(uid)
}

// Handler - REST API of the icon library.
type Handler struct {
	DB    *database.DB
	Auth  *auth.Service
	Icons *icons.Store
}

// Register wires up the library routes. Only sign-in is required: the library
// is personal, and administrators share one.
func (h *Handler) Register(mux *http.ServeMux) {
	user := func(f http.HandlerFunc) http.Handler { return h.Auth.Require(f) }
	mux.Handle("GET /api/icon-library", user(h.list))
	mux.Handle("POST /api/icon-library", user(h.upload))
	mux.Handle("PUT /api/icon-library/{id}", user(h.rename))
	mux.Handle("DELETE /api/icon-library/{id}", user(h.remove))
	mux.Handle("GET /api/icon-library/{id}/image", user(h.image))
}

func (h *Handler) audit(r *http.Request, action, detail string) {
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.DB.Audit(ss.User.ID, ss.User.Username, action, detail, auth.ClientIP(r))
	}
}

// pathID parses {id} from the path; on error it writes a 400 response.
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "некорректный id")
		return 0, false
	}
	return id, true
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	uid := auth.FromContext(r.Context()).User.ID
	items, err := List(h.DB, h.Icons, uid)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	httpx.JSON(w, http.StatusOK, items)
}

// upload - POST /api/icon-library: the request body is the picture, the optional ?name= sets the name.
// 201 - the icon was added; 200 - such a picture is already in the library (created=false).
func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	uid := auth.FromContext(r.Context()).User.ID
	if !icons.SupportedContentType(r.Header.Get("Content-Type")) {
		httpx.Err(w, http.StatusUnsupportedMediaType, "поддерживаются PNG, JPEG, WEBP и SVG")
		return
	}
	if h.Icons == nil {
		httpx.Err(w, http.StatusInternalServerError, "каталог иконок не настроен")
		return
	}
	if r.ContentLength > icons.MaxBytes {
		httpx.Err(w, http.StatusRequestEntityTooLarge, "файл больше 512 КБ")
		return
	}
	name := ""
	if q := r.URL.Query().Get("name"); q != "" {
		n, msg := CleanName(q)
		if msg != "" {
			httpx.Err(w, http.StatusBadRequest, msg)
			return
		}
		name = n
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, icons.MaxBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			httpx.Err(w, http.StatusRequestEntityTooLarge, "файл больше 512 КБ")
			return
		}
		httpx.Err(w, http.StatusBadRequest, "не удалось прочитать файл")
		return
	}
	clean, info, err := icons.Clean(data)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Icons.Save(clean, info.Hash); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "не удалось сохранить иконку")
		return
	}
	item, created, err := Remember(h.DB, h.Icons, uid, info, name)
	if err == ErrLimit {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	code := http.StatusOK
	if created {
		code = http.StatusCreated
		h.audit(r, "icon_library_add", item.Name+" · "+info.Hash[:12])
	}
	httpx.JSON(w, code, uploadResult{Item: item, Created: created})
}

// rename - PUT /api/icon-library/{id}: body {"name": "..."}.
func (h *Handler) rename(w http.ResponseWriter, r *http.Request) {
	uid := auth.FromContext(r.Context()).User.ID
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if !httpx.Decode(w, r, &in, 1<<12) {
		return
	}
	name, msg := CleanName(in.Name)
	if msg != "" {
		httpx.Err(w, http.StatusBadRequest, msg)
		return
	}
	item, err := Rename(h.DB, h.Icons, uid, id, name)
	if err == ErrNotFound {
		httpx.Err(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	h.audit(r, "icon_library_rename", name)
	httpx.JSON(w, http.StatusOK, item)
}

// remove - DELETE /api/icon-library/{id}. Machines that use the picture are not changed:
// the file remains while at least one machine references it.
func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	uid := auth.FromContext(r.Context()).User.ID
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	hash, err := Delete(h.DB, h.Icons, uid, id)
	if err == ErrNotFound {
		httpx.Err(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	h.Icons.Remove(h.DB, hash)
	h.audit(r, "icon_library_delete", hash[:12])
	w.WriteHeader(http.StatusNoContent)
}

// image - GET /api/icon-library/{id}/image: the picture itself, available to the entry's user.
func (h *Handler) image(w http.ResponseWriter, r *http.Request) {
	uid := auth.FromContext(r.Context()).User.ID
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	data, contentType, err := ImageData(h.DB, h.Icons, uid, id)
	if err != nil {
		httpx.Err(w, http.StatusNotFound, "иконка не найдена")
		return
	}
	// The file name is the content hash, so the picture can be cached; private, as it depends on the user.
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

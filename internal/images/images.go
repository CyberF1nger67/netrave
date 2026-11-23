// SPDX-License-Identifier: GPL-3.0-only

// Package images: the NetRave-style image catalog.
//
// An image is a FOLDER in the image catalog (e.g. linux-alpine/). The folder contains files:
//
//	hda.qcow2, hdb.qcow2, hdc.qcow2 ... - disks in order (hda is the boot disk);
//	*.iso - CD-ROM (read-only).
//
// If there are no hdX files, any .qcow2/.raw/.img in the folder is considered a disk (alphabetically).
// All disks of the folder are attached to the VM; each VM has its own working layer on top of the folder files.
package images

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"netrave/internal/auth"
	"netrave/internal/catalog"
	"netrave/internal/config"
	"netrave/internal/database"
	"netrave/internal/httpx"
	"netrave/internal/msgs"
	"netrave/internal/names"
	"netrave/internal/notify"
	"netrave/internal/qemu"
)

var (
	diskRe  = regexp.MustCompile(`^hd[a-z]\.(qcow2|raw|img)$`)
	diskExt = map[string]bool{".qcow2": true, ".raw": true, ".img": true}
)

// Disk - an image disk.
type Disk struct {
	Name   string `json:"name"`
	Format string `json:"format"`
}

// Layout - the contents of the image folder.
type Layout struct {
	Disks  []Disk   `json:"disks"`
	CDROMs []string `json:"cdroms"`
}

type item struct {
	ID      int64             `json:"id"`
	Name    string            `json:"name"`
	Dirname string            `json:"dirname"`
	Disks   []Disk            `json:"disks"`
	CDROMs  []string          `json:"cdroms"`
	Size    int64             `json:"size_bytes"`
	UsedBy  int               `json:"used_by"`
	Type    string            `json:"device_type"`
	Qemu    map[string]string `json:"qemu"`
}

// Handler - the image catalog: listing, folder file upload, scan, rename, delete.
type Handler struct {
	DB     *database.DB
	Auth   *auth.Service
	Cfg    *config.Config
	Notify *notify.Service // notification centre (nil - not stored)
	M      *qemu.Manager   // running machine state - in memory
}

func (h *Handler) Register(mux *http.ServeMux) {
	user := func(f http.HandlerFunc) http.Handler { return h.Auth.Require(f) }
	perm := func(p string, f http.HandlerFunc) http.Handler { return h.Auth.RequirePerm(p, f) }
	mux.Handle("GET /api/images", user(h.list))
	mux.Handle("POST /api/images/upload", perm("images", h.upload))
	mux.Handle("POST /api/images/scan", perm("images", h.scan))
	mux.Handle("PUT /api/images/{id}", perm("images", h.rename))
	mux.Handle("DELETE /api/images/{id}", perm("images", h.remove))
}

func (h *Handler) audit(r *http.Request, action, detail string) {
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.DB.Audit(ss.User.ID, ss.User.Username, action, detail, auth.ClientIP(r))
	}
}

func (h *Handler) auditKey(r *http.Request, action, key string, params map[string]string) {
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.DB.AuditKey(ss.User.ID, ss.User.Username, action, key, params, nil, auth.ClientIP(r))
	}
}

// inspectDisk validates a disk via qemu-img and returns its format.
func (h *Handler) inspectDisk(path string) (string, error) {
	img, err := qemu.FindQemuImg(h.Cfg.QEMUPath)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, img, "info", "--output=json", path).Output()
	if err != nil {
		return "", errors.New("qemu-img could not read the file (not a disk image or damaged)")
	}
	var info struct {
		Format          string `json:"format"`
		BackingFilename string `json:"backing-filename"`
		FormatSpecific  struct {
			Data struct {
				DataFile string `json:"data-file"`
			} `json:"data"`
		} `json:"format-specific"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return "", errors.New("Could not parse the qemu-img response")
	}
	if info.Format != "qcow2" && info.Format != "raw" {
		return "", msgs.New("Format «{format}» is not supported (qcow2 or raw is needed)", map[string]string{"format": info.Format})
	}
	if info.BackingFilename != "" || info.FormatSpecific.Data.DataFile != "" {
		return "", errors.New("The disk refers to external files (backing/data-file) — not supported")
	}
	return info.Format, nil
}

// checkISO validates the ISO 9660 signature.
func checkISO(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 5)
	if _, err := f.ReadAt(buf, 0x8001); err != nil || string(buf) != "CD001" {
		return errors.New("Not an ISO 9660 image")
	}
	return nil
}

// inspectDir parses the image folder: disks in order and ISO files. It also returns the total size and problems.
func (h *Handler) inspectDir(dir string) (Layout, int64, []msgs.Message) {
	var lay Layout
	var problems []msgs.Message
	var size int64
	ents, err := os.ReadDir(dir)
	if err != nil {
		return lay, 0, []msgs.Message{msgs.Of(err)}
	}
	var hdFiles, other, isos []string
	for _, e := range ents {
		name := e.Name()
		if !e.Type().IsRegular() || strings.HasPrefix(name, ".") {
			continue
		}
		if info, err := e.Info(); err == nil {
			size += info.Size()
		}
		ext := strings.ToLower(filepath.Ext(name))
		switch {
		case ext == ".iso":
			isos = append(isos, name)
		case diskRe.MatchString(name):
			hdFiles = append(hdFiles, name)
		case diskExt[ext]:
			other = append(other, name)
		}
	}
	disks := hdFiles
	if len(disks) == 0 {
		disks = other
	}
	sort.Strings(disks)
	sort.Strings(isos)
	for _, name := range disks {
		format, err := h.inspectDisk(filepath.Join(dir, name))
		if err != nil {
			problems = append(problems, msgs.M("{name}: {detail}", map[string]string{"name": name, "detail": err.Error()}))
			continue
		}
		lay.Disks = append(lay.Disks, Disk{Name: name, Format: format})
	}
	for _, name := range isos {
		if err := checkISO(filepath.Join(dir, name)); err != nil {
			problems = append(problems, msgs.M("{name}: {detail}", map[string]string{"name": name, "detail": err.Error()}))
			continue
		}
		lay.CDROMs = append(lay.CDROMs, name)
	}
	return lay, size, problems
}

// imageUsage counts the machines that reference the image (the default image in lab topology).
func imageUsage(db *database.DB) map[string]int {
	out := map[string]int{}
	for _, d := range db.Labs.AllDevices() {
		if d.ImageID != "" {
			out[d.ImageID]++
		}
	}
	return out
}

// dropImage deletes the image from the catalog. Instance row references to it are nulled:
// the machines that used the image lose it.
func (h *Handler) dropImage(id int64) error {
	im, _ := h.DB.Catalog.Image(id)
	if err := h.DB.Catalog.DeleteImage(id); err != nil {
		return err
	}
	if im.Dirname == "" {
		return nil
	}
	_, err := h.DB.Exec(`UPDATE instance_devices SET image_id=NULL WHERE image_id=?`, im.Dirname)
	return err
}

// parseQemu parses the image's QEMU parameters. Empty, corrupted or null JSON yields an empty map, not nil:
// nil goes into the response as null, and the UI crashes when reading image fields.
func parseQemu(raw string) map[string]string {
	out := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return map[string]string{}
	}
	return out
}

// ---------- listing ----------

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	imgs := h.DB.Catalog.Images()
	sort.SliceStable(imgs, func(i, j int) bool { return imgs[i].Name < imgs[j].Name })
	use := imageUsage(h.DB)
	out := []item{}
	for _, im := range imgs {
		it := item{ID: im.ID, Name: im.Name, Dirname: im.Dirname, Size: im.SizeBytes, Type: im.DeviceType}
		it.UsedBy = use[im.Dirname]
		var lay Layout
		_ = json.Unmarshal([]byte(im.LayoutJSON), &lay)
		it.Qemu = parseQemu(im.QemuJSON)
		it.Disks = lay.Disks
		if it.Disks == nil {
			it.Disks = []Disk{}
		}
		it.CDROMs = lay.CDROMs
		if it.CDROMs == nil {
			it.CDROMs = []string{}
		}
		out = append(out, it)
	}
	httpx.JSON(w, http.StatusOK, out)
}

// ---------- folder file upload ----------
// The browser sends the folder files one by one: POST /api/images/upload?dir=<folder>&file=<file name>, the body is the file itself.
// After uploading all files the client calls scan, which registers the folder.

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	file := r.URL.Query().Get("file")
	if err := names.Check("Image folder", dir); err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	ext := strings.ToLower(filepath.Ext(file))
	if !names.ValidFile(file) || !(diskExt[ext] || ext == ".iso") {
		httpx.Err(w, http.StatusBadRequest, "Allowed files: .qcow2, .raw, .img, .iso with a valid file name")
		return
	}
	limit := h.Cfg.MaxUploadBytes()
	if r.ContentLength > limit {
		httpx.ErrKey(w, http.StatusRequestEntityTooLarge, "File is over the {limit} MB limit (set in System settings)", map[string]string{"limit": fmt.Sprint(limit >> 20)})
		return
	}
	if r.ContentLength > 0 && qemu.DiskFree(h.Cfg.DisksDir) < uint64(r.ContentLength)+(1<<30) {
		httpx.Err(w, http.StatusInsufficientStorage, "Not enough disk space for the upload")
		return
	}
	var used int
	// The state lives in instances (instance_devices): a machine that is currently running holds the image.
	if im, ok := h.DB.Catalog.ImageByDirname(dir); ok && h.M != nil {
		if rows, err := h.DB.Query(`SELECT id FROM instance_devices WHERE image_id=?`, im.Dirname); err == nil {
			for rows.Next() {
				var rid int64
				if rows.Scan(&rid) == nil && h.M.Active(rid) {
					used++
				}
			}
			rows.Close()
		}
	}
	if used > 0 {
		httpx.Err(w, http.StatusConflict, "The image is used by running devices — stop them to replace the files")
		return
	}

	target := filepath.Join(h.Cfg.DisksDir, dir)
	if err := os.MkdirAll(target, 0o750); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Could not create the image folder")
		return
	}
	tmp, err := os.CreateTemp(h.Cfg.TmpDir, "upload-*.part")
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Could not create a temporary file")
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	r.Body = http.MaxBytesReader(w, r.Body, limit)
	n, err := io.Copy(tmp, r.Body)
	tmp.Close()
	if err != nil {
		httpx.Err(w, http.StatusRequestEntityTooLarge, "The upload was interrupted or exceeds the size limit")
		return
	}
	if n == 0 {
		httpx.Err(w, http.StatusBadRequest, "Empty file")
		return
	}
	dest := filepath.Join(target, file)
	if err := moveFile(tmpName, dest); err != nil {
		httpx.ErrKey(w, http.StatusInternalServerError, "Could not save the file: {detail}", map[string]string{"detail": err.Error()})
		return
	}
	_ = os.Chmod(dest, 0o640)
	h.audit(r, "image_upload", dir+"/"+file)
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.Notify.Activity(ss.User.ID, 0, notify.Success, "image", "Image file uploaded", "{path} ({size}).", "/images", notify.P("path", dir+"/"+file, "size", fmtSize(n)))
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"size": n})
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return os.Remove(src)
}

// ---------- catalog scan ----------

func (h *Handler) scan(w http.ResponseWriter, r *http.Request) {
	// quiet=1 - automatic discovery when the menu opens: without notifications and audit.
	quiet := r.URL.Query().Get("quiet") == "1"
	qemu.ImagesMu.Lock()
	defer qemu.ImagesMu.Unlock()
	ents, err := os.ReadDir(h.Cfg.DisksDir)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Could not read the image catalog")
		return
	}
	seen := map[string]bool{}
	problems := []msgs.Message{}
	added, updated := 0, 0
	for _, e := range ents {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		if !names.Valid(name) {
			problems = append(problems, msgs.M("{name}: invalid folder name", map[string]string{"name": name}))
			continue
		}
		seen[name] = true
		lay, size, probs := h.inspectDir(filepath.Join(h.Cfg.DisksDir, name))
		for _, p := range probs {
			problems = append(problems, msgs.Message{Key: "{name}: {detail_key}", Params: msgs.Merge(p.Params, map[string]string{"name": name, "detail_key": p.Key})})
		}
		if len(lay.Disks) == 0 && len(lay.CDROMs) == 0 {
			problems = append(problems, msgs.M("{name}: no disks (.qcow2/.raw/.img) and no ISO — folder skipped", map[string]string{"name": name}))
			continue
		}
		lj, _ := json.Marshal(lay)
		if im, ok := h.DB.Catalog.ImageByDirname(name); ok {
			_ = h.DB.Catalog.UpdateImage(im.ID, func(x *catalog.Image) error {
				x.LayoutJSON = string(lj)
				x.SizeBytes = size
				return nil
			})
			updated++
		} else {
			if _, err := h.DB.Catalog.AddImage(catalog.Image{Name: name, Dirname: name,
				LayoutJSON: string(lj), SizeBytes: size, CreatedAt: time.Now().Unix()}); err != nil {
				problems = append(problems, msgs.M("{name}: {detail}", map[string]string{"name": name, "detail": err.Error()}))
				continue
			}
			added++
		}
	}

	// records for folders that no longer exist
	type rec struct {
		id   int64
		name string
		used int
	}
	var recs []rec
	use := imageUsage(h.DB)
	for _, im := range h.DB.Catalog.Images() {
		recs = append(recs, rec{id: im.ID, name: im.Dirname, used: use[im.Dirname]})
	}
	for _, rc := range recs {
		if seen[rc.name] {
			continue
		}
		if rc.used == 0 {
			_ = h.dropImage(rc.id)
		} else {
			problems = append(problems, msgs.M("{name}: folder deleted, but the image is used by machines", map[string]string{"name": rc.name}))
		}
	}
	if !quiet {
		h.auditKey(r, "image_scan", "Added {added}, updated {updated}", map[string]string{"added": strconv.Itoa(added), "updated": strconv.Itoa(updated)})
	}
	if ss := auth.FromContext(r.Context()); ss != nil && !quiet {
		h.Notify.Activity(ss.User.ID, 0, notify.Info, "image", "Image catalog scanned",
			"Images added: {added}, updated: {updated}, problems: {problems}.", "/images",
			notify.P("added", fmt.Sprint(added), "updated", fmt.Sprint(updated), "problems", fmt.Sprint(len(problems))))
	}
	for _, pr := range problems {
		if !quiet {
			h.Notify.SendAdmins(notify.Warning, "image", "Image problem", pr.Key, "/images", pr.Params)
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"added": added, "updated": updated, "problems": problems})
}

// ---------- rename and delete ----------

func (h *Handler) rename(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid id")
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if err := names.Check("Image name", in.Name); err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.DB.Catalog.UpdateImage(id, func(im *catalog.Image) error {
		im.Name = in.Name
		return nil
	}); errors.Is(err, catalog.ErrNotFound) {
		httpx.Err(w, http.StatusNotFound, "Image not found")
		return
	} else if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid id")
		return
	}
	im, ok := h.DB.Catalog.Image(id)
	if !ok {
		httpx.Err(w, http.StatusNotFound, "Image not found")
		return
	}
	dirname := im.Dirname
	if used := imageUsage(h.DB)[im.Dirname]; used > 0 {
		httpx.ErrKey(w, http.StatusConflict, "The image is used by devices ({count}) — replace it on them first", map[string]string{"count": fmt.Sprint(used)})
		return
	}
	if err := h.dropImage(id); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	if names.Valid(dirname) {
		_ = os.RemoveAll(filepath.Join(h.Cfg.DisksDir, dirname))
	}
	h.audit(r, "image_delete", dirname)
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.Notify.SendActive(ss.User.ID, notify.Info, "image", "Image deleted", "Image folder «{name}» removed from the catalog.", "/images", notify.P("name", dirname))
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// fmtSize prints the file size for the notification text.
func fmtSize(n int64) string {
	const k = 1024
	switch {
	case n >= k*k*k:
		return fmt.Sprintf("%.1f GB", float64(n)/(k*k*k))
	case n >= k*k:
		return fmt.Sprintf("%.1f MB", float64(n)/(k*k))
	case n >= k:
		return fmt.Sprintf("%.1f KB", float64(n)/k)
	}
	return fmt.Sprintf("%d B", n)
}

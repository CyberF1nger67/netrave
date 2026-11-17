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
	"unicode/utf8"

	"netrave/internal/auth"
	"netrave/internal/catalog"
	"netrave/internal/config"
	"netrave/internal/database"
	"netrave/internal/httpx"
	"netrave/internal/notify"
	"netrave/internal/qemu"
)

var (
	dirRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	fileRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
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
	Kind    string            `json:"kind"`
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
		return "", errors.New("qemu-img не смог прочитать файл (не образ диска или повреждён)")
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
		return "", errors.New("не удалось разобрать ответ qemu-img")
	}
	if info.Format != "qcow2" && info.Format != "raw" {
		return "", fmt.Errorf("формат %q не поддерживается (нужен qcow2 или raw)", info.Format)
	}
	if info.BackingFilename != "" || info.FormatSpecific.Data.DataFile != "" {
		return "", errors.New("диск ссылается на внешние файлы (backing/data-file) — не поддерживается")
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
		return errors.New("не похож на ISO 9660")
	}
	return nil
}

// inspectDir parses the image folder: disks in order and ISO files. It also returns the total size and problems.
func (h *Handler) inspectDir(dir string) (Layout, int64, []string) {
	var lay Layout
	var problems []string
	var size int64
	ents, err := os.ReadDir(dir)
	if err != nil {
		return lay, 0, []string{err.Error()}
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
			problems = append(problems, name+": "+err.Error())
			continue
		}
		lay.Disks = append(lay.Disks, Disk{Name: name, Format: format})
	}
	for _, name := range isos {
		if err := checkISO(filepath.Join(dir, name)); err != nil {
			problems = append(problems, name+": "+err.Error())
			continue
		}
		lay.CDROMs = append(lay.CDROMs, name)
	}
	return lay, size, problems
}

// imageUsage counts the machines that reference the image (the default image in lab topology).
func imageUsage(db *database.DB) map[int64]int {
	out := map[int64]int{}
	for _, d := range db.Labs.AllDevices() {
		if d.ImageID != 0 {
			out[d.ImageID]++
		}
	}
	return out
}

// dropImage deletes the image from the catalog. Instance row references to it are nulled:
// the disk and layer of instance machines lose the image.
func (h *Handler) dropImage(id int64) error {
	if err := h.DB.Catalog.DeleteImage(id); err != nil {
		return err
	}
	if _, err := h.DB.Exec(`UPDATE instance_devices SET image_id=NULL WHERE image_id=?`, id); err != nil {
		return err
	}
	_, err := h.DB.Exec(`UPDATE instance_devices SET layer_image_id=NULL WHERE layer_image_id=?`, id)
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
		it := item{ID: im.ID, Name: im.Name, Dirname: im.Dirname, Size: im.SizeBytes, Type: im.DeviceType, Kind: im.Kind}
		it.UsedBy = use[it.ID]
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
	if !dirRe.MatchString(dir) {
		httpx.Err(w, http.StatusBadRequest, "недопустимое имя папки образа (буквы, цифры, . _ -)")
		return
	}
	ext := strings.ToLower(filepath.Ext(file))
	if !fileRe.MatchString(file) || !(diskExt[ext] || ext == ".iso") {
		httpx.Err(w, http.StatusBadRequest, "допустимы файлы .qcow2, .raw, .img, .iso")
		return
	}
	limit := h.Cfg.MaxUploadBytes()
	if r.ContentLength > limit {
		httpx.Err(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("файл больше лимита %d МБ (настройка в System Settings)", limit>>20))
		return
	}
	if r.ContentLength > 0 && qemu.DiskFree(h.Cfg.ImagesDir) < uint64(r.ContentLength)+(1<<30) {
		httpx.Err(w, http.StatusInsufficientStorage, "недостаточно места на диске для загрузки")
		return
	}
	var used int
	// The state lives in instances (instance_devices): a machine that is currently running holds the image.
	if im, ok := h.DB.Catalog.ImageByDirname(dir); ok && h.M != nil {
		if rows, err := h.DB.Query(`SELECT id FROM instance_devices WHERE image_id=?`, im.ID); err == nil {
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
		httpx.Err(w, http.StatusConflict, "образ используется запущенными устройствами — остановите их, чтобы заменить файлы")
		return
	}

	target := filepath.Join(h.Cfg.ImagesDir, dir)
	if err := os.MkdirAll(target, 0o750); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "не удалось создать папку образа")
		return
	}
	tmp, err := os.CreateTemp(h.Cfg.TmpDir, "upload-*.part")
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "не удалось создать временный файл")
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	r.Body = http.MaxBytesReader(w, r.Body, limit)
	n, err := io.Copy(tmp, r.Body)
	tmp.Close()
	if err != nil {
		httpx.Err(w, http.StatusRequestEntityTooLarge, "загрузка прервана или превышает лимит размера")
		return
	}
	if n == 0 {
		httpx.Err(w, http.StatusBadRequest, "пустой файл")
		return
	}
	dest := filepath.Join(target, file)
	if err := moveFile(tmpName, dest); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "не удалось сохранить файл: "+err.Error())
		return
	}
	_ = os.Chmod(dest, 0o640)
	h.audit(r, "image_upload", dir+"/"+file)
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.Notify.Activity(ss.User.ID, 0, notify.Success, "image", "Файл образа загружен", dir+"/"+file+" ("+fmtSize(n)+").", "/images")
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
	ents, err := os.ReadDir(h.Cfg.ImagesDir)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "не удалось прочитать каталог образов")
		return
	}
	seen := map[string]bool{}
	problems := []string{}
	added, updated := 0, 0
	for _, e := range ents {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		if !dirRe.MatchString(name) {
			problems = append(problems, name+": недопустимое имя папки (буквы, цифры, . _ -)")
			continue
		}
		seen[name] = true
		lay, size, probs := h.inspectDir(filepath.Join(h.Cfg.ImagesDir, name))
		for _, p := range probs {
			problems = append(problems, name+": "+p)
		}
		if len(lay.Disks) == 0 && len(lay.CDROMs) == 0 {
			problems = append(problems, name+": нет дисков (.qcow2/.raw/.img) и нет ISO — папка пропущена")
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
			if _, err := h.DB.Catalog.AddImage(catalog.Image{Name: name, Dirname: name, Kind: "image",
				LayoutJSON: string(lj), SizeBytes: size, CreatedAt: time.Now().Unix()}); err != nil {
				problems = append(problems, name+": "+err.Error())
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
		recs = append(recs, rec{id: im.ID, name: im.Dirname, used: use[im.ID]})
	}
	for _, rc := range recs {
		if seen[rc.name] {
			continue
		}
		if rc.used == 0 {
			_ = h.dropImage(rc.id)
		} else {
			problems = append(problems, rc.name+": папка удалена, но образ используется устройствами")
		}
	}
	if !quiet {
		h.audit(r, "image_scan", fmt.Sprintf("добавлено %d, обновлено %d", added, updated))
	}
	if ss := auth.FromContext(r.Context()); ss != nil && !quiet {
		h.Notify.Activity(ss.User.ID, 0, notify.Info, "image", "Каталог образов просканирован",
			fmt.Sprintf("Добавлено образов: %d, обновлено: %d, проблем: %d.", added, updated, len(problems)), "/images")
	}
	for _, pr := range problems {
		if !quiet {
			h.Notify.SendAdmins(notify.Warning, "image", "Проблема с образом", pr, "/images")
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"added": added, "updated": updated, "problems": problems})
}

// ---------- rename and delete ----------

func (h *Handler) rename(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "некорректный id")
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 100 {
		httpx.Err(w, http.StatusBadRequest, "название: от 1 до 100 символов")
		return
	}
	if err := h.DB.Catalog.UpdateImage(id, func(im *catalog.Image) error {
		im.Name = in.Name
		return nil
	}); errors.Is(err, catalog.ErrNotFound) {
		httpx.Err(w, http.StatusNotFound, "образ не найден")
		return
	} else if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "некорректный id")
		return
	}
	im, ok := h.DB.Catalog.Image(id)
	if !ok {
		httpx.Err(w, http.StatusNotFound, "образ не найден")
		return
	}
	dirname := im.Dirname
	var layerRefs int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM instance_devices WHERE layer_image_id=?`, id).Scan(&layerRefs)
	layers := h.DB.Catalog.ImageChildren(id) + layerRefs
	if used := imageUsage(h.DB)[id]; used > 0 {
		httpx.Err(w, http.StatusConflict, fmt.Sprintf("образ используется устройствами (%d) — сначала замените его у них", used))
		return
	}
	if layers > 0 {
		httpx.Err(w, http.StatusConflict, "образ нужен снапшотам: сначала удалите зависящие от него снапшоты")
		return
	}
	if err := h.dropImage(id); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if dirRe.MatchString(dirname) {
		_ = os.RemoveAll(filepath.Join(h.Cfg.ImagesDir, dirname))
	}
	h.audit(r, "image_delete", dirname)
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.Notify.SendActive(ss.User.ID, notify.Info, "image", "Образ удалён", "Папка образа «"+dirname+"» удалена из каталога.", "/images")
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// fmtSize prints the file size for the notification text.
func fmtSize(n int64) string {
	const k = 1024
	switch {
	case n >= k*k*k:
		return fmt.Sprintf("%.1f ГБ", float64(n)/(k*k*k))
	case n >= k*k:
		return fmt.Sprintf("%.1f МБ", float64(n)/(k*k))
	case n >= k:
		return fmt.Sprintf("%.1f КБ", float64(n)/k)
	}
	return fmt.Sprintf("%d Б", n)
}

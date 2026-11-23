// SPDX-License-Identifier: GPL-3.0-only

package qemu

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"netrave/internal/catalog"
	"netrave/internal/msgs"
	"netrave/internal/names"
)

// Commit modes.
const (
	CommitImage    = "image"    // a new independent image; the machine switches to it
	CommitOriginal = "original" // overwrite the source image with the machine's current state
)

// ImagesMu serializes operations on the image catalog (commit and scan, images.scan).
var ImagesMu sync.Mutex

// Commit performs the commit mode. stopFirst: stop a running machine first and wait for it to stop.
// Returns the folder name of the created image (for CommitOriginal - of the overwritten image).
func (m *Manager) Commit(rid int64, mode, title string, stopFirst bool) (string, error) {
	if stopFirst {
		if err := m.stopAndWait(rid); err != nil {
			return "", err
		}
	}
	switch mode {
	case CommitImage:
		return m.newImage(rid, title)
	case CommitOriginal:
		return m.overwriteOriginal(rid)
	}
	return "", errors.New("Unknown commit mode")
}

func checkTitle(title string) error {
	return names.Check("Title", title)
}

// beginCommit locks the machine and the image catalog for the duration of the operation.
func (m *Manager) beginCommit(rid int64) (func(), error) {
	l, ok := m.lock(rid)
	if !ok {
		return nil, ErrBusy
	}
	ImagesMu.Lock()
	return func() { ImagesMu.Unlock(); l.Unlock() }, nil
}

// stopAndWait stops the machine and waits until the lock is released (the QEMU process has exited).
func (m *Manager) stopAndWait(rid int64) error {
	_, state, err := m.loadSpec(rid)
	if err != nil {
		return err
	}
	if state == "running" || state == "paused" {
		if err := m.Stop(rid, false); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		if l, ok := m.lock(rid); ok {
			_, st, err := m.loadSpec(rid)
			l.Unlock()
			if err != nil {
				return err
			}
			if st == "stopped" || st == "error" {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return errors.New("The device did not stop in time")
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// requireStopped checks that the machine is stopped and its disks exist; returns the spec.
func (m *Manager) requireStopped(rid int64) (*Spec, error) {
	s, state, err := m.loadSpec(rid)
	if err != nil {
		return nil, err
	}
	if state != "stopped" && state != "error" {
		return nil, errors.New("Commit is available only for a stopped machine")
	}
	if len(s.Disks) == 0 {
		return nil, errors.New("The device has no image selected")
	}
	for _, d := range s.Disks {
		if !fileExists(d.Overlay) {
			return nil, errors.New("The device disks are not created yet — start the device at least once")
		}
	}
	return s, nil
}

// sourceImage returns the machine's base image and the base image layout.
func (m *Manager) sourceImage(rid int64) (im catalog.Image, layout string, err error) {
	var dirname string
	if err = m.db.QueryRow(`SELECT COALESCE(image_id,'') FROM instance_devices WHERE id=?`, rid).
		Scan(&dirname); err != nil {
		return catalog.Image{}, "", errors.New("The device has no image selected")
	}
	if dirname != "" {
		if got, ok := m.db.Catalog.ImageByDirname(dirname); ok {
			im, layout = got, got.LayoutJSON
		}
	}
	if layout == "" || dirname == "" {
		return catalog.Image{}, "", errors.New("The device has no image selected")
	}
	return im, layout, nil
}

// convertDisk merges the machine's overlay with the whole chain (layers and base image) into a standalone dst file.
func (m *Manager) convertDisk(d DiskSpec, dst string) error {
	f := d.BaseFmt
	if f == "" {
		f = "qcow2"
	}
	_, err := m.qemuImg("convert", "-f", "qcow2", "-O", f, d.Overlay, dst)
	return err
}

// baseNeed - space for building the image: the size of the base images plus a margin.
func baseNeed(s *Spec) uint64 {
	need := uint64(1 << 30)
	for _, d := range s.Disks {
		if fi, err := os.Stat(d.Base); err == nil {
			need += uint64(fi.Size())
		}
	}
	return need
}

func diskDirName(prefix, title string) string {
	return fmt.Sprintf("%s%s-%d", prefix, slugName(title), time.Now().Unix())
}

// linkCDROMs adds the image's ISOs to the directory (hard link, copy on failure). ISOs are read-only.
func linkCDROMs(cdroms []string, dir string) error {
	for _, c := range cdroms {
		dst := filepath.Join(dir, filepath.Base(c))
		if err := os.Link(c, dst); err != nil {
			if err := copyFile(c, dst); err != nil {
				return err
			}
		}
	}
	return nil
}

func dirSize(dir string) int64 {
	var n int64
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if fi, err := e.Info(); err == nil {
			n += fi.Size()
		}
	}
	return n
}

// newRow - data for a new image catalog row.
type newRow struct {
	title, dirname, layout string
	size                   int64
}

// recordImage adds an image record and calls after in the same transaction (switching the machine, etc.).
func (m *Manager) recordImage(rid int64, r newRow, after func(tx *sql.Tx, imgID int64) error) (int64, error) {
	dtype := m.deviceType(rid) // queries outside the transaction
	qemu := m.qemuDefaults(rid)
	// The image lives in the catalog (files), so it is added first; if the SQL part fails, the record is rolled back.
	id, err := m.db.Catalog.AddImage(catalog.Image{
		Name: r.title, Dirname: r.dirname, DeviceType: dtype,
		LayoutJSON: r.layout, QemuJSON: qemu, SizeBytes: r.size, CreatedAt: time.Now().Unix(),
	})
	if err != nil {
		return 0, err
	}
	tx, err := m.db.Begin()
	if err != nil {
		_ = m.db.Catalog.DeleteImage(id)
		return 0, err
	}
	defer tx.Rollback()
	if err := after(tx, id); err != nil {
		_ = m.db.Catalog.DeleteImage(id)
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		_ = m.db.Catalog.DeleteImage(id)
		return 0, err
	}
	return id, nil
}

// newImage: an independent image from the machine's current state (the disk is merged into one file without references
// to other images). The machine switches to the new image and its overlay is deleted.
func (m *Manager) newImage(rid int64, title string) (string, error) {
	if err := checkTitle(title); err != nil {
		return "", err
	}
	unlock, err := m.beginCommit(rid)
	if err != nil {
		return "", err
	}
	defer unlock()
	s, err := m.requireStopped(rid)
	if err != nil {
		return "", err
	}
	_, layout, err := m.sourceImage(rid)
	if err != nil {
		return "", err
	}
	if DiskFree(m.cfg.DisksDir) < baseNeed(s) {
		return "", errors.New("Not enough space in the image catalog to commit")
	}

	dirname := diskDirName("commit-", title)
	final := filepath.Join(m.cfg.DisksDir, dirname)
	tmp := filepath.Join(m.cfg.DisksDir, "."+dirname+".tmp")
	if err := os.Mkdir(tmp, 0o750); err != nil {
		return "", err
	}
	renamed, done := false, false
	var moved []string
	defer func() {
		if done {
			return
		}
		for _, o := range moved {
			_ = os.Rename(o+".committed", o)
		}
		if renamed {
			_ = os.RemoveAll(final)
		} else {
			_ = os.RemoveAll(tmp)
		}
	}()

	for _, d := range s.Disks {
		if err := m.convertDisk(d, filepath.Join(tmp, filepath.Base(d.Base))); err != nil {
			return "", msgs.Wrap(err, "Creating image: {detail}", map[string]string{"detail": err.Error()})
		}
	}
	if err := linkCDROMs(s.CDROMs, tmp); err != nil {
		return "", err
	}
	size := dirSize(tmp)
	if err := os.Rename(tmp, final); err != nil {
		return "", err
	}
	renamed = true
	for _, d := range s.Disks {
		if err := os.Rename(d.Overlay, d.Overlay+".committed"); err != nil {
			return "", msgs.Wrap(err, "Moving disk: {detail}", map[string]string{"detail": err.Error()})
		}
		moved = append(moved, d.Overlay)
	}
	_, err = m.recordImage(rid, newRow{title: title, dirname: dirname, layout: layout, size: size},
		func(tx *sql.Tx, imgID int64) error {
			_, err := tx.Exec(`UPDATE instance_devices SET image_id=?, image_own=1 WHERE id=?`, dirname, rid)
			return err
		})
	if err != nil {
		return "", err
	}
	done = true
	for _, o := range moved {
		_ = os.Remove(o + ".committed")
	}
	return dirname, nil
}

// originalDependency - another machine whose working disk depends on the image.
type originalDependency struct {
	rid int64
}

// originalDependencies returns the machines that use the image (except the current one).
func (m *Manager) originalDependencies(dirname string, rid int64) ([]originalDependency, error) {
	rows, err := m.db.Query(`SELECT r.id FROM instance_devices r
		WHERE r.image_id=? AND r.id<>?`, dirname, rid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deps []originalDependency
	for rows.Next() {
		var d originalDependency
		if rows.Scan(&d.rid) == nil {
			deps = append(deps, d)
		}
	}
	return deps, rows.Err()
}

// rebaseLayer switches a QCOW2 layer's backing file to a new base disk without rewriting data.
func (m *Manager) rebaseLayer(overlay, base, format string) error {
	args := []string{"rebase", "-u", "-f", "qcow2"}
	if format != "" {
		args = append(args, "-F", format)
	}
	args = append(args, "-b", base, overlay)
	_, err := m.qemuImg(args...)
	return err
}

// backingPointsTo checks that the working layer still depends on the base file being replaced.
// A layer already switched to the saved copy of the previous generation is left alone: its data
// refers specifically to that content, not to the new image.
func (m *Manager) backingPointsTo(overlay, base string) (bool, error) {
	out, err := m.qemuImg("info", "--output=json", overlay)
	if err != nil {
		return false, err
	}
	var info struct {
		Backing string `json:"backing-filename"`
	}
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		return false, err
	}
	if info.Backing == "" {
		return false, nil
	}
	// QEMU stores the relative backing path relative to the layer's own directory.
	backing := info.Backing
	if !filepath.IsAbs(backing) {
		backing = filepath.Join(filepath.Dir(overlay), backing)
	}
	return filepath.Clean(backing) == filepath.Clean(base), nil
}

// overwriteOriginal rewrites the base image with the machine's current state (the whole chain is merged
// into a new file). Other machines are neither stopped nor deleted: the previous base disk is kept in a
// hidden generation directory, and their backing files are switched to it (stopped ones immediately,
// running ones at the next start via a marker). This keeps their saved changes intact, and only machines
// without their own disk or after a wipe (Wipe) receive the new image. The swap is safe: new files are
// prepared alongside, the old ones are kept as hard links (.bak), the swap is an atomic rename; on error
// everything is rolled back.
func (m *Manager) overwriteOriginal(rid int64) (string, error) {
	unlock, err := m.beginCommit(rid)
	if err != nil {
		return "", err
	}
	defer unlock()
	s, err := m.requireStopped(rid)
	if err != nil {
		return "", err
	}
	im, _, err := m.sourceImage(rid)
	if err != nil {
		return "", err
	}
	imgID, dirname := im.ID, im.Dirname
	deps, err := m.originalDependencies(dirname, rid)
	if err != nil {
		return "", err
	}
	// We do not stop running machines: their QEMU already holds the previous files open,
	// and their working disks switch to the saved copy of the previous image at the next
	// start (see applyPendingRebases). They get the new image only after a disk wipe.
	depActive := map[int64]bool{}
	for _, dep := range deps {
		_, state, err := m.loadSpec(dep.rid)
		if err != nil {
			return "", err
		}
		depActive[dep.rid] = m.get(dep.rid) != nil || (state != "stopped" && state != "error")
	}
	if DiskFree(m.cfg.DisksDir) < baseNeed(s) {
		return "", errors.New("Not enough space to overwrite the image")
	}

	ts := time.Now().UnixNano()
	// Hidden directory next to the image: image directory scans do not see it,
	// and the machines' old layers keep working after the base file is swapped.
	// Without dependent machines no copy is needed - then the old behavior.
	backupDir := ""
	if len(deps) > 0 {
		backupDir = filepath.Join(filepath.Dir(s.Disks[0].Base), fmt.Sprintf(".orig-%s-%d", dirname, ts))
		if err := os.MkdirAll(backupDir, 0o750); err != nil {
			return "", msgs.Wrap(err, "Image backup: {detail}", map[string]string{"detail": err.Error()})
		}
	}
	type rebase struct {
		overlay string
		oldBase string
		format  string
	}
	var news, baks, moved []string
	var rebases []rebase
	// deferred - markers written in this pass. They must be removed on error:
	// the previous generation directory gets deleted and the reference would become stale.
	var deferred []string
	swapped, done := 0, false
	defer func() {
		if done {
			return
		}
		for _, marker := range deferred {
			_ = os.Remove(marker)
		}
		for i := range baks {
			if i < swapped {
				_ = os.Rename(baks[i], s.Disks[i].Base)
			} else {
				_ = os.Remove(baks[i])
			}
		}
		// Return dependent layers to their previous backing files; the backup
		// directory itself can be removed only if all switches were rolled back.
		for _, r := range rebases {
			_ = m.rebaseLayer(r.overlay, r.oldBase, r.format)
		}
		for _, f := range news {
			_ = os.Remove(f)
		}
		for _, o := range moved {
			_ = os.Rename(o+".committed", o)
		}
		if backupDir != "" {
			_ = os.RemoveAll(backupDir)
		}
	}()

	for _, d := range s.Disks {
		nf := d.Base + fmt.Sprintf(".new-%d", ts)
		news = append(news, nf)
		if err := m.convertDisk(d, nf); err != nil {
			return "", msgs.Wrap(err, "Preparing disk: {detail}", map[string]string{"detail": err.Error()})
		}
	}
	for _, d := range s.Disks {
		bk := d.Base + fmt.Sprintf(".bak-%d", ts)
		if err := os.Link(d.Base, bk); err != nil {
			return "", msgs.Wrap(err, "Image backup: {detail}", map[string]string{"detail": err.Error()})
		}
		baks = append(baks, bk)
	}
	// Copy of the previous disk for dependent machines. A hard link takes no space:
	// after the base file is swapped, this very inode remains their backing file.
	baseByName := map[string]string{}
	backupByName := map[string]string{}
	if backupDir != "" {
		for _, d := range s.Disks {
			name := filepath.Base(d.Base)
			baseByName[name] = d.Base
			bk := filepath.Join(backupDir, name)
			if err := os.Link(d.Base, bk); err != nil {
				return "", msgs.Wrap(err, "Saving the previous image: {detail}", map[string]string{"detail": err.Error()})
			}
			backupByName[name] = bk
		}
	}
	for _, dep := range deps {
		spec, _, err := m.loadSpec(dep.rid)
		if err != nil {
			return "", err
		}
		// A running machine or one transitioning between states keeps writing to its
		// disk: its backing file cannot be switched on the fly. We remember the previous image
		// with a marker and return the disk to it before the next start.
		active := depActive[dep.rid]
		for _, d := range spec.Disks {
			old := backupByName[filepath.Base(d.Base)]
			if old == "" || !fileExists(d.Overlay) {
				continue
			}
			marker := rebaseMarker(d.Overlay)
			if fileExists(marker) {
				// The disk is already deferred to an image of an earlier generation: its data
				// belongs to that image, and it must not be re-pointed to the current one.
				continue
			}
			if active {
				if err := os.WriteFile(marker, []byte(old), 0o640); err != nil {
					return "", msgs.Wrap(err, "Deferred disk switch of a dependent machine: {detail}", map[string]string{"detail": err.Error()})
				}
				deferred = append(deferred, marker)
				continue
			}
			same, err := m.backingPointsTo(d.Overlay, d.Base)
			if err != nil {
				return "", msgs.Wrap(err, "Checking the disk of a dependent machine: {detail}", map[string]string{"detail": err.Error()})
			}
			if !same {
				continue
			}
			format := d.BaseFmt
			if format == "" {
				format = "qcow2"
			}
			if err := m.rebaseLayer(d.Overlay, old, format); err != nil {
				return "", msgs.Wrap(err, "Switching the disk of a dependent machine: {detail}", map[string]string{"detail": err.Error()})
			}
			rebases = append(rebases, rebase{overlay: d.Overlay, oldBase: d.Base, format: format})
		}
	}
	for _, d := range s.Disks {
		if err := os.Rename(d.Overlay, d.Overlay+".committed"); err != nil {
			return "", msgs.Wrap(err, "Moving disk: {detail}", map[string]string{"detail": err.Error()})
		}
		moved = append(moved, d.Overlay)
	}
	for i, d := range s.Disks {
		if err := os.Rename(news[i], d.Base); err != nil {
			return "", msgs.Wrap(err, "Replacing the image: {detail}", map[string]string{"detail": err.Error()})
		}
		swapped++
	}

	var size int64
	for _, d := range s.Disks {
		if fi, err := os.Stat(d.Base); err == nil {
			size += fi.Size()
		}
	}
	for _, c := range s.CDROMs {
		if fi, err := os.Stat(c); err == nil {
			size += fi.Size()
		}
	}
	if err := m.db.Catalog.UpdateImage(imgID, func(im *catalog.Image) error {
		im.SizeBytes = size
		return nil
	}); err != nil {
		return "", err
	}
	done = true
	for _, b := range baks {
		_ = os.Remove(b)
	}
	for _, o := range moved {
		_ = os.Remove(o + ".committed")
	}
	if backupDir != "" && len(rebases) == 0 && len(deferred) == 0 {
		_ = os.RemoveAll(backupDir) // nobody needed the copy
	}
	return dirname, nil
}

// slugName builds a safe directory name from a title (latin letters, digits, "-", "_").
func slugName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteRune('-')
		}
		if b.Len() >= 40 {
			break
		}
	}
	if out := strings.Trim(b.String(), "-"); out != "" {
		return out
	}
	return "node"
}

// deviceType returns the device type (template type); images are filtered by it in the add-machine menu.
func (m *Manager) deviceType(rid int64) string {
	var devID, labName string
	_ = m.db.QueryRow(`SELECT r.device_id, i.lab_id FROM instance_devices r JOIN lab_instances i ON i.id=r.instance_id WHERE r.id=?`, rid).Scan(&devID, &labName)
	t := "generic"
	if fd, _, ok := m.db.Labs.Device(labName, devID); ok && fd.TemplateID != 0 {
		if tpl, ok := m.db.Catalog.Template(fd.TemplateID); ok && tpl.Type != "" {
			t = tpl.Type
		}
	}
	return t
}

// qemuDefaults - the machine's QEMU parameters in JSON: they become the new image's defaults.
func (m *Manager) qemuDefaults(rid int64) string {
	var mach, bios, bus, cons, extra string
	_ = m.db.QueryRow(`SELECT machine,bios,disk_bus,console,extra_args FROM instance_devices WHERE id=?`, rid).
		Scan(&mach, &bios, &bus, &cons, &extra)
	b, _ := json.Marshal(map[string]string{"machine": mach, "bios": bios, "disk_bus": bus, "console": cons, "extra_args": extra})
	return string(b)
}

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
	"netrave/internal/names"
)

// Snapshot menu modes.
const (
	CommitSnapshot = "snapshot" // snapshot: a separate QCOW2 layer on top of the base image; the main disk is not copied
	CommitImage    = "image"    // a new independent image; the machine switches to it
	CommitOriginal = "original" // overwrite the source image with the machine's current state
)

// ImagesMu serializes operations on the image catalog (commit and scan, images.scan).
var ImagesMu sync.Mutex

// Commit performs the commit mode. stopFirst: stop a running machine first and wait for it to stop.
// Returns the id of the created image or snapshot (for CommitOriginal - the id of the overwritten image).
func (m *Manager) Commit(rid int64, mode, title string, stopFirst bool) (int64, error) {
	if stopFirst {
		if err := m.stopAndWait(rid); err != nil {
			return 0, err
		}
	}
	switch mode {
	case CommitSnapshot:
		return m.takeSnapshot(rid, title)
	case CommitImage:
		return m.newImage(rid, title)
	case CommitOriginal:
		return m.overwriteOriginal(rid)
	}
	return 0, errors.New("неизвестный режим фиксации")
}

func checkTitle(title string) error {
	return names.Check("название", title)
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
			return errors.New("устройство не остановилось за отведённое время")
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
		return nil, errors.New("фиксация доступна только для остановленной машины")
	}
	if len(s.Disks) == 0 {
		return nil, errors.New("у устройства не выбран образ")
	}
	for _, d := range s.Disks {
		if !fileExists(d.Overlay) {
			return nil, errors.New("диски устройства ещё не созданы — запустите устройство хотя бы один раз")
		}
	}
	return s, nil
}

// sourceImage returns the machine's base image, its current layer (0 - none) and the base image layout.
func (m *Manager) sourceImage(rid int64) (baseID, layerID int64, layout string, err error) {
	if err = m.db.QueryRow(`SELECT COALESCE(image_id,0), COALESCE(layer_image_id,0) FROM instance_devices WHERE id=?`, rid).
		Scan(&baseID, &layerID); err != nil {
		return 0, 0, "", errors.New("у устройства не выбран образ")
	}
	if baseID != 0 {
		if im, ok := m.db.Catalog.Image(baseID); ok {
			layout = im.LayoutJSON
		}
	}
	if layout == "" || baseID == 0 {
		return 0, 0, "", errors.New("у устройства не выбран образ")
	}
	return baseID, layerID, layout, nil
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

// layerLayout - the layout of an image whose disks are all QCOW2 layers (layers are always created in this format).
func layerLayout(src string) (string, error) {
	var lay map[string]any
	if err := json.Unmarshal([]byte(src), &lay); err != nil {
		return "", err
	}
	if ds, ok := lay["disks"].([]any); ok {
		for _, d := range ds {
			if mm, ok := d.(map[string]any); ok {
				mm["format"] = "qcow2"
			}
		}
	}
	b, err := json.Marshal(lay)
	return string(b), err
}

// newRow - data for a new image catalog row.
type newRow struct {
	title, dirname, layout, kind string
	size, parent                 int64
}

// recordImage adds an image record and calls after in the same transaction (switching the machine, etc.).
func (m *Manager) recordImage(rid int64, r newRow, after func(tx *sql.Tx, imgID int64) error) (int64, error) {
	dtype := m.deviceType(rid) // queries outside the transaction
	qemu := m.qemuDefaults(rid)
	// The image lives in the catalog (files), so it is added first; if the SQL part fails, the record is rolled back.
	id, err := m.db.Catalog.AddImage(catalog.Image{
		Name: r.title, Dirname: r.dirname, Kind: r.kind, ParentID: r.parent, DeviceType: dtype,
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

// takeSnapshot: the machine's current overlay is moved to the image catalog as a snapshot layer (it already contains changes
// on top of the base), and a new empty overlay is created for the machine on top of that layer. The main disk is not copied.
// Chain: base <- snapshot layer <- machine overlay. Layers do not change after creation.
func (m *Manager) takeSnapshot(rid int64, title string) (int64, error) {
	if err := checkTitle(title); err != nil {
		return 0, err
	}
	unlock, err := m.beginCommit(rid)
	if err != nil {
		return 0, err
	}
	defer unlock()
	s, err := m.requireStopped(rid)
	if err != nil {
		return 0, err
	}
	baseID, layerID, layout, err := m.sourceImage(rid)
	if err != nil {
		return 0, err
	}
	lay, err := layerLayout(layout)
	if err != nil {
		return 0, err
	}
	parent := layerID
	if parent == 0 {
		parent = baseID
	}
	if DiskFree(m.cfg.ImagesDir) < 1<<30 {
		return 0, errors.New("недостаточно места в каталоге образов для снапшота")
	}

	dirname := diskDirName("snap-", title)
	final := filepath.Join(m.cfg.ImagesDir, dirname)
	tmp := filepath.Join(m.cfg.ImagesDir, "."+dirname+".tmp")
	if err := os.Mkdir(tmp, 0o750); err != nil {
		return 0, err
	}
	type move struct{ name, overlay string }
	var moves []move
	var created []string
	renamed, done := false, false
	defer func() {
		if done {
			return
		}
		for _, c := range created {
			_ = os.Remove(c)
		}
		dir := tmp
		if renamed {
			dir = final
		}
		for _, mv := range moves {
			_ = os.Rename(filepath.Join(dir, mv.name), mv.overlay)
		}
		if renamed {
			_ = os.RemoveAll(final)
		} else {
			_ = os.RemoveAll(tmp)
		}
	}()

	for _, d := range s.Disks {
		name := filepath.Base(d.Base)
		if err := os.Rename(d.Overlay, filepath.Join(tmp, name)); err != nil {
			return 0, fmt.Errorf("перенос диска: %w", err)
		}
		moves = append(moves, move{name, d.Overlay})
	}
	if err := linkCDROMs(s.CDROMs, tmp); err != nil {
		return 0, err
	}
	size := dirSize(tmp)
	if err := os.Rename(tmp, final); err != nil {
		return 0, err
	}
	renamed = true
	for _, mv := range moves {
		if _, err := m.qemuImg("create", "-f", "qcow2", "-F", "qcow2", "-b", filepath.Join(final, mv.name), mv.overlay); err != nil {
			return 0, fmt.Errorf("новый слой поверх снапшота: %w", err)
		}
		created = append(created, mv.overlay)
	}
	id, err := m.recordImage(rid, newRow{title: title, dirname: dirname, layout: lay, kind: "snapshot", size: size, parent: parent},
		func(tx *sql.Tx, imgID int64) error {
			_, err := tx.Exec(`UPDATE instance_devices SET layer_image_id=? WHERE id=?`, imgID, rid)
			return err
		})
	if err != nil {
		return 0, err
	}
	done = true
	return id, nil
}

// newImage: an independent image from the machine's current state (the whole chain is merged into one file without references
// to other images). The machine switches to the new image, its overlay and layer are deleted; snapshot layers
// remain in the catalog because other snapshots and machines may reference them.
func (m *Manager) newImage(rid int64, title string) (int64, error) {
	if err := checkTitle(title); err != nil {
		return 0, err
	}
	unlock, err := m.beginCommit(rid)
	if err != nil {
		return 0, err
	}
	defer unlock()
	s, err := m.requireStopped(rid)
	if err != nil {
		return 0, err
	}
	_, _, layout, err := m.sourceImage(rid)
	if err != nil {
		return 0, err
	}
	if DiskFree(m.cfg.ImagesDir) < baseNeed(s) {
		return 0, errors.New("недостаточно места в каталоге образов для фиксации")
	}

	dirname := diskDirName("commit-", title)
	final := filepath.Join(m.cfg.ImagesDir, dirname)
	tmp := filepath.Join(m.cfg.ImagesDir, "."+dirname+".tmp")
	if err := os.Mkdir(tmp, 0o750); err != nil {
		return 0, err
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
			return 0, fmt.Errorf("создание образа: %w", err)
		}
	}
	if err := linkCDROMs(s.CDROMs, tmp); err != nil {
		return 0, err
	}
	size := dirSize(tmp)
	if err := os.Rename(tmp, final); err != nil {
		return 0, err
	}
	renamed = true
	for _, d := range s.Disks {
		if err := os.Rename(d.Overlay, d.Overlay+".committed"); err != nil {
			return 0, fmt.Errorf("перенос диска: %w", err)
		}
		moved = append(moved, d.Overlay)
	}
	id, err := m.recordImage(rid, newRow{title: title, dirname: dirname, layout: layout, kind: "image", size: size},
		func(tx *sql.Tx, imgID int64) error {
			if _, err := tx.Exec(`UPDATE instance_devices SET image_id=?, image_own=1, layer_image_id=NULL WHERE id=?`, imgID, rid); err != nil {
				return err
			}
			// The machine's snapshots are deleted before the commit: if it fails, the instance record is rolled back.
			return m.db.Catalog.DeleteSnapshotsOf(rid)
		})
	if err != nil {
		return 0, err
	}
	done = true
	for _, o := range moved {
		_ = os.Remove(o + ".committed")
	}
	return id, nil
}

// imageDisk - a disk from the image catalog's layout_json.
type imageDisk struct {
	Name   string `json:"name"`
	Format string `json:"format"`
}

// imageDisks parses the disk list from layout_json.
func imageDisks(layout string) []imageDisk {
	var lay struct {
		Disks []imageDisk `json:"disks"`
	}
	if json.Unmarshal([]byte(layout), &lay) != nil {
		return nil
	}
	return lay.Disks
}

// originalDependency - another machine whose working disk depends on the image.
type originalDependency struct {
	rid     int64
	layerID int64
}

// originalDependencies returns the machines that use the image (except the current one).
func (m *Manager) originalDependencies(imgID, rid int64) ([]originalDependency, error) {
	rows, err := m.db.Query(`SELECT r.id, COALESCE(r.layer_image_id,0) FROM instance_devices r
		WHERE r.image_id=? AND r.id<>?`, imgID, rid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deps []originalDependency
	for rows.Next() {
		var d originalDependency
		if rows.Scan(&d.rid, &d.layerID) == nil {
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
func (m *Manager) overwriteOriginal(rid int64) (int64, error) {
	unlock, err := m.beginCommit(rid)
	if err != nil {
		return 0, err
	}
	defer unlock()
	s, err := m.requireStopped(rid)
	if err != nil {
		return 0, err
	}
	imgID, _, _, err := m.sourceImage(rid)
	if err != nil {
		return 0, err
	}
	deps, err := m.originalDependencies(imgID, rid)
	if err != nil {
		return 0, err
	}
	// We do not stop running machines: their QEMU already holds the previous files open,
	// and their working disks switch to the saved copy of the previous image at the next
	// start (see applyPendingRebases). They get the new image only after a disk wipe.
	var children []catalog.Image
	parents := map[int64]int64{}
	for _, im := range m.db.Catalog.Images() {
		parents[im.ID] = im.ParentID
		if im.ParentID == imgID {
			children = append(children, im)
		}
	}
	// The state of dependent machines is also needed for snapshot layers: a running machine
	// prevents changing a layer it is currently using.
	depActive := map[int64]bool{}
	for _, dep := range deps {
		_, state, err := m.loadSpec(dep.rid)
		if err != nil {
			return 0, err
		}
		depActive[dep.rid] = m.get(dep.rid) != nil || (state != "stopped" && state != "error")
	}
	activeLayers := map[int64]bool{}
	for _, dep := range deps {
		if dep.layerID == 0 || !depActive[dep.rid] {
			continue
		}
		seen := map[int64]bool{}
		for id := dep.layerID; id != 0 && !seen[id]; id = parents[id] {
			seen[id] = true
			activeLayers[id] = true
		}
	}
	if DiskFree(m.cfg.ImagesDir) < baseNeed(s) {
		return 0, errors.New("недостаточно места для перезаписи образа")
	}

	ts := time.Now().UnixNano()
	// Hidden directory next to the image: image directory scans do not see it,
	// and the machines' old layers keep working after the base file is swapped.
	// Without dependent machines and snapshots no copy is needed - then the old behavior.
	backupDir := ""
	if len(deps) > 0 || len(children) > 0 {
		backupDir = filepath.Join(filepath.Dir(s.Disks[0].Base), fmt.Sprintf(".orig-%d-%d", imgID, ts))
		if err := os.MkdirAll(backupDir, 0o750); err != nil {
			return 0, fmt.Errorf("резервная копия образа: %w", err)
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
			return 0, fmt.Errorf("подготовка диска: %w", err)
		}
	}
	for _, d := range s.Disks {
		bk := d.Base + fmt.Sprintf(".bak-%d", ts)
		if err := os.Link(d.Base, bk); err != nil {
			return 0, fmt.Errorf("резервная копия образа: %w", err)
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
				return 0, fmt.Errorf("сохранение прежнего образа: %w", err)
			}
			backupByName[name] = bk
		}
	}
	// Switch snapshot layers first: their files live in the image directory and are not
	// tied to a single machine. Then the working disks of machines without a snapshot layer.
	for _, child := range children {
		// Do not change a snapshot layer that a running machine currently uses on
		// the fly: set a marker and return the layer to the previous image before it starts.
		if activeLayers[child.ID] {
			for _, disk := range imageDisks(child.LayoutJSON) {
				old := backupByName[disk.Name]
				if old == "" {
					continue
				}
				layer := filepath.Join(m.cfg.ImagesDir, child.Dirname, disk.Name)
				if !fileExists(layer) {
					continue
				}
				marker := rebaseMarker(layer)
				if fileExists(marker) {
					continue // already deferred to an earlier generation
				}
				if err := os.WriteFile(marker, []byte(old), 0o640); err != nil {
					return 0, fmt.Errorf("отложенное переключение слоя снапшота: %w", err)
				}
				deferred = append(deferred, marker)
			}
			continue
		}
		for _, disk := range imageDisks(child.LayoutJSON) {
			base := baseByName[disk.Name]
			old := backupByName[disk.Name]
			if base == "" || old == "" {
				continue
			}
			overlay := filepath.Join(m.cfg.ImagesDir, child.Dirname, disk.Name)
			if !fileExists(overlay) {
				continue
			}
			same, err := m.backingPointsTo(overlay, base)
			if err != nil {
				return 0, fmt.Errorf("проверка снапшота: %w", err)
			}
			if !same {
				continue // the layer already points to the saved copy of the previous generation
			}
			format := disk.Format
			if format == "" {
				format = "qcow2"
			}
			if err := m.rebaseLayer(overlay, old, format); err != nil {
				return 0, fmt.Errorf("переключение снапшота: %w", err)
			}
			rebases = append(rebases, rebase{overlay: overlay, oldBase: base, format: format})
		}
	}
	for _, dep := range deps {
		if dep.layerID != 0 {
			continue // the working disk refers to a snapshot layer already switched above
		}
		spec, _, err := m.loadSpec(dep.rid)
		if err != nil {
			return 0, err
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
					return 0, fmt.Errorf("отложенное переключение диска зависимой машины: %w", err)
				}
				deferred = append(deferred, marker)
				continue
			}
			same, err := m.backingPointsTo(d.Overlay, d.Base)
			if err != nil {
				return 0, fmt.Errorf("проверка диска зависимой машины: %w", err)
			}
			if !same {
				continue
			}
			format := d.BaseFmt
			if format == "" {
				format = "qcow2"
			}
			if err := m.rebaseLayer(d.Overlay, old, format); err != nil {
				return 0, fmt.Errorf("переключение диска зависимой машины: %w", err)
			}
			rebases = append(rebases, rebase{overlay: d.Overlay, oldBase: d.Base, format: format})
		}
	}
	for _, d := range s.Disks {
		if err := os.Rename(d.Overlay, d.Overlay+".committed"); err != nil {
			return 0, fmt.Errorf("перенос диска: %w", err)
		}
		moved = append(moved, d.Overlay)
	}
	for i, d := range s.Disks {
		if err := os.Rename(news[i], d.Base); err != nil {
			return 0, fmt.Errorf("замена образа: %w", err)
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
		return 0, err
	}
	if err := m.db.Catalog.DeleteSnapshotsOf(rid); err != nil {
		return 0, err
	}
	// The current machine now runs directly from the updated base image.
	if _, err := m.db.Exec(`UPDATE instance_devices SET layer_image_id=NULL WHERE id=?`, rid); err != nil {
		return 0, err
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
	return imgID, nil
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
	var devID int64
	_ = m.db.QueryRow(`SELECT device_id FROM instance_devices WHERE id=?`, rid).Scan(&devID)
	t := "generic"
	if fd, _, ok := m.db.Labs.Device(devID); ok && fd.TemplateID != 0 {
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

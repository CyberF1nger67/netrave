// SPDX-License-Identifier: GPL-3.0-only

// Package catalog stores catalogs: images, machine templates and snapshots. Each entry is a separate
// YAML file whose name equals the entry name: <dir>/<table>/<name>.yaml. The file stores
// the numeric entry number (labs reference it). The number counter lives in index.yaml and is never
// reused.
package catalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"netrave/internal/names"
)

// ErrNotFound - no row with this number.
var ErrNotFound = errors.New("запись не найдена")

const indexFile = "index.yaml"

// index - the table's number counter.
type index struct {
	NextID int64 `yaml:"next_id"`
}

// table - a table of entries T. id returns a pointer to the entry number field, key is the entry name,
// used to build the file name.
type table[T any] struct {
	dir  string
	id   func(*T) *int64
	key  func(*T) string
	mu   sync.Mutex
	next int64
	rows []T
}

// openTable opens the table in directory dir. fresh reports that the table does not exist yet.
func openTable[T any](dir string, id func(*T) *int64, key func(*T) string) (*table[T], bool, error) {
	t := &table[T]{dir: dir, id: id, key: key, next: 1}
	_, err := os.Stat(filepath.Join(dir, indexFile))
	fresh := errors.Is(err, os.ErrNotExist)
	if err != nil && !fresh {
		return nil, false, err
	}
	if err := t.load(); err != nil {
		return nil, false, err
	}
	return t, fresh, nil
}

// load reads the counter and all table entries from its directory.
func (t *table[T]) load() error {
	t.next = 1
	idxPath := filepath.Join(t.dir, indexFile)
	if data, err := os.ReadFile(idxPath); err == nil {
		var idx index
		if err := yaml.Unmarshal(data, &idx); err != nil {
			return fmt.Errorf("%s: %w", idxPath, err)
		}
		if idx.NextID > t.next {
			t.next = idx.NextID
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	entries, err := os.ReadDir(t.dir)
	if errors.Is(err, os.ErrNotExist) {
		t.rows = nil
		return nil
	}
	if err != nil {
		return err
	}
	rows := []T{}
	seenIDs := map[int64]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == indexFile || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		stem := strings.TrimSuffix(name, ".yaml")
		if !names.Valid(stem) {
			return fmt.Errorf("%s: имя файла должно быть латинским (буквы, цифры, '-', '_')", filepath.Join(t.dir, name))
		}
		path := filepath.Join(t.dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var r T
		if err := yaml.Unmarshal(data, &r); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if got := t.key(&r); got != stem {
			return fmt.Errorf("%s: имя записи %q не совпадает с именем файла", path, got)
		}
		id := *t.id(&r)
		if other, dup := seenIDs[id]; dup {
			return fmt.Errorf("%s: номер %d уже занят файлом %s.yaml", path, id, other)
		}
		seenIDs[id] = stem
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return *t.id(&rows[i]) < *t.id(&rows[j]) })
	for i := range rows {
		if id := *t.id(&rows[i]); id >= t.next {
			t.next = id + 1
		}
	}
	t.rows = rows
	return nil
}

// path - the entry file named key.
func (t *table[T]) path(key string) string {
	return filepath.Join(t.dir, key+".yaml")
}

// persist writes the new table state: first the counter, then the changed entries, then deletes files
// of entries that no longer exist or were renamed. Counter first: after a failure, the number already issued to an entry
// will not be issued again. Each entry is a separate file, so the catalog remains readable after a failure.
func (t *table[T]) persist(rows []T, next int64) error {
	if err := os.MkdirAll(t.dir, 0o750); err != nil {
		return err
	}
	byKey := make(map[string]int64, len(rows))
	for i := range rows {
		key := t.key(&rows[i])
		if err := names.Check("имя", key); err != nil {
			return err
		}
		if id, dup := byKey[key]; dup && id != *t.id(&rows[i]) {
			return fmt.Errorf("имя %q уже занято", key)
		}
		byKey[key] = *t.id(&rows[i])
	}
	idx, err := yaml.Marshal(index{NextID: next})
	if err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(t.dir, indexFile), idx); err != nil {
		return err
	}
	old := make(map[int64]T, len(t.rows))
	for _, r := range t.rows {
		old[*t.id(&r)] = r
	}
	// First the files of deleted and renamed entries are removed, then new ones are written: this way swapping names
	// between entries in one change does not overwrite each other's files.
	newState := make(map[int64]T, len(rows))
	for i := range rows {
		newState[*t.id(&rows[i])] = rows[i]
	}
	for id, prev := range old {
		cur, still := newState[id]
		if still && t.key(&cur) == t.key(&prev) {
			continue
		}
		if err := os.Remove(t.path(t.key(&prev))); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	for i := range rows {
		prev, ok := old[*t.id(&rows[i])]
		if ok && reflect.DeepEqual(prev, rows[i]) {
			continue
		}
		data, err := yaml.Marshal(rows[i])
		if err != nil {
			return err
		}
		if err := writeAtomic(t.path(t.key(&rows[i])), data); err != nil {
			return err
		}
	}
	return nil
}

// All returns copies of all rows by number.
func (t *table[T]) All() []T {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]T, len(t.rows))
	copy(out, t.rows)
	return out
}

// Get returns a row by number.
func (t *table[T]) Get(id int64) (T, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, r := range t.rows {
		r := r
		if *t.id(&r) == id {
			return r, true
		}
	}
	var zero T
	return zero, false
}

// update changes the table atomically: fn receives a copy of the rows and the counter; on error nothing changes.
// fn may add rows (newID assigns the number), change and delete them.
func (t *table[T]) update(fn func(rows *[]T, newID func() int64) error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	rows := make([]T, len(t.rows))
	copy(rows, t.rows)
	next := t.next
	newID := func() int64 {
		n := next
		next++
		return n
	}
	if err := fn(&rows, newID); err != nil {
		return err
	}
	if err := t.persist(rows, next); err != nil {
		return err
	}
	t.rows = rows
	t.next = next
	return nil
}

// writeAtomic writes a file via a temporary file and rename.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// SPDX-License-Identifier: GPL-3.0-only

// Package catalog stores catalogs: images and machine templates. Each entry is a separate
// YAML file whose name equals the entry name: <dir>/<table>/<name>.yaml. The file stores
// the numeric entry number (labs reference it). A new number is one more than the largest
// number in use; references to deleted entries are cleared by their owners.
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
	"netrave/internal/yamlfmt"

	"netrave/internal/msgs"
	"netrave/internal/names"
)

// ErrNotFound - no row with this number.
var ErrNotFound = errors.New("Record not found")

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

// openTable opens the table in directory dir.
func openTable[T any](dir string, id func(*T) *int64, key func(*T) string) (*table[T], error) {
	t := &table[T]{dir: dir, id: id, key: key, next: 1}
	if err := t.load(); err != nil {
		return nil, err
	}
	return t, nil
}

// load reads the counter and all table entries from its directory.
func (t *table[T]) load() error {
	t.next = 1

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
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		stem := strings.TrimSuffix(name, ".yaml")
		if !names.Valid(stem) {
			return msgs.New("{path}: invalid file name", map[string]string{"path": filepath.Join(t.dir, name)})
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
			return msgs.New("{path}: record name «{name}» does not match the file name", map[string]string{"path": path, "name": got})
		}
		id := *t.id(&r)
		if other, dup := seenIDs[id]; dup {
			return msgs.New("{path}: number {id} is already taken by file {file}.yaml", map[string]string{"path": path, "id": fmt.Sprint(id), "file": other})
		}
		seenIDs[id] = stem
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return *t.id(&rows[i]) < *t.id(&rows[j]) })
	t.next = nextNumber(rows, t.id)
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
func (t *table[T]) persist(rows []T) error {
	if err := os.MkdirAll(t.dir, 0o750); err != nil {
		return err
	}
	byKey := make(map[string]int64, len(rows))
	for i := range rows {
		key := t.key(&rows[i])
		if err := names.Check("Name", key); err != nil {
			return err
		}
		if id, dup := byKey[key]; dup && id != *t.id(&rows[i]) {
			return msgs.New("The name «{name}» is already taken", map[string]string{"name": key})
		}
		byKey[key] = *t.id(&rows[i])
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
		data, err := yamlfmt.Marshal(rows[i])
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
	if err := t.persist(rows); err != nil {
		return err
	}
	t.rows = rows
	t.next = nextNumber(rows, t.id)
	return nil
}

// nextNumber returns one more than the largest number among rows, or 1 when there are none.
func nextNumber[T any](rows []T, id func(*T) *int64) int64 {
	next := int64(1)
	for i := range rows {
		if n := *id(&rows[i]); n >= next {
			next = n + 1
		}
	}
	return next
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

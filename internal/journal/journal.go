// SPDX-License-Identifier: GPL-3.0-only

// Package journal - the event journal (audit) in a file. One entry is one JSON line (JSON Lines format):
// the file can be read line by line, appended without blocking readers and opened in any editor or jq.
//
// The journal is rotated by size: the current file is renamed to <name>.1 and a new one is started.
// Reading returns entries from newest to oldest and goes into the previous file until the requested number is collected.
package journal

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// MaxBytes - the size after which the current file goes to the archive (<name>.1).
const MaxBytes int64 = 64 << 20

// Entry - one journal entry.
type Entry struct {
	ID       int64          `json:"id"`
	TS       int64          `json:"ts"`
	UserID   int64          `json:"user_id,omitempty"`
	Username string         `json:"username"`
	Action   string         `json:"action"`
	Detail   string         `json:"detail"`
	IP       string         `json:"ip"`
	Data     map[string]any `json:"data,omitempty"`
}

// Journal - the journal file with order preservation and the last wipe time of each lab
// and each lab instance (a Wipe of one instance must not affect the others).
type Journal struct {
	mu           sync.Mutex
	path         string
	nextID       int64
	size         int64
	lastWipe     map[int64]int64
	lastWipeInst map[int64]int64
}

// ActionLabWipe - the "lab wipe" action. Data contains lab_id, instance, devices, failed.
const ActionLabWipe = "lab_wipe"

// Open opens the journal (the file is created on the first write). Numbers and last wipe times
// are restored from the current file.
func Open(path string) (*Journal, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	j := &Journal{path: path, lastWipe: map[int64]int64{}, lastWipeInst: map[int64]int64{}}
	if fi, err := os.Stat(path); err == nil {
		j.size = fi.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	// The number continues from the last entry if it is in the current or archive file.
	for _, p := range []string{path + ".1", path} {
		entries, err := readAll(p)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.ID > j.nextID {
				j.nextID = e.ID
			}
			if e.Action == ActionLabWipe {
				if lab, ok := labOf(e); ok && e.TS > j.lastWipe[lab] {
					j.lastWipe[lab] = e.TS
				}
				if inst, ok := instanceOf(e); ok && e.TS > j.lastWipeInst[inst] {
					j.lastWipeInst[inst] = e.TS
				}
			}
		}
	}
	return j, nil
}

// Append appends an entry. The number and time are set if they are not provided.
func (j *Journal) Append(e Entry) error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.nextID++
	e.ID = j.nextID
	if e.TS == 0 {
		e.TS = time.Now().Unix()
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if j.size+int64(len(line)) > MaxBytes {
		if err := os.Rename(j.path, j.path+".1"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		j.size = 0
	}
	f, err := os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := f.Write(line)
	j.size += int64(n)
	if err != nil {
		return err
	}
	if e.Action == ActionLabWipe {
		if lab, ok := labOf(e); ok && e.TS > j.lastWipe[lab] {
			j.lastWipe[lab] = e.TS
		}
		if inst, ok := instanceOf(e); ok && e.TS > j.lastWipeInst[inst] {
			j.lastWipeInst[inst] = e.TS
		}
	}
	return nil
}

// LastWipe returns the time of the last lab wipe (0 - never wiped).
func (j *Journal) LastWipe(labID int64) int64 {
	if j == nil {
		return 0
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.lastWipe[labID]
}

// LastWipeInstance returns the time of the last wipe of a lab instance (0 - never wiped).
// A Wipe of one instance does not change the wipe time of other instances of the same lab.
func (j *Journal) LastWipeInstance(instanceID int64) int64 {
	if j == nil {
		return 0
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.lastWipeInst[instanceID]
}

// Tail returns up to limit entries matching match (nil - all), newest first.
func (j *Journal) Tail(limit int, match func(Entry) bool) ([]Entry, error) {
	out := []Entry{}
	if j == nil || limit <= 0 {
		return out, nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, p := range []string{j.path, j.path + ".1"} {
		err := readBackward(p, func(e Entry) bool {
			if match == nil || match(e) {
				out = append(out, e)
			}
			return len(out) < limit
		})
		if err != nil {
			return nil, err
		}
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Clear deletes the whole journal (current file and archive). Numbers continue.
func (j *Journal) Clear() error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, p := range []string{j.path, j.path + ".1"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	j.size = 0
	j.lastWipe = map[int64]int64{}
	j.lastWipeInst = map[int64]int64{}
	return nil
}

func labOf(e Entry) (int64, bool) {
	// Fresh entries store the number as int, ones read from the file as float64 (JSON).
	switch v := e.Data["lab_id"].(type) {
	case float64:
		return int64(v), true
	case int:
		return int64(v), true
	case int64:
		return v, true
	}
	return 0, false
}

// instanceOf extracts the instance id from a Wipe entry. Entries without this field belong to no instance.
func instanceOf(e Entry) (int64, bool) {
	switch v := e.Data["instance"].(type) {
	case float64:
		return int64(v), true
	case int:
		return int64(v), true
	case int64:
		return v, true
	}
	return 0, false
}

// readAll reads the whole file (a missing file - an empty list).
func readAll(path string) ([]Entry, error) {
	var out []Entry
	err := readForward(path, func(e Entry) { out = append(out, e) })
	return out, err
}

func readForward(path string, fn func(Entry)) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			fn(e)
		}
	}
	return sc.Err()
}

// readBackward walks the file lines from the end to the beginning. fn returns false to stop.
func readBackward(path string, fn func(Entry) bool) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	const chunk = 64 << 10
	var carry []byte
	pos := fi.Size()
	for pos > 0 {
		n := int64(chunk)
		if pos < n {
			n = pos
		}
		pos -= n
		buf := make([]byte, n)
		if _, err := f.ReadAt(buf, pos); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		data := append(buf, carry...)
		// The first part (before the first newline) may be cut off - leave it for the next step.
		lines := bytes.Split(data, []byte{'\n'})
		carry = lines[0]
		for i := len(lines) - 1; i >= 1; i-- {
			if len(bytes.TrimSpace(lines[i])) == 0 {
				continue
			}
			var e Entry
			if json.Unmarshal(lines[i], &e) == nil && !fn(e) {
				return nil
			}
		}
	}
	if len(bytes.TrimSpace(carry)) > 0 {
		var e Entry
		if json.Unmarshal(carry, &e) == nil {
			fn(e)
		}
	}
	return nil
}

// LabWipes returns the recent lab wipes (matching lab_id), newest first.
func (j *Journal) LabWipes(labID int64, limit int) ([]Entry, error) {
	return j.Tail(limit, func(e Entry) bool {
		if e.Action != ActionLabWipe {
			return false
		}
		lab, ok := labOf(e)
		return ok && lab == labID
	})
}

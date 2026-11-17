// SPDX-License-Identifier: GPL-3.0-only

package journal

import (
	"path/filepath"
	"testing"
)

func TestAppendTailAndWipes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := j.Append(Entry{Username: "u", Action: "login"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := j.Append(Entry{Username: "admin", Action: ActionLabWipe, Data: map[string]any{"lab_id": 7, "devices": 2}}); err != nil {
		t.Fatal(err)
	}
	all, err := j.Tail(10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 || all[0].Action != ActionLabWipe || all[0].ID != 4 {
		t.Fatalf("порядок записей неверный: %+v", all)
	}
	if j.LastWipe(7) == 0 || j.LastWipe(8) != 0 {
		t.Fatalf("время сброса: %d %d", j.LastWipe(7), j.LastWipe(8))
	}
	wipes, err := j.LabWipes(7, 5)
	if err != nil || len(wipes) != 1 {
		t.Fatalf("сбросы лаборатории: %v %+v", err, wipes)
	}
	// The journal is reopened with the same numbers and wipe time.
	j2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if j2.LastWipe(7) != j.LastWipe(7) {
		t.Fatal("время сброса не восстановлено из файла")
	}
	if err := j2.Append(Entry{Action: "login"}); err != nil {
		t.Fatal(err)
	}
	last, _ := j2.Tail(1, nil)
	if len(last) != 1 || last[0].ID != 5 {
		t.Fatalf("номер не продолжился: %+v", last)
	}
	if err := j2.Clear(); err != nil {
		t.Fatal(err)
	}
	if rest, _ := j2.Tail(10, nil); len(rest) != 0 {
		t.Fatalf("после очистки остались записи: %d", len(rest))
	}
}

// The Wipe time is also stored per instance: wiping one lab instance does not make
// its other instances wiped, and it is restored when the journal is reopened.
func TestLastWipeIsPerInstance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Append(Entry{Action: ActionLabWipe, TS: 100, Data: map[string]any{"lab_id": 7, "instance": 11, "devices": 2}}); err != nil {
		t.Fatal(err)
	}
	if err := j.Append(Entry{Action: ActionLabWipe, TS: 200, Data: map[string]any{"lab_id": 7, "instance": 12, "devices": 1}}); err != nil {
		t.Fatal(err)
	}
	// An old entry without the instance field must not be attributed to any instance.
	if err := j.Append(Entry{Action: ActionLabWipe, TS: 300, Data: map[string]any{"lab_id": 8, "devices": 3}}); err != nil {
		t.Fatal(err)
	}

	if j.LastWipe(7) != 200 || j.LastWipe(8) != 300 {
		t.Fatalf("время сброса лаборатории потеряно: %d %d", j.LastWipe(7), j.LastWipe(8))
	}
	if j.LastWipeInstance(11) != 100 || j.LastWipeInstance(12) != 200 {
		t.Fatalf("время сброса экземпляров потеряно: %d %d", j.LastWipeInstance(11), j.LastWipeInstance(12))
	}
	if j.LastWipeInstance(11) == j.LastWipeInstance(12) {
		t.Fatalf("сбросы разных экземпляров не различаются: %d", j.LastWipeInstance(11))
	}
	if j.LastWipeInstance(13) != 0 {
		t.Fatalf("экземпляр без сброса получил время %d", j.LastWipeInstance(13))
	}

	j2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if j2.LastWipeInstance(11) != j.LastWipeInstance(11) || j2.LastWipeInstance(12) != j.LastWipeInstance(12) {
		t.Fatalf("время сброса экземпляров не восстановлено: %d %d", j2.LastWipeInstance(11), j2.LastWipeInstance(12))
	}
}

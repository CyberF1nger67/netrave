// SPDX-License-Identifier: GPL-3.0-only

package msgs

import (
	"errors"
	"testing"
)

func TestRenderSubstitutesParameters(t *testing.T) {
	got := New("Machine {id}: {detail}", map[string]string{"id": "7", "detail": "boom"}).Error()
	if got != "Machine 7: boom" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderNestedKeyWithItsParameters(t *testing.T) {
	got := Render("{name}: {detail_key}", map[string]string{"name": "lab", "detail_key": "Folder {id} not found", "id": "3"})
	if got != "lab: Folder 3 not found" {
		t.Fatalf("got %q", got)
	}
}

func TestOfKeepsKeyAndParameters(t *testing.T) {
	m := Of(New("Folder {id} not found", map[string]string{"id": "3"}))
	if m.Key != "Folder {id} not found" || m.Params["id"] != "3" {
		t.Fatalf("keyed error: %+v", m)
	}
	plain := Of(errors.New("disk is full"))
	if plain.Key != "disk is full" || plain.Params != nil {
		t.Fatalf("plain error: %+v", plain)
	}
}

func TestJoinRendersEnglish(t *testing.T) {
	got := Join([]Message{M("A {x}", map[string]string{"x": "1"}), M("B", nil)}, "; ")
	if got != "A 1; B" {
		t.Fatalf("got %q", got)
	}
}

func TestMergeLaterWins(t *testing.T) {
	got := Merge(map[string]string{"a": "1", "b": "2"}, map[string]string{"b": "3"})
	if got["a"] != "1" || got["b"] != "3" {
		t.Fatalf("got %v", got)
	}
}

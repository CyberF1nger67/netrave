// SPDX-License-Identifier: GPL-3.0-only

package themepresets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadsSeededThemes(t *testing.T) {
	catalog, err := Load(testDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Standard) != 2 {
		t.Fatalf("standard themes: %d", len(catalog.Standard))
	}
	if len(catalog.Presets) != 16 {
		t.Fatalf("presets: %d", len(catalog.Presets))
	}
	ids := map[string]bool{}
	for _, item := range append(append([]Theme{}, catalog.Standard...), catalog.Presets...) {
		if ids[item.ID] {
			t.Fatalf("duplicate identifier %q", item.ID)
		}
		ids[item.ID] = true
		if item.Name == "" || (item.Base != "dark" && item.Base != "light") {
			t.Fatalf("theme %q: %+v", item.ID, item)
		}
	}
	for _, id := range []string{"dark", "light", "ocean", "forest", "lavender", "lime"} {
		if !ids[id] {
			t.Fatalf("no theme %q", id)
		}
	}
}

func TestPresetsDefineEveryColor(t *testing.T) {
	catalog, err := Load(testDir(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range catalog.Presets {
		if len(item.Colors) != len(ColorKeys) {
			t.Fatalf("theme %q: colors %d, expected %d", item.ID, len(item.Colors), len(ColorKeys))
		}
		for _, key := range ColorKeys {
			if item.Colors[key] == "" {
				t.Fatalf("theme %q: no color %q", item.ID, key)
			}
		}
	}
}

func TestStandardThemesHaveSwatches(t *testing.T) {
	catalog, err := Load(testDir(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range catalog.Standard {
		if len(item.Swatches) < 2 {
			t.Fatalf("theme %q has no color swatches", item.ID)
		}
		// Standard themes carry their palette in the file as well: the colors must be
		// present for both base themes, so editing the file changes the interface.
		if len(item.Colors) < len(ColorKeys)-2 {
			t.Fatalf("standard theme %q: palette not set in file", item.ID)
		}
	}
}

// testDir returns a fresh shared-themes directory, which Load fills with the ready-made set.
func testDir(t *testing.T) string {
	return Dir(t.TempDir())
}

// Built-in dark and light are not read from files: old copies in the directory are ignored.
func TestBuiltinThemesIgnoreFiles(t *testing.T) {
	dir := testDir(t)
	if _, err := Load(dir); err != nil {
		t.Fatal(err)
	}
	legacy := "id: dark\nname: Old\nbase: dark\nstandard: true\nsw: [\"#000000\", \"#111111\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "10-dark.yaml"), []byte(legacy), 0o640); err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Standard) != 2 || catalog.Standard[0].Name != "Dark" {
		t.Fatalf("built-in themes changed by file: %+v", catalog.Standard)
	}
	for _, item := range catalog.Presets {
		if item.ID == "dark" {
			t.Fatal("built-in theme file ended up in user themes")
		}
	}
}

// User presets are read as regular files and can be changed or deleted.
func TestPresetFilesAreEditable(t *testing.T) {
	dir := testDir(t)
	if _, err := Load(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "ocean.yaml")); err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Presets) != 15 {
		t.Fatalf("after deleting presets file: %d", len(catalog.Presets))
	}
}

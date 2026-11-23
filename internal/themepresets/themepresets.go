// SPDX-License-Identifier: GPL-3.0-only

// Package themepresets loads the theme catalog from YAML files. Every theme is an ordinary
// file: the ready-made set is written there once, and after that the files are the only source.
//
// Standard themes (dark/light) define only the name, base and color swatches: their
// palette lives in CSS so theme switching does not depend on loading data. Presets
// contain the full set of CSS variable colors. The theme set is not embedded in the frontend:
// the client receives it from the server and uses it for listing and applying themes.
package themepresets

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
	"netrave/internal/msgs"
)

// embedded holds the ready-made user themes. They are written to the themes directory once,
// when it is created; after that they are ordinary files that can be edited or deleted.
//
//go:embed presets/*.yaml
var embedded embed.FS

// builtinThemes - the built-in standard dark and light themes. They are not stored in files,
// cannot be changed or deleted, and files with such identifiers are ignored.
var builtinThemes = []Theme{
	{
		ID: "dark", Name: "Dark", Base: "dark", Standard: true,
		Swatches: []string{"#0a0f16", "#121a25", "#6366f1", "#e7edf5"},
		Colors: map[string]string{
			"bg": "#0a0f16", "bg-2": "#0e1520", "panel": "#121a25", "panel2": "#18222f",
			"panel3": "#202c3c", "line": "#22303f", "line-2": "#2e3e52", "text": "#e7edf5",
			"muted": "#8b99ad", "accent": "#6366f1", "accent-2": "#3b82f6", "ok": "#22c55e",
			"bad": "#f25f5c", "warn": "#f5a524", "on-accent": "#ffffff",
		},
	},
	{
		ID: "light", Name: "Light", Base: "light", Standard: true,
		Swatches: []string{"#f3f6fb", "#ffffff", "#4f46e5", "#0f1a2b"},
		Colors: map[string]string{
			"bg": "#f3f6fb", "bg-2": "#f7f9fd", "panel": "#ffffff", "panel2": "#f4f7fc",
			"panel3": "#e9eef7", "line": "#e0e6ef", "line-2": "#cbd5e3", "text": "#0f1a2b",
			"muted": "#667489", "accent": "#4f46e5", "accent-2": "#2563eb", "ok": "#16a34a",
			"bad": "#dc2626", "warn": "#d97706", "on-accent": "#ffffff",
		},
	},
}

// isBuiltinID reports that the identifier belongs to a built-in theme.
func isBuiltinID(id string) bool {
	for _, item := range builtinThemes {
		if item.ID == id {
			return true
		}
	}
	return false
}

// ColorKeys - allowed color keys, matching CSS variable names without the "--" prefix.
var ColorKeys = []string{
	"bg", "bg-2", "panel", "panel2", "panel3", "side", "btn",
	"line", "line-2", "text", "muted",
	"accent", "accent-2", "on-accent", "ok", "bad", "warn",
}

var (
	colorSet = func() map[string]bool {
		out := make(map[string]bool, len(ColorKeys))
		for _, key := range ColorKeys {
			out[key] = true
		}
		return out
	}()
	idRe  = regexp.MustCompile(`^[a-z0-9]{1,24}$`)
	hexRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// Theme is one theme stored in a YAML file.
type Theme struct {
	ID       string            `yaml:"id" json:"id"`
	Name     string            `yaml:"name" json:"name"`
	Base     string            `yaml:"base" json:"base"`
	Standard bool              `yaml:"standard,omitempty" json:"standard,omitempty"`
	Swatches []string          `yaml:"sw,omitempty" json:"sw,omitempty"`
	Colors   map[string]string `yaml:"colors,omitempty" json:"colors,omitempty"`
}

// Catalog holds all themes in display order.
type Catalog struct {
	Standard []Theme `json:"standard"`
	Presets  []Theme `json:"presets"`
}

// Dir returns the directory of shared themes inside the themes directory.
func Dir(themesDir string) string {
	return filepath.Join(themesDir, "shared")
}

// Load returns the themes stored in dir. When dir does not exist, it is created and
// filled once with the ready-made set. After that the files are the only source: an
// edit is visible on the next request, and a deleted file stays deleted.
func Load(dir string) (Catalog, error) {
	if err := seed(dir); err != nil {
		return Catalog{}, err
	}
	return load(dir)
}

// seed writes the ready-made themes into dir, but only when dir does not exist yet.
func seed(dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	entries, err := fs.ReadDir(embedded, "presets")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		data, err := embedded.ReadFile("presets/" + entry.Name())
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, entry.Name()), data, 0o640); err != nil {
			return err
		}
	}
	return nil
}

func load(dir string) (Catalog, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Catalog{}, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".yaml") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	out := Catalog{Standard: append([]Theme(nil), builtinThemes...)}
	seen := map[string]bool{}
	for _, item := range builtinThemes {
		seen[item.ID] = true
	}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return Catalog{}, err
		}
		var item Theme
		if err := yaml.Unmarshal(data, &item); err != nil {
			return Catalog{}, fmt.Errorf("%s: %w", name, err)
		}
		// Built-in theme files (including ones left from earlier versions) are not read.
		if item.Standard || isBuiltinID(item.ID) {
			continue
		}
		if err := validate(item); err != nil {
			return Catalog{}, fmt.Errorf("%s: %w", name, err)
		}
		if seen[item.ID] {
			return Catalog{}, msgs.New("{file}: duplicate theme id «{id}»", map[string]string{"file": name, "id": item.ID})
		}
		seen[item.ID] = true
		out.Presets = append(out.Presets, item)
	}
	return out, nil
}

func validate(item Theme) error {
	if !idRe.MatchString(item.ID) {
		return msgs.New("Invalid theme id «{id}»", map[string]string{"id": item.ID})
	}
	name := strings.TrimSpace(item.Name)
	if name == "" || len([]rune(name)) > 40 {
		return msgs.New("Theme name «{id}»: from 1 to 40 characters", map[string]string{"id": item.ID})
	}
	if item.Base != "dark" && item.Base != "light" {
		return msgs.New("Theme «{id}»: unknown base «{base}»", map[string]string{"id": item.ID, "base": item.Base})
	}
	if len(item.Colors) != len(ColorKeys) {
		return msgs.New("Theme «{id}»: the full set of colors must be given", map[string]string{"id": item.ID})
	}
	for key, value := range item.Colors {
		if !colorSet[key] {
			return msgs.New("Theme «{id}»: unknown color «{color}»", map[string]string{"id": item.ID, "color": key})
		}
		if !hexRe.MatchString(value) {
			return msgs.New("Theme «{id}»: invalid color «{color}»", map[string]string{"id": item.ID, "color": key})
		}
	}
	for _, key := range ColorKeys {
		if _, ok := item.Colors[key]; !ok {
			return msgs.New("Theme «{id}»: color «{color}» is missing", map[string]string{"id": item.ID, "color": key})
		}
	}
	return nil
}

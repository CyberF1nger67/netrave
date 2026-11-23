// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
	"net/http"
	"netrave/internal/httpx"
	"netrave/internal/msgs"
	"netrave/internal/yamlfmt"
	"regexp"
	"strings"

	"netrave/internal/auth"
	"netrave/internal/names"
	"netrave/internal/permcatalog"
	"netrave/internal/themepresets"
)

// User themes. The client sends the whole state: the active theme and its own themes.
// Everything is validated here because the values end up in the page's CSS variables.

var (
	themeHexRe    = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	themeIDRe     = regexp.MustCompile(`^[a-z0-9]{1,24}$`)
	themePresetRe = regexp.MustCompile(`^preset:[a-z0-9]{1,24}$`)
)

// Keys match CSS variable names without the "--" prefix.
var themeColorKeys = map[string]bool{
	"bg": true, "bg-2": true, "panel": true, "panel2": true, "panel3": true, "side": true, "btn": true,
	"line": true, "line-2": true, "text": true, "muted": true,
	"accent": true, "accent-2": true, "on-accent": true, "ok": true, "bad": true, "warn": true,
}

const maxCustomThemes = 30

type customTheme struct {
	ID     string            `json:"id" yaml:"id"`
	Name   string            `json:"name" yaml:"name"`
	Base   string            `json:"base" yaml:"base"`
	Colors map[string]string `json:"colors" yaml:"colors"`
}

// presetOverride is the user's own version of a ready-made theme. The shared file is not changed:
// the override is kept in the user's file and applies to this user only.
type presetOverride struct {
	Name   string            `json:"name" yaml:"name"`
	Base   string            `json:"base" yaml:"base"`
	Colors map[string]string `json:"colors" yaml:"colors"`
}

type userTheme struct {
	Active string        `json:"active" yaml:"active"`
	Custom []customTheme `json:"custom" yaml:"custom"`
	// Presets - ready-made themes changed by this user, by preset id.
	Presets map[string]presetOverride `json:"presets,omitempty" yaml:"presets,omitempty"`
	// Hidden - ready-made themes deleted by this user, by preset id.
	Hidden []string `json:"hidden,omitempty" yaml:"hidden,omitempty"`
}

// maxThemeFileBytes is the size limit of a user's theme file.
const maxThemeFileBytes = 64 << 10

// themesDir is the theme file directory (one YAML file per user).
func (h *Handler) themesDir() (string, error) {
	if h.Cfg == nil || h.Cfg.ThemesDir == "" {
		return "", errors.New("The theme catalog is not configured")
	}
	return h.Cfg.ThemesDir, nil
}

// themeFile is the path to a user's theme file. The file name is the user id, not the login: the login can change.
func (h *Handler) themeFile(uid int64) (string, error) {
	dir, err := h.themesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, strconv.FormatInt(uid, 10)+".yaml"), nil
}

// loadTheme reads the user's theme. No file - empty theme and exists=false.
func (h *Handler) loadTheme(uid int64) (t userTheme, exists bool, err error) {
	path, err := h.themeFile(uid)
	if err != nil {
		return t, false, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return t, false, nil
	}
	if err != nil {
		return t, false, err
	}
	// A corrupted file is treated as missing: the theme is simply not applied.
	if err := yaml.Unmarshal(data, &t); err != nil {
		return userTheme{}, false, nil
	}
	return t, true, nil
}

// saveTheme writes the theme atomically: to a temporary file and then by rename.
func (h *Handler) saveTheme(uid int64, t userTheme) error {
	path, err := h.themeFile(uid)
	if err != nil {
		return err
	}
	data, err := yamlfmt.Marshal(t)
	if err != nil {
		return err
	}
	if len(data) > maxThemeFileBytes {
		return errors.New("The theme is too large")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RemoveTheme deletes the user's theme file (called when a user is deleted).
func RemoveTheme(cfgDir string, uid int64) error {
	if cfgDir == "" {
		return nil
	}
	err := os.Remove(filepath.Join(cfgDir, strconv.FormatInt(uid, 10)+".yaml"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// themePresets - GET /api/me/theme/presets: themes from YAML files in the shared directory.
// The list is needed by all logged-in users: it contains no personal data.
func (h *Handler) themePresets(w http.ResponseWriter, r *http.Request) {
	dir, err := h.themesDir()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "The theme catalog is not configured")
		return
	}
	catalog, err := themepresets.Load(themepresets.Dir(dir))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Themes read error")
		return
	}
	writeJSON(w, http.StatusOK, catalog)
}

func (h *Handler) getTheme(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	t, exists, err := h.loadTheme(ss.User.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Theme read error")
		return
	}
	if !exists {
		writeJSON(w, http.StatusOK, map[string]any{"theme": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"theme": t})
}

func (h *Handler) putTheme(w http.ResponseWriter, r *http.Request) {
	ss := auth.FromContext(r.Context())
	var in userTheme
	if !decode(w, r, &in) {
		return
	}
	dir, err := h.themesDir()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "The theme catalog is not configured")
		return
	}
	catalog, err := themepresets.Load(themepresets.Dir(dir))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Themes read error")
		return
	}
	all := map[string]bool{}
	presets := map[string]bool{}
	for _, item := range catalog.Standard {
		all[item.ID] = true
	}
	for _, item := range catalog.Presets {
		all[item.ID] = true
		presets[item.ID] = true
	}
	clean, verr := validateTheme(in, all, presets)
	if verr != nil {
		httpx.ErrFrom(w, http.StatusBadRequest, verr)
		return
	}
	stored, _, err := h.loadTheme(ss.User.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Theme read error")
		return
	}
	// Selecting a ready-made theme is available to everyone. Only those with the themes.create permission
	// can create a new custom theme; existing custom themes can be changed and deleted.
	if !ss.User.Can(permcatalog.ThemesCreate) {
		existing := map[string]bool{}
		for _, c := range stored.Custom {
			existing[c.ID] = true
		}
		for _, c := range clean.Custom {
			if !existing[c.ID] {
				writeErr(w, http.StatusForbidden, "No permission to create themes")
				return
			}
		}
	}
	if err := h.saveTheme(ss.User.ID, clean); err != nil {
		writeErr(w, http.StatusInternalServerError, "Theme save error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"theme": clean})
}

// validateTheme validates the theme state and returns it in canonical form. all holds every theme
// identifier of the catalog (standard and ready-made), presets only the ready-made ones.
func validateTheme(t userTheme, all, presets map[string]bool) (userTheme, error) {
	if len(t.Custom) > maxCustomThemes {
		return t, msgs.New("Too many custom themes", nil)
	}
	out := userTheme{Custom: []customTheme{}}
	ids := map[string]bool{}
	for _, c := range t.Custom {
		if !themeIDRe.MatchString(c.ID) || ids[c.ID] {
			return t, msgs.New("Invalid theme identifier", nil)
		}
		name := strings.TrimSpace(c.Name)
		if err := names.Check("Theme name", name); err != nil {
			return t, err
		}
		if c.Base != "dark" && c.Base != "light" {
			return t, msgs.New("Invalid theme base", nil)
		}
		if len(c.Colors) == 0 || len(c.Colors) > len(themeColorKeys) {
			return t, msgs.New("Invalid colour set", nil)
		}
		colors := map[string]string{}
		for k, v := range c.Colors {
			if !themeColorKeys[k] || !themeHexRe.MatchString(v) {
				return t, msgs.New("Invalid colour: {name}", map[string]string{"name": k})
			}
			colors[k] = strings.ToLower(v)
		}
		ids[c.ID] = true
		out.Custom = append(out.Custom, customTheme{ID: c.ID, Name: name, Base: c.Base, Colors: colors})
	}

	hidden := map[string]bool{}
	for _, id := range t.Hidden {
		if !presets[id] {
			return t, msgs.New("Unknown theme", nil)
		}
		hidden[id] = true
	}
	for id := range hidden {
		out.Hidden = append(out.Hidden, id)
	}

	if len(t.Presets) > 0 {
		out.Presets = map[string]presetOverride{}
	}
	for id, p := range t.Presets {
		if !presets[id] {
			return t, msgs.New("Unknown theme", nil)
		}
		name := strings.TrimSpace(p.Name)
		if err := names.Check("Theme name", name); err != nil {
			return t, err
		}
		if p.Base != "dark" && p.Base != "light" {
			return t, msgs.New("Invalid theme base", nil)
		}
		if len(p.Colors) == 0 || len(p.Colors) > len(themeColorKeys) {
			return t, msgs.New("Invalid colour set", nil)
		}
		colors := map[string]string{}
		for k, v := range p.Colors {
			if !themeColorKeys[k] || !themeHexRe.MatchString(v) {
				return t, msgs.New("Invalid colour: {name}", map[string]string{"name": k})
			}
			colors[k] = strings.ToLower(v)
		}
		out.Presets[id] = presetOverride{Name: name, Base: p.Base, Colors: colors}
	}

	switch {
	case t.Active == "dark" || t.Active == "light":
		out.Active = t.Active
	case themePresetRe.MatchString(t.Active):
		// The identifier must exist in the catalog and must not be deleted by this user.
		// Old saved values preset:dark and preset:light remain valid.
		preset := strings.TrimPrefix(t.Active, "preset:")
		if !all[preset] || hidden[preset] {
			return t, msgs.New("Unknown theme", nil)
		}
		out.Active = t.Active
	case strings.HasPrefix(t.Active, "custom:") && ids[strings.TrimPrefix(t.Active, "custom:")]:
		out.Active = t.Active
	default:
		return t, msgs.New("Unknown theme", nil)
	}
	return out, nil
}

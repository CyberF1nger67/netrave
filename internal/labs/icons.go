// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"netrave/internal/access"
	"netrave/internal/auth"
	"netrave/internal/httpx"
	"netrave/internal/iconlib"
	"netrave/internal/icons"
)

// iconUpload - POST /api/labs/{id}/icons: accepts PNG, JPEG, WEBP or SVG, validates and stores the picture.
// The response contains the hash (custom_icon). The file is bound to a machine when it is created or its settings are saved;
// the upload is also saved in the user's library (iconlib). Permissions: adding or changing machines.
func (h *Handler) iconUpload(w http.ResponseWriter, r *http.Request) {
	lab, ok := h.labFor(w, r, "")
	if !ok {
		return
	}
	if !lab.Perms[access.MachAdd] && !lab.Perms[access.MachEdit] {
		httpx.Err(w, http.StatusForbidden, "нет права: "+access.Title(access.MachAdd))
		return
	}
	if !icons.SupportedContentType(r.Header.Get("Content-Type")) {
		httpx.Err(w, http.StatusUnsupportedMediaType, "поддерживаются PNG, JPEG, WEBP и SVG")
		return
	}
	if h.Icons == nil {
		httpx.Err(w, http.StatusInternalServerError, "каталог иконок не настроен")
		return
	}
	if r.ContentLength > icons.MaxBytes {
		httpx.Err(w, http.StatusRequestEntityTooLarge, "файл больше 512 КБ")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, icons.MaxBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			httpx.Err(w, http.StatusRequestEntityTooLarge, "файл больше 512 КБ")
			return
		}
		httpx.Err(w, http.StatusBadRequest, "не удалось прочитать файл")
		return
	}
	clean, info, err := icons.Clean(data)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Icons.Save(clean, info.Hash); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "не удалось сохранить иконку")
		return
	}
	// The uploaded picture also goes into the user's library (it can be chosen for another machine).
	// A library error (e.g. a limit) does not cancel the upload for this machine.
	_, _, _ = iconlib.Remember(h.DB, h.Icons, auth.FromContext(r.Context()).User.ID, info, "")
	h.audit(r, "icon_upload", lab.Name+" · "+info.Hash[:12])
	httpx.JSON(w, http.StatusCreated, info)
}

// iconGet - GET /api/labs/{id}/icons/{hash}: serves the icon if it is used by a machine of this lab.
// Permission - lab view; icons of other labs do not get here.
func (h *Handler) iconGet(w http.ResponseWriter, r *http.Request) {
	lab, ok := h.labFor(w, r, "")
	if !ok {
		return
	}
	hash := r.PathValue("hash")
	if !icons.ValidHash(hash) {
		httpx.Err(w, http.StatusNotFound, "иконка не найдена")
		return
	}
	// The icon is served if it is used by a machine of this lab or by a template (a template icon is available to everyone).
	inLab := false
	if fl, ok := labData(h.DB, lab.ID); ok {
		for _, d := range fl.Devices {
			if d.CustomIcon == hash {
				inLab = true
				break
			}
		}
	}
	if !inLab {
		tpl := 0
		for _, t := range h.DB.Catalog.Templates() {
			if t.CustomIcon == hash {
				tpl++
			}
		}
		if tpl == 0 {
			httpx.Err(w, http.StatusNotFound, "иконка не найдена")
			return
		}
	}
	data, err := h.Icons.Read(hash)
	if err != nil {
		httpx.Err(w, http.StatusNotFound, "иконка не найдена")
		return
	}
	// The file name is the content hash, so the icon is cached for a long time; private, as access depends on the user.
	w.Header().Set("Content-Type", h.Icons.ContentType(hash))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

// checkIcon validates the machine icon choice: icon - the standard picture or custom, custom - the hash of an uploaded file.
// A custom icon without an uploaded file, an unknown hash and a file missing on disk produce an error.
// The validated file is marked fresh (see icons.Sweep) so it is not swept before saving.
func (h *Handler) checkIcon(icon, custom string) error {
	if custom != "" && !h.Icons.Exists(custom) {
		return errors.New("иконка не найдена: загрузите изображение заново")
	}
	if !DeviceIcons[icon] {
		return errors.New("иконка: стандартная, custom или пусто (автоматически)")
	}
	if icon == "custom" && custom == "" {
		return errors.New("сначала загрузите свою иконку")
	}
	if custom != "" {
		if !icons.ValidHash(custom) || !h.Icons.Exists(custom) {
			return errors.New("иконка не найдена: загрузите изображение заново")
		}
		_ = h.Icons.Touch(custom)
	}
	return nil
}

// dropIcon deletes the icon file if machines no longer use it (after replacement or icon reset).
func (h *Handler) dropIcon(hash string) {
	if hash != "" {
		h.Icons.Remove(h.DB, hash)
	}
}

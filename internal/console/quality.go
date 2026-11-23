// SPDX-License-Identifier: GPL-3.0-only

package console

import (
	"net/http"
	"strconv"
	"sync"

	"netrave/internal/auth"
	"netrave/internal/catalog"
	"netrave/internal/httpx"
)

// qualityStore keeps the current VNC console image settings in the server's memory.
// The key is the instance machine row number (rid). The value is initialized from the template on first
// access and is then changed only through the API. After a server restart each machine
// again receives its template's settings.
type qualityStore struct {
	mu sync.Mutex
	m  map[int64]catalog.VNCQuality
}

func newQualityStore() *qualityStore {
	return &qualityStore{m: map[int64]catalog.VNCQuality{}}
}

// get returns the settings for rid; if they do not exist yet, it takes them from the template via fromTemplate.
func (s *qualityStore) get(rid int64, fromTemplate func() catalog.VNCQuality) catalog.VNCQuality {
	s.mu.Lock()
	defer s.mu.Unlock()
	if q, ok := s.m[rid]; ok {
		return q
	}
	q := fromTemplate().Normalized()
	s.m[rid] = q
	return q
}

func (s *qualityStore) set(rid int64, q catalog.VNCQuality) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[rid] = q.Normalized()
}

// qualityResponse provides the current settings and the template values: the client shows them in the form
// and uses them for the "Default" button.
type qualityResponse struct {
	Settings catalog.VNCQuality `json:"settings"`
	Defaults catalog.VNCQuality `json:"defaults"`
}

// templateQuality - the machine template's settings; without a template "Auto" applies.
func (h *Handler) templateQuality(templateID int64) catalog.VNCQuality {
	if templateID != 0 {
		if t, ok := h.DB.Catalog.Template(templateID); ok {
			return t.VNC.Normalized()
		}
	}
	return catalog.DefaultVNCQuality()
}

func (h *Handler) getQuality(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid id")
		return
	}
	info, code, message := h.deviceAccess(r, id, "vnc")
	if code != 0 {
		httpx.ErrFrom(w, code, message)
		return
	}
	defaults := h.templateQuality(info.templateID)
	settings := h.quality.get(id, func() catalog.VNCQuality { return defaults })
	httpx.JSON(w, http.StatusOK, qualityResponse{Settings: settings, Defaults: defaults})
}

func (h *Handler) putQuality(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid id")
		return
	}
	info, code, message := h.deviceAccess(r, id, "vnc")
	if code != 0 {
		httpx.ErrFrom(w, code, message)
		return
	}
	var q catalog.VNCQuality
	if !httpx.Decode(w, r, &q, 1<<16) {
		return
	}
	q = q.Normalized()
	if err := q.Validate(); err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	h.quality.set(id, q)
	if ss := auth.FromContext(r.Context()); ss != nil {
		h.DB.Audit(ss.User.ID, ss.User.Username, "vnc_quality", info.name, auth.ClientIP(r))
	}
	httpx.JSON(w, http.StatusOK, qualityResponse{Settings: q, Defaults: h.templateQuality(info.templateID)})
}

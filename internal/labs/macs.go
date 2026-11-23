// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"net/http"

	"netrave/internal/httpx"
	"netrave/internal/qemu"
)

// nicItem - the address of a machine interface. The address is deterministic (see qemu.MAC), so it cannot
// be set manually and is not stored anywhere.
type nicItem struct {
	NIC int    `json:"nic"`
	MAC string `json:"mac"`
}

// nicList - interface addresses of an instance machine.
func (h *Handler) nicList(w http.ResponseWriter, r *http.Request) {
	_, rid, _, ok := h.rowLabRT(w, r)
	if !ok {
		return
	}
	var nics int
	if err := h.DB.QueryRow(`SELECT nics FROM instance_devices WHERE id=?`, rid.rid).Scan(&nics); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Database error")
		return
	}
	out := make([]nicItem, 0, nics)
	for i := 0; i < nics; i++ {
		out = append(out, nicItem{NIC: i, MAC: qemu.MAC(rid.rid, i)})
	}
	httpx.JSON(w, http.StatusOK, out)
}

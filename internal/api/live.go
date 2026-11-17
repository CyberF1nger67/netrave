// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"net/http"
	"runtime"
)

// live serves metrics for dashboard cards, updated every 2-3 s.
func (h *Handler) live(w http.ResponseWriter, r *http.Request) {
	cpu, ready := 0.0, false
	if h.CPU != nil {
		cpu, ready = h.CPU.Percent()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"running_vms": h.countRunningVMs(),
		"cpu_percent": cpu,
		"cpu_ready":   ready,
		"cpus":        runtime.NumCPU(),
	})
}

// countRunningVMs counts running machines of all labs and all users.
// Each instance row (instance_devices) is a separate user machine with its own QEMU process.
// Process state is stored only in the QEMU manager's memory.
func (h *Handler) countRunningVMs() int {
	if h.M == nil {
		return 0
	}
	return h.M.RunningCount()
}

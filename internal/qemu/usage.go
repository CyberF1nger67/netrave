// SPDX-License-Identifier: GPL-3.0-only

package qemu

import (
	"io/fs"
	"path/filepath"
	"syscall"
)

// DirAllocated returns the space actually used by a directory in bytes: the sum of allocated blocks.
// Sparse qcow2 files are counted by actually written blocks, not by their apparent size.
// A nonexistent directory (the machine has never started) yields 0.
func DirAllocated(dir string) uint64 {
	var total uint64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			total += uint64(st.Blocks) * 512
		}
		return nil
	})
	return total
}

// Disk returns the space used by one instance machine: the labs/<lab>/<rid> directory
// (working overlays disk*.qcow2, UEFI variables, boot log). Other machines and the server are excluded.
func (m *Manager) Disk(labID, rid int64) uint64 {
	return DirAllocated(DevicePaths(m.cfg.LabsDir, m.cfg.RunDir, labID, rid).Dir)
}

// ProcessRSS returns the resident memory (VmRSS) of a live QEMU process by its pid.
// Used when the manager has not adopted the process yet; 0 if there is no process.
func ProcessRSS(pid int) uint64 {
	if !pidAlive(pid) {
		return 0
	}
	return readRSS(pid)
}

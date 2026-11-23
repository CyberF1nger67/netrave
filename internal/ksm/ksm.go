// SPDX-License-Identifier: GPL-3.0-only

// Package ksm controls merging of identical memory pages: standard KSM and
// the separate UKSM mechanism.
//
// In the kernel KSM is enabled by the /sys/kernel/mm/ksm/run parameter, UKSM - by the same
// parameter in /sys/kernel/mm/uksm. If there is no support, the package does not break the service:
// the state reports the reason, and enabling returns an error.
//
// KSM and UKSM solve the same task, so they never run at the same time:
// enabling one mechanism first fully disables the other. Both can be
// disabled - that is a valid state.
package ksm

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Page merging kind.
const (
	// KindKSM - the kernel's standard KSM.
	KindKSM = "ksm"
	// KindUKSM - UKSM (a separate KSM variant).
	KindUKSM = "uksm"
)

// Parameter directories in sysfs.
var (
	DirKSM  = "/sys/kernel/mm/ksm"
	DirUKSM = "/sys/kernel/mm/uksm"
)

// mu protects directory replacement and the switching sequence (directories are replaced in tests).
var mu sync.Mutex

// ErrUnavailable - the kernel has no page merging (not built in or not loaded).
var ErrUnavailable = errors.New("ksm: page merging support not found in the kernel")

// ErrUnknownKind - an unknown mechanism was requested.
var ErrUnknownKind = errors.New("ksm: unknown page merging mechanism")

// Mechanism - the state of one mechanism.
type Mechanism struct {
	// Supported - the mechanism is in the kernel build (otherwise control is unavailable).
	Supported bool `json:"supported"`
	// Available - the parameters are visible now: the mechanism can be enabled and disabled.
	Available bool `json:"available"`
	// Enabled - the mechanism is enabled in the kernel.
	Enabled bool `json:"enabled"`
	// SavedBytes - memory saved by this mechanism.
	SavedBytes uint64 `json:"saved_bytes"`
	// Merged - the number of merged pages.
	Merged uint64 `json:"merged"`
	// Reason - why the mechanism is unavailable (for the UI).
	Reason string `json:"reason,omitempty"`
}

// Status - the state of both mechanisms.
type Status struct {
	KSM  Mechanism `json:"ksm"`
	UKSM Mechanism `json:"uksm"`
}

// SetDirs replaces the parameter directories. Used in tests; empty values are skipped.
func SetDirs(standard, uksmDir string) {
	mu.Lock()
	defer mu.Unlock()
	if standard != "" {
		DirKSM = standard
	}
	if uksmDir != "" {
		DirUKSM = uksmDir
	}
}

// dirOf returns the mechanism's parameter directory. Call with mu held.
func dirOf(kind string) string {
	if kind == KindUKSM {
		return DirUKSM
	}
	return DirKSM
}

// parseNumber reads a numeric kernel parameter.
func parseNumber(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("ksm: %s: %w", path, err)
	}
	return value, nil
}

// sysfsMounted reports whether sysfs is mounted: with sysfs not mounted the path
// /sys/kernel/mm/ksm may be missing even if KSM is built into the kernel.
func sysfsMounted() bool {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "/sys" {
			return true
		}
	}
	return false
}

// mechanism provides the state of one mechanism. Call with mu held.
func mechanism(kind string) Mechanism {
	dir := dirOf(kind)
	runPath := filepath.Join(dir, "run")
	if _, err := os.Stat(runPath); err != nil {
		// The run parameter is missing: the mechanism is either not built in or its parameters are not visible.
		if _, err := os.Stat(filepath.Join(dir, "pages_sharing")); err != nil {
			reason := "page merging is not available in the kernel"
			if !sysfsMounted() {
				reason = "sysfs is not mounted"
			}
			return Mechanism{Reason: reason}
		}
		return Mechanism{Supported: true, Reason: "the kernel does not allow switching page merging"}
	}
	out := Mechanism{Supported: true, Available: true, Enabled: true}
	if run, err := parseNumber(runPath); err == nil {
		out.Enabled = run != 0
	}
	if pages, err := parseNumber(filepath.Join(dir, "pages_sharing")); err == nil {
		out.Merged = pages
		out.SavedBytes = pages * uint64(os.Getpagesize())
	} else if bytes, err := parseNumber(filepath.Join(dir, "saved_bytes")); err == nil {
		out.SavedBytes = bytes
	}
	return out
}

// writeRun writes the mechanism switch. An empty value is skipped.
func writeRun(kind string, enabled bool) error {
	path := filepath.Join(dirOf(kind), "run")
	if _, err := os.Stat(path); err != nil {
		return ErrUnavailable
	}
	value := "0"
	if enabled {
		value = "1"
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		return fmt.Errorf("ksm: %s=%s: %w", kind, value, err)
	}
	return nil
}

// Service - page merging control. Allows replacing the state source in tests.
type Service interface {
	Status() Status
	// Set enables one mechanism and always disables the other.
	Set(kind string, enabled bool) error
}

// sysfs - the implementation on top of kernel parameters.
type sysfs struct{}

// SYSFS - KSM and UKSM control via sysfs.
var SYSFS Service = sysfs{}

func (sysfs) Status() Status {
	mu.Lock()
	defer mu.Unlock()
	return Status{KSM: mechanism(KindKSM), UKSM: mechanism(KindUKSM)}
}

// Set enables or disables a mechanism. Enabling one always stops
// the other: first its run=0 parameter, and only then the chosen one is enabled. This way KSM and
// UKSM never run simultaneously.
func (sysfs) Set(kind string, enabled bool) error {
	if kind != KindKSM && kind != KindUKSM {
		return ErrUnknownKind
	}
	mu.Lock()
	defer mu.Unlock()
	if enabled {
		// First check that the chosen mechanism can be enabled at all: otherwise it is not
		// worth disabling the other one.
		if _, err := os.Stat(filepath.Join(dirOf(kind), "run")); err != nil {
			return ErrUnavailable
		}
		other := KindKSM
		if kind == KindKSM {
			other = KindUKSM
		}
		if err := writeRun(other, false); err != nil && !errors.Is(err, ErrUnavailable) {
			return err
		}
		return writeRun(kind, true)
	}
	return writeRun(kind, false)
}

// Read returns the current state of both mechanisms.
func Read() Status { return SYSFS.Status() }

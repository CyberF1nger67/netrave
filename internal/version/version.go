// SPDX-License-Identifier: GPL-3.0-only

// Package version exposes build metadata shared by the CLI and the HTTP API.
package version

// These values can be overridden at build time with:
//
//	-ldflags "-X netrave/internal/version.Version=... -X ...Commit=... -X ...Date=..."
var (
	Version = "0.6.0"
	Commit  = "unknown"
	Date    = "unknown"
)

// String returns a compact human-readable version.
func String() string { return Version }

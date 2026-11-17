// SPDX-License-Identifier: GPL-3.0-only

// Package names validates names that users assign to objects: labs, folders,
// templates, images and snapshots. The name ends up in URLs and config file names, so
// only Latin letters, digits, hyphens and underscores are allowed.
package names

import (
	"fmt"
	"regexp"
)

// MaxLen is the maximum name length.
const MaxLen = 64

var pattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Valid reports whether the name is acceptable.
func Valid(name string) bool {
	return pattern.MatchString(name)
}

// Check returns a descriptive error for an invalid name; kind is the object type used in the message.
func Check(kind, name string) error {
	if Valid(name) {
		return nil
	}
	return fmt.Errorf("%s %q: допустимы латинские буквы, цифры, '-' и '_', длина до %d символов", kind, name, MaxLen)
}

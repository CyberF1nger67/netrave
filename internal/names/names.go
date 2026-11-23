// SPDX-License-Identifier: GPL-3.0-only

// Package names holds the naming rules shared by the whole application.
//
// Object names (labs, folders, templates, devices, networks, images, icons, themes, commit titles)
// may use any character Linux allows in a file name, so Cyrillic and other scripts are fine. The
// only exceptions are '/', NUL and control characters, which cannot be represented in a path or
// a line of text; a leading '.' is also rejected because the application treats dot-entries as hidden.
//
// Roles and login names keep a Latin-only alphabet (letters, digits, '_' and '-'); they may contain
// single spaces between words. Passwords are checked by their own rule.
//
// The package never rewrites a name: a name either passes or is rejected with an explanation.
package names

import (
	"fmt"
	"regexp"
	"unicode"
	"unicode/utf8"

	"netrave/internal/msgs"
)

// MaxLen is the length limit in characters for object names, roles and login names.
const MaxLen = 64

// MaxFileBytes is the Linux limit for a single path component, in bytes.
const MaxFileBytes = 255

// Password length limits; the character rule is separate.
const (
	PasswordMinLen = 8
	PasswordMaxLen = 256
)

var (
	loginPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]+( [A-Za-z0-9_-]+)*$`)
	passwordPattern = regexp.MustCompile(`^[!-~]{8,256}$`)
)

// validChars reports whether the text can be used as one path component: non-empty, valid UTF-8,
// no '/', no NUL or other control characters, and no leading '.'.
func validChars(name string) bool {
	if name == "" || name[0] == '.' || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if r == '/' || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// Valid reports whether an object name is acceptable: 1–64 characters that Linux allows in a file name.
func Valid(name string) bool {
	return utf8.RuneCountInString(name) <= MaxLen && validChars(name)
}

// Check returns a descriptive error for an invalid object name; kind is the object type used in the message.
func Check(kind, name string) error {
	if Valid(name) {
		return nil
	}
	return msgs.New("{kind_key} «{name}»: 1–{max} characters, no «/», control characters or leading dot", map[string]string{"kind_key": kind, "name": name, "max": fmt.Sprint(MaxLen)})
}

// ValidFile reports whether a file or folder name on disk is acceptable: a name Linux allows, up to
// 255 bytes. Extensions are not checked here; the caller decides which extensions it accepts.
func ValidFile(name string) bool {
	return len(name) <= MaxFileBytes && validChars(name)
}

// ValidLogin reports whether a role or login name is acceptable: 1–64 characters from Latin letters,
// digits, '_' and '-', with single spaces allowed between words. Leading, trailing and doubled spaces are rejected.
func ValidLogin(name string) bool {
	return utf8.RuneCountInString(name) <= MaxLen && loginPattern.MatchString(name)
}

// CheckLogin is ValidLogin with an error for a role or other named object of the given kind.
func CheckLogin(kind, name string) error {
	if ValidLogin(name) {
		return nil
	}
	return msgs.New("{kind_key} «{name}»: 1–{max} characters, Latin letters, digits, underscore, hyphen and single spaces between words", map[string]string{"kind_key": kind, "name": name, "max": fmt.Sprint(MaxLen)})
}

// ValidUsername reports whether a login name is acceptable: the same Latin rule as roles.
// Accounts created before this rule may still hold other names; the rule is applied on creation.
func ValidUsername(name string) bool {
	return ValidLogin(name)
}

// CheckUsername returns a user-facing error for a login name that ValidUsername rejects.
func CheckUsername(name string) error {
	if ValidUsername(name) {
		return nil
	}
	return msgs.New("Username: 1–{max} characters (Latin letters, digits, underscore, hyphen and single spaces between words)", map[string]string{"max": fmt.Sprint(MaxLen)})
}

// ValidPassword reports whether a password is acceptable: 8–256 printable ASCII characters without spaces.
func ValidPassword(p string) bool {
	return passwordPattern.MatchString(p)
}

// CheckPassword returns a user-facing error for a password that ValidPassword rejects.
func CheckPassword(p string) error {
	if ValidPassword(p) {
		return nil
	}
	return msgs.New("Password: {min}–{max} characters (Latin letters, digits and symbols, no spaces)", map[string]string{"min": fmt.Sprint(PasswordMinLen), "max": fmt.Sprint(PasswordMaxLen)})
}

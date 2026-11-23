// SPDX-License-Identifier: GPL-3.0-only

package names

import (
	"strings"
	"testing"
)

func TestValid(t *testing.T) {
	good := []string{"test", "Lab-01", "ubuntu_24", "A", "1", "тест", "Диск 1", "my lab", "a.b", "a.b.c", "a~b", "a;b", "a'b", "a\"b", "a\\b", "a$b", strings.Repeat("a", MaxLen), strings.Repeat("я", MaxLen)}
	bad := []string{"", ".", "..", ".hidden", "a/b", "../x", "a\x00b", "a\nb", "a\tb", "a\x7fb", strings.Repeat("a", MaxLen+1), strings.Repeat("я", MaxLen+1)}
	for _, name := range good {
		if !Valid(name) {
			t.Errorf("%q should be valid", name)
		}
		if err := Check("Name", name); err != nil {
			t.Errorf("Check(%q): %v", name, err)
		}
	}
	for _, name := range bad {
		if Valid(name) {
			t.Errorf("%q should be invalid", name)
		}
		if err := Check("Name", name); err == nil {
			t.Errorf("Check(%q) did not return an error", name)
		}
	}
}

func TestValidFile(t *testing.T) {
	good := []string{"disk.qcow2", "Диск один.iso", "my disk.v1.raw", "a", strings.Repeat("a", MaxFileBytes)}
	bad := []string{"", ".disk.iso", "a/b.iso", "a\x00.iso", strings.Repeat("a", MaxFileBytes+1), strings.Repeat("я", 128)}
	for _, name := range good {
		if !ValidFile(name) {
			t.Errorf("%q should be valid", name)
		}
	}
	for _, name := range bad {
		if ValidFile(name) {
			t.Errorf("%q should be invalid", name)
		}
	}
}

func TestValidLogin(t *testing.T) {
	good := []string{"admin", "Admin_01", "john-doe", "John Doe", "a b c", strings.Repeat("a", MaxLen)}
	bad := []string{"", " john", "john ", "john  doe", "john.doe", "тест", "a/b", "a;b", "a'b", "a\tb", strings.Repeat("a", MaxLen+1)}
	for _, name := range good {
		if !ValidLogin(name) || CheckLogin("Role name", name) != nil {
			t.Errorf("%q should be a valid login/role name", name)
		}
	}
	for _, name := range bad {
		if ValidLogin(name) || CheckLogin("Role name", name) == nil {
			t.Errorf("%q should be rejected as a login/role name", name)
		}
	}
}

func TestUsername(t *testing.T) {
	if !ValidUsername("John Doe") || CheckUsername("John Doe") != nil {
		t.Error("username with a space should be valid")
	}
	if ValidUsername("тест") || CheckUsername("тест") == nil {
		t.Error("Cyrillic username should be rejected")
	}
}

func TestValidPassword(t *testing.T) {
	good := []string{"12345678", "Pass_word-1", "pass!word1", "p@ss#w0rd$%", strings.Repeat("p", PasswordMaxLen)}
	bad := []string{"", "1234567", strings.Repeat("p", PasswordMaxLen+1), "pass word1", "пароль123", "pass\tword1"}
	for _, p := range good {
		if !ValidPassword(p) {
			t.Errorf("password of length %d should be valid", len(p))
		}
	}
	for _, p := range bad {
		if ValidPassword(p) || CheckPassword(p) == nil {
			t.Errorf("password %q should be invalid", p)
		}
	}
}

// SPDX-License-Identifier: GPL-3.0-only

package names

import (
	"strings"
	"testing"
)

func TestValid(t *testing.T) {
	good := []string{"test", "Lab-01", "ubuntu_24", "A"}
	bad := []string{"", "тест", "lab 1", "lab.yaml", "../x", "a/b", "~"}
	for _, name := range good {
		if !Valid(name) {
			t.Errorf("%q должно быть допустимым", name)
		}
	}
	for _, name := range bad {
		if Valid(name) {
			t.Errorf("%q должно быть недопустимым", name)
		}
	}
	if Valid(strings.Repeat("a", MaxLen+1)) {
		t.Error("слишком длинное имя принято")
	}
	if err := Check("шаблон", "тест"); err == nil {
		t.Error("Check не вернул ошибку")
	}
}

// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"net/http"
	"testing"
)

// An opened instance of another lab is not visible to an outsider: neither without a search nor with one.
// Previously the WHERE condition without parentheses let such rows slip past all checks.
func TestOpenedForeignInstanceIsHidden(t *testing.T) {
	e := newTenv(t)
	owner := e.user(t, "owner", "user")
	stranger := e.user(t, "stranger", "user")

	lab := e.lab(t, owner.id)
	dev := e.device(t, lab)
	// The machine was started at some point (started>0) and is now stopped: the row appears in the list based on "openness".
	e.runInstance(t, lab, dev, owner.id, "stopped", 0, 1000)

	fetch := func(u sess, query string) []RunningLab {
		t.Helper()
		var out []RunningLab
		rec := e.do(t, u, http.MethodGet, "/api/labs/running"+query, nil)
		expect(t, rec, http.StatusOK, "список запущенных")
		decodeJSON(t, rec.Body.Bytes(), &out)
		return out
	}

	if rows := fetch(stranger, ""); len(rows) != 0 {
		t.Fatalf("посторонний видит открытый экземпляр чужой лаборатории: %+v", rows)
	}
	if rows := fetch(stranger, "?q=lab"); len(rows) != 0 {
		t.Fatalf("поиск не должен открывать чужую лабораторию: %+v", rows)
	}
	// The owner sees their lab's row, and a search for a non-matching name hides it.
	if rows := fetch(owner, ""); len(rows) != 1 {
		t.Fatalf("владелец должен видеть открытый экземпляр своей лаборатории: %+v", rows)
	}
	if rows := fetch(owner, "?q=zzz"); len(rows) != 0 {
		t.Fatalf("поиск должен скрывать открытые строки, не совпадающие с запросом: %+v", rows)
	}
}

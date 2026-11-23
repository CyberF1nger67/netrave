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
		expect(t, rec, http.StatusOK, "running list")
		decodeJSON(t, rec.Body.Bytes(), &out)
		return out
	}

	if rows := fetch(stranger, ""); len(rows) != 0 {
		t.Fatalf("outsider sees an opened instance of a foreign lab: %+v", rows)
	}
	if rows := fetch(stranger, "?q=lab"); len(rows) != 0 {
		t.Fatalf("search must not reveal a foreign lab: %+v", rows)
	}
	// The owner sees their lab's row, and a search for a non-matching name hides it.
	if rows := fetch(owner, ""); len(rows) != 1 {
		t.Fatalf("owner must see the opened instance of their lab: %+v", rows)
	}
	if rows := fetch(owner, "?q=zzz"); len(rows) != 0 {
		t.Fatalf("search must hide opened rows that do not match the query: %+v", rows)
	}
}

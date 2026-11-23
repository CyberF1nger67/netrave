// SPDX-License-Identifier: GPL-3.0-only

package notify

import (
	"testing"
	"time"
)

// An inactive user without a WebSocket loses their list in memory, while a connected one keeps it.
func TestIdleUsersAreReleasedUnlessConnected(t *testing.T) {
	st := newStore()
	none := func(int64) bool { return false }
	now := time.Now()
	st.add(1, Item{Level: Info, Category: CatSystem, Title: "a"}, now, none)
	st.add(2, Item{Level: Info, Category: CatSystem, Title: "b"}, now, none)

	later := now.Add(IdleTTL + 2*time.Minute)
	connected := func(uid int64) bool { return uid == 2 }
	st.list(3, later, connected, false, "", 10) // access triggers cleanup

	if _, ok := st.users[1]; ok {
		t.Fatal("inactive user's list must be released")
	}
	if b, ok := st.users[2]; !ok || len(b.items) != 1 {
		t.Fatal("connected user's list must be kept")
	}
}

// An identical unread notification is not duplicated, and after reading it is created again.
func TestStoreDuplicateRules(t *testing.T) {
	st := newStore()
	none := func(int64) bool { return false }
	now := time.Now()
	it := Item{Level: Warning, Category: CatSystem, Title: "t", Body: "b"}
	if _, ok := st.add(1, it, now, none); !ok {
		t.Fatal("first notification must be stored")
	}
	if _, ok := st.add(1, it, now, none); ok {
		t.Fatal("duplicate unread item must not be stored")
	}
	st.readAll(1, now)
	if _, ok := st.add(1, it, now, none); !ok {
		t.Fatal("after reading, the same notification is a new event")
	}
}

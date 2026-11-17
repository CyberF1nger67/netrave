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
		t.Fatal("список неактивного пользователя должен освободиться")
	}
	if b, ok := st.users[2]; !ok || len(b.items) != 1 {
		t.Fatal("список подключённого пользователя должен сохраниться")
	}
}

// An identical unread notification is not duplicated, and after reading it is created again.
func TestStoreDuplicateRules(t *testing.T) {
	st := newStore()
	none := func(int64) bool { return false }
	now := time.Now()
	it := Item{Level: Warning, Category: CatSystem, Title: "t", Body: "b"}
	if _, ok := st.add(1, it, now, none); !ok {
		t.Fatal("первое уведомление должно записаться")
	}
	if _, ok := st.add(1, it, now, none); ok {
		t.Fatal("дубликат непрочитанного не должен записываться")
	}
	st.readAll(1, now)
	if _, ok := st.add(1, it, now, none); !ok {
		t.Fatal("после прочтения такое же уведомление — новое событие")
	}
}

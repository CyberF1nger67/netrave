// SPDX-License-Identifier: GPL-3.0-only

package instances

import (
	"path/filepath"
	"testing"

	"netrave/internal/database"
	"netrave/internal/labtest"
)

func TestEnsureIsIdempotentPerUser(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.Exec(`INSERT INTO users(id,username,password_hash,role) VALUES(1,'admin','x','admin'),(2,'vlad','x','user'),(3,'alex','x','user')`)
	lab := labtest.Lab(t, db, 1, "Network Lab", "")

	a1, err := Ensure(db, lab, 2)
	if err != nil {
		t.Fatal(err)
	}
	a2, _ := Ensure(db, lab, 2)
	if a1 != a2 {
		t.Fatalf("повторный вызов должен вернуть тот же экземпляр: %d != %d", a1, a2)
	}
	b, _ := Ensure(db, lab, 3)
	if b == a1 {
		t.Fatal("разные пользователи получили один экземпляр")
	}
}

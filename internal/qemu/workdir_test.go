// SPDX-License-Identifier: GPL-3.0-only

package qemu

import (
	"os"
	"path/filepath"
	"testing"

	"netrave/internal/database"
)

// mustWorkDir returns the working directory of the machine with row number rid.
func mustWorkDir(t *testing.T, db *database.DB, instancesDir string, rid int64) string {
	t.Helper()
	p, err := WorkDir(db, instancesDir, rid)
	if err != nil {
		t.Fatal(err)
	}
	return p.Dir
}

// A machine's working directory is named after the owner, the laboratory and the machine.
func TestWorkPathsAreNamed(t *testing.T) {
	root := filepath.Join("/data", "instances")
	if got, want := workPaths(root, "alice", "perimeter", "fw1").Dir, filepath.Join(root, "alice", "perimeter", "fw1"); got != want {
		t.Fatalf("work dir %q, want %q", got, want)
	}
	if got := workPaths(root, "alice", "..", "fw1").Dir; got != "" {
		t.Fatalf("unsafe lab name gave a directory: %q", got)
	}
}

// A failed move undoes the moves made before it, so the files stay where the database still says they are.
func TestMoveDirsUndoesOnFailure(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	pairs := [][2]string{
		{filepath.Join(root, "a"), filepath.Join(root, "x")},
		{filepath.Join(root, "b"), filepath.Join(root, "missing", "y")},
	}
	if _, err := moveDirs(pairs); err == nil {
		t.Fatal("move into a missing parent did not fail")
	}
	if _, err := os.Stat(filepath.Join(root, "a")); err != nil {
		t.Fatalf("first directory not moved back: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "x")); !os.IsNotExist(err) {
		t.Fatalf("target of the undone move remains: %v", err)
	}
}

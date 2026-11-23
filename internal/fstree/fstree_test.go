// SPDX-License-Identifier: GPL-3.0-only

package fstree

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// makeTree creates files and directories under a new temporary root.
func makeTree(t *testing.T, dirs []string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range dirs {
		if err := os.MkdirAll(Abs(root, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for rel, content := range files {
		if err := os.WriteFile(Abs(root, rel), []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func labKeys(t Tree) map[string]bool {
	out := map[string]bool{}
	for _, l := range t.Labs {
		out[LabPath(l.Folder, l.Name)] = true
	}
	return out
}

func TestScanListsFoldersAndLabs(t *testing.T) {
	root := makeTree(t,
		[]string{"F/G"},
		map[string]string{
			"A.yaml":           "x",
			"F/B.yaml":         "x",
			"F/G/C.yaml":       "x",
			".hidden.yaml":     "x",
			"F/notes.txt":      "x",
			"F/draft.yaml.tmp": "x",
		})
	tree, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Folders) != 2 || tree.Folders[0].Path != "F" || tree.Folders[1].Path != "F/G" {
		t.Fatalf("folders: %+v", tree.Folders)
	}
	want := map[string]bool{"A.yaml": true, "F/B.yaml": true, "F/G/C.yaml": true}
	got := labKeys(tree)
	if len(got) != len(want) {
		t.Fatalf("labs: %v", got)
	}
	for k := range want {
		if !got[k] {
			t.Fatalf("missing lab %s in %v", k, got)
		}
	}
}

func TestScanReportsDuplicateLabNamesCaseInsensitively(t *testing.T) {
	root := makeTree(t, []string{"a", "b"}, map[string]string{"a/X.yaml": "x", "b/x.yaml": "x"})
	tree, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Labs) != 1 {
		t.Fatalf("labs: %+v", tree.Labs)
	}
	if len(tree.Problems) != 1 || !errors.Is(tree.Problems[0].Err, ErrExists) {
		t.Fatalf("problems: %+v", tree.Problems)
	}
}

func TestMkdirRejectsCaseInsensitiveDuplicate(t *testing.T) {
	root := t.TempDir()
	if err := Mkdir(root, "", "Folder"); err != nil {
		t.Fatal(err)
	}
	if err := Mkdir(root, "", "folder"); !errors.Is(err, ErrExists) {
		t.Fatalf("expected ErrExists, got %v", err)
	}
}

func TestMkdirRequiresExistingParent(t *testing.T) {
	root := t.TempDir()
	if err := Mkdir(root, "Missing", "Child"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRenameFolderAllowsCaseChangeAndRejectsSibling(t *testing.T) {
	root := makeTree(t, []string{"F", "G"}, nil)
	if err := RenameFolder(root, "F", "f"); err != nil {
		t.Fatalf("case change: %v", err)
	}
	if err := RenameFolder(root, "f", "G"); !errors.Is(err, ErrExists) {
		t.Fatalf("expected ErrExists, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "f")); err != nil {
		t.Fatalf("folder lost after rejected rename: %v", err)
	}
}

func TestRenameFolderMovesSubfoldersAndLabs(t *testing.T) {
	root := makeTree(t, []string{"F/Sub"}, map[string]string{"F/Sub/L.yaml": "x"})
	if err := RenameFolder(root, "F", "Renamed"); err != nil {
		t.Fatal(err)
	}
	tree, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if !labKeys(tree)["Renamed/Sub/L.yaml"] {
		t.Fatalf("lab did not follow the folder: %v", labKeys(tree))
	}
}

func TestMoveFolderIntoItselfIsRejected(t *testing.T) {
	root := makeTree(t, []string{"F/Sub"}, nil)
	if err := MoveFolder(root, "F", "F/Sub"); !errors.Is(err, ErrInside) {
		t.Fatalf("expected ErrInside, got %v", err)
	}
	if err := MoveFolder(root, "F", "F"); !errors.Is(err, ErrInside) {
		t.Fatalf("expected ErrInside for self, got %v", err)
	}
}

func TestMoveFolderToRoot(t *testing.T) {
	root := makeTree(t, []string{"Outer/Inner"}, nil)
	if err := MoveFolder(root, "Outer/Inner", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "Inner")); err != nil {
		t.Fatalf("folder not at root: %v", err)
	}
}

func TestRemoveFolderRequiresEmpty(t *testing.T) {
	root := makeTree(t, []string{"F"}, map[string]string{"F/L.yaml": "x"})
	if err := RemoveFolder(root, "F"); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("expected ErrNotEmpty, got %v", err)
	}
	if err := RemoveLab(root, "F", "L"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveFolder(root, "F"); err != nil {
		t.Fatal(err)
	}
}

func TestWriteLabIsUniqueAndLeavesNoTemporaryFile(t *testing.T) {
	root := makeTree(t, []string{"F", "G"}, nil)
	if err := WriteLab(root, "F", "A", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := WriteLab(root, "G", "a", []byte("two")); !errors.Is(err, ErrExists) {
		t.Fatalf("expected ErrExists, got %v", err)
	}
	if err := WriteLab(root, "F", "A", []byte("three")); err != nil {
		t.Fatalf("replace in place: %v", err)
	}
	data, err := os.ReadFile(Abs(root, "F/A.yaml"))
	if err != nil || string(data) != "three" {
		t.Fatalf("content %q, %v", data, err)
	}
	if _, err := os.Stat(Abs(root, "F/A.yaml.tmp")); !os.IsNotExist(err) {
		t.Fatalf("temporary file left behind: %v", err)
	}
}

func TestRenameLabAndMoveLab(t *testing.T) {
	root := makeTree(t, []string{"F", "G"}, map[string]string{"F/A.yaml": "x"})
	if err := RenameLab(root, "F", "A", "B"); err != nil {
		t.Fatal(err)
	}
	if err := MoveLab(root, "F", "B", "G"); err != nil {
		t.Fatal(err)
	}
	tree, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if !labKeys(tree)["G/B.yaml"] {
		t.Fatalf("lab not moved: %v", labKeys(tree))
	}
}

func TestMoveLabRejectsNameTakenInTarget(t *testing.T) {
	root := makeTree(t, []string{"F", "G"}, map[string]string{"F/A.yaml": "x", "G/A.yaml": "y"})
	if err := MoveLab(root, "F", "A", "G"); !errors.Is(err, ErrExists) {
		t.Fatalf("expected ErrExists, got %v", err)
	}
}

func TestValidPathRejectsTraversal(t *testing.T) {
	for _, bad := range []string{"..", "a/../b", "/abs", ".hidden", "a//b"} {
		if err := ValidPath(bad); !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("%q accepted: %v", bad, err)
		}
	}
	if err := ValidPath(""); err != nil {
		t.Fatalf("root rejected: %v", err)
	}
}

func TestWriteLabRejectsInvalidName(t *testing.T) {
	root := makeTree(t, []string{"F"}, nil)
	if err := WriteLab(root, "F", "a/b", []byte("x")); err == nil {
		t.Fatal("name with slash accepted")
	}
}

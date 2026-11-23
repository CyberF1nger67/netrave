// SPDX-License-Identifier: GPL-3.0-only

// Package fstree maps the laboratory directory onto a tree of folders and lab files.
//
// A folder is a directory. A laboratory is a file <name>.yaml inside a folder. The directory tree is
// the only record of placement: moving a file moves the lab, renaming a directory renames the folder.
// Lab names are identifiers, so they are unique across the whole tree, compared case-insensitively.
// Hidden entries (names starting with a dot), temporary files and files without the .yaml extension
// are not part of the tree.
//
// Paths are slash-separated and relative to the root directory; the empty path is the root itself.
// The package returns sentinel errors; callers translate them for the user.
package fstree

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"netrave/internal/names"
)

// Ext is the extension of lab files.
const Ext = ".yaml"

// tmpSuffix marks temporary files written by WriteAtomic; they are never listed.
const tmpSuffix = ".tmp"

var (
	// ErrExists - a folder or a lab with this name is already in use (case-insensitively).
	ErrExists = errors.New("name is already in use")
	// ErrNotFound - no such folder or lab.
	ErrNotFound = errors.New("not found")
	// ErrNotEmpty - the folder still contains entries.
	ErrNotEmpty = errors.New("folder is not empty")
	// ErrInvalidPath - a folder path is malformed or points outside the root.
	ErrInvalidPath = errors.New("invalid folder path")
	// ErrInvalidName - an entry name cannot be used on disk.
	ErrInvalidName = errors.New("invalid name")
	// ErrInside - a folder would be moved into itself or into its own subfolder.
	ErrInside = errors.New("folder cannot be moved into itself")
)

// Folder is a directory of the tree.
type Folder struct {
	Path string
	Name string
}

// Lab is a lab file. Folder is the path of the containing folder ("" - the root).
type Lab struct {
	Name   string
	Folder string
}

// Problem is an entry that was skipped during a scan.
type Problem struct {
	Path string
	Err  error
}

// Tree is the scanned state of the laboratory directory.
type Tree struct {
	Folders  []Folder  // sorted by path
	Labs     []Lab     // sorted by path
	Problems []Problem // skipped entries
}

// sameName compares names the way lab and folder names are compared everywhere in the application.
func sameName(a, b string) bool {
	return strings.ToLower(a) == strings.ToLower(b)
}

// join builds a relative path from a folder path and an entry name.
func join(rel, name string) string {
	if rel == "" {
		return name
	}
	return rel + "/" + name
}

// splitRel separates the parent folder path and the base name of a relative path.
func splitRel(rel string) (parent, base string) {
	i := strings.LastIndex(rel, "/")
	if i < 0 {
		return "", rel
	}
	return rel[:i], rel[i+1:]
}

// Abs returns the absolute location of a relative path.
func Abs(root, rel string) string {
	return filepath.Join(root, filepath.FromSlash(rel))
}

// LabPath returns the relative path of a lab file.
func LabPath(folder, name string) string {
	return join(folder, name+Ext)
}

// ValidPath reports whether a folder path consists only of valid names. The empty path (the root) is valid.
func ValidPath(folder string) error {
	if folder == "" {
		return nil
	}
	for _, seg := range strings.Split(folder, "/") {
		if !names.ValidFile(seg) {
			return ErrInvalidPath
		}
	}
	return nil
}

// Scan walks the root directory and returns folders and labs. A missing root is an error;
// the caller creates the root beforehand.
func Scan(root string) (Tree, error) {
	var t Tree
	seen := map[string]string{} // lower-cased lab name -> path of the first lab with that name

	var walk func(rel string) error
	walk = func(rel string) error {
		entries, err := os.ReadDir(Abs(root, rel))
		if err != nil {
			return err
		}
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".") || strings.HasSuffix(name, tmpSuffix) {
				continue
			}
			childRel := join(rel, name)
			switch {
			case e.IsDir():
				if !names.ValidFile(name) {
					t.Problems = append(t.Problems, Problem{Path: childRel, Err: ErrInvalidName})
					continue
				}
				t.Folders = append(t.Folders, Folder{Path: childRel, Name: name})
				if err := walk(childRel); err != nil {
					return err
				}
			case e.Type().IsRegular() && strings.HasSuffix(name, Ext):
				labName := strings.TrimSuffix(name, Ext)
				if !names.Valid(labName) {
					t.Problems = append(t.Problems, Problem{Path: childRel, Err: ErrInvalidName})
					continue
				}
				key := strings.ToLower(labName)
				if _, dup := seen[key]; dup {
					t.Problems = append(t.Problems, Problem{Path: childRel, Err: ErrExists})
					continue
				}
				seen[key] = childRel
				t.Labs = append(t.Labs, Lab{Name: labName, Folder: rel})
			}
		}
		return nil
	}
	if err := walk(""); err != nil {
		return Tree{}, err
	}
	sort.Slice(t.Folders, func(i, j int) bool { return t.Folders[i].Path < t.Folders[j].Path })
	sort.Slice(t.Labs, func(i, j int) bool {
		return LabPath(t.Labs[i].Folder, t.Labs[i].Name) < LabPath(t.Labs[j].Folder, t.Labs[j].Name)
	})
	return t, nil
}

// requireDir reports ErrNotFound unless p is an existing directory.
func requireDir(p string) error {
	info, err := os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return ErrNotFound
	}
	return nil
}

// requireFile reports ErrNotFound unless p is an existing regular file.
func requireFile(p string) error {
	info, err := os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return ErrNotFound
	}
	return nil
}

// dirHasFolder reports whether dir contains a directory with the name (case-insensitively),
// ignoring the entry called except.
func dirHasFolder(dir, name, except string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.IsDir() && sameName(e.Name(), name) && e.Name() != except {
			return true, nil
		}
	}
	return false, nil
}

// checkLabName reports ErrExists when another lab file in the tree already has the name.
// self is the relative path of the lab that is being saved; it does not conflict with itself.
// The check walks the directories directly because Scan reports a duplicate name as a problem
// and keeps only the first file, which would hide the conflict from the caller.
func checkLabName(root, name, self string) error {
	var walk func(rel string) error
	walk = func(rel string) error {
		entries, err := os.ReadDir(Abs(root, rel))
		if err != nil {
			return err
		}
		for _, e := range entries {
			entry := e.Name()
			if strings.HasPrefix(entry, ".") || strings.HasSuffix(entry, tmpSuffix) {
				continue
			}
			childRel := join(rel, entry)
			if e.IsDir() {
				if err := walk(childRel); err != nil {
					return err
				}
				continue
			}
			if !e.Type().IsRegular() || !strings.HasSuffix(entry, Ext) {
				continue
			}
			if childRel != self && sameName(strings.TrimSuffix(entry, Ext), name) {
				return ErrExists
			}
		}
		return nil
	}
	return walk("")
}

// Mkdir creates a folder inside parent ("" - the root).
func Mkdir(root, parent, name string) error {
	if err := ValidPath(parent); err != nil {
		return err
	}
	if err := names.Check("Folder name", name); err != nil {
		return err
	}
	dir := Abs(root, parent)
	if err := requireDir(dir); err != nil {
		return err
	}
	taken, err := dirHasFolder(dir, name, "")
	if err != nil {
		return err
	}
	if taken {
		return ErrExists
	}
	return os.Mkdir(filepath.Join(dir, name), 0o750)
}

// RenameFolder changes the name of a folder. Changing only the letter case of the name is allowed.
func RenameFolder(root, rel, newName string) error {
	if rel == "" || ValidPath(rel) != nil {
		return ErrInvalidPath
	}
	if err := names.Check("Folder name", newName); err != nil {
		return err
	}
	parent, base := splitRel(rel)
	if err := requireDir(Abs(root, rel)); err != nil {
		return err
	}
	if newName == base {
		return nil
	}
	taken, err := dirHasFolder(Abs(root, parent), newName, base)
	if err != nil {
		return err
	}
	if taken {
		return ErrExists
	}
	return os.Rename(Abs(root, rel), filepath.Join(Abs(root, parent), newName))
}

// MoveFolder moves a folder, together with its subfolders, into target ("" - the root).
func MoveFolder(root, rel, target string) error {
	if rel == "" || ValidPath(rel) != nil || ValidPath(target) != nil {
		return ErrInvalidPath
	}
	if target == rel || strings.HasPrefix(target, rel+"/") {
		return ErrInside
	}
	_, base := splitRel(rel)
	if err := requireDir(Abs(root, rel)); err != nil {
		return err
	}
	if err := requireDir(Abs(root, target)); err != nil {
		return err
	}
	if join(target, base) == rel {
		return nil
	}
	taken, err := dirHasFolder(Abs(root, target), base, "")
	if err != nil {
		return err
	}
	if taken {
		return ErrExists
	}
	return os.Rename(Abs(root, rel), filepath.Join(Abs(root, target), base))
}

// RemoveFolder deletes an empty folder. Labs must be moved out first.
func RemoveFolder(root, rel string) error {
	if rel == "" || ValidPath(rel) != nil {
		return ErrInvalidPath
	}
	dir := Abs(root, rel)
	if err := requireDir(dir); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return ErrNotEmpty
	}
	return os.Remove(dir)
}

// WriteLab creates or replaces a lab file. The folder must exist and the name must not be used by another lab.
func WriteLab(root, folder, name string, data []byte) error {
	if err := ValidPath(folder); err != nil {
		return err
	}
	if !names.Valid(name) {
		return names.Check("Laboratory name", name)
	}
	if err := requireDir(Abs(root, folder)); err != nil {
		return err
	}
	rel := LabPath(folder, name)
	if err := checkLabName(root, name, rel); err != nil {
		return err
	}
	return WriteAtomic(Abs(root, rel), data)
}

// RenameLab changes the name of a lab in its folder.
func RenameLab(root, folder, oldName, newName string) error {
	if err := ValidPath(folder); err != nil {
		return err
	}
	if !names.Valid(newName) {
		return names.Check("Laboratory name", newName)
	}
	oldRel := LabPath(folder, oldName)
	if err := requireFile(Abs(root, oldRel)); err != nil {
		return err
	}
	newRel := LabPath(folder, newName)
	if newRel == oldRel {
		return nil
	}
	if err := checkLabName(root, newName, oldRel); err != nil {
		return err
	}
	return os.Rename(Abs(root, oldRel), Abs(root, newRel))
}

// MoveLab moves a lab file into the target folder ("" - the root).
func MoveLab(root, folder, name, target string) error {
	if err := ValidPath(folder); err != nil {
		return err
	}
	if err := ValidPath(target); err != nil {
		return err
	}
	rel := LabPath(folder, name)
	if err := requireFile(Abs(root, rel)); err != nil {
		return err
	}
	if err := requireDir(Abs(root, target)); err != nil {
		return err
	}
	newRel := LabPath(target, name)
	if newRel == rel {
		return nil
	}
	if err := checkLabName(root, name, rel); err != nil {
		return err
	}
	return os.Rename(Abs(root, rel), Abs(root, newRel))
}

// RemoveLab deletes a lab file.
func RemoveLab(root, folder, name string) error {
	if err := ValidPath(folder); err != nil {
		return err
	}
	rel := LabPath(folder, name)
	if err := requireFile(Abs(root, rel)); err != nil {
		return err
	}
	return os.Remove(Abs(root, rel))
}

// WriteAtomic writes data to path through a temporary file and a rename. The directory must exist.
func WriteAtomic(path string, data []byte) error {
	tmp := path + tmpSuffix
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

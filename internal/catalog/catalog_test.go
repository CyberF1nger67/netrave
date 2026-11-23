// SPDX-License-Identifier: GPL-3.0-only

package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "catalog")
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func baseImage(t *testing.T, s *Store, dirname string) int64 {
	t.Helper()
	id, err := s.AddImage(Image{Name: dirname, Dirname: dirname})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestImageDirnameIsUnique(t *testing.T) {
	s, _ := openTemp(t)
	baseImage(t, s, "disk")
	if _, err := s.AddImage(Image{Name: "x", Dirname: "disk"}); err == nil {
		t.Fatal("second record with the same folder must be rejected")
	}
}

func TestDeletingImageClearsTemplateReference(t *testing.T) {
	s, _ := openTemp(t)
	img := baseImage(t, s, "disk")
	tpl, err := s.AddTemplate(Template{Name: "t", Arch: "x86_64", Machine: "pc", BIOS: "seabios", Console: "serial", ImageID: img})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteImage(img); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Template(tpl)
	if got.ImageID != 0 {
		t.Fatalf("template reference to deleted image must be reset: %d", got.ImageID)
	}
}

func TestTemplateValuesAreChecked(t *testing.T) {
	s, _ := openTemp(t)
	base := Template{Name: "t", Arch: "x86_64", Machine: "pc", BIOS: "seabios", Console: "serial"}
	bad := base
	bad.BIOS = "coreboot"
	if _, err := s.AddTemplate(bad); err == nil {
		t.Fatal("unknown BIOS must be rejected")
	}
	bad = base
	bad.Console = "telnet"
	if _, err := s.AddTemplate(bad); err == nil {
		t.Fatal("unknown console must be rejected")
	}
	bad = base
	bad.CustomIcon = "ZZ"
	if _, err := s.AddTemplate(bad); err == nil {
		t.Fatal("invalid icon hash must be rejected")
	}
	bad = base
	bad.ImageID = 999
	if _, err := s.AddTemplate(bad); err == nil {
		t.Fatal("nonexistent image must be rejected")
	}
	if _, err := s.AddTemplate(base); err != nil {
		t.Fatalf("valid template: %v", err)
	}
	if _, err := s.AddTemplate(base); err == nil {
		t.Fatal("template name must be unique")
	}
}

// Records keep their numbers after the catalog is reopened, and a new record gets a number above
// every record that still exists.
func TestIDsSurviveReopen(t *testing.T) {
	s, dir := openTemp(t)
	first := baseImage(t, s, "a")
	second := baseImage(t, s, "b")
	if second <= first {
		t.Fatalf("second number %d not above first %d", second, first)
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Image(second); !ok {
		t.Fatal("record disappeared after restart")
	}
	third := baseImage(t, again, "c")
	if third <= second {
		t.Fatalf("after restart number %d not above existing %d", third, second)
	}
}

func TestFailedUpdateChangesNothing(t *testing.T) {
	s, dir := openTemp(t)
	id := baseImage(t, s, "keep")
	err := s.UpdateImage(id, func(im *Image) error {
		im.Name = "changed"
		return os.ErrInvalid
	})
	if err == nil {
		t.Fatal("fn error must be returned")
	}
	if im, _ := s.Image(id); im.Name != "keep" {
		t.Fatalf("error did not roll back in-memory change: %q", im.Name)
	}
	again, _ := Open(dir)
	if im, _ := again.Image(id); im.Name != "keep" {
		t.Fatalf("error did not roll back file change: %q", im.Name)
	}
}

// Each entry lives in a file named after it; no counter file is written.
func TestEachRecordHasItsOwnFile(t *testing.T) {
	s, dir := openTemp(t)
	baseImage(t, s, "disk")
	if _, err := os.Stat(filepath.Join(dir, "images", "disk.yaml")); err != nil {
		t.Fatalf("record file not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "images", "index.yaml")); !os.IsNotExist(err) {
		t.Fatalf("counter file must not exist, stat error: %v", err)
	}
	if _, err := s.AddTemplate(Template{Name: "t1", Arch: "x86_64", Machine: "pc", BIOS: "seabios", Console: "serial"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "templates"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d files in template directory, need 1", len(entries))
	}
}

// Deleting an entry deletes its file; other entries remain.
func TestDeleteRemovesRecordFile(t *testing.T) {
	s, dir := openTemp(t)
	keep := baseImage(t, s, "keep")
	gone := baseImage(t, s, "gone")
	if err := s.DeleteImage(gone); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "images", "gone.yaml")); !os.IsNotExist(err) {
		t.Fatal("deleted record file remains")
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Image(keep); !ok {
		t.Fatal("neighboring record disappeared")
	}
	if _, ok := again.Image(gone); ok {
		t.Fatal("deleted record came back after restart")
	}
}

// The template name is the file name; renaming moves the file and the old one disappears.
func TestTemplateFileFollowsName(t *testing.T) {
	s, dir := openTemp(t)
	id, err := s.AddTemplate(Template{Name: "ubuntu", Arch: "x86_64", Machine: "pc", BIOS: "seabios", Console: "serial"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "templates", "ubuntu.yaml")); err != nil {
		t.Fatalf("file by name not created: %v", err)
	}
	if err := s.UpdateTemplate(id, func(tp *Template) error { tp.Name = "debian"; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "templates", "ubuntu.yaml")); !os.IsNotExist(err) {
		t.Fatal("old file remains after rename")
	}
	if _, err := os.Stat(filepath.Join(dir, "templates", "debian.yaml")); err != nil {
		t.Fatalf("new file not created: %v", err)
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if tp, ok := again.Template(id); !ok || tp.Name != "debian" {
		t.Fatalf("after restart template %v", tp)
	}
}

// Swapping the names of two templates does not overwrite each other's files.
func TestTemplateNameSwapKeepsBoth(t *testing.T) {
	s, dir := openTemp(t)
	a, _ := s.AddTemplate(Template{Name: "alpha", Arch: "x86_64", Machine: "pc", BIOS: "seabios", Console: "serial"})
	b, _ := s.AddTemplate(Template{Name: "beta", Arch: "x86_64", Machine: "pc", BIOS: "seabios", Console: "serial"})
	if err := s.UpdateTemplate(a, func(tp *Template) error { tp.Name = "tmp"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTemplate(b, func(tp *Template) error { tp.Name = "alpha"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTemplate(a, func(tp *Template) error { tp.Name = "beta"; return nil }); err != nil {
		t.Fatal(err)
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if tp, _ := again.Template(a); tp.Name != "beta" {
		t.Fatalf("template a: %q", tp.Name)
	}
	if tp, _ := again.Template(b); tp.Name != "alpha" {
		t.Fatalf("template b: %q", tp.Name)
	}
}

// Names follow the Linux file-name rule: Cyrillic and spaces are accepted, '/' and control characters are not.
func TestNamesFollowLinuxRules(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.AddTemplate(Template{Name: "тест", Arch: "x86_64", Machine: "pc", BIOS: "seabios", Console: "serial"}); err != nil {
		t.Fatalf("Cyrillic template name rejected: %v", err)
	}
	if _, err := s.AddTemplate(Template{Name: "my tpl", Arch: "x86_64", Machine: "pc", BIOS: "seabios", Console: "serial"}); err != nil {
		t.Fatalf("name with space rejected: %v", err)
	}
	if _, err := s.AddTemplate(Template{Name: "a/b", Arch: "x86_64", Machine: "pc", BIOS: "seabios", Console: "serial"}); err == nil {
		t.Fatal("name with a slash accepted")
	}
	if _, err := s.AddImage(Image{Name: "диск", Dirname: "disk"}); err != nil {
		t.Fatalf("Cyrillic image name rejected: %v", err)
	}
	if _, err := s.AddImage(Image{Name: "x", Dirname: "a/b"}); err == nil {
		t.Fatal("folder name with a slash accepted")
	}
}

// A file whose name does not match the entry is rejected on load.
func TestMismatchedFileNameRejected(t *testing.T) {
	s, dir := openTemp(t)
	if _, err := s.AddTemplate(Template{Name: "one", Arch: "x86_64", Machine: "pc", BIOS: "seabios", Console: "serial"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "templates", "one.yaml"), filepath.Join(dir, "templates", "two.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("file with foreign name uploaded")
	}
}

// A new number is one more than the largest number in use, so after the largest entry is deleted and the
// catalog is reopened, its number is given to the next entry.
func TestNumbersFollowLargestExisting(t *testing.T) {
	s, dir := openTemp(t)
	first, err := s.AddTemplate(Template{Name: "first", Arch: "x86_64", Machine: "pc", BIOS: "seabios", Console: "serial"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.AddTemplate(Template{Name: "second", Arch: "x86_64", Machine: "pc", BIOS: "seabios", Console: "serial"})
	if err != nil {
		t.Fatal(err)
	}
	if second != first+1 {
		t.Fatalf("second number %d, want %d", second, first+1)
	}
	if err := s.DeleteTemplate(second); err != nil {
		t.Fatal(err)
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	next, err := again.AddTemplate(Template{Name: "third", Arch: "x86_64", Machine: "pc", BIOS: "seabios", Console: "serial"})
	if err != nil {
		t.Fatal(err)
	}
	if next != second {
		t.Fatalf("number after reopen %d, want %d (largest in use plus one)", next, second)
	}
}

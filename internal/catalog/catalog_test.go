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
	id, err := s.AddImage(Image{Name: dirname, Dirname: dirname, Kind: "image"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestImageDirnameIsUnique(t *testing.T) {
	s, _ := openTemp(t)
	baseImage(t, s, "disk")
	if _, err := s.AddImage(Image{Name: "x", Dirname: "disk", Kind: "image"}); err == nil {
		t.Fatal("вторая запись с той же папкой должна отклоняться")
	}
}

func TestSnapshotLayerNeedsExistingParent(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.AddImage(Image{Name: "s", Dirname: "s1", Kind: "snapshot"}); err == nil {
		t.Fatal("слой без родителя должен отклоняться")
	}
	base := baseImage(t, s, "base")
	if _, err := s.AddImage(Image{Name: "s", Dirname: "s2", Kind: "snapshot", ParentID: base}); err != nil {
		t.Fatalf("корректный слой: %v", err)
	}
	if _, err := s.AddImage(Image{Name: "x", Dirname: "x1", Kind: "image", ParentID: base}); err == nil {
		t.Fatal("образ с родителем, но не слой, должен отклоняться")
	}
}

func TestImageWithLayersCannotBeDeleted(t *testing.T) {
	s, _ := openTemp(t)
	base := baseImage(t, s, "base")
	if _, err := s.AddImage(Image{Name: "s", Dirname: "s2", Kind: "snapshot", ParentID: base}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteImage(base); err != ErrInUse {
		t.Fatalf("образ со слоями должен защищаться, получено %v", err)
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
		t.Fatalf("ссылка шаблона на удалённый образ должна сброситься: %d", got.ImageID)
	}
}

func TestTemplateValuesAreChecked(t *testing.T) {
	s, _ := openTemp(t)
	base := Template{Name: "t", Arch: "x86_64", Machine: "pc", BIOS: "seabios", Console: "serial"}
	bad := base
	bad.BIOS = "coreboot"
	if _, err := s.AddTemplate(bad); err == nil {
		t.Fatal("неизвестный BIOS должен отклоняться")
	}
	bad = base
	bad.Console = "telnet"
	if _, err := s.AddTemplate(bad); err == nil {
		t.Fatal("неизвестная консоль должна отклоняться")
	}
	bad = base
	bad.CustomIcon = "ZZ"
	if _, err := s.AddTemplate(bad); err == nil {
		t.Fatal("некорректный хеш иконки должен отклоняться")
	}
	bad = base
	bad.ImageID = 999
	if _, err := s.AddTemplate(bad); err == nil {
		t.Fatal("несуществующий образ должен отклоняться")
	}
	if _, err := s.AddTemplate(base); err != nil {
		t.Fatalf("корректный шаблон: %v", err)
	}
	if _, err := s.AddTemplate(base); err == nil {
		t.Fatal("имя шаблона должно быть уникальным")
	}
}

func TestSnapshotsDropWithTheirDevice(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.AddSnapshot(10, "before", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddSnapshot(10, "after", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddSnapshot(20, "other", 3); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddSnapshot(10, "before", 4); err == nil {
		t.Fatal("имя снапшота уникально в пределах машины")
	}
	if err := s.DeleteSnapshotsOf(10); err != nil {
		t.Fatal(err)
	}
	if n := len(s.SnapshotsOf(10)); n != 0 {
		t.Fatalf("снапшоты удалённой машины остались: %d", n)
	}
	if n := len(s.SnapshotsOf(20)); n != 1 {
		t.Fatalf("снапшоты другой машины пропали: %d", n)
	}
}

func TestIDsAreNeverReusedAndSurviveReopen(t *testing.T) {
	s, dir := openTemp(t)
	first := baseImage(t, s, "a")
	if err := s.DeleteImage(first); err != nil {
		t.Fatal(err)
	}
	second := baseImage(t, s, "b")
	if second <= first {
		t.Fatalf("номер переиспользован: %d после %d", second, first)
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	third := baseImage(t, again, "c")
	if third <= second {
		t.Fatalf("после перезапуска номер переиспользован: %d", third)
	}
	if _, ok := again.Image(second); !ok {
		t.Fatal("запись пропала после перезапуска")
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
		t.Fatal("ошибка fn должна вернуться")
	}
	if im, _ := s.Image(id); im.Name != "keep" {
		t.Fatalf("ошибка не откатила изменение в памяти: %q", im.Name)
	}
	again, _ := Open(dir)
	if im, _ := again.Image(id); im.Name != "keep" {
		t.Fatalf("ошибка не откатила изменение в файле: %q", im.Name)
	}
}

// Each entry lives in a file named after it, the counter is in index.yaml.
func TestEachRecordHasItsOwnFile(t *testing.T) {
	s, dir := openTemp(t)
	baseImage(t, s, "disk")
	if _, err := os.Stat(filepath.Join(dir, "images", "disk.yaml")); err != nil {
		t.Fatalf("файл записи не создан: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "images", "index.yaml")); err != nil {
		t.Fatalf("счётчик не записан: %v", err)
	}
	if _, err := s.AddTemplate(Template{Name: "t1", Arch: "x86_64", Machine: "pc", BIOS: "seabios", Console: "serial"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "templates"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 { // index.yaml and the template file
		t.Fatalf("в каталоге шаблонов %d файлов, нужно 2", len(entries))
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
		t.Fatal("файл удалённой записи остался")
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Image(keep); !ok {
		t.Fatal("соседняя запись пропала")
	}
	if _, ok := again.Image(gone); ok {
		t.Fatal("удалённая запись вернулась после перезапуска")
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
		t.Fatalf("файл по имени не создан: %v", err)
	}
	if err := s.UpdateTemplate(id, func(tp *Template) error { tp.Name = "debian"; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "templates", "ubuntu.yaml")); !os.IsNotExist(err) {
		t.Fatal("старый файл после переименования остался")
	}
	if _, err := os.Stat(filepath.Join(dir, "templates", "debian.yaml")); err != nil {
		t.Fatalf("новый файл не создан: %v", err)
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if tp, ok := again.Template(id); !ok || tp.Name != "debian" {
		t.Fatalf("после перезапуска шаблон %v", tp)
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
		t.Fatalf("шаблон a: %q", tp.Name)
	}
	if tp, _ := again.Template(b); tp.Name != "alpha" {
		t.Fatalf("шаблон b: %q", tp.Name)
	}
}

// Names are Latin only: Cyrillic and spaces are rejected.
func TestNamesMustBeLatin(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.AddTemplate(Template{Name: "тест", Arch: "x86_64", Machine: "pc", BIOS: "seabios", Console: "serial"}); err == nil {
		t.Fatal("кириллическое имя шаблона принято")
	}
	if _, err := s.AddTemplate(Template{Name: "my tpl", Arch: "x86_64", Machine: "pc", BIOS: "seabios", Console: "serial"}); err == nil {
		t.Fatal("имя с пробелом принято")
	}
	if _, err := s.AddImage(Image{Name: "диск", Dirname: "disk", Kind: "image"}); err == nil {
		t.Fatal("кириллическое имя образа принято")
	}
	if _, err := s.AddSnapshot(1, "до", 1); err == nil {
		t.Fatal("кириллическое имя снапшота принято")
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
		t.Fatal("файл с чужим именем загружен")
	}
}

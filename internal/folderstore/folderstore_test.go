// SPDX-License-Identifier: GPL-3.0-only

package folderstore

import (
	"path/filepath"
	"reflect"
	"testing"
)

func ptr(v int64) *int64 { return &v }

func TestTreeGrantsAndPlacements(t *testing.T) {
	path := filepath.Join(t.TempDir(), "folders.yaml")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := s.Create(nil, "Учебные", 1)
	child, _ := s.Create(ptr(root.ID), "Группа А", 1)
	leaf, _ := s.Create(ptr(child.ID), "Лето", 1)
	other, _ := s.Create(nil, "Прочее", 1)

	if got := s.Chain(leaf.ID); !reflect.DeepEqual(got, []int64{leaf.ID, child.ID, root.ID}) {
		t.Fatalf("путь к корню: %v", got)
	}
	if h := s.Height(root.ID); h != 3 {
		t.Fatalf("высота: %d", h)
	}

	// Assigning the role opens the subtree; ancestors of the assigned folder are visible to show the path.
	if err := s.SetRoleFolders(7, []int64{child.ID}); err != nil {
		t.Fatal(err)
	}
	vis := s.VisibleFolders(1, []int64{7})
	if !vis[child.ID] || !vis[leaf.ID] || vis[root.ID] || vis[other.ID] {
		t.Fatalf("видимость через роль: %v", vis)
	}
	if path := s.WithAncestors(vis); !path[root.ID] {
		t.Fatalf("предки должны входить в путь: %v", path)
	}

	// Placements: a lab in a leaf, moving when a subtree is deleted.
	if err := s.Place(100, leaf.ID); err != nil {
		t.Fatal(err)
	}
	labs, moved, err := s.Remove(child.ID, other.ID)
	if err != nil || labs != 1 || moved != 1 {
		t.Fatalf("удаление с переносом: labs=%d moved=%d err=%v", labs, moved, err)
	}
	if f, ok := s.Placement(100); !ok || f != other.ID {
		t.Fatalf("лаборатория должна перейти в «Прочее»: %d %v", f, ok)
	}
	if _, ok := s.Folder(leaf.ID); ok {
		t.Fatal("вложенная папка должна удалиться")
	}
	if vis := s.VisibleFolders(1, []int64{7}); len(vis) != 0 {
		t.Fatalf("назначение удалённой папки должно исчезнуть: %v", vis)
	}

	// Everything is saved to the file and read back.
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Folders(); len(got) != 2 || got[0].Name != "Прочее" {
		t.Fatalf("после перезагрузки: %+v", got)
	}
	if f, ok := s2.Placement(100); !ok || f != other.ID {
		t.Fatal("размещение не сохранилось")
	}
}

func TestUserGrantsAndLabRemoval(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "folders.yaml"))
	a, _ := s.Create(nil, "A", 1)
	if err := s.GrantUserFolder(a.ID, 5, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.GrantUserFolder(a.ID, 5, 1); err != nil {
		t.Fatal(err)
	}
	if got := s.UserFolderIDs(5); !reflect.DeepEqual(got, []int64{a.ID}) {
		t.Fatalf("повторное назначение не должно дублироваться: %v", got)
	}
	_ = s.Place(9, a.ID)
	_ = s.RemoveLab(9)
	if _, ok := s.Placement(9); ok {
		t.Fatal("после удаления лаборатории размещения быть не должно")
	}
	_ = s.RemoveUser(5)
	if got := s.UserFolderIDs(5); len(got) != 0 {
		t.Fatalf("назначения пользователя должны удалиться: %v", got)
	}
}

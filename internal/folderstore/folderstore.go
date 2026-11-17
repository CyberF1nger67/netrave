// SPDX-License-Identifier: GPL-3.0-only

// Package folderstore - shared lab folders, folder assignments to users and roles, and lab
// placement in folders. Everything is stored in one file <labs dir>/folders.yaml and read into memory.
//
// Folder ids are assigned in increasing order and are not reused. A lab without a placement record
// is "No folder". Folder permissions also apply to its subfolders.
package folderstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"gopkg.in/yaml.v3"
)

// Folder - a tree folder. ParentID 0 - root.
type Folder struct {
	ID        int64  `yaml:"id"`
	ParentID  int64  `yaml:"parent_id,omitempty"`
	Name      string `yaml:"name"`
	CreatedAt int64  `yaml:"created_at"`
	UpdatedAt int64  `yaml:"updated_at"`
}

// Grant - a folder assignment to a user (UserID) or a role (RoleID); exactly one of the two.
type Grant struct {
	FolderID  int64 `yaml:"folder_id"`
	UserID    int64 `yaml:"user_id,omitempty"`
	RoleID    int64 `yaml:"role_id,omitempty"`
	GrantedBy int64 `yaml:"granted_by,omitempty"`
}

// Placement - a lab in a folder.
type Placement struct {
	LabID    int64 `yaml:"lab_id"`
	FolderID int64 `yaml:"folder_id"`
}

type file struct {
	NextID     int64       `yaml:"next_id"`
	Folders    []Folder    `yaml:"folders"`
	Grants     []Grant     `yaml:"grants"`
	Placements []Placement `yaml:"placements"`
}

// Store - the contents of folders.yaml in memory. All methods are safe for concurrent use.
type Store struct {
	mu   sync.Mutex
	path string
	f    file
}

// Memory returns an empty store without a file: for databases without a labs directory. Changes are not persisted.
func Memory() *Store { return &Store{} }

// Open reads the folders file. No file - an empty tree.
func Open(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, &s.f); err != nil {
		return nil, fmt.Errorf("папки: %w", err)
	}
	return s, nil
}

func (f file) clone() file {
	return file{
		NextID:     f.NextID,
		Folders:    append([]Folder(nil), f.Folders...),
		Grants:     append([]Grant(nil), f.Grants...),
		Placements: append([]Placement(nil), f.Placements...),
	}
}

// mutate applies a change to a copy, writes the file and only then changes memory.
func (s *Store) mutate(fn func(*file) error) error {
	if s.path == "" {
		return errors.New("файл папок не задан")
	}
	next := s.f.clone()
	if err := fn(&next); err != nil {
		return err
	}
	data, err := yaml.Marshal(next)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	s.f = next
	return nil
}

func (s *Store) folder(id int64) (Folder, int, bool) {
	for i, f := range s.f.Folders {
		if f.ID == id {
			return f, i, true
		}
	}
	return Folder{}, -1, false
}

// Folders returns all folders by name (case-insensitive), then by number.
func (s *Store) Folders() []Folder {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Folder(nil), s.f.Folders...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := asciiLower(out[i].Name), asciiLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// asciiLower lowercases only Latin letters - like SQLite's NOCASE comparison.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// Folder returns a folder by number.
func (s *Store) Folder(id int64) (Folder, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, _, ok := s.folder(id)
	return f, ok
}

// Count - the number of folders.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.f.Folders)
}

// children returns a map "parent -> children". Call under s.mu.
func (s *Store) children() map[int64][]int64 {
	m := map[int64][]int64{}
	for _, f := range s.f.Folders {
		m[f.ParentID] = append(m[f.ParentID], f.ID)
	}
	return m
}

// Subtree returns the folder and all its subfolders.
func (s *Store) Subtree(id int64) []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.subtree(id)
}

func (s *Store) subtree(id int64) []int64 {
	ch := s.children()
	out := []int64{}
	stack := []int64{id}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		out = append(out, cur)
		stack = append(stack, ch[cur]...)
	}
	return out
}

// Chain returns the path from the folder to the root: the folder itself first, then its ancestors. Empty - no folder.
func (s *Store) Chain(id int64) []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []int64
	for cur := id; cur != 0; {
		f, _, ok := s.folder(cur)
		if !ok {
			return out
		}
		out = append(out, cur)
		cur = f.ParentID
		if len(out) > len(s.f.Folders) {
			break // protection against cycles in the file
		}
	}
	return out
}

// Height returns the height of the folder's subtree (1 - no nested folders).
func (s *Store) Height(id int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.height(id, s.children())
}

func (s *Store) height(id int64, ch map[int64][]int64) int {
	best := 0
	for _, c := range ch[id] {
		if h := s.height(c, ch); h > best {
			best = h
		}
	}
	return best + 1
}

// Create creates a folder. parent nil - at the root.
func (s *Store) Create(parent *int64, name string, now int64) (Folder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out Folder
	err := s.mutate(func(f *file) error {
		f.NextID++
		out = Folder{ID: f.NextID, Name: name, CreatedAt: now, UpdatedAt: now}
		if parent != nil {
			out.ParentID = *parent
		}
		f.Folders = append(f.Folders, out)
		return nil
	})
	return out, err
}

// Update changes the folder's name and parent. parent nil - to the root.
func (s *Store) Update(id int64, name string, parent *int64, now int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutate(func(f *file) error {
		for i := range f.Folders {
			if f.Folders[i].ID == id {
				f.Folders[i].Name = name
				f.Folders[i].ParentID = 0
				if parent != nil {
					f.Folders[i].ParentID = *parent
				}
				f.Folders[i].UpdatedAt = now
				return nil
			}
		}
		return ErrNotFound
	})
}

// ErrNotFound - no such folder.
var ErrNotFound = errors.New("папка не найдена")

// Remove deletes the folder together with its subfolders and their assignments. Labs from the deleted subtree
// are moved to the target folder (0 - "No folder"). Returns the number of labs in the subtree and the number moved.
func (s *Store) Remove(id, target int64) (labs, moved int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inside := map[int64]bool{}
	for _, v := range s.subtree(id) {
		inside[v] = true
	}
	err = s.mutate(func(f *file) error {
		var kept []Folder
		for _, fo := range f.Folders {
			if !inside[fo.ID] {
				kept = append(kept, fo)
			}
		}
		f.Folders = kept
		var grants []Grant
		for _, g := range f.Grants {
			if !inside[g.FolderID] {
				grants = append(grants, g)
			}
		}
		f.Grants = grants
		var placements []Placement
		for _, p := range f.Placements {
			if !inside[p.FolderID] {
				placements = append(placements, p)
				continue
			}
			labs++
			if target != 0 {
				p.FolderID = target
				placements = append(placements, p)
				moved++
			}
		}
		f.Placements = placements
		return nil
	})
	return labs, moved, err
}

// VisibleFolders returns folders assigned to the user directly or through roles, and all their subfolders.
func (s *Store) VisibleFolders(uid int64, roles []int64) map[int64]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	roleSet := map[int64]bool{}
	for _, r := range roles {
		roleSet[r] = true
	}
	ch := s.children()
	out := map[int64]bool{}
	for _, g := range s.f.Grants {
		if g.UserID == uid || (g.RoleID != 0 && roleSet[g.RoleID]) {
			for _, v := range s.subtreeWith(g.FolderID, ch) {
				out[v] = true
			}
		}
	}
	return out
}

func (s *Store) subtreeWith(id int64, ch map[int64][]int64) []int64 {
	out := []int64{}
	stack := []int64{id}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		out = append(out, cur)
		stack = append(stack, ch[cur]...)
	}
	return out
}

// WithAncestors adds all ancestors of the folders to the set (to show the path to assigned folders).
func (s *Store) WithAncestors(set map[int64]bool) map[int64]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[int64]bool{}
	for id := range set {
		for cur := id; cur != 0; {
			if out[cur] {
				break
			}
			f, _, ok := s.folder(cur)
			if !ok {
				break
			}
			out[cur] = true
			cur = f.ParentID
		}
	}
	return out
}

// UserFolderIDs returns folders assigned directly to the user (without subfolders), in ascending order.
func (s *Store) UserFolderIDs(uid int64) []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []int64{}
	for _, g := range s.f.Grants {
		if g.UserID == uid {
			out = append(out, g.FolderID)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// RoleFolderIDs returns folders assigned to the role, in ascending order.
func (s *Store) RoleFolderIDs(roleID int64) []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []int64{}
	for _, g := range s.f.Grants {
		if g.RoleID == roleID {
			out = append(out, g.FolderID)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// SetUserFolders replaces the user's assignments. An empty list revokes everything.
func (s *Store) SetUserFolders(uid int64, folders []int64, grantedBy int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutate(func(f *file) error {
		var kept []Grant
		for _, g := range f.Grants {
			if g.UserID != uid {
				kept = append(kept, g)
			}
		}
		for _, fid := range folders {
			kept = append(kept, Grant{FolderID: fid, UserID: uid, GrantedBy: grantedBy})
		}
		f.Grants = kept
		return nil
	})
}

// GrantUserFolder assigns a folder to the user; assigning again changes nothing.
func (s *Store) GrantUserFolder(folder, uid, grantedBy int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutate(func(f *file) error {
		for _, g := range f.Grants {
			if g.FolderID == folder && g.UserID == uid {
				return nil
			}
		}
		f.Grants = append(f.Grants, Grant{FolderID: folder, UserID: uid, GrantedBy: grantedBy})
		return nil
	})
}

// SetRoleFolders replaces the role's assignments.
func (s *Store) SetRoleFolders(roleID int64, folders []int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutate(func(f *file) error {
		var kept []Grant
		for _, g := range f.Grants {
			if g.RoleID != roleID {
				kept = append(kept, g)
			}
		}
		seen := map[int64]bool{}
		for _, fid := range folders {
			if seen[fid] {
				continue
			}
			seen[fid] = true
			kept = append(kept, Grant{FolderID: fid, RoleID: roleID})
		}
		f.Grants = kept
		return nil
	})
}

// RemoveUser deletes all of the user's assignments (when a user is deleted).
func (s *Store) RemoveUser(uid int64) error { return s.SetUserFolders(uid, nil, 0) }

// RemoveRole deletes all of the role's assignments (when a role is deleted).
func (s *Store) RemoveRole(roleID int64) error { return s.SetRoleFolders(roleID, nil) }

// Placement returns the lab's folder; false - the lab is "No folder".
func (s *Store) Placement(lab int64) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.f.Placements {
		if p.LabID == lab {
			return p.FolderID, true
		}
	}
	return 0, false
}

// Placements returns a map "lab -> folder" for all placed labs.
func (s *Store) Placements() map[int64]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[int64]int64, len(s.f.Placements))
	for _, p := range s.f.Placements {
		out[p.LabID] = p.FolderID
	}
	return out
}

// Place puts a lab into a folder; folder 0 - "No folder".
func (s *Store) Place(lab, folder int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutate(func(f *file) error {
		var kept []Placement
		for _, p := range f.Placements {
			if p.LabID != lab {
				kept = append(kept, p)
			}
		}
		if folder != 0 {
			kept = append(kept, Placement{LabID: lab, FolderID: folder})
		}
		f.Placements = kept
		return nil
	})
}

// RemoveLab deletes the lab's placement (when it is deleted).
func (s *Store) RemoveLab(lab int64) error { return s.Place(lab, 0) }

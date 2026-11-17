// SPDX-License-Identifier: GPL-3.0-only

// Package icons stores user machine icons.
//
// An uploaded file is validated and rebuilt from scratch (see Clean): raster formats
// (PNG, JPEG, WEBP) are decoded and saved as PNG - only pixels and the
// alpha channel remain; SVG is validated and stripped of scripts, external links and dangerous
// elements, and then saved as SVG without rasterization. The file on disk
// is named after the SHA-256 of the cleaned content (<hash>.png or <hash>.svg), so the
// user's name is never used and path traversal is impossible. A machine references the
// icon via devices.custom_icon, the user's library via icon_library.hash;
// identical images are stored once. The file is deleted when neither a machine
// nor a library references it.
package icons

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg" // JPEG icons are decoded and rebuilt as PNG
	"image/png"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "golang.org/x/image/webp" // WEBP icons are decoded and rebuilt as PNG
	"gopkg.in/yaml.v3"

	"netrave/internal/database"
)

const (
	// MaxBytes is the file size limit (also after rebuilding).
	MaxBytes = 512 << 10
	// MinSide and MaxSide are the allowed sizes of each side in pixels.
	MinSide = 16
	MaxSide = 512
)

// ErrNotFound - no icon with this hash exists in the catalog.
var ErrNotFound = errors.New("иконка не найдена")

var (
	hashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)
	pngSig = []byte("\x89PNG\r\n\x1a\n")
	tmpRe  = regexp.MustCompile(`^\.upload-[0-9]+\.tmp$`)
)

// ValidHash checks that the string is a lowercase SHA-256 (the only allowed form of a file name).
func ValidHash(s string) bool { return hashRe.MatchString(s) }

// Info describes an accepted icon. Format is "png" or "svg" (raster formats
// are normalized to PNG, SVG is kept as a vector).
type Info struct {
	Hash   string `json:"hash"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Size   int    `json:"size_bytes"`
	Alpha  bool   `json:"has_alpha"`
	Format string `json:"format"`
}

// Ext returns the icon file extension without the dot.
func (i Info) Ext() string {
	if i.Format == "svg" {
		return "svg"
	}
	return "png"
}

// FileName is the icon file name in the icons directory.
func (i Info) FileName() string { return i.Hash + "." + i.Ext() }

// Clean validates the uploaded bytes and returns a safe copy together with its description.
// PNG, JPEG and WEBP are decoded and rebuilt as PNG (transparency is preserved).
// SVG is validated and cleaned (see cleanSVG) and remains a vector.
func Clean(data []byte) ([]byte, Info, error) {
	if len(data) == 0 {
		return nil, Info{}, errors.New("файл пуст")
	}
	if len(data) > MaxBytes {
		return nil, Info{}, fmt.Errorf("файл больше %d КБ", MaxBytes>>10)
	}
	if looksLikeSVG(data) {
		return cleanSVG(data)
	}
	if !isRasterSignature(data) {
		return nil, Info{}, errors.New("поддерживаются PNG, JPEG, WEBP и SVG")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, Info{}, fmt.Errorf("повреждённый %s: %v", formatName(data), err)
	}
	// Sizes are checked before decoding: the decoded buffer is limited to MaxSide x MaxSide.
	if cfg.Width < MinSide || cfg.Height < MinSide || cfg.Width > MaxSide || cfg.Height > MaxSide {
		return nil, Info{}, fmt.Errorf("размер %d×%d: стороны от %d до %d пикселей", cfg.Width, cfg.Height, MinSide, MaxSide)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, Info{}, fmt.Errorf("повреждённый %s: %v", formatName(data), err)
	}
	b := img.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)

	alpha := false
	for i := 3; i < len(dst.Pix); i += 4 {
		if dst.Pix[i] != 0xff {
			alpha = true
			break
		}
	}

	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, dst); err != nil {
		return nil, Info{}, fmt.Errorf("пересборка PNG: %v", err)
	}
	if buf.Len() > MaxBytes {
		return nil, Info{}, fmt.Errorf("файл больше %d КБ после обработки", MaxBytes>>10)
	}
	out := buf.Bytes()
	sum := sha256.Sum256(out)
	return out, Info{
		Hash:   hex.EncodeToString(sum[:]),
		Width:  b.Dx(),
		Height: b.Dy(),
		Size:   len(out),
		Alpha:  alpha,
		Format: "png",
	}, nil
}

// isRasterSignature recognizes only supported raster formats.
func isRasterSignature(data []byte) bool {
	if bytes.HasPrefix(data, pngSig) {
		return true
	}
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return true
	}
	return len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
}

func formatName(data []byte) string {
	switch {
	case bytes.HasPrefix(data, pngSig):
		return "PNG"
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8:
		return "JPEG"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "WEBP"
	default:
		return "файл"
	}
}

// SupportedContentType reports whether the Content-Type is a supported image type.
// An empty type is allowed: the format is still checked by content.
func SupportedContentType(value string) bool {
	if strings.TrimSpace(value) == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}
	switch strings.ToLower(mediaType) {
	case "image/png", "image/jpeg", "image/jpg", "image/webp", "image/svg+xml", "application/octet-stream":
		return true
	default:
		return false
	}
}

// ContentType returns the icon's MIME type from its file name.
func ContentType(file string) string {
	if strings.HasSuffix(strings.ToLower(file), ".svg") {
		return "image/svg+xml"
	}
	return "image/png"
}

// Store is the icon catalog. All methods are safe on a nil receiver (the catalog is not configured).
type Store struct {
	Dir string
}

// New creates a store in directory dir (the directory is created on the first save).
func New(dir string) *Store { return &Store{Dir: dir} }

func (s *Store) path(hash, ext string) string { return filepath.Join(s.Dir, hash+"."+ext) }

// locate finds the icon file by hash. Raster icons are stored as PNG, vector ones as SVG.
func (s *Store) locate(hash string) (path, ext string, ok bool) {
	if s == nil || s.Dir == "" || !ValidHash(hash) {
		return "", "", false
	}
	for _, candidate := range []string{"svg", "png"} {
		path := s.path(hash, candidate)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, candidate, true
		}
	}
	return "", "", false
}

// Save writes the cleaned icon under its hash. Saving the same picture again only updates the time.
func (s *Store) Save(clean []byte, hash string) error {
	if s == nil || s.Dir == "" {
		return errors.New("каталог иконок не настроен")
	}
	if !ValidHash(hash) {
		return errors.New("некорректный хеш иконки")
	}
	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		return err
	}
	ext := "png"
	if looksLikeSVG(clean) {
		ext = "svg"
	}
	final := s.path(hash, ext)
	if _, err := os.Stat(final); err == nil {
		return s.Touch(hash)
	}
	tmp, err := os.CreateTemp(s.Dir, ".upload-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // the file no longer exists after the rename - the error does not matter
	if _, err := tmp.Write(clean); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o640); err != nil {
		return err
	}
	return os.Rename(tmpName, final)
}

// Read returns the icon contents.
func (s *Store) Read(hash string) ([]byte, error) {
	path, _, ok := s.locate(hash)
	if !ok {
		return nil, ErrNotFound
	}
	return os.ReadFile(path)
}

// Exists reports whether an icon file with this hash exists.
func (s *Store) Exists(hash string) bool {
	_, _, ok := s.locate(hash)
	return ok
}

// ContentType returns the MIME type of the stored icon.
func (s *Store) ContentType(hash string) string {
	_, ext, ok := s.locate(hash)
	if !ok {
		return ""
	}
	return ContentType("." + ext)
}

// Touch updates the file time: an icon just attached to a machine will not be picked up by cleanup.
func (s *Store) Touch(hash string) error {
	path, _, ok := s.locate(hash)
	if !ok {
		return ErrNotFound
	}
	now := time.Now()
	return os.Chtimes(path, now, now)
}

// Remove deletes the icon file if no one references it anymore. No errors are returned: the file will still
// be deleted by the sweep (Sweep).
func (s *Store) Remove(db *database.DB, hash string) {
	if s == nil || !ValidHash(hash) {
		return
	}
	refs, err := s.ReferencedByLibrary()
	if err != nil || refs[hash] {
		return
	}
	if deviceIconUsed(db, hash) {
		return
	}
	if path, _, ok := s.locate(hash); ok {
		_ = os.Remove(path)
	}
}

// deviceIconUsed reports whether any machine in all labs uses the icon with this hash.
func deviceIconUsed(db *database.DB, hash string) bool {
	for _, d := range db.Labs.AllDevices() {
		if d.CustomIcon == hash {
			return true
		}
	}
	return false
}

// Sweep deletes icons that no machine or library entry references and that are older than
// minAge. This cleans up uploads the user did not finish saving to a machine, and icons of deleted machines.
func (s *Store) Sweep(db *database.DB, minAge time.Duration) error {
	if s == nil || s.Dir == "" {
		return nil
	}
	lib, err := s.ReferencedByLibrary()
	if err != nil {
		return err
	}
	used := lib
	for _, d := range db.Labs.AllDevices() {
		if d.CustomIcon != "" {
			used[d.CustomIcon] = true
		}
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		name := e.Name()
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) < minAge {
			continue
		}
		switch {
		case tmpRe.MatchString(name):
			_ = os.Remove(filepath.Join(s.Dir, name))
		case strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".svg"):
			ext := filepath.Ext(name)
			hash := strings.TrimSuffix(name, ext)
			if ValidHash(hash) && !used[hash] {
				_ = os.Remove(filepath.Join(s.Dir, name))
			}
		}
	}
	return nil
}

// URL returns the machine icon URL for the web interface. Empty if the machine uses the standard picture.
func URL(labID int64, icon, custom string) string {
	if icon != "custom" || !ValidHash(custom) {
		return ""
	}
	return fmt.Sprintf("/api/labs/%d/icons/%s", labID, custom)
}

// ---------- user icon libraries (library/<owner>.yaml files) ----------

// LibraryEntry - an icon in the owner's library. File is the image path relative to the icons directory.
type LibraryEntry struct {
	ID        int64  `yaml:"id"`
	Hash      string `yaml:"hash"`
	Name      string `yaml:"name"`
	Width     int    `yaml:"width"`
	Height    int    `yaml:"height"`
	SizeBytes int64  `yaml:"size_bytes"`
	HasAlpha  bool   `yaml:"has_alpha"`
	CreatedAt int64  `yaml:"created_at"`
	File      string `yaml:"file"`
}

// LibraryFile - the whole library file of one owner.
type LibraryFile struct {
	Items []LibraryEntry `yaml:"items"`
}

// LibraryDir - the library file directory: one YAML file per owner.
func (s *Store) LibraryDir() string { return filepath.Join(s.Dir, "library") }

func (s *Store) libraryPath(owner int64) string {
	return filepath.Join(s.LibraryDir(), strconv.FormatInt(owner, 10)+".yaml")
}

// ReadLibrary reads the owner's library; if its file is missing - an empty library.
func (s *Store) ReadLibrary(owner int64) (LibraryFile, error) {
	data, err := os.ReadFile(s.libraryPath(owner))
	if errors.Is(err, os.ErrNotExist) {
		return LibraryFile{}, nil
	}
	if err != nil {
		return LibraryFile{}, err
	}
	var f LibraryFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return LibraryFile{}, fmt.Errorf("библиотека %d: %w", owner, err)
	}
	return f, nil
}

// WriteLibrary writes the owner's library atomically.
func (s *Store) WriteLibrary(owner int64, f LibraryFile) error {
	if err := os.MkdirAll(s.LibraryDir(), 0o750); err != nil {
		return err
	}
	data, err := yaml.Marshal(f)
	if err != nil {
		return err
	}
	path := s.libraryPath(owner)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RemoveLibrary deletes the owner's library file (when a user is deleted). Images remain
// while machines reference them; unused ones are removed by Sweep.
func (s *Store) RemoveLibrary(owner int64) error {
	if s == nil || s.Dir == "" {
		return nil
	}
	err := os.Remove(s.libraryPath(owner))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// LibraryOwners returns the ids of owners that have a library file.
func (s *Store) LibraryOwners() ([]int64, error) {
	entries, err := os.ReadDir(s.LibraryDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []int64
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".yaml")
		if id, err := strconv.ParseInt(name, 10, 64); err == nil && e.Name() == name+".yaml" {
			out = append(out, id)
		}
	}
	return out, nil
}

// ReferencedByLibrary returns image hashes referenced by at least one library.
func (s *Store) ReferencedByLibrary() (map[string]bool, error) {
	out := map[string]bool{}
	if s == nil || s.Dir == "" {
		return out, nil
	}
	owners, err := s.LibraryOwners()
	if err != nil {
		return nil, err
	}
	for _, o := range owners {
		f, err := s.ReadLibrary(o)
		if err != nil {
			return nil, err
		}
		for _, e := range f.Items {
			out[e.Hash] = true
		}
	}
	return out, nil
}

// ReadFile reads an image by a library path (<hash>.png or <hash>.svg).
// Other paths are rejected.
func (s *Store) ReadFile(file string) ([]byte, error) {
	rawExt := filepath.Ext(file)
	ext := strings.ToLower(rawExt)
	hash := strings.TrimSuffix(file, rawExt)
	if (ext != ".png" && ext != ".svg") || !ValidHash(hash) || filepath.Base(file) != file {
		return nil, errors.New("некорректный путь к картинке")
	}
	return os.ReadFile(filepath.Join(s.Dir, file))
}

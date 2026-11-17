// SPDX-License-Identifier: GPL-3.0-only

package icons

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testPNG(t *testing.T, side int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			img.Set(x, y, color.NRGBA{R: 20, G: 120, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCleanNormalizesPNGAndJPEG(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.NRGBA{R: 200, G: 40, B: 20, A: 255})
		}
	}
	var jpegData bytes.Buffer
	if err := jpeg.Encode(&jpegData, img, nil); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"png": testPNG(t, 48), "jpeg": jpegData.Bytes()} {
		clean, info, err := Clean(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if info.Format != "png" || !bytes.HasPrefix(clean, pngSig) {
			t.Fatalf("%s должен сохраняться как PNG, получено %q", name, info.Format)
		}
		if info.Width == 0 || info.Height == 0 || info.FileName() != info.Hash+".png" {
			t.Fatalf("%s: описание %+v", name, info)
		}
	}
}

const validSVG = `<?xml version="1.0" encoding="UTF-8"?>
<!-- comment must be dropped -->
<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 64 48" width="64" height="48">
  <defs><linearGradient id="g"><stop offset="0" stop-color="#fff"/><stop offset="1" stop-color="#000"/></linearGradient></defs>
  <rect x="0" y="0" width="64" height="48" fill="#123456"/>
  <circle cx="32" cy="24" r="12" fill="url(#g)" stroke="#fff" stroke-width="2"/>
</svg>`

func TestCleanSVGKeepsVector(t *testing.T) {
	clean, info, err := Clean([]byte(validSVG))
	if err != nil {
		t.Fatal(err)
	}
	if info.Format != "svg" || info.Width != 64 || info.Height != 48 || !info.Alpha {
		t.Fatalf("описание SVG: %+v", info)
	}
	if info.FileName() != info.Hash+".svg" || !bytes.Contains(clean, []byte("xmlns=\"http://www.w3.org/2000/svg\"")) {
		t.Fatalf("очищенный SVG: %s", clean)
	}
	if bytes.Contains(clean, []byte("comment")) || bytes.Contains(clean, []byte("xlink")) {
		t.Fatalf("комментарии и лишние пространства имён должны исчезнуть: %s", clean)
	}
	// Re-cleaning an already cleaned file must produce the same hash.
	again, second, err := Clean(clean)
	if err != nil {
		t.Fatal(err)
	}
	if second.Hash != info.Hash || !bytes.Equal(again, clean) {
		t.Fatal("повторная очистка изменила файл")
	}
}

func TestCleanSVGRejectsUnsafeContent(t *testing.T) {
	cases := map[string]string{
		"script":         `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32"><script>alert(1)</script></svg>`,
		"event handler":  `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" onload="alert(1)"><rect width="32" height="32"/></svg>`,
		"external href":  `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32"><a href="https://example.com"><rect width="32" height="32"/></a></svg>`,
		"foreignObject":  `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32"><foreignObject><body xmlns="http://www.w3.org/1999/xhtml">x</body></foreignObject></svg>`,
		"external image": `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32"><image href="https://example.com/x.png" width="32" height="32"/></svg>`,
		"bad namespace":  `<svg xmlns="http://evil.example/svg" width="32" height="32"><rect width="32" height="32"/></svg>`,
	}
	for name, data := range cases {
		if _, _, err := Clean([]byte(data)); err == nil {
			t.Fatalf("%s: небезопасный SVG принят", name)
		}
	}
}

func TestCleanRejectsUnknownAndOversizedData(t *testing.T) {
	if _, _, err := Clean([]byte("not an image")); err == nil {
		t.Fatal("произвольные байты приняты")
	}
	if _, _, err := Clean(nil); err == nil {
		t.Fatal("пустой файл принят")
	}
	if _, _, err := Clean([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32"/></svg>`)); err == nil {
		t.Fatal("SVG без размеров принят")
	}
}

func TestStoreResolvesPNGAndSVG(t *testing.T) {
	st := New(filepath.Join(t.TempDir(), "icons"))
	pngData, pngInfo, err := Clean(testPNG(t, 32))
	if err != nil {
		t.Fatal(err)
	}
	svgData, svgInfo, err := Clean([]byte(validSVG))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		data []byte
		info Info
		ct   string
	}{{pngData, pngInfo, "image/png"}, {svgData, svgInfo, "image/svg+xml"}} {
		if err := st.Save(item.data, item.info.Hash); err != nil {
			t.Fatal(err)
		}
		if !st.Exists(item.info.Hash) || st.ContentType(item.info.Hash) != item.ct {
			t.Fatalf("иконка %s не найдена или неверный MIME", item.info.Hash)
		}
		data, err := st.Read(item.info.Hash)
		if err != nil || !bytes.Equal(data, item.data) {
			t.Fatalf("чтение %s: %v", item.info.Hash, err)
		}
		if err := st.Touch(item.info.Hash); err != nil {
			t.Fatalf("touch %s: %v", item.info.Hash, err)
		}
	}
}

func TestBundledDefaultSVGsAreSafe(t *testing.T) {
	dir := filepath.Join("..", "..", "frontend", "public", "img")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("каталог стандартных иконок недоступен: %v", err)
	}
	found := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".svg") {
			continue
		}
		found++
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		_, info, err := Clean(data)
		if err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if info.Format != "svg" || info.Width <= 0 || info.Height <= 0 {
			t.Fatalf("%s: %+v", entry.Name(), info)
		}
	}
	if found == 0 {
		t.Fatal("стандартные SVG-иконки не найдены")
	}
}

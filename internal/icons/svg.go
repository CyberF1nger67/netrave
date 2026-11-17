// SPDX-License-Identifier: GPL-3.0-only

package icons

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// SVG icons are cleaned against a whitelist: scripts, event handlers, external
// links, <foreignObject>, <image>, <use>, <style> and animations do not make it into
// the saved file. Geometry and gradients are preserved without rasterization.

var allowedSVGElements = map[string]bool{
	"svg": true, "g": true, "defs": true,
	"path": true, "rect": true, "circle": true, "ellipse": true, "line": true,
	"polyline": true, "polygon": true,
	"linearGradient": true, "radialGradient": true, "stop": true,
	"clipPath": true, "text": true, "tspan": true, "title": true, "desc": true,
}

var allowedSVGAttrs = map[string]bool{
	"id": true, "viewBox": true, "width": true, "height": true, "version": true,
	"preserveAspectRatio": true, "fill": true, "fill-opacity": true, "fill-rule": true,
	"stroke": true, "stroke-width": true, "stroke-opacity": true, "stroke-linecap": true,
	"stroke-linejoin": true, "stroke-dasharray": true, "stroke-dashoffset": true,
	"clip-path": true, "clip-rule": true, "opacity": true, "transform": true,
	"d": true, "points": true, "x": true, "y": true, "x1": true, "y1": true, "x2": true,
	"y2": true, "cx": true, "cy": true, "r": true, "rx": true, "ry": true, "fx": true,
	"fy": true, "fr": true, "pathLength": true, "offset": true, "stop-color": true,
	"stop-opacity": true, "gradientUnits": true, "gradientTransform": true,
	"spreadMethod": true, "clipPathUnits": true, "font-size": true, "font-family": true,
	"font-weight": true, "font-style": true, "text-anchor": true, "dominant-baseline": true,
	"letter-spacing": true, "word-spacing": true, "text-decoration": true, "vector-effect": true,
}

var (
	idValueRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.:-]{0,63}$`)
	svgNumberRe = regexp.MustCompile(`^-?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?$`)
	urlRefRe    = regexp.MustCompile(`url\(\s*['"]?#[A-Za-z_][A-Za-z0-9_.:-]*['"]?\s*\)`)
	svgSizeRe   = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)(?:px|pt|pc|mm|cm|in)?$`)
)

const maxSVGSide = 8192

func looksLikeSVG(data []byte) bool {
	clean := bytes.TrimSpace(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))
	lower := bytes.ToLower(clean[:min(len(clean), 512)])
	if bytes.HasPrefix(lower, []byte("<svg")) || bytes.HasPrefix(lower, []byte("<?xml")) {
		return bytes.Contains(lower, []byte("<svg")) || bytes.HasPrefix(lower, []byte("<svg"))
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type svgDims struct {
	width, height float64
}

// cleanSVG parses the SVG, keeps only allowed elements and attributes and
// returns compact XML with an explicit SVG namespace.
func cleanSVG(data []byte) ([]byte, Info, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	var out bytes.Buffer
	encoder := xml.NewEncoder(&out)
	var dims svgDims
	rootSeen := false
	open := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, Info{}, fmt.Errorf("повреждённый SVG: %v", err)
		}
		switch value := token.(type) {
		case xml.ProcInst:
			// The XML declaration is not needed; any other processing instructions are not allowed.
			if !rootSeen && strings.EqualFold(value.Target, "xml") {
				continue
			}
			return nil, Info{}, errors.New("SVG: недопустимая инструкция обработки")
		case xml.Directive:
			return nil, Info{}, errors.New("SVG: недопустимая директива")
		case xml.Comment:
			continue
		case xml.CharData:
			// Text inside shapes is inert but needed for <text>/<title>. Whitespace
			// between elements need not be preserved.
			if len(bytes.TrimSpace(value)) == 0 {
				continue
			}
			if err := encoder.EncodeToken(value); err != nil {
				return nil, Info{}, err
			}
		case xml.StartElement:
			local := value.Name.Local
			// <metadata> does not affect rendering (often a C2PA manifest with a large
			// base64 payload). Skip the whole subtree, including auxiliary
			// elements with their own namespaces.
			if rootSeen && local == "metadata" {
				if err := decoder.Skip(); err != nil {
					return nil, Info{}, fmt.Errorf("SVG: повреждённые метаданные: %v", err)
				}
				continue
			}
			allowed := allowedSVGElements[local]
			if !allowed {
				return nil, Info{}, fmt.Errorf("SVG: элемент <%s> не разрешён", local)
			}
			if !rootSeen {
				if local != "svg" {
					return nil, Info{}, errors.New("SVG: корневой элемент должен быть <svg>")
				}
				if value.Name.Space != "" && value.Name.Space != "http://www.w3.org/2000/svg" {
					return nil, Info{}, errors.New("SVG: неверное пространство имён")
				}
				rootSeen = true
				attrs, size, err := sanitizeSVGAttrs(value.Attr, true)
				if err != nil {
					return nil, Info{}, err
				}
				dims = size
				attrs = append([]xml.Attr{{Name: xml.Name{Local: "xmlns"}, Value: "http://www.w3.org/2000/svg"}}, attrs...)
				if err := encoder.EncodeToken(xml.StartElement{Name: xml.Name{Local: "svg"}, Attr: attrs}); err != nil {
					return nil, Info{}, err
				}
				open = 1
				continue
			}
			if value.Name.Space != "" && value.Name.Space != "http://www.w3.org/2000/svg" {
				return nil, Info{}, errors.New("SVG: неверное пространство имён")
			}
			attrs, _, err := sanitizeSVGAttrs(value.Attr, false)
			if err != nil {
				return nil, Info{}, err
			}
			if err := encoder.EncodeToken(xml.StartElement{Name: xml.Name{Local: local}, Attr: attrs}); err != nil {
				return nil, Info{}, err
			}
			open++
		case xml.EndElement:
			if !rootSeen {
				return nil, Info{}, errors.New("SVG: некорректный корневой элемент")
			}
			if err := encoder.EncodeToken(xml.EndElement{Name: xml.Name{Local: value.Name.Local}}); err != nil {
				return nil, Info{}, err
			}
			open--
			if open < 0 {
				return nil, Info{}, errors.New("SVG: лишний закрывающий элемент")
			}
		}
	}
	if !rootSeen || open != 0 {
		return nil, Info{}, errors.New("SVG: незавершённый документ")
	}
	if err := encoder.Flush(); err != nil {
		return nil, Info{}, err
	}
	if out.Len() > MaxBytes {
		return nil, Info{}, fmt.Errorf("файл больше %d КБ после обработки", MaxBytes>>10)
	}
	width, height := normalizeSVGDims(dims)
	if width < 1 || height < 1 {
		return nil, Info{}, errors.New("SVG: не удалось определить размеры (нужны width/height или viewBox)")
	}
	sum := sha256.Sum256(out.Bytes())
	return out.Bytes(), Info{
		Hash:   hex.EncodeToString(sum[:]),
		Width:  width,
		Height: height,
		Size:   out.Len(),
		Alpha:  true,
		Format: "svg",
	}, nil
}

func sanitizeSVGAttrs(attrs []xml.Attr, root bool) ([]xml.Attr, svgDims, error) {
	out := make([]xml.Attr, 0, len(attrs))
	var dims svgDims
	var width, height float64
	var haveW, haveH bool
	var viewBox []float64
	for _, attr := range attrs {
		// Namespace declarations are not carried over: the root will have exactly one xmlns.
		if attr.Name.Space == "xmlns" || attr.Name.Local == "xmlns" {
			continue
		}
		if attr.Name.Space != "" {
			return nil, dims, fmt.Errorf("SVG: атрибут %q не разрешён", attr.Name.Local)
		}
		local := attr.Name.Local
		if !allowedSVGAttrs[local] {
			return nil, dims, fmt.Errorf("SVG: атрибут %q не разрешён", local)
		}
		if local == "id" && !idValueRe.MatchString(attr.Value) {
			return nil, dims, fmt.Errorf("SVG: некорректный id %q", attr.Value)
		}
		if !safeSVGValue(attr.Value) {
			return nil, dims, fmt.Errorf("SVG: недопустимое значение атрибута %q", local)
		}
		out = append(out, xml.Attr{Name: xml.Name{Local: local}, Value: attr.Value})
		if !root {
			continue
		}
		switch local {
		case "width":
			if value, ok := parseSVGLength(attr.Value); ok {
				width, haveW = value, true
			}
		case "height":
			if value, ok := parseSVGLength(attr.Value); ok {
				height, haveH = value, true
			}
		case "viewBox":
			viewBox = parseViewBox(attr.Value)
		}
	}
	if root {
		if len(viewBox) == 4 && viewBox[2] > 0 && viewBox[3] > 0 {
			dims.width, dims.height = viewBox[2], viewBox[3]
		}
		if haveW && haveH {
			dims.width, dims.height = width, height
		} else if haveW && dims.width == 0 {
			dims.width, dims.height = width, width
		} else if haveH && dims.height == 0 {
			dims.width, dims.height = height, height
		}
	}
	return out, dims, nil
}

// safeSVGValue allows colors, numbers, paths and internal url(#id) references.
// External addresses, data: and javascript: are rejected.
func safeSVGValue(value string) bool {
	lower := strings.ToLower(value)
	if strings.ContainsAny(value, "\x00\r\n") {
		return false
	}
	for _, bad := range []string{"javascript:", "vbscript:", "data:", "http:", "https:", "file:", "blob:"} {
		if strings.Contains(lower, bad) {
			return false
		}
	}
	// All url(...) must reference an element inside this same SVG.
	for i := 0; i < len(lower); {
		index := strings.Index(lower[i:], "url(")
		if index < 0 {
			break
		}
		start := i + index
		end := strings.IndexByte(lower[start:], ')')
		if end < 0 {
			return false
		}
		segment := lower[start : start+end+1]
		if !urlRefRe.MatchString(segment) {
			return false
		}
		i = start + end + 1
	}
	return true
}

func parseSVGLength(value string) (float64, bool) {
	match := svgSizeRe.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return 0, false
	}
	number, err := strconv.ParseFloat(match[1], 64)
	if err != nil || number <= 0 || math.IsInf(number, 0) || math.IsNaN(number) {
		return 0, false
	}
	return number, true
}

func parseViewBox(value string) []float64 {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	if len(fields) != 4 {
		return nil
	}
	out := make([]float64, 4)
	for index, field := range fields {
		if !svgNumberRe.MatchString(field) {
			return nil
		}
		number, err := strconv.ParseFloat(field, 64)
		if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
			return nil
		}
		out[index] = number
	}
	return out
}

// normalizeSVGDims returns positive dimensions for the icon card. Excessively
// large coordinates are rejected so the browser does not allocate a huge buffer.
func normalizeSVGDims(dims svgDims) (int, int) {
	if dims.width <= 0 || dims.height <= 0 || dims.width > maxSVGSide || dims.height > maxSVGSide {
		return 0, 0
	}
	return int(math.Round(dims.width)), int(math.Round(dims.height))
}

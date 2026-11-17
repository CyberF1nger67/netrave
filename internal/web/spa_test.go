// SPDX-License-Identifier: GPL-3.0-only

package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func spaTestFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":      {Data: []byte("<div id=\"root\"></div>")},
		"console.html":    {Data: []byte("console")},
		"assets/app-1.js": {Data: []byte("script")},
		"img/device.png":  {Data: []byte("png")},
		"favicon.ico":     {Data: []byte("ico")},
	}
}

func spaRequest(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestSPAHandlerServesIndexForNestedRoutes(t *testing.T) {
	h := SPAHandler(spaTestFS())
	for _, target := range []string{"/", "/labs", "/labs/7/editor", "/labs/folders/3", "/settings"} {
		rec := spaRequest(t, h, target)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: код %d, ожидался 200", target, rec.Code)
		}
		body, _ := io.ReadAll(rec.Result().Body)
		if string(body) != "<div id=\"root\"></div>" {
			t.Fatalf("%s: отдан не index.html: %q", target, body)
		}
	}
}

func TestSPAHandlerServesStaticFiles(t *testing.T) {
	h := SPAHandler(spaTestFS())
	for target, want := range map[string]string{
		"/console.html":    "console",
		"/assets/app-1.js": "script",
		"/img/device.png":  "png",
		"/favicon.ico":     "ico",
	} {
		rec := spaRequest(t, h, target)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: код %d, ожидался 200", target, rec.Code)
		}
		body, _ := io.ReadAll(rec.Result().Body)
		if string(body) != want {
			t.Fatalf("%s: тело %q, ожидалось %q", target, body, want)
		}
	}
}

func TestSPAHandlerDoesNotHideMissingAssetsOrAPI(t *testing.T) {
	h := SPAHandler(spaTestFS())
	for _, target := range []string{"/assets/missing.js", "/api/unknown", "/ws/unknown", "/img/missing.png"} {
		if rec := spaRequest(t, h, target); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: код %d, ожидался 404", target, rec.Code)
		}
	}
}

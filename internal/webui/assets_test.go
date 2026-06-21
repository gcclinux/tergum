package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestIsFingerprinted(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		// Fingerprinted assets (hex hash).
		{"tailwind.abc123de.min.css", true},
		{"app.1a2b3c4d5e6f7890.js", true},
		{"styles.deadbeef.css", true},
		{"chunk.a1b2c3d4e5f6.min.js", true},

		// Fingerprinted assets (version string).
		{"htmx.2.0.0.min.js", true},
		{"alpine.3.14.1.min.js", true},

		// Non-fingerprinted assets.
		{"tailwind.min.css", false},
		{"htmx.min.js", false},
		{"style.css", false},
		{"app.js", false},
		{"favicon.ico", false},
		{"logo.svg", false},
		{"font.woff2", false},

		// Edge cases — short hex that shouldn't match (< 8 chars).
		{"app.abc123.js", false},
		{"file.dead.css", false},

		// Paths with directories.
		{"css/tailwind.abc123de.min.css", true},
		{"js/htmx.2.0.0.min.js", true},
		{"css/base.css", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := IsFingerprinted(tt.path)
			if got != tt.want {
				t.Errorf("IsFingerprinted(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// testFS creates an in-memory filesystem for testing.
func testFS() fs.FS {
	return fstest.MapFS{
		"style.css":                    {Data: []byte("body { color: red; }")},
		"tailwind.abcdef12.min.css":    {Data: []byte(".tw { display: flex; }")},
		"htmx.2.0.0.min.js":           {Data: []byte("var htmx = {};")},
		"app.js":                       {Data: []byte("console.log('hi');")},
		"index.html":                   {Data: []byte("<html><body>Hello</body></html>")},
		"subdir/nested.css":            {Data: []byte("p { margin: 0; }")},
		"subdir/chunk.a1b2c3d4.min.js": {Data: []byte("var x = 1;")},
	}
}

func TestAssetHandler_FingerprintedAsset(t *testing.T) {
	handler := NewAssetHandler(testFS())

	req := httptest.NewRequest(http.MethodGet, "/tailwind.abcdef12.min.css", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	cc := rr.Header().Get("Cache-Control")
	expected := "public, max-age=31536000, immutable"
	if cc != expected {
		t.Errorf("Cache-Control = %q, want %q", cc, expected)
	}
	ct := rr.Header().Get("Content-Type")
	if ct != "text/css; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/css; charset=utf-8", ct)
	}
}

func TestAssetHandler_NonFingerprintedAsset(t *testing.T) {
	handler := NewAssetHandler(testFS())

	req := httptest.NewRequest(http.MethodGet, "/style.css", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	cc := rr.Header().Get("Cache-Control")
	expected := "public, max-age=3600"
	if cc != expected {
		t.Errorf("Cache-Control = %q, want %q", cc, expected)
	}
}

func TestAssetHandler_HTMLAsset(t *testing.T) {
	handler := NewAssetHandler(testFS())

	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	cc := rr.Header().Get("Cache-Control")
	expected := "no-cache"
	if cc != expected {
		t.Errorf("Cache-Control = %q, want %q", cc, expected)
	}
}

func TestAssetHandler_MissingAsset(t *testing.T) {
	handler := NewAssetHandler(testFS())

	req := httptest.NewRequest(http.MethodGet, "/nonexistent.js", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestAssetHandler_NestedPath(t *testing.T) {
	handler := NewAssetHandler(testFS())

	req := httptest.NewRequest(http.MethodGet, "/subdir/nested.css", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	cc := rr.Header().Get("Cache-Control")
	expected := "public, max-age=3600"
	if cc != expected {
		t.Errorf("Cache-Control = %q, want %q", cc, expected)
	}
}

func TestAssetHandler_NestedFingerprintedPath(t *testing.T) {
	handler := NewAssetHandler(testFS())

	req := httptest.NewRequest(http.MethodGet, "/subdir/chunk.a1b2c3d4.min.js", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	cc := rr.Header().Get("Cache-Control")
	expected := "public, max-age=31536000, immutable"
	if cc != expected {
		t.Errorf("Cache-Control = %q, want %q", cc, expected)
	}
}

func TestAssetHandler_VersionedAsset(t *testing.T) {
	handler := NewAssetHandler(testFS())

	req := httptest.NewRequest(http.MethodGet, "/htmx.2.0.0.min.js", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	cc := rr.Header().Get("Cache-Control")
	expected := "public, max-age=31536000, immutable"
	if cc != expected {
		t.Errorf("Cache-Control = %q, want %q", cc, expected)
	}
}

func TestAssetHandler_EmptyPath(t *testing.T) {
	handler := NewAssetHandler(testFS())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

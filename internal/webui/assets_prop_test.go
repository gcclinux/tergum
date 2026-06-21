package webui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"pgregory.net/rapid"
)

// Feature: webui-redesign, Property 11: Cache-Control header correctness

// **Validates: Requirements 10.2, 10.3, 10.4**

// TestProperty_CacheControlHeaderCorrectness verifies that for any asset URL served
// by the AssetHandler, the Cache-Control header is correctly set based on the asset
// type: HTML files get "no-cache", fingerprinted assets get "public, max-age=31536000, immutable",
// and non-fingerprinted assets get "public, max-age=3600".
func TestProperty_CacheControlHeaderCorrectness(t *testing.T) {
	// Generators for filename parts.
	baseName := rapid.StringMatching(`[a-z]{3,10}`)
	hexHash := rapid.StringMatching(`[a-f0-9]{8,16}`)
	nonHTMLExt := rapid.SampledFrom([]string{".js", ".css", ".svg", ".png", ".woff2", ".json", ".ico"})

	type assetCategory int
	const (
		categoryHTML assetCategory = iota
		categoryFingerprinted
		categoryNonFingerprinted
	)

	rapid.Check(t, func(rt *rapid.T) {
		// Pick which category to generate.
		category := assetCategory(rapid.IntRange(0, 2).Draw(rt, "category"))

		var filename string
		var expectedCacheControl string

		switch category {
		case categoryHTML:
			// HTML file: baseName.html
			name := baseName.Draw(rt, "htmlBase")
			filename = name + ".html"
			expectedCacheControl = "no-cache"

		case categoryFingerprinted:
			// Fingerprinted file: baseName.hexHash.ext
			name := baseName.Draw(rt, "fpBase")
			hash := hexHash.Draw(rt, "hash")
			ext := nonHTMLExt.Draw(rt, "fpExt")
			filename = fmt.Sprintf("%s.%s%s", name, hash, ext)
			expectedCacheControl = "public, max-age=31536000, immutable"

		case categoryNonFingerprinted:
			// Non-fingerprinted file: baseName.ext (no hash segment)
			name := baseName.Draw(rt, "nfpBase")
			ext := nonHTMLExt.Draw(rt, "nfpExt")
			filename = name + ext
			expectedCacheControl = "public, max-age=3600"
		}

		// Create an in-memory filesystem with the generated file.
		memFS := fstest.MapFS{
			filename: {Data: []byte("test content")},
		}

		// Create the handler and serve a request.
		handler := NewAssetHandler(memFS)
		req := httptest.NewRequest(http.MethodGet, "/"+filename, nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		// Verify the response is 200 OK.
		if rr.Code != http.StatusOK {
			rt.Fatalf("expected 200 for %q, got %d", filename, rr.Code)
		}

		// Verify Cache-Control header.
		got := rr.Header().Get("Cache-Control")
		if got != expectedCacheControl {
			rt.Fatalf("Cache-Control for %q (category %d): got %q, want %q",
				filename, category, got, expectedCacheControl)
		}
	})
}

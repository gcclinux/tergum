package webui

import (
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// fingerprintPattern matches a hex hash segment of 8+ characters, which indicates
// a content fingerprint in the filename (e.g., tailwind.abc123def456.min.css).
var fingerprintPattern = regexp.MustCompile(`[a-f0-9]{8,}`)

// versionPattern matches a semver-like version segment (e.g., 2.0.0 in htmx.2.0.0.min.js).
var versionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// AssetHandler serves embedded filesystem assets with appropriate Cache-Control headers
// based on whether the asset URL contains a content fingerprint.
type AssetHandler struct {
	fs fs.FS
}

// NewAssetHandler creates a new AssetHandler that serves files from the given filesystem.
func NewAssetHandler(fsys fs.FS) *AssetHandler {
	return &AssetHandler{fs: fsys}
}

// ServeHTTP implements http.Handler. It serves the requested file from the embedded
// filesystem with appropriate Cache-Control headers.
func (h *AssetHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Clean the path and remove leading slash.
	reqPath := path.Clean(r.URL.Path)
	reqPath = strings.TrimPrefix(reqPath, "/")

	// If path is empty, return 404.
	if reqPath == "" || reqPath == "." {
		http.NotFound(w, r)
		return
	}

	// Open the file from the embedded FS.
	f, err := h.fs.Open(reqPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	// Check that it's not a directory.
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		http.NotFound(w, r)
		return
	}

	// Determine Content-Type from the file extension.
	ext := filepath.Ext(reqPath)
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Set Content-Type header.
	w.Header().Set("Content-Type", contentType)

	// Set Cache-Control based on content type and fingerprint.
	if isHTMLContentType(contentType) {
		w.Header().Set("Cache-Control", "no-cache")
	} else if IsFingerprinted(reqPath) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}

	// Serve the file content.
	seeker, ok := f.(readSeekCloser)
	if ok {
		http.ServeContent(w, r, reqPath, stat.ModTime(), seeker)
	} else {
		// Fallback: read file and write directly.
		data, err := fs.ReadFile(h.fs, reqPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
		w.Write(data)
	}
}

// readSeekCloser combines io.ReadSeeker with io.Closer for http.ServeContent.
type readSeekCloser interface {
	Read(p []byte) (n int, err error)
	Seek(offset int64, whence int) (int64, error)
}

// IsFingerprinted returns true if the given path contains a content hash fingerprint
// or version number in its filename. It checks segments of the filename (split by '.'
// and '-') for hex hashes of 8+ characters or semver-like version strings.
func IsFingerprinted(filePath string) bool {
	// Get just the filename.
	name := path.Base(filePath)

	// Split filename into segments by '.' and '-'.
	segments := splitFilename(name)

	for _, seg := range segments {
		// Skip known extension segments.
		if isKnownExtension(seg) {
			continue
		}
		// Check if segment is a hex hash (8+ chars).
		if len(seg) >= 8 && fingerprintPattern.MatchString(seg) && isFullHexMatch(seg) {
			return true
		}
		// Check if segment is a version string (e.g., 2.0.0).
		if versionPattern.MatchString(seg) {
			return true
		}
	}

	// Also check for version patterns that span across dot-separated segments.
	// e.g., "htmx.2.0.0.min.js" — rejoin and look for version patterns.
	if containsVersionPattern(name) {
		return true
	}

	return false
}

// splitFilename splits a filename into segments by '.' and '-'.
func splitFilename(name string) []string {
	// First split by '-'.
	parts := strings.Split(name, "-")
	var segments []string
	for _, p := range parts {
		// Then split each part by '.'.
		dotParts := strings.Split(p, ".")
		segments = append(segments, dotParts...)
	}
	return segments
}

// isFullHexMatch returns true if the entire string is a hex hash.
func isFullHexMatch(s string) bool {
	return fingerprintPattern.FindString(s) == s
}

// isKnownExtension returns true for common file extension segments.
func isKnownExtension(seg string) bool {
	switch strings.ToLower(seg) {
	case "css", "js", "html", "htm", "json", "svg", "png", "jpg", "jpeg",
		"gif", "ico", "woff", "woff2", "ttf", "eot", "map", "min", "txt",
		"xml", "webp", "avif":
		return true
	}
	return false
}

// isHTMLContentType returns true if the content type indicates HTML.
func isHTMLContentType(contentType string) bool {
	return strings.HasPrefix(contentType, "text/html")
}

// containsVersionPattern checks if the filename contains a version like X.Y.Z
// by looking at consecutive numeric dot-separated segments.
func containsVersionPattern(name string) bool {
	// Remove the base name prefix (first segment before a dot) and extension.
	parts := strings.Split(name, ".")
	if len(parts) < 4 {
		return false
	}

	// Look for three consecutive numeric segments.
	for i := 0; i < len(parts)-2; i++ {
		if isNumeric(parts[i]) && isNumeric(parts[i+1]) && isNumeric(parts[i+2]) {
			return true
		}
	}
	return false
}

// isNumeric returns true if the string consists only of digits.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

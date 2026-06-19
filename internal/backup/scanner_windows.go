//go:build windows

package backup

import (
	"os"
	"syscall"
	"time"
)

// fillPlatformMetadata fills platform-specific metadata fields on Windows.
// Owner and group are left empty on Windows as they require different APIs.
func fillPlatformMetadata(sf *ScannedFile, path string, info os.FileInfo) {
	data := info.Sys()
	if data == nil {
		return
	}

	// Try to extract Windows file attribute data.
	if wfa, ok := data.(*syscall.Win32FileAttributeData); ok {
		// CreatedAt from CreationTime.
		ctime := time.Unix(0, wfa.CreationTime.Nanoseconds())
		sf.CreatedAt = &ctime

		// AccessedAt from LastAccessTime.
		atime := time.Unix(0, wfa.LastAccessTime.Nanoseconds())
		sf.AccessedAt = &atime
	}

	// Owner and Group are left empty on Windows.
}

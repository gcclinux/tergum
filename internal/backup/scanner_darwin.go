//go:build darwin

package backup

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
	"time"
)

// fillPlatformMetadata fills platform-specific metadata fields (owner, group, timestamps).
func fillPlatformMetadata(sf *ScannedFile, path string, info os.FileInfo) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return
	}

	// Owner and group from UID/GID.
	if u, err := user.LookupId(strconv.FormatUint(uint64(stat.Uid), 10)); err == nil {
		sf.Owner = u.Username
	}
	if g, err := user.LookupGroupId(strconv.FormatUint(uint64(stat.Gid), 10)); err == nil {
		sf.FileGroup = g.Name
	}

	// Access time. On Darwin, the field is Atimespec (not Atim).
	atime := time.Unix(stat.Atimespec.Sec, stat.Atimespec.Nsec)
	sf.AccessedAt = &atime

	// Create time (birth time). On Darwin, Birthtimespec is the actual birth time.
	ctime := time.Unix(stat.Birthtimespec.Sec, stat.Birthtimespec.Nsec)
	sf.CreatedAt = &ctime
}

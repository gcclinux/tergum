//go:build !windows

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

	// Access time.
	atime := time.Unix(stat.Atim.Sec, stat.Atim.Nsec)
	sf.AccessedAt = &atime

	// Create time (birth time) - use Ctim as a proxy on Linux.
	// On Linux, Ctim is the status change time, not birth time,
	// but it's the closest we can get without statx.
	ctime := time.Unix(stat.Ctim.Sec, stat.Ctim.Nsec)
	sf.CreatedAt = &ctime
}

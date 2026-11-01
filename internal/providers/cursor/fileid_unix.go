//go:build !windows

package cursor

import (
	"os"
	"syscall"
)

// fileIdentity returns the device and inode of a file, used to notice when a
// DB file is replaced rather than written in place.
func fileIdentity(_ string, fi os.FileInfo) (dev, ino uint64, ok bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return 0, 0, false
	}
	return uint64(st.Dev), uint64(st.Ino), true //nolint:unconvert // Dev and Ino widths differ per OS
}

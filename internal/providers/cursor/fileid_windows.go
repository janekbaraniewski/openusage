//go:build windows

package cursor

import "os"

// fileIdentity is not available from os.FileInfo on Windows. A replaced file
// is still caught by the size check and the rowid anchors.
func fileIdentity(os.FileInfo) (dev, ino uint64, ok bool) {
	return 0, 0, false
}

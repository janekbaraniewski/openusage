//go:build windows

package cursor

import (
	"os"

	"golang.org/x/sys/windows"
)

// fileIdentity returns the volume serial number and file index of a file, the
// Windows equivalent of device and inode, used to notice when a DB file is
// replaced rather than written in place.
func fileIdentity(path string, _ os.FileInfo) (dev, ino uint64, ok bool) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, false
	}
	// FILE_SHARE_* on all modes so Cursor's own handles are never blocked.
	h, err := windows.CreateFile(p, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return 0, 0, false
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return 0, 0, false
	}
	return uint64(info.VolumeSerialNumber), uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow), true
}

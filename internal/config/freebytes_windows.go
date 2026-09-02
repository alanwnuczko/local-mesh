//go:build windows

package config

import "golang.org/x/sys/windows"

// FreeBytes returns the number of bytes available to the calling user on the
// filesystem that contains path.
func FreeBytes(path string) (uint64, error) {
	var free, total, totalFree uint64
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	if err := windows.GetDiskFreeSpaceEx(ptr, &free, &total, &totalFree); err != nil {
		return 0, err
	}
	return free, nil
}

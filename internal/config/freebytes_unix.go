//go:build unix

package config

import "golang.org/x/sys/unix"

// FreeBytes returns the number of bytes available to the calling user on the
// filesystem that contains path.
func FreeBytes(path string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}

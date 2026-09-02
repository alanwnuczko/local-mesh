package config

import "fmt"

// diskMargin is reserved headroom so a receive that matches Size still leaves
// a little free space for the OS / rename / logs.
const diskMargin = 1 << 20 // 1 MiB

// EnsureSpace reports an error if path's filesystem cannot hold need bytes
// (plus a small margin). If free space cannot be queried, it returns nil so a
// receive is not blocked on exotic filesystems.
func EnsureSpace(path string, need int64) error {
	if need <= 0 {
		return nil
	}
	free, err := FreeBytes(path)
	if err != nil {
		return nil
	}
	want := uint64(need) + diskMargin
	if free < want {
		return fmt.Errorf("insufficient disk space: need %d bytes, have %d", want, free)
	}
	return nil
}

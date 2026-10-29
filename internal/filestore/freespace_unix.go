//go:build unix

package filestore

import "golang.org/x/sys/unix"

// freeSpace is how many bytes may still be written in dir's filesystem,
// by someone not root.
func freeSpace(dir string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil //nolint:unconvert // the fields' types differ by system
}

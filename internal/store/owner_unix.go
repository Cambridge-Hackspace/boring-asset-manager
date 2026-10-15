//go:build unix

package store

import (
	"os"
	"syscall"
)

// sameOwner gives path the owner and group of info's file. Only root may
// change them, so it is left alone when they already match.
func sameOwner(path string, info os.FileInfo) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	mine, err := os.Stat(path)
	if err != nil {
		return err
	}
	if cur, ok := mine.Sys().(*syscall.Stat_t); ok && cur.Uid == st.Uid && cur.Gid == st.Gid {
		return nil
	}
	return os.Chown(path, int(st.Uid), int(st.Gid))
}

//go:build unix

package store

import (
	"os"
	"path/filepath"
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

// OwnLike gives root, and everything below it, root's owner and group: for
// files a restore run as root made where the service keeps its own (README
// §2.3). A root that is not there is left alone.
func OwnLike(root string) error {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return filepath.Walk(root, func(path string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return sameOwner(path, info)
	})
}

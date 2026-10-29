//go:build !unix

package store

import "os"

// sameOwner does nothing where files have no Unix owner.
func sameOwner(string, os.FileInfo) error { return nil }

// OwnLike does nothing here: files have no owner to match.
func OwnLike(string) error { return nil }

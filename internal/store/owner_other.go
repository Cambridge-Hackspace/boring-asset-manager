//go:build !unix

package store

import "os"

// sameOwner does nothing where files have no Unix owner.
func sameOwner(string, os.FileInfo) error { return nil }

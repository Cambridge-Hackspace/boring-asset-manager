//go:build !unix && !windows

package filestore

// freeSpace is unknown here: -1, and staging is not refused for it.
func freeSpace(string) (int64, error) { return -1, nil }

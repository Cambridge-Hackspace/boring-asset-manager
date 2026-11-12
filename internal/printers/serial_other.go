//go:build !linux

package printers

import "os"

// openSerial opens a serial device for a conversation, read and write. Only
// on Linux is a terminal set raw; elsewhere, set it so first (stty raw).
func openSerial(name string) (*os.File, error) {
	return os.OpenFile(name, os.O_RDWR, 0)
}

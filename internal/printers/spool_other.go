//go:build !windows

package printers

import "errors"

// spoolerAvailable reports whether windows connections can be used.
const spoolerAvailable = false

func spool(printer string, job []byte) error {
	return errors.New("the Windows spooler is only available when BAM runs on Windows")
}

// Package serialport opens a serial port on the BAM server, such as a USB
// laser controller's /dev/ttyUSB0, raw, at a baud rate: what a device
// connection to a laser is (docs/printing.md#lasers). Label printers'
// device connections only write, and need none of this.
package serialport

import "slices"

// Bauds are the rates a port is opened at.
var Bauds = []int{9600, 19200, 38400, 57600, 115200, 230400, 460800, 921600}

// ValidBaud reports whether a rate is one of Bauds.
func ValidBaud(baud int) bool {
	return slices.Contains(Bauds, baud)
}

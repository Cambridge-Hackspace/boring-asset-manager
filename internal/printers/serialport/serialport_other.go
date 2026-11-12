//go:build !linux

package serialport

import (
	"errors"
	"os"
)

// Open refuses: BAM! opens serial ports on Linux only. Elsewhere a laser
// is reached over the network, or through a serial-to-network bridge such
// as ser2net.
func Open(string, int, bool) (*os.File, error) {
	return nil, errors.New("BAM! opens serial ports on Linux only; reach the laser over the network, or through a bridge such as ser2net")
}

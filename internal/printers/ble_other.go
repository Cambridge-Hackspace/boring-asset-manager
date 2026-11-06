//go:build !linux

package printers

// systemBLE is nil: BAM! reaches Bluetooth LE printers through BlueZ, so on
// Linux alone. Elsewhere, a relay on Linux reaches them.
var systemBLE BLEDialer

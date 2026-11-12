//go:build linux

package printers

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// openSerial opens a serial device for a conversation, read and write,
// without making it the server's terminal. A terminal, such as a USB or
// Bluetooth serial port, is set raw, as cfmakeraw(3) does, so that bytes
// pass through unchanged both ways: a newline is not sent as two bytes, nor
// is anything echoed or held back for a line.
func openSerial(name string) (*os.File, error) {
	f, err := os.OpenFile(name, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	conn, err := f.SyscallConn()
	if err != nil {
		f.Close()
		return nil, err
	}
	// Through the raw descriptor, since Fd would make it blocking and lose
	// the deadlines.
	cerr := conn.Control(func(fd uintptr) {
		var t *unix.Termios
		t, err = unix.IoctlGetTermios(int(fd), unix.TCGETS)
		if errors.Is(err, unix.ENOTTY) || errors.Is(err, unix.EINVAL) {
			err = nil
			return
		}
		if err != nil {
			return
		}
		t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
		t.Oflag &^= unix.OPOST
		t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
		t.Cflag &^= unix.CSIZE | unix.PARENB
		t.Cflag |= unix.CS8
		t.Cc[unix.VMIN], t.Cc[unix.VTIME] = 1, 0
		err = unix.IoctlSetTermios(int(fd), unix.TCSETS, t)
	})
	if err == nil {
		err = cerr
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

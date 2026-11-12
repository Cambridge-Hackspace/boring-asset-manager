//go:build linux

package serialport

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

var speeds = map[int]uint32{
	9600: unix.B9600, 19200: unix.B19200, 38400: unix.B38400, 57600: unix.B57600,
	115200: unix.B115200, 230400: unix.B230400, 460800: unix.B460800, 921600: unix.B921600,
}

// Open opens a serial port raw, 8 data bits, no parity, one stop bit, at a
// baud rate, with hardware flow control when rtscts is set. It is opened
// non-blocking, so its reads and writes take deadlines.
func Open(path string, baud int, rtscts bool) (*os.File, error) {
	speed, ok := speeds[baud]
	if !ok {
		return nil, fmt.Errorf("serial port: %d baud is not a rate BAM! opens a port at", baud)
	}
	f, err := os.OpenFile(path, os.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	raw, err := f.SyscallConn()
	if err != nil {
		f.Close()
		return nil, err
	}
	var setErr error
	err = raw.Control(func(fd uintptr) {
		t, err := unix.IoctlGetTermios(int(fd), unix.TCGETS)
		if err != nil {
			setErr = fmt.Errorf("%s is not a serial port: %w", path, err)
			return
		}
		// As cfmakeraw(3).
		t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON | unix.IXOFF | unix.IXANY
		t.Oflag &^= unix.OPOST
		t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
		t.Cflag &^= unix.CSIZE | unix.PARENB | unix.CSTOPB | unix.CBAUD | unix.CRTSCTS
		t.Cflag |= unix.CS8 | unix.CREAD | unix.CLOCAL | speed
		if rtscts {
			t.Cflag |= unix.CRTSCTS
		}
		t.Ispeed, t.Ospeed = speed, speed
		t.Cc[unix.VMIN], t.Cc[unix.VTIME] = 1, 0
		setErr = unix.IoctlSetTermios(int(fd), unix.TCSETS, t)
	})
	if err == nil {
		err = setErr
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

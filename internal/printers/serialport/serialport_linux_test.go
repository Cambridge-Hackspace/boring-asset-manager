//go:build linux

package serialport

import (
	"fmt"
	"os"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// pty opens a pseudo-terminal, returning its master side and the path of
// its other: a serial port as far as termios goes.
func pty(t *testing.T) (*os.File, string) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminals here: %v", err)
	}
	t.Cleanup(func() { master.Close() })
	var unlock int32
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, master.Fd(), unix.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		t.Skipf("unlocking the pseudo-terminal: %v", errno)
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Skipf("naming the pseudo-terminal: %v", err)
	}
	return master, fmt.Sprintf("/dev/pts/%d", n)
}

func TestAPortOpensRawAtItsBaudAndTakesDeadlines(t *testing.T) {
	master, path := pty(t)
	port, err := Open(path, 115200, false)
	if err != nil {
		t.Fatal(err)
	}
	defer port.Close()
	var got *unix.Termios
	raw, _ := port.SyscallConn()
	_ = raw.Control(func(fd uintptr) { got, err = unix.IoctlGetTermios(int(fd), unix.TCGETS) })
	if err != nil {
		t.Fatal(err)
	}
	if got.Cflag&unix.CBAUD != unix.B115200 || got.Lflag&unix.ICANON != 0 || got.Cflag&unix.CSIZE != unix.CS8 {
		t.Errorf("termios %+v", got)
	}
	// Raw: a carriage return comes through as it is.
	if _, err := master.Write([]byte("ok\r\n")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 8)
	_ = port.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := port.Read(buf)
	if err != nil || string(buf[:n]) != "ok\r\n" {
		t.Errorf("read %q, %v", buf[:n], err)
	}
	_ = port.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	if _, err := port.Read(buf); !os.IsTimeout(err) {
		t.Errorf("a read with nothing to read: %v", err)
	}
	if _, err := Open(path, 1234, false); err == nil {
		t.Error("an odd baud rate was taken")
	}
	if _, err := Open("/dev/null", 115200, false); err == nil {
		t.Error("/dev/null opened as a serial port")
	}
}

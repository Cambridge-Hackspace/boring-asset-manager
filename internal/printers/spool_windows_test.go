//go:build windows

package printers

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestSpoolingToAMissingPrinterSaysSo(t *testing.T) {
	conn := Connection{Type: ConnWindows, Address: "BAM test printer that is not there"}
	if err := NewDispatcher(nil).Check(conn); err != nil {
		t.Fatalf("windows connection refused on Windows: %v", err)
	}
	err := NewDispatcher(nil).Send(context.Background(), conn, []byte("job"))
	if !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "opening printer") {
		t.Errorf("got %v", err)
	}
}

// TestSpoolingToARealPrinter sends a job through the spooler, as RAW, to the
// printer BAM_TEST_WINDOWS_PRINTER names. A queue on the nul: port, such as
// "OneNote (Desktop)", takes it and prints nothing.
func TestSpoolingToARealPrinter(t *testing.T) {
	printer := os.Getenv("BAM_TEST_WINDOWS_PRINTER")
	if printer == "" {
		t.Skip("BAM_TEST_WINDOWS_PRINTER names no printer")
	}
	// Larger than one write is likely to take, so that every byte is sent
	// however the spooler splits it.
	job := bytes.Repeat([]byte("BAM spooler check\r\n"), 64<<10)
	if err := NewDispatcher(nil).Send(context.Background(), Connection{Type: ConnWindows, Address: printer}, job); err != nil {
		t.Fatal(err)
	}
}

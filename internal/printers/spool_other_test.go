//go:build !windows

package printers

import (
	"context"
	"errors"
	"testing"
)

func TestWindowsConnectionsNeedWindows(t *testing.T) {
	conn := Connection{Type: ConnWindows, Address: `\\desk-pc\DYMO`}
	if err := NewDispatcher(nil).Check(conn); !errors.Is(err, ErrInvalidPrinter) {
		t.Errorf("windows connection allowed off Windows: %v", err)
	}
	if err := NewDispatcher(nil).Send(context.Background(), conn, []byte("job")); !errors.Is(err, ErrUnreachable) {
		t.Errorf("windows send off Windows: got %v", err)
	}
}

//go:build windows

package printers

import (
	"fmt"
	"syscall"
	"unsafe"
)

// spoolerAvailable reports whether windows connections can be used.
const spoolerAvailable = true

var (
	winspool             = syscall.NewLazyDLL("winspool.drv")
	procOpenPrinter      = winspool.NewProc("OpenPrinterW")
	procClosePrinter     = winspool.NewProc("ClosePrinter")
	procStartDocPrinter  = winspool.NewProc("StartDocPrinterW")
	procEndDocPrinter    = winspool.NewProc("EndDocPrinter")
	procStartPagePrinter = winspool.NewProc("StartPagePrinter")
	procEndPagePrinter   = winspool.NewProc("EndPagePrinter")
	procWritePrinter     = winspool.NewProc("WritePrinter")
)

// docInfo1 is the Win32 DOC_INFO_1W structure.
type docInfo1 struct {
	docName    *uint16
	outputFile *uint16
	datatype   *uint16
}

// spool sends a job to a Windows printer, local or \\host\share, in the RAW
// datatype so the spooler passes it to the printer unchanged.
func spool(printer string, job []byte) error {
	name, err := syscall.UTF16PtrFromString(printer)
	if err != nil {
		return err
	}
	var handle syscall.Handle
	if r, _, err := procOpenPrinter.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&handle)), 0); r == 0 {
		return fmt.Errorf("opening printer %q: %v", printer, err)
	}
	defer procClosePrinter.Call(uintptr(handle))

	docName, _ := syscall.UTF16PtrFromString("BAM labels")
	datatype, _ := syscall.UTF16PtrFromString("RAW")
	doc := docInfo1{docName: docName, datatype: datatype}
	if r, _, err := procStartDocPrinter.Call(uintptr(handle), 1, uintptr(unsafe.Pointer(&doc))); r == 0 {
		return fmt.Errorf("starting document: %v", err)
	}
	defer procEndDocPrinter.Call(uintptr(handle))
	if r, _, err := procStartPagePrinter.Call(uintptr(handle)); r == 0 {
		return fmt.Errorf("starting page: %v", err)
	}
	defer procEndPagePrinter.Call(uintptr(handle))

	for len(job) > 0 {
		var written uint32
		r, _, err := procWritePrinter.Call(uintptr(handle), uintptr(unsafe.Pointer(&job[0])), uintptr(len(job)), uintptr(unsafe.Pointer(&written)))
		if r == 0 {
			return fmt.Errorf("sending job: %v", err)
		}
		if written == 0 {
			return fmt.Errorf("sending job: the spooler accepted no bytes")
		}
		job = job[written:]
	}
	return nil
}

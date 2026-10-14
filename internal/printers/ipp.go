package printers

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// IPP encoding (RFC 8010) for the one request BAM makes: Print-Job, with
// the job sent as raw bytes for the queue to pass straight to the printer.
const (
	ippVersionMajor, ippVersionMinor = 1, 1
	ippOpPrintJob                    = 0x0002

	ippTagOperation = 0x01
	ippTagEnd       = 0x03

	ippValueName     = 0x42 // nameWithoutLanguage
	ippValueURI      = 0x45
	ippValueCharset  = 0x47
	ippValueLanguage = 0x48
	ippValueMIMEType = 0x49

	// ippStatusSuccessMax is the last successful status code.
	ippStatusSuccessMax = 0x00FF

	// ippDocumentFormat asks the queue to pass the job through unchanged.
	ippDocumentFormat = "application/octet-stream"
)

// ippRequest builds a Print-Job request for printerURI carrying job.
func ippRequest(printerURI string, job []byte) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{ippVersionMajor, ippVersionMinor})
	_ = binary.Write(&buf, binary.BigEndian, uint16(ippOpPrintJob))
	_ = binary.Write(&buf, binary.BigEndian, uint32(1)) // request ID
	buf.WriteByte(ippTagOperation)
	attr := func(tag byte, name, value string) {
		buf.WriteByte(tag)
		_ = binary.Write(&buf, binary.BigEndian, uint16(len(name)))
		buf.WriteString(name)
		_ = binary.Write(&buf, binary.BigEndian, uint16(len(value)))
		buf.WriteString(value)
	}
	// RFC 8011 requires the charset and language first, then the target.
	attr(ippValueCharset, "attributes-charset", "utf-8")
	attr(ippValueLanguage, "attributes-natural-language", "en")
	attr(ippValueURI, "printer-uri", printerURI)
	attr(ippValueName, "requesting-user-name", "bam")
	attr(ippValueName, "job-name", "BAM labels")
	attr(ippValueMIMEType, "document-format", ippDocumentFormat)
	buf.WriteByte(ippTagEnd)
	buf.Write(job)
	return buf.Bytes()
}

// ippEndpoint is the HTTP URL an IPP URI is posted to: ipp and ipps use
// port 631 unless one is given.
func ippEndpoint(printerURI string) (string, error) {
	u, err := url.Parse(printerURI)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "ipp", "ipps":
		if u.Port() == "" {
			u.Host += ":631"
		}
		if u.Scheme == "ipp" {
			u.Scheme = "http"
		} else {
			u.Scheme = "https"
		}
	case "http", "https":
	default:
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	return u.String(), nil
}

func (d *Dispatcher) sendIPP(ctx context.Context, printerURI string, job []byte) error {
	endpoint, err := ippEndpoint(printerURI)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(ippRequest(printerURI, job)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/ipp")
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("IPP server answered HTTP %s", resp.Status)
	}
	head := make([]byte, 8)
	if _, err := io.ReadFull(resp.Body, head); err != nil {
		return fmt.Errorf("reading IPP response: %v", err)
	}
	if status := binary.BigEndian.Uint16(head[2:4]); status > ippStatusSuccessMax {
		return fmt.Errorf("IPP server refused the job (status 0x%04X)", status)
	}
	return nil
}

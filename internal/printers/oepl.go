package printers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// Pushing to OpenEPaperLink (docs/printing.md#openepaperlink): the image is
// uploaded to the access point, which sends it to the tag when the tag
// next checks in, typically within a minute. The access point's wiki
// documents the upload as a POST of the image, a baseline JPEG of the
// tag's exact size, to /imgupload with the tag's mac and dither.

// oeplTries is how many times an upload is tried while the access point
// says it is busy, and oeplBusyWait the longest it is waited for between
// them, whatever its Retry-After says.
var (
	oeplTries    = 5
	oeplBusyWait = 10 * time.Second
)

// sendOEPL uploads a tag's image to its access point.
func (d *Dispatcher) sendOEPL(ctx context.Context, address string, job []byte) error {
	a, err := ParseOEPLAddress(address)
	if err != nil {
		return err
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	// Dithering is off: the image is drawn in the tag's inks already.
	for _, f := range [][2]string{{"mac", a.Tag}, {"dither", "0"}} {
		if err := form.WriteField(f[0], f[1]); err != nil {
			return err
		}
	}
	head := textproto.MIMEHeader{}
	head.Set("Content-Disposition", `form-data; name="file"; filename="bam.jpg"`)
	head.Set("Content-Type", "image/jpeg")
	part, err := form.CreatePart(head)
	if err != nil {
		return err
	}
	if _, err := part.Write(job); err != nil {
		return err
	}
	if err := form.Close(); err != nil {
		return err
	}
	for try := 1; ; try++ {
		wait, err := d.uploadOEPL(ctx, a, form.FormDataContentType(), body.Bytes())
		if err == nil {
			return nil
		}
		if wait < 0 || try >= oeplTries {
			return err
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return fmt.Errorf("%v, and the job was given up: %v", err, ctx.Err())
		}
	}
}

// uploadOEPL makes one upload. When the access point is busy it says so,
// with how long to wait before trying again; otherwise the wait is
// negative.
func (d *Dispatcher) uploadOEPL(ctx context.Context, a OEPLAddress, contentType string, body []byte) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, d.WriteTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.Base+"/imgupload", bytes.NewReader(body))
	if err != nil {
		return -1, err
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := d.client.Do(req)
	if err != nil {
		return -1, fmt.Errorf("uploading to the access point: %v", err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
	said := strings.TrimSpace(string(answer))
	switch {
	case resp.StatusCode == http.StatusOK:
		return 0, nil
	case resp.StatusCode == http.StatusConflict:
		wait := 2 * time.Second
		if n, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && n >= 0 {
			wait = time.Duration(n) * time.Second
		}
		return min(wait, oeplBusyWait), fmt.Errorf("the access point is busy: %s", said)
	case said != "":
		return -1, fmt.Errorf("the access point answered HTTP %s: %s", resp.Status, said)
	}
	return -1, fmt.Errorf("the access point answered HTTP %s", resp.Status)
}

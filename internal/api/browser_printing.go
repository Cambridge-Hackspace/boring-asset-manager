package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/coder/websocket"
)

// Printing from a browser (docs/printing.md#from-a-browser): a printer
// whose connection is the browser's own Bluetooth is printed to over a
// WebSocket. The server renders the job as it does for any printer, and the
// browser, holding the printer the person there picked, writes what the
// server sends it and passes back what the printer answers. A printer that
// holds a conversation holds it with the server, through the browser, so
// no driver runs in the browser.

var (
	// browserTimeout is how long the browser, and the printer through it,
	// may take to answer: the print request, and each of the printer's
	// answers in a conversation.
	browserTimeout = 60 * time.Second
	// browserJobTimeout bounds writing a whole job that the printer does
	// not answer: the browser says when it has written the last byte.
	browserJobTimeout = 15 * time.Minute
	// browserPiece is the most of a job one message carries.
	browserPiece = 16 << 10
)

// browserReadLimit bounds a message from the browser, of which the print
// request is the largest.
const browserReadLimit = 1 << 20

// browserMessage is a message either way, as text. The browser sends the
// print request first, as "print", then "done" once it has written the
// whole of a job that is not a conversation, or "failed" when it cannot
// write to the printer. The server sends "job", what it rendered, then
// "sent" after the last of a job that is not a conversation, and last
// "printed" or "error". The job itself goes to the browser, and the
// printer's answers come back, as binary messages.
type browserMessage struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
	PrintRequest
}

// handleBrowserPrint prints to a printer over the browser's own
// Bluetooth: GET /api/v1/printers/{id}/print/browser, as a WebSocket. The
// WebSocket library refuses a page of another origin.
func (s *Server) handleBrowserPrint(w http.ResponseWriter, r *http.Request) {
	if !webSocketRequest(r) {
		w.Header().Set("Upgrade", "websocket")
		writeJSONError(w, http.StatusUpgradeRequired, "Connect with a WebSocket (version 13) to print from the browser")
		return
	}
	p, ok := s.loadPrinter(w, r.PathValue("id"))
	if !ok {
		return
	}
	if p.Connection.Type != printers.ConnBrowser {
		writeJSONError(w, http.StatusConflict, fmt.Sprintf("%s is not printed to from a browser", p.Name))
		return
	}
	profile, err := browserProfile(p)
	if err != nil {
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	// Printing waits on the person at the printer, past the server's read
	// and write timeouts.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(browserReadLimit)

	ctx := context.WithoutCancel(r.Context())
	labels, err := s.printThroughBrowser(ctx, ws, r, p, profile)
	if err != nil {
		writeBrowserMessage(ctx, ws, map[string]any{"type": "error", "message": err.Error()})
	} else {
		writeBrowserMessage(ctx, ws, map[string]any{"type": "printed", "labels": labels})
	}
	ws.Close(websocket.StatusNormalClosure, "")
}

// browserProfile is how a browser reaches a printer: its driver's
// Bluetooth LE profile, with what its connection sets.
func browserProfile(p *models.Printer) (printers.BLEProfile, error) {
	return printers.BrowserProfile(p.Driver, p.Connection)
}

// printThroughBrowser reads the print request, renders its job, and sends
// it through the browser, returning how many labels it printed.
func (s *Server) printThroughBrowser(ctx context.Context, ws *websocket.Conn, r *http.Request, p *models.Printer, profile printers.BLEProfile) (int, error) {
	rctx, cancel := context.WithTimeout(ctx, browserTimeout)
	typ, data, err := ws.Read(rctx)
	cancel()
	var req browserMessage
	if err != nil || typ != websocket.MessageText || json.Unmarshal(data, &req) != nil || req.Type != "print" {
		return 0, errors.New("the browser sent no print request")
	}
	answer := &capturedAnswer{header: http.Header{}}
	job, ok := s.preparePrint(answer, r, p.ID, req.PrintRequest)
	if !ok {
		return 0, errors.New(answer.message())
	}
	if err := writeBrowserMessage(ctx, ws, map[string]any{"type": "job", "labels": job.labels, "bytes": len(job.data), "warnings": job.warnings}); err != nil {
		return 0, err
	}

	// The browser's messages: the printer's answers, and whether it wrote
	// the job. A failure, or the browser going, stops the job. They are
	// read until the connection closes, never with the job's context: the
	// WebSocket closes itself when a read's context ends, which would cut
	// off the answer that follows the job.
	jctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	link := &browserLink{ws: ws, received: make(chan []byte, 64), done: make(chan struct{})}
	go link.read(ctx, jctx, stop)

	if profile.Protocol != "" {
		err = printers.Converse(jctx, profile.Protocol, link, job.data, browserTimeout)
	} else {
		err = link.stream(jctx, job.data)
	}
	if cause := context.Cause(jctx); cause != nil && !errors.Is(cause, context.Canceled) {
		err = cause
	}
	if err != nil {
		return 0, fmt.Errorf("Could not deliver the job to %s: %v", p.Name, err)
	}
	s.recordPrinted(r, job)
	return job.labels, nil
}

// browserLink is a print's WebSocket to the browser, as a conversation's
// port: a write goes to the printer through the browser, and what the
// printer answers comes back.
type browserLink struct {
	ws       *websocket.Conn
	received chan []byte
	// done is closed when the browser says it has written the whole job.
	done chan struct{}
}

// Write sends a piece of the job. Once the job stops it sends no more, but
// a write under way runs on to ctx's deadline rather than ending with it:
// the WebSocket closes itself when a write's context ends, which would cut
// off the message saying why the job stopped.
func (b *browserLink) Write(ctx context.Context, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	wctx, cancel := uncancelled(ctx)
	defer cancel()
	return b.ws.Write(wctx, websocket.MessageBinary, data)
}

// uncancelled is ctx without its cancellation but with its deadline, or
// browserTimeout from now where it has none.
func uncancelled(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(browserTimeout)
	}
	return context.WithDeadline(context.WithoutCancel(ctx), deadline)
}

func (b *browserLink) Received() <-chan []byte { return b.received }

// read passes on the browser's messages until the connection closes,
// stopping the job with its reason when the browser cannot write to the
// printer or goes. It reads with ctx, which never ends, and passes answers
// on while the job, job, lasts.
func (b *browserLink) read(ctx, job context.Context, stop context.CancelCauseFunc) {
	defer close(b.received)
	for {
		typ, data, err := b.ws.Read(ctx)
		if err != nil {
			stop(errors.New("the browser disconnected"))
			return
		}
		if typ == websocket.MessageBinary {
			select {
			case b.received <- data:
			case <-job.Done():
			}
			continue
		}
		var m browserMessage
		_ = json.Unmarshal(data, &m)
		switch m.Type {
		case "done":
			// Said once; a browser that says it again changes nothing.
			select {
			case <-b.done:
			default:
				close(b.done)
			}
		case "failed":
			stop(fmt.Errorf("the browser could not write to the printer: %s", m.Message))
		}
	}
}

// stream sends a job the printer does not answer, and waits for the
// browser to have written all of it.
func (b *browserLink) stream(ctx context.Context, job []byte) error {
	ctx, cancel := context.WithTimeout(ctx, browserJobTimeout)
	defer cancel()
	for sent := 0; sent < len(job); sent += browserPiece {
		if err := b.Write(ctx, job[sent:min(len(job), sent+browserPiece)]); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := writeBrowserMessage(context.WithoutCancel(ctx), b.ws, map[string]string{"type": "sent"}); err != nil {
		return err
	}
	select {
	case <-b.done:
		return nil
	case <-ctx.Done():
		if context.Cause(ctx) == context.DeadlineExceeded {
			return fmt.Errorf("the browser did not finish writing the job within %s", browserJobTimeout)
		}
		return ctx.Err()
	}
}

// writeBrowserMessage sends a message to the browser as text.
func writeBrowserMessage(ctx context.Context, ws *websocket.Conn, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return ws.Write(wctx, websocket.MessageText, data)
}

// capturedAnswer keeps what a handler answered, for a request answered
// over a WebSocket instead.
type capturedAnswer struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (c *capturedAnswer) Header() http.Header         { return c.header }
func (c *capturedAnswer) WriteHeader(status int)      { c.status = status }
func (c *capturedAnswer) Write(b []byte) (int, error) { return c.body.Write(b) }

// message is the error the handler answered with.
func (c *capturedAnswer) message() string {
	var e ErrorResponse
	if json.Unmarshal(c.body.Bytes(), &e) == nil && e.Error != "" {
		return e.Error
	}
	return http.StatusText(c.status)
}

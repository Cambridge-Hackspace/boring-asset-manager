package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/relay"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/google/uuid"
)

// Printers through a relay (docs/relays.md#printers-through-a-relay): the
// main node still renders, plans and encodes each job, so drivers stay in
// one place, and the relay only delivers the bytes, to an address on its
// own network, within its own allowlist.

// relayJobTimeout bounds a job sent through a relay, from sending it to
// the relay's answer that the printer took it. The relay's own timeouts
// fail a printer that stalls well before.
var relayJobTimeout = 15 * time.Minute

// relayJobPiece is the most of a job one message carries; a larger job
// goes in pieces.
var relayJobPiece = relay.JobPieceBytes

// printTransport delivers jobs: through a relay for a printer whose
// connection names one, and from this server otherwise.
type printTransport struct {
	s     *Server
	local printers.Transport
}

// Check refuses a connection through a relay that does not exist, or
// while relays are off. What the relay may reach is its own to say, when a
// job arrives.
func (t *printTransport) Check(c printers.Connection) error {
	if c.Relay == "" {
		return t.local.Check(c)
	}
	if t.s.relays == nil {
		return fmt.Errorf("%w: relays are off on this server; turn on [relay] websocket or bonemesh", printers.ErrInvalidPrinter)
	}
	rl, err := t.s.store.RelayByName(c.Relay)
	if errors.Is(err, store.ErrRelayNotFound) || (err == nil && rl.RevokedAt != nil) {
		return fmt.Errorf("%w: there is no relay named %s; add it under Admin › Relays", printers.ErrInvalidPrinter, c.Relay)
	}
	return err
}

// Send delivers a job, through the relay its connection names if any. A
// job too large for one message goes in pieces, and the relay answers
// once the printer has taken the whole of it.
func (t *printTransport) Send(ctx context.Context, c printers.Connection, job []byte) error {
	if c.Relay == "" {
		return t.local.Send(ctx, c, job)
	}
	if t.s.relays == nil {
		return fmt.Errorf("%w: relays are off on this server", printers.ErrUnreachable)
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, relayJobTimeout)
		defer cancel()
	}
	dest := relay.Destination{Type: c.Type, Address: c.Address}
	n := max(1, (len(job)+relayJobPiece-1)/relayJobPiece)
	of := uuid.NewString()
	piece := func(i int) relay.Job {
		j := relay.Job{Destination: dest, Data: job[i*relayJobPiece : min(len(job), (i+1)*relayJobPiece)]}
		if n > 1 {
			j.Piece = &relay.JobPiece{Of: of, I: i, N: n}
		}
		return j
	}
	for i := 0; i < n-1; i++ {
		if _, err := t.s.relays.Send(ctx, c.Relay, relay.TypeJob, piece(i)); err != nil {
			return relayFailure(c.Relay, err)
		}
	}
	reply, err := t.s.relays.Request(ctx, c.Relay, relay.TypeJob, piece(n-1))
	if err != nil {
		return relayFailure(c.Relay, err)
	}
	var res relay.JobResult
	if err := reply.Decode(&res); err != nil {
		return fmt.Errorf("%w: relay %s answered with %s", printers.ErrUnreachable, c.Relay, reply.Type)
	}
	if !res.OK {
		return fmt.Errorf("%w: through relay %s: %s", printers.ErrUnreachable, c.Relay, res.Reason)
	}
	return nil
}

// relayFailure says why a job did not reach a relay: offline, when no
// server holds it or it did not answer, or the relay's own reason.
func relayFailure(name string, err error) error {
	var refused *relay.Refused
	if errors.Is(err, relay.ErrUnreachable) || (errors.As(err, &refused) && strings.Contains(refused.Reason, "not connected")) {
		return fmt.Errorf("%w: relay %s is offline", printers.ErrUnreachable, name)
	}
	return fmt.Errorf("%w: through relay %s: %v", printers.ErrUnreachable, name, err)
}

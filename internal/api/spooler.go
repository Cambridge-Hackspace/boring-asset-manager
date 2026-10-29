package api

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/render"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// The spooler (README §6.2) sends print jobs from the spool in the
// background: each printer takes one batch at a time, and printers work in
// parallel. A batch being sent beats a heartbeat, so one a stopped server
// left is found by the sweep and its labels put back.

var (
	// spoolPoll is how often the spooler looks for work it was not woken
	// for, such as jobs another server made.
	spoolPoll = 2 * time.Second
	// spoolHeartbeat is how often a batch being sent says so, and
	// spoolStale how long without one before it counts as abandoned.
	// Delivering one has no limit of its own: the printer's connection
	// fails when the printer stops taking it.
	spoolHeartbeat = 5 * time.Second
	spoolStale     = 30 * time.Second
)

// spooler is the Server's part in printing the spool.
type spooler struct {
	wake chan struct{}
	mu   sync.Mutex
	// busy holds the printers this server is sending to; turns holds
	// each printer's lock, which direct printing takes too, so two jobs
	// never interleave on one printer.
	busy  map[string]bool
	turns map[string]*sync.Mutex
	work  sync.WaitGroup
}

func newSpooler() *spooler {
	return &spooler{wake: make(chan struct{}, 1), busy: map[string]bool{}, turns: map[string]*sync.Mutex{}}
}

// printerTurn is the lock held while sending to a printer.
func (s *Server) printerTurn(printerID string) *sync.Mutex {
	s.spool.mu.Lock()
	defer s.spool.mu.Unlock()
	if s.spool.turns[printerID] == nil {
		s.spool.turns[printerID] = &sync.Mutex{}
	}
	return s.spool.turns[printerID]
}

// wakeSpool asks the spooler to look for work now.
func (s *Server) wakeSpool() {
	select {
	case s.spool.wake <- struct{}{}:
	default:
	}
}

// RunSpool sends print jobs from the spool until ctx ends, then waits for
// the batches being sent.
func (s *Server) RunSpool(ctx context.Context) {
	ticker := time.NewTicker(spoolPoll)
	defer ticker.Stop()
	for {
		s.spoolRound(ctx)
		select {
		case <-ctx.Done():
			s.spool.work.Wait()
			return
		case <-s.spool.wake:
		case <-ticker.C:
		}
	}
}

// spoolRound sweeps up what stopped servers left, then starts a batch on
// every printer that has one waiting and is not already sending. It
// reports whether it started any.
func (s *Server) spoolRound(ctx context.Context) bool {
	if _, err := s.store.SweepSpool(time.Now().Add(-spoolStale)); err != nil {
		log.Printf("sweeping the spool: %v", err)
	}
	list, err := s.store.ListPrinters()
	if err != nil {
		log.Printf("spooler: listing printers: %v", err)
		return false
	}
	started := false
	for _, p := range list {
		s.spool.mu.Lock()
		if s.spool.busy[p.ID] {
			s.spool.mu.Unlock()
			continue
		}
		b, err := s.store.ClaimSpoolBatch(p.ID)
		if err != nil || b == nil {
			s.spool.mu.Unlock()
			if err != nil {
				log.Printf("spooler: claiming a batch for %s: %v", p.Name, err)
			}
			continue
		}
		s.spool.busy[p.ID] = true
		s.spool.mu.Unlock()
		started = true
		s.spool.work.Add(1)
		go func() {
			defer s.spool.work.Done()
			s.sendBatch(ctx, b)
			s.spool.mu.Lock()
			delete(s.spool.busy, b.PrinterID)
			s.spool.mu.Unlock()
			s.wakeSpool()
		}()
	}
	return started
}

// drainSpool sends everything waiting, round after round, until nothing is
// left to start; for tests, which run no RunSpool.
func (s *Server) drainSpool(ctx context.Context) {
	for s.spoolRound(ctx) {
		s.spool.work.Wait()
	}
	s.spool.work.Wait()
}

// sendBatch lays out a batch's labels as whoever asked for its job may see
// them, sends them, and records how that went. It beats the batch's
// heartbeat from the start: laying out hundreds of labels, or waiting for
// the printer's turn, can take longer than the sweep allows a batch whose
// server stopped.
func (s *Server) sendBatch(ctx context.Context, b *store.SpoolBatch) {
	acting := s.store.As(b.CreatedBy)
	if b.CreatedByToken {
		acting = s.store.AsToken(b.CreatedBy)
	}
	beat, stop := context.WithCancel(ctx)
	beating := make(chan struct{})
	go func() {
		defer close(beating)
		tick := time.NewTicker(spoolHeartbeat)
		defer tick.Stop()
		for {
			select {
			case <-beat.Done():
				return
			case <-tick.C:
				if err := s.store.HeartbeatSpoolBatch(b.ID); err != nil {
					log.Printf("spooler: %v", err)
				}
			}
		}
	}()
	outcome := s.printBatch(ctx, b)
	stop()
	<-beating
	if err := acting.FinishSpoolBatch(b, outcome); err != nil {
		log.Printf("spooler: finishing a batch: %v", err)
	}
}

// printBatch does the work of sendBatch, saying how it went.
func (s *Server) printBatch(ctx context.Context, b *store.SpoolBatch) store.SpoolOutcome {
	p, err := s.store.GetPrinter(b.PrinterID)
	if err != nil {
		return store.SpoolOutcome{Failure: "Its printer could not be read: it may have been deleted"}
	}
	if b.TemplateID == nil {
		return store.SpoolOutcome{Failure: b.TemplateName + " was deleted before these were printed"}
	}
	t, err := s.store.GetTemplate(*b.TemplateID)
	if err != nil {
		return store.SpoolOutcome{Failure: b.TemplateName + " could not be read: it may have been deleted"}
	}
	plan, t, _, err := s.planFor(p, t)
	if err != nil {
		// In a pool, the rest take it.
		return store.SpoolOutcome{Failure: unfitMessage(t, p, err), HandBack: b.PoolID != ""}
	}
	r, err := s.requestAs(ctx, b.CreatedBy, b.CreatedByToken)
	if err != nil {
		return store.SpoolOutcome{Failure: "Whoever printed these can no longer: " + err.Error()}
	}

	out := store.SpoolOutcome{Printer: p.Name, Problems: map[string]string{}, Moves: map[string]string{}}
	images := s.newLabelImages(ctx)
	var rendered []*image.Gray
	for _, e := range b.Entries {
		scene, problem, err := s.layoutAsset(r, t, e.AssetID, images)
		if err != nil {
			return store.SpoolOutcome{Failure: "These labels could not be laid out: " + err.Error()}
		}
		if problem != nil {
			msg := problem.Message
			if problem.Path != "" {
				msg = problem.Path + ": " + msg
			}
			out.Problems[e.ID] = "Could not be produced: " + msg
			// One whose categories offer another template that suits the
			// printer and holds it moves to that, to print next time.
			if next, ok := s.nextTemplate(r, p, t, e, images); ok {
				out.Moves[e.ID] = next.ID
				out.Problems[e.ID] = fmt.Sprintf("Could not be produced with %s (%s), so it moved to %s; print it again", t.Name, msg, next.Name)
			}
			continue
		}
		img := scene.Raster(true)
		for c := 0; c < e.Copies; c++ {
			rendered = append(rendered, img)
		}
		out.Printed = append(out.Printed, e.ID)
	}
	if len(rendered) == 0 {
		out.Printed, out.Failure = nil, "None of these labels could be produced"
		return out
	}
	job, err := plan.Encode(rendered)
	if err != nil {
		return store.SpoolOutcome{Failure: "The print job could not be made: " + err.Error(), Problems: out.Problems}
	}

	// Only now, with everything ready, wait for the printer's turn.
	turn := s.printerTurn(p.ID)
	turn.Lock()
	defer turn.Unlock()
	if err := s.transport.Send(ctx, p.Connection, job); err != nil {
		if ctx.Err() != nil {
			return store.SpoolOutcome{Failure: fmt.Sprintf("The server stopped while sending these to %s; they may have printed. Check before printing them again", p.Name), Problems: out.Problems}
		}
		return store.SpoolOutcome{Failure: fmt.Sprintf("Could not deliver these to %s: %v", p.Name, err), Problems: out.Problems, HandBack: b.PoolID != ""}
	}
	return out
}

// requestAs is a request carrying the identity and permissions of a user
// or API token as they are now, for laying out labels as they may see
// them away from any request of theirs.
func (s *Server) requestAs(ctx context.Context, id string, token bool) (*http.Request, error) {
	public, err := s.store.PublicPermissions()
	if err != nil {
		return nil, err
	}
	if token {
		t, perms, err := s.store.TokenPermissions(id)
		if err != nil {
			return nil, errors.New("the API token no longer works")
		}
		ctx = WithPermissions(WithToken(ctx, t), union(perms, public))
	} else {
		u, err := s.store.GetUserByID(id)
		if err != nil || !u.IsActive || u.ArchivedAt != nil {
			return nil, errors.New("the account is no longer active")
		}
		perms, err := s.store.GetUserPermissions(id)
		if err != nil {
			return nil, err
		}
		ctx = WithPermissions(WithUser(ctx, u), union(perms, public))
	}
	return http.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
}

// nextTemplate is the template a label that could not be produced with t
// moves to: when its template came from its categories, the first of
// theirs after t that suits the printer and holds its asset.
func (s *Server) nextTemplate(r *http.Request, p *models.Printer, t *labels.Template, e store.SpoolBatchEntry, images render.ImageSource) (*labels.Template, bool) {
	entries, err := s.store.ListSpool(store.SpoolFilter{IDs: []string{e.ID}})
	if err != nil || len(entries) != 1 || !entries[0].TemplateFromCategory {
		return nil, false
	}
	a, err := s.store.GetAssetByID(e.AssetID)
	if err != nil {
		return nil, false
	}
	list := a.LabelTemplate.TemplateIDs
	for i, id := range list {
		if id != t.ID {
			continue
		}
		for _, nextID := range list[i+1:] {
			next, err := s.store.GetTemplate(nextID)
			if err != nil {
				continue
			}
			if _, laid, _, err := s.planFor(p, next); err == nil {
				if _, problem, err := s.layoutAsset(r, laid, e.AssetID, images); err == nil && problem == nil {
					return next, true
				}
			}
		}
	}
	return nil, false
}

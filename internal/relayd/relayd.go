// Package relayd runs a relay (docs/relays.md): BAM! on a machine on a
// local network, linked to the main node by the relay bus, delivering the
// print jobs the main node sends to the printers it may reach.
//
// The relay holds no database. It keeps only its pairing, links to the
// main node over BoneMesh, WebSocket or both, as its pairing offers and its
// owner allows, renews its certificate before it expires, and reports its
// status. What it may reach is its own allowlist's to say, not the main
// node's.
package relayd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"forge.axonibyte.com/axonibyte/bonemesh/go/v3/node"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/relay"
)

var (
	// renewBefore is how long before its certificate expires a relay
	// renews it.
	renewBefore = 3 * 24 * time.Hour
	// renewCheck is how often a relay looks at whether to renew, and
	// tries again after failing to.
	renewCheck = time.Hour
	// pieceWait bounds how long a job's last piece waits for the others.
	pieceWait = 30 * time.Second
	// probeTimeout bounds a reachability check.
	probeTimeout = 3 * time.Second
)

// maxPieceBytes bounds the bytes of jobs arriving in pieces at once.
const maxPieceBytes = 64 << 20

// Config is what a relay runs with.
type Config struct {
	// Pairing is the relay's pairing, kept at State, where renewals are
	// saved.
	Pairing *relay.Pairing
	State   string
	// Version is this build's version, reported to the main node.
	Version string
	// BoneMesh and WebSocket turn each transport on, where the pairing
	// offers it.
	BoneMesh, WebSocket bool
	// Allow is what the relay may reach.
	Allow *Allowlist
	// Transport delivers jobs locally: a printers.Dispatcher allowing the
	// allowlist's device paths and Bluetooth printers.
	Transport printers.Transport
	// Client renews the certificate; nil is a default client.
	Client *http.Client
	// Retry and Poll are the bus's, for tests; zero is the default.
	Retry, Poll time.Duration
}

// Relay is a running relay.
type Relay struct {
	cfg Config
	bus *relay.Bus

	mu      sync.Mutex
	pairing *relay.Pairing
	mesh    *relay.Mesh
	pieces  map[string]*pieces
	held    int
	linked  bool
}

// pieces are the pieces of a job arriving in several messages.
type pieces struct {
	data    [][]byte
	got     int
	started time.Time
}

// trust is the relay's trust, from its pairing as it is now, so a renewal
// takes effect without the bus starting again.
type trust struct{ r *Relay }

func (t trust) current() relay.Trust {
	t.r.mu.Lock()
	defer t.r.mu.Unlock()
	return t.r.pairing.Trust()
}

func (t trust) Credential(s string) (string, error)     { return t.current().Credential(s) }
func (t trust) IdentityKey(name string) ([]byte, error) { return t.current().IdentityKey(name) }
func (t trust) Mesh() (node.Config, error)              { return t.current().Mesh() }

// New makes a relay, not yet linked.
func New(cfg Config) (*Relay, error) {
	if cfg.Pairing == nil {
		return nil, errors.New("the relay is not paired: run bam relay join first")
	}
	if cfg.Allow == nil {
		cfg.Allow = &Allowlist{}
	}
	if cfg.Transport == nil {
		return nil, errors.New("the relay has no transport to deliver jobs with")
	}
	r := &Relay{cfg: cfg, pairing: cfg.Pairing, pieces: map[string]*pieces{}}
	r.bus = relay.New(relay.Options{Name: cfg.Pairing.Name, Trust: trust{r}, Retry: cfg.Retry, Poll: cfg.Poll,
		Changed: func(peer string) { go r.changed(peer) }})
	r.bus.Handle(relay.TypeJob, r.job)
	r.bus.Handle(relay.TypeStatus, r.status)
	return r, nil
}

// Bus is the relay's end of the relay bus.
func (r *Relay) Bus() *relay.Bus { return r.bus }

// Run links the relay to the main node and serves it until ctx ends.
func (r *Relay) Run(ctx context.Context) error {
	p := r.current()
	bonemesh, websocket := r.cfg.BoneMesh && p.BoneMesh != "", r.cfg.WebSocket && p.WebSocket
	if !bonemesh && !websocket {
		return errors.New("no transport is on: the main node offers " + offered(p) + ", and this relay takes " + taken(r.cfg))
	}
	defer r.bus.Close()
	// A certificate that expired while the relay was stopped is renewed
	// before BoneMesh starts with it.
	r.renew(ctx)
	if bonemesh {
		r.startMesh()
		defer r.stopMesh()
	}
	var wg sync.WaitGroup
	if websocket {
		wg.Add(1)
		go func() {
			defer wg.Done()
			relay.DialWebSocket(ctx, r.bus, p.Main, p.Credential)
		}()
	}
	log.Printf("Relay %s is linking to %s over %s", p.Name, p.Main, describe(bonemesh, websocket))
	tick := time.NewTicker(renewCheck)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			r.bus.Close()
			wg.Wait()
			return nil
		case <-tick.C:
			if r.renew(ctx) && bonemesh {
				// BoneMesh holds its certificate from when it started.
				r.stopMesh()
				r.startMesh()
			}
		}
	}
}

func (r *Relay) current() *relay.Pairing {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pairing
}

// renew renews the certificate when it nears its end, saving the pairing.
// It reports whether it renewed.
func (r *Relay) renew(ctx context.Context) bool {
	p := r.current()
	if time.Until(p.ExpiresAt) > renewBefore {
		return false
	}
	next := *p
	if err := next.Renew(ctx, r.cfg.Client, r.cfg.Version); err != nil {
		log.Printf("Relay %s could not renew its certificate, which expires %s: %v", p.Name, p.ExpiresAt.Local().Format(time.RFC1123), err)
		return false
	}
	if r.cfg.State != "" {
		if err := next.Save(r.cfg.State); err != nil {
			log.Printf("Relay %s renewed its certificate, but could not keep it in %s: %v", p.Name, r.cfg.State, err)
		}
	}
	r.mu.Lock()
	r.pairing = &next
	r.mu.Unlock()
	log.Printf("Relay %s renewed its certificate, until %s", p.Name, next.ExpiresAt.Local().Format(time.RFC1123))
	return true
}

func (r *Relay) startMesh() {
	mesh, err := relay.StartBoneMesh(r.bus, relay.BoneMeshOptions{Dial: r.current().BoneMesh})
	if err != nil {
		log.Printf("Relay %s: BoneMesh is off: %v", r.current().Name, err)
		return
	}
	r.mu.Lock()
	r.mesh = mesh
	r.mu.Unlock()
}

func (r *Relay) stopMesh() {
	r.mu.Lock()
	mesh := r.mesh
	r.mesh = nil
	r.mu.Unlock()
	if mesh != nil {
		mesh.Close()
	}
}

// changed reports the relay's status to the main node each time it links
// to it, having had no link.
func (r *Relay) changed(peer string) {
	if peer != relay.Main {
		return
	}
	up := len(r.bus.Transports(relay.Main)) > 0
	r.mu.Lock()
	was := r.linked
	r.linked = up
	r.mu.Unlock()
	if !up || was {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := r.bus.Send(ctx, relay.Main, relay.TypeStatus, r.statusOf(ctx, nil)); err != nil {
		log.Printf("Relay %s could not report its status: %v", r.current().Name, err)
	}
}

// statusOf is the relay's status, with whether each destination asked
// about can be reached.
func (r *Relay) statusOf(ctx context.Context, asked []relay.Destination) relay.Status {
	s := relay.Status{Version: r.cfg.Version, Protocol: relay.Version, Allowlist: r.cfg.Allow.Summary()}
	for _, d := range asked {
		reach := relay.Reachability{Destination: d, Reachable: true}
		if err := r.reach(ctx, d); err != nil {
			reach.Reachable, reach.Reason = false, err.Error()
		}
		s.Destinations = append(s.Destinations, reach)
	}
	return s
}

// status answers the main node's request for the relay's status.
func (r *Relay) status(ctx context.Context, m relay.Message) {
	var req relay.StatusRequest
	if len(m.Body) > 0 {
		_ = m.Decode(&req)
	}
	if err := r.bus.Reply(ctx, m, relay.TypeStatus, r.statusOf(ctx, req.Destinations)); err != nil {
		log.Printf("Relay %s could not answer for its status: %v", r.current().Name, err)
	}
}

// destination is a destination as a printer's connection, checked and
// within the allowlist.
func (r *Relay) destination(d relay.Destination) (printers.Connection, error) {
	c, err := printers.NormalizeConnection(printers.Connection{Type: d.Type, Address: d.Address})
	if err != nil {
		return c, err
	}
	return c, r.cfg.Allow.Allows(c)
}

// reach says whether a destination can be reached now, without printing:
// a TCP printer that takes a connection, a device that is there.
func (r *Relay) reach(ctx context.Context, d relay.Destination) error {
	c, err := r.destination(d)
	if err != nil {
		return err
	}
	if err := r.cfg.Transport.Check(c); err != nil {
		return err
	}
	switch c.Type {
	case printers.ConnTCP:
		dialer := net.Dialer{Timeout: probeTimeout}
		conn, err := dialer.DialContext(ctx, "tcp", printers.TCPHostPort(c.Address))
		if err != nil {
			return err
		}
		conn.Close()
	case printers.ConnDevice:
		if _, err := os.Stat(printers.DevicePath(c.Address)); err != nil {
			return err
		}
	}
	return nil
}

// job delivers a job, or a piece of one, and answers how it went.
func (r *Relay) job(ctx context.Context, m relay.Message) {
	answer := func(err error) {
		res := relay.JobResult{OK: err == nil}
		if err != nil {
			res.Reason = err.Error()
		}
		if err := r.bus.Reply(ctx, m, relay.TypeJobResult, res); err != nil {
			log.Printf("Relay %s could not answer a job: %v", r.current().Name, err)
		}
	}
	var job relay.Job
	if err := m.Decode(&job); err != nil {
		answer(fmt.Errorf("the job could not be read: %v", err))
		return
	}
	data := job.Data
	if p := job.Piece; p != nil {
		whole, last, err := r.piece(*p, job.Data)
		if !last {
			if err != nil {
				log.Printf("Relay %s refused a piece of a job: %v", r.current().Name, err)
			}
			return
		}
		if err != nil {
			answer(err)
			return
		}
		data = whole
	}
	c, err := r.destination(job.Destination)
	if err != nil {
		answer(fmt.Errorf("refused by the relay: %v", err))
		return
	}
	if job.TimeoutMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(job.TimeoutMS)*time.Millisecond)
		defer cancel()
	}
	answer(r.cfg.Transport.Send(ctx, c, data))
}

// piece keeps a piece of a job. On the last, it waits for the others,
// which may still be on their way to it, and returns the whole job.
func (r *Relay) piece(p relay.JobPiece, data []byte) ([]byte, bool, error) {
	last := p.I == p.N-1
	if p.Of == "" || p.N < 2 || p.N > relay.MaxParts || p.I < 0 || p.I >= p.N {
		return nil, last, errors.New("a piece of a job is one of 2 or more")
	}
	r.mu.Lock()
	for of, ps := range r.pieces {
		if time.Since(ps.started) > 10*time.Minute {
			for _, d := range ps.data {
				r.held -= len(d)
			}
			delete(r.pieces, of)
		}
	}
	ps := r.pieces[p.Of]
	if ps == nil {
		ps = &pieces{data: make([][]byte, p.N), started: time.Now()}
		r.pieces[p.Of] = ps
	}
	if len(ps.data) != p.N {
		r.mu.Unlock()
		return nil, last, errors.New("the pieces of a job disagree on how many there are")
	}
	if ps.data[p.I] == nil {
		if r.held+len(data) > maxPieceBytes {
			r.mu.Unlock()
			return nil, last, errors.New("too many bytes of jobs are arriving in pieces")
		}
		ps.data[p.I] = append([]byte{}, data...)
		ps.got++
		r.held += len(data)
	}
	r.mu.Unlock()
	if !last {
		return nil, false, nil
	}
	deadline := time.Now().Add(pieceWait)
	for {
		r.mu.Lock()
		if ps.got == p.N {
			delete(r.pieces, p.Of)
			var whole []byte
			for _, d := range ps.data {
				whole = append(whole, d...)
				r.held -= len(d)
			}
			r.mu.Unlock()
			return whole, true, nil
		}
		r.mu.Unlock()
		if time.Now().After(deadline) {
			return nil, true, errors.New("the job's pieces did not all arrive")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func describe(bonemesh, websocket bool) string {
	switch {
	case bonemesh && websocket:
		return "BoneMesh and WebSocket"
	case bonemesh:
		return "BoneMesh"
	}
	return "WebSocket"
}

func offered(p *relay.Pairing) string {
	if p.BoneMesh == "" && !p.WebSocket {
		return "neither"
	}
	return describe(p.BoneMesh != "", p.WebSocket)
}

func taken(cfg Config) string {
	if !cfg.BoneMesh && !cfg.WebSocket {
		return "neither"
	}
	return describe(cfg.BoneMesh, cfg.WebSocket)
}

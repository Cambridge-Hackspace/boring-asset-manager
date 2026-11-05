package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

var (
	// holdKeep is how often a server says it still holds its relays, and
	// checks they are still trusted.
	holdKeep = 10 * time.Second
	// holdStale is how long a claim goes unkept before it counts as left
	// by a stopped server.
	holdStale = 30 * time.Second
	// mailStale is how long a message may wait for a server that never
	// takes it.
	mailStale = 2 * time.Minute
)

const (
	// seenFor is how long a message's ID is remembered, so it is delivered
	// once however often it arrives.
	seenFor = 10 * time.Minute
	// maxSeen bounds the IDs remembered.
	maxSeen = 1 << 16
	// partsInFlight is how many parts of one message are sent at once.
	partsInFlight = 16
)

// fromServer is the peer a message arrives from when another server
// passed it on through the database.
const fromServer = "server"

// Options set up a bus.
type Options struct {
	// Name is this node's name: Main on the main node, a relay's name on a
	// relay.
	Name string
	// Server is this server's ID on the main node, when servers share a
	// database; with it, the server's own name is Main, a dot and Server.
	Server string
	// Trust is what pairing issued.
	Trust Trust
	// Cluster, on the main node, is the database the servers share. Nil
	// is a server on its own.
	Cluster Cluster
	// Retry is how long a send waits for an acknowledgment before sending
	// again; zero is 2 seconds.
	Retry time.Duration
	// Timeout is how long a send with no deadline of its own tries before
	// giving up; zero is a minute.
	Timeout time.Duration
	// Poll is how often messages other servers left are read, and BoneMesh
	// links are looked at; zero is a second.
	Poll time.Duration
}

// Handler handles a message of one type. It runs on its own goroutine,
// with a context that ends when the bus closes.
type Handler func(ctx context.Context, m Message)

// link is one way to send to a peer, over one transport.
type link interface {
	// send hands a message to the transport.
	send(data []byte) error
	// close ends the link, saying why, where the transport can.
	close(reason string)
	// check says whether the peer is still trusted.
	check() error
}

// holding is a relay a server holds over a transport.
type holding struct{ node, transport string }

// Bus is one node's end of the relay bus.
type Bus struct {
	opts Options
	self string
	ctx  context.Context
	stop context.CancelFunc

	mu       sync.Mutex
	links    map[string]map[string]link
	handlers map[string]Handler
	// acks are the sends waiting for an ack or a nak, by ID.
	acks map[string]chan error
	// replies are the requests waiting for a reply, by ID.
	replies map[string]chan Message
	// seen is the IDs delivered lately, and when; seenOrder is them,
	// oldest first.
	seen      map[string]time.Time
	seenOrder []string
	parts     *reassembler
	held      map[holding]bool
	// changed is closed, and replaced, whenever a link comes up, so sends
	// waiting for one try at once.
	changed chan struct{}
}

// New makes a bus. It routes nothing until a transport links it to a
// peer; Run keeps it up to date while servers share a database.
func New(opts Options) *Bus {
	if opts.Retry <= 0 {
		opts.Retry = 2 * time.Second
	}
	if opts.Timeout <= 0 {
		opts.Timeout = time.Minute
	}
	if opts.Poll <= 0 {
		opts.Poll = time.Second
	}
	if opts.Trust == nil {
		opts.Trust = Unpaired{}
	}
	self := opts.Name
	if opts.Name == Main && opts.Server != "" {
		self = Main + "." + opts.Server
	}
	ctx, stop := context.WithCancel(context.Background())
	return &Bus{opts: opts, self: self, ctx: ctx, stop: stop,
		links: map[string]map[string]link{}, handlers: map[string]Handler{},
		acks: map[string]chan error{}, replies: map[string]chan Message{},
		seen: map[string]time.Time{}, parts: newReassembler(), held: map[holding]bool{},
		changed: make(chan struct{})}
}

// Self is this node's own name: a relay's name, or the server's on the
// main node.
func (b *Bus) Self() string { return b.self }

// main reports whether this is the main node.
func (b *Bus) main() bool { return b.opts.Name == Main }

// here reports whether a message to name is for this node.
func (b *Bus) here(name string) bool {
	return name == b.self || (b.main() && name == Main)
}

// Handle has h handle messages of a type.
func (b *Bus) Handle(typ string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[typ] = h
}

// Close closes every link and ends the sends under way.
func (b *Bus) Close() {
	b.stop()
	b.mu.Lock()
	var all []link
	for _, ls := range b.links {
		for _, l := range ls {
			all = append(all, l)
		}
	}
	b.mu.Unlock()
	for _, l := range all {
		l.close("the node is stopping")
	}
}

// Transports lists the transports this node has a link to a peer over,
// BoneMesh first.
func (b *Bus) Transports(peer string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, t := range []string{BoneMesh, WebSocket} {
		if b.links[peer][t] != nil {
			out = append(out, t)
		}
	}
	return out
}

// Drop closes a peer's links, saying why: as when a relay is revoked.
// BoneMesh sessions cannot be closed from outside; the peer's messages are
// refused once its trust is.
func (b *Bus) Drop(peer, reason string) {
	b.mu.Lock()
	var ls []link
	for _, l := range b.links[peer] {
		ls = append(ls, l)
	}
	b.mu.Unlock()
	for _, l := range ls {
		l.close(reason)
	}
}

// linkUp adds a link to a peer, replacing any it had over that transport,
// and on the main node claims the relay for this server.
func (b *Bus) linkUp(transport, peer string, l link) {
	b.mu.Lock()
	if b.links[peer] == nil {
		b.links[peer] = map[string]link{}
	}
	old := b.links[peer][transport]
	b.links[peer][transport] = l
	close(b.changed)
	b.changed = make(chan struct{})
	claim := b.main() && b.opts.Cluster != nil
	if claim {
		b.held[holding{peer, transport}] = true
	}
	b.mu.Unlock()
	if old != nil && old != l {
		old.close("replaced by a newer connection")
	}
	if claim {
		if err := b.opts.Cluster.HoldRelay(peer, transport, b.opts.Server); err != nil {
			log.Printf("relays: claiming %s over %s: %v", peer, transport, err)
		}
	}
}

// linkDown removes a link, if it is still the peer's over its transport,
// and lets go of the claim on the relay.
func (b *Bus) linkDown(transport, peer string, l link) {
	b.mu.Lock()
	current := b.links[peer][transport] == l
	if current {
		delete(b.links[peer], transport)
		if len(b.links[peer]) == 0 {
			delete(b.links, peer)
		}
	}
	release := current && b.held[holding{peer, transport}]
	delete(b.held, holding{peer, transport})
	b.mu.Unlock()
	if release && b.opts.Cluster != nil {
		if err := b.opts.Cluster.ReleaseRelay(peer, transport, b.opts.Server); err != nil {
			log.Printf("relays: releasing %s over %s: %v", peer, transport, err)
		}
	}
}

// Run keeps the bus up to date until ctx ends, then closes it: it reads
// the messages other servers leave this one, keeps this server's claims on
// its relays, and checks its peers are still trusted.
func (b *Bus) Run(ctx context.Context) {
	defer b.Close()
	poll, keep := time.NewTicker(b.opts.Poll), time.NewTicker(holdKeep)
	defer poll.Stop()
	defer keep.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.ctx.Done():
			return
		case <-poll.C:
			b.readMail()
		case <-keep.C:
			b.keep()
		}
	}
}

// readMail routes the messages other servers left this one.
func (b *Bus) readMail() {
	if b.opts.Cluster == nil {
		return
	}
	mail, err := b.opts.Cluster.TakeRelayMail(b.opts.Server, time.Now().Add(-mailStale))
	if err != nil {
		log.Printf("relays: reading messages from other servers: %v", err)
		return
	}
	for _, data := range mail {
		b.receive(fromServer, fromServer, data)
	}
}

// keep closes the links of peers no longer trusted, and keeps this
// server's claims, giving up those another server has taken.
func (b *Bus) keep() {
	type peerLink struct {
		peer, transport string
		l               link
	}
	b.mu.Lock()
	var all []peerLink
	for peer, ls := range b.links {
		for t, l := range ls {
			all = append(all, peerLink{peer, t, l})
		}
	}
	b.mu.Unlock()
	for _, pl := range all {
		if err := pl.l.check(); err != nil {
			pl.l.close("the relay is no longer trusted")
			b.linkDown(pl.transport, pl.peer, pl.l)
			continue
		}
		b.mu.Lock()
		held := b.held[holding{pl.peer, pl.transport}]
		b.mu.Unlock()
		if !held || b.opts.Cluster == nil {
			continue
		}
		still, err := b.opts.Cluster.KeepRelay(pl.peer, pl.transport, b.opts.Server)
		if err != nil {
			log.Printf("relays: keeping %s over %s: %v", pl.peer, pl.transport, err)
			continue
		}
		if !still {
			b.mu.Lock()
			delete(b.held, holding{pl.peer, pl.transport})
			b.mu.Unlock()
			pl.l.close("another server holds this relay now")
		}
	}
}

// Send sends a message and waits until it arrives, or ctx ends. Without a
// deadline it tries for the bus's timeout. It returns the message's ID.
func (b *Bus) Send(ctx context.Context, to, typ string, body any) (string, error) {
	m, err := b.message(to, typ, "", body)
	if err != nil {
		return "", err
	}
	return m.ID, b.send(ctx, m)
}

// Request sends a message and waits for the reply to it.
func (b *Bus) Request(ctx context.Context, to, typ string, body any) (Message, error) {
	m, err := b.message(to, typ, "", body)
	if err != nil {
		return Message{}, err
	}
	ch := make(chan Message, 1)
	b.mu.Lock()
	b.replies[m.ID] = ch
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.replies, m.ID)
		b.mu.Unlock()
	}()
	ctx, cancel := b.deadline(ctx)
	defer cancel()
	if err := b.send(ctx, m); err != nil {
		return Message{}, err
	}
	select {
	case reply := <-ch:
		return reply, nil
	case <-ctx.Done():
		return Message{}, fmt.Errorf("%s did not answer: %w", to, ctx.Err())
	case <-b.ctx.Done():
		return Message{}, ErrClosed
	}
}

// Reply answers a message.
func (b *Bus) Reply(ctx context.Context, to Message, typ string, body any) error {
	m, err := b.message(to.From, typ, to.ID, body)
	if err != nil {
		return err
	}
	return b.send(ctx, m)
}

// message makes a message from this node.
func (b *Bus) message(to, typ, re string, body any) (Message, error) {
	m := Message{V: Version, ID: newID(), Type: typ, From: b.self, To: to, Re: re}
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return Message{}, err
		}
		m.Body = data
	}
	return m, nil
}

// deadline gives ctx the bus's timeout, unless it has a deadline.
func (b *Bus) deadline(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, b.opts.Timeout)
}

// send sends a message, in parts if it is large, and waits until every
// part is acknowledged.
func (b *Bus) send(ctx context.Context, m Message) error {
	parts, err := split(m)
	if err != nil {
		return err
	}
	ctx, cancel := b.deadline(ctx)
	defer cancel()
	if len(parts) == 1 {
		return b.sendOne(ctx, parts[0])
	}
	errs := make(chan error, len(parts))
	turns := make(chan struct{}, partsInFlight)
	for _, p := range parts {
		go func() {
			select {
			case turns <- struct{}{}:
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}
			err := b.sendOne(ctx, p)
			<-turns
			if err != nil {
				cancel()
			}
			errs <- err
		}()
	}
	var first error
	for range parts {
		if err := <-errs; err != nil && (first == nil || errors.Is(first, context.Canceled)) {
			first = err
		}
	}
	return first
}

// sendOne sends one message until it is acknowledged or refused, sending
// again every retry, and over the next transport each time, so a link
// that went quiet is passed over.
func (b *Bus) sendOne(ctx context.Context, m Message) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	ch := make(chan error, 1)
	b.mu.Lock()
	b.acks[m.ID] = ch
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.acks, m.ID)
		b.mu.Unlock()
	}()
	for attempt := 0; ; attempt++ {
		b.mu.Lock()
		changed := b.changed
		b.mu.Unlock()
		if err := b.route(m.To, data, attempt, false); err != nil {
			var refused *Refused
			if errors.As(err, &refused) {
				return err
			}
		}
		timer := time.NewTimer(b.opts.Retry)
		select {
		case err := <-ch:
			timer.Stop()
			return err
		case <-timer.C:
		case <-changed:
			timer.Stop()
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w: %s, after %d tries: %v", ErrUnreachable, m.To, attempt+1, ctx.Err())
		case <-b.ctx.Done():
			timer.Stop()
			return ErrClosed
		}
	}
}

// errNoLink is a send no link could take.
var errNoLink = errors.New("no link is up")

// route sends a message on toward to: over a link to it, or to the main
// node from a relay, or through the database to the server holding it. A
// message another server passed on is not passed on again. A node the
// main node cannot reach is a *Refused.
func (b *Bus) route(to string, data []byte, attempt int, passed bool) error {
	if !b.main() {
		return b.over(Main, data, attempt)
	}
	if isMain(to) {
		server := strings.TrimPrefix(to, Main+".")
		if passed || b.opts.Cluster == nil || server == to {
			return &Refused{Reason: to + " cannot be reached"}
		}
		return b.opts.Cluster.PostRelayMail(server, data)
	}
	if err := b.over(to, data, attempt); !errors.Is(err, errNoLink) {
		return err
	}
	offline := &Refused{Reason: "relay " + to + " is not connected"}
	if passed || b.opts.Cluster == nil {
		return offline
	}
	server, err := b.opts.Cluster.RelayHolder(to, time.Now().Add(-holdStale))
	if err != nil {
		return err
	}
	if server == "" || server == b.opts.Server {
		return offline
	}
	return b.opts.Cluster.PostRelayMail(server, data)
}

// over sends over a link to peer, preferring BoneMesh, starting the
// attempt-th time with the next transport up, and trying the others if
// that one fails.
func (b *Bus) over(peer string, data []byte, attempt int) error {
	b.mu.Lock()
	var up []link
	for _, t := range []string{BoneMesh, WebSocket} {
		if l := b.links[peer][t]; l != nil {
			up = append(up, l)
		}
	}
	b.mu.Unlock()
	if len(up) == 0 {
		return errNoLink
	}
	var err error
	for i := range up {
		if err = up[(attempt+i)%len(up)].send(data); err == nil {
			return nil
		}
	}
	return err
}

// receive takes a message a transport brought from peer.
func (b *Bus) receive(transport, peer string, data []byte) {
	var m Message
	if err := json.Unmarshal(data, &m); err != nil {
		log.Printf("relays: a message from %s over %s is not JSON: %v", peer, transport, err)
		return
	}
	// A relay speaks only for itself; the main node, for anyone.
	if b.main() && peer != fromServer && m.From != peer {
		log.Printf("relays: %s sent a message as %s over %s, which is refused", peer, m.From, transport)
		return
	}
	if !b.main() && peer != Main {
		return
	}
	if m.V != Version {
		if m.Type != TypeAck && m.Type != TypeNak && m.From != "" && m.ID != "" {
			b.control(TypeNak, m, fmt.Sprintf("relay protocol version %d is not spoken here: this node speaks version %d", m.V, Version))
		}
		return
	}
	if err := m.check(); err != nil {
		log.Printf("relays: a message from %s over %s is refused: %v", peer, transport, err)
		return
	}
	if !b.here(m.To) {
		if !b.main() {
			return
		}
		if err := b.route(m.To, data, 0, peer == fromServer); err != nil {
			var refused *Refused
			if errors.As(err, &refused) && m.Type != TypeAck && m.Type != TypeNak {
				b.control(TypeNak, m, refused.Reason)
			}
		}
		return
	}
	b.accept(m)
}

// accept takes a message for this node: an ack or a nak for a send under
// way, or a message, acknowledged and handled once.
func (b *Bus) accept(m Message) {
	switch m.Type {
	case TypeAck, TypeNak:
		b.mu.Lock()
		ch := b.acks[m.Re]
		b.mu.Unlock()
		var result error
		if m.Type == TypeNak {
			result = &Refused{Reason: m.Reason}
		}
		if ch != nil {
			select {
			case ch <- result:
			default:
			}
		}
		return
	}
	b.mu.Lock()
	handler, waiting := b.handlers[m.Type], b.replies[m.Re]
	b.mu.Unlock()
	if handler == nil && (m.Re == "" || waiting == nil) {
		b.control(TypeNak, m, fmt.Sprintf("%s does not handle %s messages", b.self, m.Type))
		return
	}
	whole, deliver, err := b.take(m)
	if err != nil {
		b.control(TypeNak, m, err.Error())
		return
	}
	b.control(TypeAck, m, "")
	if !deliver {
		return
	}
	m = whole
	if waiting != nil {
		select {
		case waiting <- m:
		default:
		}
		return
	}
	go handler(b.ctx, m)
}

// take notes a message as arrived, once, however many times and over
// however many transports it comes. It returns the message to deliver,
// true: the message itself the first time it comes, or the whole of a
// message split into parts once its last part comes.
func (b *Bus) take(m Message) (Message, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.delivered(m.ID) {
		return Message{}, false, nil
	}
	if m.Part == nil {
		b.remember(m.ID)
		return m, true, nil
	}
	whole, done, err := b.parts.offer(m, time.Now().UnixMilli())
	if err != nil {
		return Message{}, false, err
	}
	b.remember(m.ID)
	if !done || b.delivered(whole.ID) {
		return Message{}, false, nil
	}
	b.remember(whole.ID)
	return whole, true, nil
}

// delivered reports whether a message's ID was delivered lately. The
// caller holds b.mu.
func (b *Bus) delivered(id string) bool {
	at, ok := b.seen[id]
	return ok && time.Since(at) < seenFor
}

// remember notes a message's ID as delivered, forgetting the oldest past
// seenFor or maxSeen. The caller holds b.mu.
func (b *Bus) remember(id string) {
	now := time.Now()
	for len(b.seenOrder) > 0 && (len(b.seenOrder) >= maxSeen || now.Sub(b.seen[b.seenOrder[0]]) >= seenFor) {
		delete(b.seen, b.seenOrder[0])
		b.seenOrder = b.seenOrder[1:]
	}
	b.seen[id] = now
	b.seenOrder = append(b.seenOrder, id)
}

// control answers a message with an ack, or a nak saying why, once: a
// lost one is answered again when the message is sent again.
func (b *Bus) control(typ string, to Message, reason string) {
	m := Message{V: Version, ID: newID(), Type: typ, From: b.self, To: to.From, Re: to.ID, Reason: reason}
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	if b.here(m.To) {
		b.accept(m)
		return
	}
	_ = b.route(m.To, data, 0, false)
}

package relay

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"forge.axonibyte.com/axonibyte/bonemesh/go/v3/canon"
	"forge.axonibyte.com/axonibyte/bonemesh/go/v3/cert"
	"forge.axonibyte.com/axonibyte/bonemesh/go/v3/crypto"
	"forge.axonibyte.com/axonibyte/bonemesh/go/v3/node"
)

// BoneMesh links are mutually authenticated and encrypted, but each hop
// reads what it routes, and a node is told what a message carries, not
// which peer sent it. So over BoneMesh each message is signed by the node
// that sends it on, with the identity key its certificate binds to its
// label, and checked against the key pairing recorded for that node. A
// relay cannot speak as another, nor reach one except through the main
// node, whose signature relays alone accept.

// signedPrefix starts what a node signs, so the signature means nothing
// elsewhere.
const signedPrefix = "bam relay 1\n"

// signed is what the node by signs to send data on to the node to.
func signed(by, to string, data string) []byte {
	return []byte(signedPrefix + by + "\n" + to + "\n" + data)
}

// BoneMeshOptions set up a node's BoneMesh transport.
type BoneMeshOptions struct {
	// Port is the port the node listens on; 0 picks one. The main node
	// listens on [relay] bonemesh_port.
	Port int
	// Dial, on a relay, is the main node's BoneMesh address as host:port,
	// dialed and dialed again whenever the link drops.
	Dial string
}

// Mesh is a node's BoneMesh transport.
type Mesh struct {
	b    *Bus
	node *node.Node
	cfg  node.Config
	dial string
	done chan struct{}
	stop sync.Once

	mu    sync.Mutex
	links map[string]*meshLink
	// dialing is set while a dial is under way, so dials do not pile up.
	dialing atomic.Bool
}

// meshLink is a BoneMesh session with a peer.
type meshLink struct {
	m    *Mesh
	peer string
}

func (l *meshLink) send(data []byte) error {
	sig, err := sign(l.m.cfg.IDPrivate, signed(l.m.cfg.Label, l.peer, string(data)))
	if err != nil {
		return err
	}
	payload := map[string]any{"bam": string(data), "by": l.m.cfg.Label, "sig": base64.StdEncoding.EncodeToString(sig)}
	if !l.m.node.Send(l.peer, payload) {
		return errors.New("BoneMesh could not take the message")
	}
	return nil
}

// close leaves the session as it is: the Go node cannot close one from
// outside. A peer no longer trusted is refused by its signature.
func (l *meshLink) close(string) {}

func (l *meshLink) check() error {
	_, err := l.m.b.opts.Trust.IdentityKey(l.peer)
	return err
}

// sign signs msg with an ML-DSA-65 private key, refusing a key that does
// not read rather than panicking as BoneMesh does.
func sign(private, msg []byte) (sig []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("the BoneMesh identity key does not read: %v", r)
		}
	}()
	return crypto.MLDSA65Sign(private, msg), nil
}

// StartBoneMesh starts the node's BoneMesh transport, with the identity
// its Trust holds: the main node listening for relays, or a relay dialing
// the main node.
func StartBoneMesh(b *Bus, opts BoneMeshOptions) (*Mesh, error) {
	cfg, err := b.opts.Trust.Mesh()
	if err != nil {
		return nil, err
	}
	if cfg.Label != b.opts.Name {
		return nil, fmt.Errorf("the BoneMesh certificate is for %q, not %q", cfg.Label, b.opts.Name)
	}
	if _, err := sign(cfg.IDPrivate, []byte(signedPrefix)); err != nil {
		return nil, err
	}
	n, err := node.Start(cfg, opts.Port)
	if err != nil {
		return nil, fmt.Errorf("listening for BoneMesh on port %d: %w", opts.Port, err)
	}
	m := &Mesh{b: b, node: n, cfg: cfg, dial: opts.Dial, done: make(chan struct{}), links: map[string]*meshLink{}}
	go m.read(n.AddListener())
	go m.watch()
	return m, nil
}

// Port is the port the node listens on.
func (m *Mesh) Port() int { return m.node.Port() }

// Close stops the transport and its node.
func (m *Mesh) Close() {
	m.stop.Do(func() {
		close(m.done)
		_ = m.node.Kill()
		m.mu.Lock()
		links := m.links
		m.links = map[string]*meshLink{}
		m.mu.Unlock()
		for peer, l := range links {
			m.b.linkDown(BoneMesh, peer, l)
		}
	})
}

// read takes what BoneMesh delivers, checks who signed it, and hands it to
// the bus as from them.
func (m *Mesh) read(in <-chan map[string]any) {
	for {
		select {
		case <-m.done:
			return
		case <-m.b.ctx.Done():
			m.Close()
			return
		case payload := <-in:
			data, _ := payload["bam"].(string)
			by, _ := payload["by"].(string)
			sig, err := base64.StdEncoding.DecodeString(fmt.Sprint(payload["sig"]))
			if data == "" || by == "" || err != nil {
				continue
			}
			if !m.b.main() && by != Main {
				continue
			}
			key, err := m.b.opts.Trust.IdentityKey(by)
			if err != nil {
				log.Printf("relays: a BoneMesh message from %s is refused: %v", by, err)
				continue
			}
			if !crypto.MLDSA65Verify(key, signed(by, m.cfg.Label, data), sig) {
				log.Printf("relays: a BoneMesh message signed as %s does not verify, and is refused", by)
				continue
			}
			m.b.receive(BoneMesh, by, []byte(data))
		}
	}
}

// watch follows BoneMesh's sessions, linking the bus to each trusted peer
// while its session lasts, and on a relay dials the main node whenever it
// has no session with it.
func (m *Mesh) watch() {
	tick := time.NewTicker(m.b.opts.Poll / 4)
	defer tick.Stop()
	wait, next := time.Second, time.Time{}
	for {
		select {
		case <-m.done:
			return
		case <-tick.C:
		}
		sessions := m.node.SessionInfo()
		m.mu.Lock()
		var up, down []*meshLink
		for peer := range sessions {
			if m.links[peer] != nil || !m.wanted(peer) {
				continue
			}
			l := &meshLink{m: m, peer: peer}
			m.links[peer] = l
			up = append(up, l)
		}
		for peer, l := range m.links {
			if _, ok := sessions[peer]; !ok {
				delete(m.links, peer)
				down = append(down, l)
			}
		}
		m.mu.Unlock()
		for _, l := range down {
			m.b.linkDown(BoneMesh, l.peer, l)
		}
		for _, l := range up {
			m.b.linkUp(BoneMesh, l.peer, l)
		}
		if _, linked := sessions[Main]; m.dial != "" && !linked && time.Now().After(next) && m.dialing.CompareAndSwap(false, true) {
			go func() {
				defer m.dialing.Store(false)
				if err := m.connect(); err != nil {
					log.Printf("relays: the BoneMesh link to %s: %v", m.dial, err)
				}
			}()
			next = time.Now().Add(wait)
			wait = min(wait*2, wsRedial)
		} else if linked {
			wait = time.Second
		}
	}
}

// wanted reports whether the bus should link to a BoneMesh peer: on the
// main node a trusted relay, and on a relay the main node.
func (m *Mesh) wanted(peer string) bool {
	if !m.b.main() {
		return peer == Main
	}
	if !ValidRelayName(peer) {
		return false
	}
	_, err := m.b.opts.Trust.IdentityKey(peer)
	return err == nil
}

// connect dials the main node once.
func (m *Mesh) connect() error {
	host, port, err := net.SplitHostPort(m.dial)
	if err != nil {
		return err
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("%q has no port", m.dial)
	}
	peer, err := m.node.Connect(host, p)
	if err != nil {
		return err
	}
	if peer != Main {
		return fmt.Errorf("the node at %s is %q, not the main node", m.dial, peer)
	}
	return nil
}

// NewMeshRoot makes a mesh root: an ML-DSA-87 key pair, whose private half
// issues certificates and whose public half every node pins.
func NewMeshRoot() (public, private []byte) {
	return crypto.MLDSA87Generate()
}

// NewIdentity makes a node's identity: an ML-DSA-65 key pair. The private
// half never leaves the node.
func NewIdentity() (public, private []byte) {
	return crypto.MLDSA65Generate()
}

// IssueCertificate issues a membership certificate for a node's identity
// key, valid from notBefore until notAfter, signed by the mesh root.
func IssueCertificate(rootPrivate []byte, mesh, label string, identity []byte, notBefore, notAfter time.Time) (map[string]any, error) {
	c := cert.Build(mesh, label, identity, notBefore.Unix(), notAfter.Unix())
	pre, err := canon.Canonicalize(c)
	if err != nil {
		return nil, err
	}
	var sig []byte
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("the mesh root's key does not read: %v", r)
			}
		}()
		sig = crypto.MLDSA87Sign(rootPrivate, []byte(pre))
	}()
	if err != nil {
		return nil, err
	}
	c["sig"] = base64.StdEncoding.EncodeToString(sig)
	return c, nil
}

// CertificateKey is the identity key a certificate binds, once the
// certificate is checked against the mesh root, the mesh and the time.
func CertificateKey(c map[string]any, rootPublic []byte, mesh string, at time.Time) ([]byte, error) {
	if err := cert.Verify(c, rootPublic, mesh, at.Unix()); err != nil {
		return nil, err
	}
	return cert.IdentityKey(c)
}

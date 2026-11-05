// Package relay is the relay bus (docs/relays.md): how the main BAM! and
// its relays, BAM! run beside printers and devices on a local network,
// talk to each other.
//
// One protocol runs over two transports, BoneMesh and WebSocket, either or
// both. A transport carries messages to the peers it links this node with;
// everything above that is the bus's, written once: messages and their
// version, addressing by node, acknowledgments and retries, splitting large
// messages into parts, and delivering each message once.
//
// The main node routes between transports, so a relay on WebSocket and a
// relay on BoneMesh reach each other through it. When several servers share
// a database, the server holding a relay over a transport claims it there,
// and the others route through that server.
package relay

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"forge.axonibyte.com/axonibyte/bonemesh/go/v3/node"
)

// Main is the main node's name. A relay addresses the main node by it,
// whichever server it reaches; a server's own name is Main, a dot, and the
// server's ID, so a reply finds the server that asked.
const Main = "main"

// The transports.
const (
	BoneMesh  = "bonemesh"
	WebSocket = "websocket"
)

// nodeName is a relay's name: lowercase letters, digits and hyphens, as
// BoneMesh compares labels without case.
var nodeName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ValidRelayName reports whether name may name a relay: lowercase letters,
// digits and hyphens, at most 63, and not Main.
func ValidRelayName(name string) bool {
	return name != Main && nodeName.MatchString(name)
}

// isMain reports whether name is the main node or one of its servers.
func isMain(name string) bool {
	return name == Main || strings.HasPrefix(name, Main+".")
}

// Errors a send ends with.
var (
	// ErrUnreachable is returned when no transport reached the node before
	// the send gave up.
	ErrUnreachable = errors.New("the node could not be reached")
	// ErrUntrusted is returned by a Trust for a credential or a node that
	// is not paired, or no longer is.
	ErrUntrusted = errors.New("not a paired relay")
	// ErrNotPaired is returned by a Trust that has no BoneMesh identity
	// for this node yet.
	ErrNotPaired = errors.New("this node has no BoneMesh identity yet: pairing makes one")
	// ErrClosed is returned by a bus that was closed.
	ErrClosed = errors.New("the relay bus is closed")
)

// Refused is a send the other end, or the main node routing it, answered
// with a reason: a node that is not connected, a type it does not handle,
// or a version it does not speak.
type Refused struct {
	Reason string
}

func (e *Refused) Error() string { return e.Reason }

// Trust is what pairing (#229) issues: each relay's WebSocket credential
// and BoneMesh certificate, and this node's own BoneMesh identity. Trust
// does not depend on the transport: a relay is the same paired identity
// over either, and revoking it ends both.
type Trust interface {
	// Credential says which relay holds a WebSocket credential, or returns
	// ErrUntrusted.
	Credential(secret string) (string, error)
	// IdentityKey is a node's ML-DSA-65 public key, from the certificate
	// pairing issued it, which signs its messages over BoneMesh. It returns
	// ErrUntrusted for a node that is not paired, or was revoked.
	IdentityKey(node string) ([]byte, error)
	// Mesh is this node's own BoneMesh identity: its label, the mesh, the
	// root's public key, its certificate and its private key. It returns
	// ErrNotPaired while there is none.
	Mesh() (node.Config, error)
}

// Unpaired trusts no relay. It is the main node's trust until pairing
// gives it relays.
type Unpaired struct{}

// Credential refuses every credential.
func (Unpaired) Credential(string) (string, error) { return "", ErrUntrusted }

// IdentityKey refuses every node.
func (Unpaired) IdentityKey(string) ([]byte, error) { return nil, ErrUntrusted }

// Mesh has no identity.
func (Unpaired) Mesh() (node.Config, error) { return node.Config{}, ErrNotPaired }

// Cluster is the database the servers of one BAM! share: which server
// holds each relay over each transport, and the messages servers pass each
// other for the relays they hold. *store.Store is one.
type Cluster interface {
	// HoldRelay records that server holds node over transport, taking it
	// from any server that held it before.
	HoldRelay(node, transport, server string) error
	// KeepRelay says server still holds node over transport. It reports
	// false when another server has taken it since.
	KeepRelay(node, transport, server string) (bool, error)
	// ReleaseRelay lets go of node over transport, if server holds it.
	ReleaseRelay(node, transport, server string) error
	// RelayHolder is the server holding node, seen since staleBefore,
	// over BoneMesh if one does and otherwise over WebSocket; "" when none
	// does.
	RelayHolder(node string, staleBefore time.Time) (string, error)
	// PostRelayMail leaves a message for server to route.
	PostRelayMail(server string, message []byte) error
	// TakeRelayMail takes the messages left for server, oldest first, and
	// drops any left before staleBefore for a server that never took them.
	TakeRelayMail(server string, staleBefore time.Time) ([][]byte, error)
}

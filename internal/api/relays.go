package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"forge.axonibyte.com/axonibyte/bonemesh/go/v3/node"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/relay"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/google/uuid"
)

// Relays (docs/relays.md): BAM! run beside printers and devices on a local
// network, linked to this one over BoneMesh, WebSocket, or both, as
// [relay] turns them on. With both off, there is no bus, and relays are
// off on this server.
//
// An admin adds a relay, which gives a one-time code; the relay pairs with
// it, and is given a BoneMesh certificate and a WebSocket credential. The
// mesh root that issues certificates is this BAM!'s, kept in the database
// sealed with [auth] secret_key.

var (
	// relayCodeFor is how long a relay's code works.
	relayCodeFor = time.Hour
	// relayCertFor is how long a relay's certificate is valid; it renews
	// it while it is paired.
	relayCertFor = 7 * 24 * time.Hour
	// mainCertFor is how long the main node's own certificate is valid;
	// the server starts BoneMesh again with a new one a week before.
	mainCertFor = 30 * 24 * time.Hour
	// relaySweep is how often a server checks which relays are online,
	// and whether the mesh root changed.
	relaySweep = 10 * time.Second
	// relayStale is how long a relay's claim lasts unkept, as the bus's
	// does.
	relayStale = 30 * time.Second
)

// newRelayBus makes the server's end of the relay bus, or nil when both
// transports are off. Each server has an ID of its own, so the servers
// sharing the database can tell which holds which relay.
func (s *Server) newRelayBus() *relay.Bus {
	if !s.cfg.Relay.BoneMesh && !s.cfg.Relay.WebSocket {
		return nil
	}
	b := relay.New(relay.Options{Name: relay.Main, Server: uuid.NewString(), Trust: relayTrust{s}, Cluster: s.store,
		Changed: func(peer string) { go s.relayChanged(peer) }})
	b.Handle(relay.TypeStatus, s.relayStatus)
	return b
}

// relayTrust is the main node's trust: the relays paired, from the
// database, and its own BoneMesh identity, from the mesh root.
type relayTrust struct{ s *Server }

func (t relayTrust) Credential(secret string) (string, error) {
	if !strings.HasPrefix(secret, relay.CredentialPrefix) {
		return "", relay.ErrUntrusted
	}
	r, err := t.s.store.RelayByCredential(store.RelayCodeKey(secret))
	if errors.Is(err, store.ErrRelayNotFound) {
		return "", relay.ErrUntrusted
	}
	if err != nil {
		return "", err
	}
	return r.Name, nil
}

func (t relayTrust) IdentityKey(name string) ([]byte, error) {
	key, err := t.s.store.RelayIdentityKey(name)
	if errors.Is(err, store.ErrRelayNotFound) {
		return nil, relay.ErrUntrusted
	}
	return key, err
}

func (t relayTrust) Mesh() (node.Config, error) {
	m, err := t.s.store.GetRelayMesh()
	if errors.Is(err, store.ErrNoRelayMesh) {
		return node.Config{}, relay.ErrNotPaired
	}
	if err != nil {
		return node.Config{}, err
	}
	private, err := t.s.sealer.Open(m.MainPrivate, mainPurpose(m.Mesh))
	if err != nil {
		return node.Config{}, fmt.Errorf("opening the main node's BoneMesh key: %w", err)
	}
	c, err := t.s.issueCertificate(m, relay.Main, m.MainPublic, time.Now().Add(mainCertFor))
	if err != nil {
		return node.Config{}, err
	}
	return node.Config{Label: relay.Main, Mesh: m.Mesh, RootPublic: m.RootPublic, Cert: c, IDPrivate: private}, nil
}

// What the mesh root's and the main node's private keys are sealed for.
func rootPurpose(mesh string) string { return "relay mesh root " + mesh }
func mainPurpose(mesh string) string { return "relay main identity " + mesh }

// errNoSecretKey says pairing needs the configuration's key.
var errNoSecretKey = errors.New("pairing relays needs [auth] secret_key in the configuration, which seals the mesh root")

// newRelayMesh makes a mesh root and the main node's identity, their
// private keys sealed, under a new mesh name.
func (s *Server) newRelayMesh() (store.RelayMesh, error) {
	if !s.sealer.HasKey() {
		return store.RelayMesh{}, errNoSecretKey
	}
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return store.RelayMesh{}, err
	}
	m := store.RelayMesh{Mesh: "bam-" + hex.EncodeToString(b)}
	var rootPrivate, mainPrivate []byte
	m.RootPublic, rootPrivate = relay.NewMeshRoot()
	m.MainPublic, mainPrivate = relay.NewIdentity()
	var err error
	if m.RootPrivate, err = s.sealer.Seal(rootPrivate, rootPurpose(m.Mesh)); err != nil {
		return m, err
	}
	m.MainPrivate, err = s.sealer.Seal(mainPrivate, mainPurpose(m.Mesh))
	return m, err
}

// relayMesh is the mesh root, made the first time a relay pairs.
func (s *Server) relayMesh() (*store.RelayMesh, error) {
	m, err := s.store.GetRelayMesh()
	if !errors.Is(err, store.ErrNoRelayMesh) {
		return m, err
	}
	fresh, err := s.newRelayMesh()
	if err != nil {
		return nil, err
	}
	m, err = s.store.CreateRelayMesh(fresh)
	if err == nil {
		s.pokeRelays()
	}
	return m, err
}

// issueCertificate issues a certificate for a node's identity key, from
// the mesh root, valid until until.
func (s *Server) issueCertificate(m *store.RelayMesh, label string, identity []byte, until time.Time) (map[string]any, error) {
	root, err := s.sealer.Open(m.RootPrivate, rootPurpose(m.Mesh))
	if err != nil {
		return nil, fmt.Errorf("opening the mesh root: %w", err)
	}
	// From a few minutes back, for clocks that differ.
	return relay.IssueCertificate(root, m.Mesh, label, identity, time.Now().Add(-5*time.Minute), until)
}

// handleRelayLive takes a relay's WebSocket connection, while relays may
// connect over WebSocket: GET /api/v1/relay/live, with the relay's
// credential as "Authorization: Relay <credential>".
func (s *Server) handleRelayLive(w http.ResponseWriter, r *http.Request) {
	if s.relays == nil || !s.cfg.Relay.WebSocket {
		writeJSONError(w, http.StatusNotFound, "Relays over WebSocket are off on this server")
		return
	}
	s.relays.ServeWebSocket(w, r)
}

// relayView is a relay as the API shows it.
type relayView struct {
	models.Relay
	// Transports are those it is linked to this server over now.
	Transports []string `json:"transports"`
}

func (s *Server) relayView(r models.Relay) relayView {
	v := relayView{Relay: r, Transports: []string{}}
	if s.relays != nil {
		if t := s.relays.Transports(r.Name); t != nil {
			v.Transports = t
		}
	}
	return v
}

// handleListRelays lists the relays, the mesh root, and which transports
// are on: GET /api/v1/relays
func (s *Server) handleListRelays(w http.ResponseWriter, r *http.Request) {
	relays, err := s.store.ListRelays()
	if err != nil {
		writeServerError(w, err, "list relays")
		return
	}
	views := make([]relayView, len(relays))
	for i, rl := range relays {
		views[i] = s.relayView(rl)
	}
	var mesh any
	if m, err := s.store.GetRelayMesh(); err == nil {
		mesh = map[string]any{"name": m.Mesh, "created_at": m.CreatedAt}
	} else if !errors.Is(err, store.ErrNoRelayMesh) {
		writeServerError(w, err, "read the mesh root")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"relays": views, "mesh": mesh, "bonemesh": s.cfg.Relay.BoneMesh,
		"websocket": s.cfg.Relay.WebSocket, "secret_key": s.sealer.HasKey(), "url": s.cfg.Server.BaseURL})
}

// relayCode answers with a relay, the code it pairs with, and the command
// that pairs it.
func (s *Server) relayCode(w http.ResponseWriter, status int, rl *models.Relay, code string) {
	writeJSON(w, status, map[string]any{"relay": s.relayView(*rl), "code": code, "expires_at": rl.CodeExpiresAt,
		"command": fmt.Sprintf("bam relay join %s %s", s.cfg.Server.BaseURL, code)})
}

// handleCreateRelay adds a relay, and gives it a code to pair with that
// works once, for an hour: POST /api/v1/relays {name}
func (s *Server) handleCreateRelay(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if !relay.ValidRelayName(in.Name) {
		writeJSONError(w, http.StatusBadRequest, "A relay's name is lowercase letters, digits and hyphens, at most 63, starting with a letter or digit, and not main")
		return
	}
	if !s.sealer.HasKey() {
		writeJSONError(w, http.StatusConflict, sentence(errNoSecretKey.Error()))
		return
	}
	code := relay.NewCode()
	rl, err := s.store.CreateRelay(in.Name, store.RelayCodeKey(relay.NormalizeCode(code)), time.Now().Add(relayCodeFor))
	var taken *store.RelayNameTakenError
	if errors.As(err, &taken) {
		writeJSONError(w, http.StatusConflict, sentence(taken.Error()))
		return
	}
	if err != nil {
		writeServerError(w, err, "add the relay")
		return
	}
	s.relayCode(w, http.StatusCreated, rl, code)
}

// sentence starts a message with a capital.
func sentence(msg string) string {
	if msg == "" {
		return msg
	}
	return strings.ToUpper(msg[:1]) + msg[1:]
}

// handleRelayCode gives a relay a new code, to pair it again: POST
// /api/v1/relays/{id}/code
func (s *Server) handleRelayCode(w http.ResponseWriter, r *http.Request) {
	if !s.sealer.HasKey() {
		writeJSONError(w, http.StatusConflict, sentence(errNoSecretKey.Error()))
		return
	}
	code := relay.NewCode()
	rl, err := s.store.SetRelayCode(r.PathValue("id"), store.RelayCodeKey(relay.NormalizeCode(code)), time.Now().Add(relayCodeFor))
	switch {
	case errors.Is(err, store.ErrRelayNotFound):
		writeJSONError(w, http.StatusNotFound, "Relay not found")
	case errors.Is(err, store.ErrRelayRevoked):
		writeJSONError(w, http.StatusConflict, sentence(err.Error()))
	case err != nil:
		writeServerError(w, err, "give the relay a code")
	default:
		s.relayCode(w, http.StatusOK, rl, code)
	}
}

// handleRevokeRelay revokes a relay: DELETE /api/v1/relays/{id}. Its
// WebSocket credential stops working, its connections to this server
// close at once and to others within seconds, and it can no longer renew.
// Its BoneMesh messages are refused at once, and its certificate until it
// expires. A relay that never paired is removed.
func (s *Server) handleRevokeRelay(w http.ResponseWriter, r *http.Request) {
	rl, err := s.store.RevokeRelay(r.PathValue("id"))
	if errors.Is(err, store.ErrRelayNotFound) {
		writeJSONError(w, http.StatusNotFound, "Relay not found")
		return
	}
	if err != nil {
		writeServerError(w, err, "revoke the relay")
		return
	}
	if s.relays != nil {
		s.relays.Drop(rl.Name, "the relay was revoked")
	}
	writeJSON(w, http.StatusOK, map[string]any{"relay": s.relayView(*rl)})
}

// handleRotateRelayMesh replaces the mesh root: POST
// /api/v1/relays/mesh/rotate. Every relay is unpaired, to pair again with
// a new code.
func (s *Server) handleRotateRelayMesh(w http.ResponseWriter, r *http.Request) {
	m, err := s.newRelayMesh()
	if errors.Is(err, errNoSecretKey) {
		writeJSONError(w, http.StatusConflict, sentence(err.Error()))
		return
	}
	if err == nil {
		err = s.store.RotateRelayMesh(m)
	}
	if err != nil {
		writeServerError(w, err, "rotate the mesh root")
		return
	}
	relays, err := s.store.ListRelays()
	if err != nil {
		writeServerError(w, err, "list relays")
		return
	}
	if s.relays != nil {
		for _, rl := range relays {
			s.relays.Drop(rl.Name, "the mesh root was rotated: pair the relay again")
		}
	}
	s.pokeRelays()
	writeJSON(w, http.StatusOK, map[string]any{"mesh": map[string]any{"name": m.Mesh}, "relays": len(relays)})
}

// tooOld refuses a relay speaking a protocol older than this BAM! takes,
// saying what to do.
func tooOld(w http.ResponseWriter, protocol int) bool {
	if protocol >= relay.MinVersion {
		return false
	}
	writeJSONError(w, http.StatusConflict, fmt.Sprintf("This relay speaks relay protocol %d, and this BAM! needs %d or later: update the relay to BAM! %s", protocol, relay.MinVersion, Version))
	return true
}

// issued is what a relay is given as it pairs or renews.
func (s *Server) issued(m *store.RelayMesh, name string, identity []byte, until time.Time) (relay.Issued, error) {
	c, err := s.issueCertificate(m, name, identity, until)
	if err != nil {
		return relay.Issued{}, err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return relay.Issued{}, err
	}
	out := relay.Issued{Name: name, Mesh: m.Mesh, RootPublic: m.RootPublic, Certificate: raw, ExpiresAt: until.UTC(),
		MainKey: m.MainPublic, WebSocket: s.cfg.Relay.WebSocket}
	if s.cfg.Relay.BoneMesh {
		if u, err := url.Parse(s.cfg.Server.BaseURL); err == nil && u.Hostname() != "" {
			out.BoneMesh = net.JoinHostPort(u.Hostname(), strconv.Itoa(s.cfg.Relay.BoneMeshPort))
		}
	}
	return out, nil
}

// handleJoinRelay pairs a relay with its code: POST /api/v1/relays/join
// {code, identity_key, version, protocol}. The relay sends only the
// public half of its identity key, and is given a certificate for it and
// a WebSocket credential. A code pairs once.
func (s *Server) handleJoinRelay(w http.ResponseWriter, r *http.Request) {
	if s.relays == nil {
		writeJSONError(w, http.StatusNotFound, "Relays are off on this server: turn on [relay] websocket or bonemesh")
		return
	}
	var in relay.JoinRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	if tooOld(w, in.Protocol) {
		return
	}
	if !relay.ValidIdentityKey(in.IdentityKey) {
		writeJSONError(w, http.StatusBadRequest, "identity_key must be the public half of an ML-DSA-65 key, as base64")
		return
	}
	m, err := s.relayMesh()
	if errors.Is(err, errNoSecretKey) {
		writeJSONError(w, http.StatusConflict, sentence(err.Error()))
		return
	}
	if err != nil {
		writeServerError(w, err, "read the mesh root")
		return
	}
	credential, until := relay.NewCredential(), time.Now().Add(relayCertFor)
	rl, err := s.store.JoinRelay(store.RelayJoin{CodeHash: store.RelayCodeKey(relay.NormalizeCode(in.Code)), IdentityKey: in.IdentityKey,
		CredentialHash: store.RelayCodeKey(credential), CertExpiresAt: until, Version: in.Version, Address: ClientAddress(r)})
	if errors.Is(err, store.ErrRelayCodeInvalid) || errors.Is(err, store.ErrRelayCodeExpired) {
		writeJSONError(w, http.StatusForbidden, sentence(err.Error()))
		return
	}
	if err != nil {
		writeServerError(w, err, "pair the relay")
		return
	}
	// A relay paired again, as on a new machine, is no longer the one
	// linked.
	s.relays.Drop(rl.Name, "the relay was paired again")
	out, err := s.issued(m, rl.Name, in.IdentityKey, until)
	if err != nil {
		writeServerError(w, err, "issue the relay's certificate")
		return
	}
	out.Credential = credential
	writeJSON(w, http.StatusOK, out)
}

// handleRenewRelay issues a paired relay a new certificate, with its
// credential: POST /api/v1/relays/renew {version, protocol}. A revoked
// relay is refused.
func (s *Server) handleRenewRelay(w http.ResponseWriter, r *http.Request) {
	scheme, secret, _ := strings.Cut(r.Header.Get("Authorization"), " ")
	if !strings.EqualFold(scheme, relay.CredentialScheme) {
		writeJSONError(w, http.StatusUnauthorized, "Renew with the relay's credential, as Authorization: Relay <credential>")
		return
	}
	rl, err := s.store.RelayByCredential(store.RelayCodeKey(strings.TrimSpace(secret)))
	if errors.Is(err, store.ErrRelayNotFound) {
		writeJSONError(w, http.StatusUnauthorized, "This credential is not a paired relay's, or the relay was revoked")
		return
	}
	if err != nil {
		writeServerError(w, err, "read the relay")
		return
	}
	var in relay.RenewRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	if tooOld(w, in.Protocol) {
		return
	}
	m, err := s.store.GetRelayMesh()
	if err != nil {
		writeServerError(w, err, "read the mesh root")
		return
	}
	key, err := s.store.RelayIdentityKey(rl.Name)
	if err != nil {
		writeServerError(w, err, "read the relay's key")
		return
	}
	until := time.Now().Add(relayCertFor)
	out, err := s.issued(m, rl.Name, key, until)
	if err == nil {
		err = s.store.RenewRelay(rl.ID, until, in.Version, ClientAddress(r))
	}
	if err != nil {
		writeServerError(w, err, "renew the relay's certificate")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// relayStatus keeps what a relay says of itself.
func (s *Server) relayStatus(_ context.Context, m relay.Message) {
	var st relay.Status
	if err := m.Decode(&st); err != nil {
		return
	}
	if err := s.store.SetRelayStatus(m.From, st.Version, st.Allowlist); err != nil {
		log.Printf("relays: %v", err)
	}
}

// relayChanged follows a relay's links to this server: one linked is
// online at once. One no server holds goes offline at the next sweep, so
// a relay dialing again at once is not said to have gone.
func (s *Server) relayChanged(name string) {
	if s.relays == nil || len(s.relays.Transports(name)) == 0 {
		return
	}
	if _, err := s.store.SetRelayOnline(name, true); err != nil {
		log.Printf("relays: %v", err)
	}
}

// sweepRelays says which relays are online: those linked to this server,
// and offline those no server holds.
func (s *Server) sweepRelays() {
	relays, err := s.store.ListRelays()
	if err != nil {
		log.Printf("relays: %v", err)
		return
	}
	for _, rl := range relays {
		if !rl.Paired || rl.RevokedAt != nil {
			continue
		}
		here := len(s.relays.Transports(rl.Name)) > 0
		switch {
		case here && !rl.Online:
			_, err = s.store.SetRelayOnline(rl.Name, true)
		case !here && rl.Online:
			var holder string
			if holder, err = s.store.RelayHolder(rl.Name, time.Now().Add(-relayStale)); err == nil && holder == "" {
				_, err = s.store.SetRelayOnline(rl.Name, false)
			}
		}
		if err != nil {
			log.Printf("relays: %v", err)
		}
	}
}

// pokeRelays has RunRelays look at once at the mesh root and the relays.
func (s *Server) pokeRelays() {
	select {
	case s.relayPoke <- struct{}{}:
	default:
	}
}

// RunRelays runs the relay bus until ctx ends: BoneMesh, when it is on,
// with the mesh root's identity, started again when the root is rotated
// or the main node's certificate nears its end; this server's claims on
// the relays it holds; and which relays are online.
func (s *Server) RunRelays(ctx context.Context) {
	if s.relays == nil {
		return
	}
	if s.cfg.Relay.WebSocket {
		log.Printf("Relays over WebSocket are taken at %s", relay.LivePath)
	}
	done := make(chan struct{})
	go func() {
		s.relays.Run(ctx)
		close(done)
	}()
	var mm meshRunner
	tick := time.NewTicker(relaySweep)
	defer tick.Stop()
	for {
		if s.cfg.Relay.BoneMesh {
			mm.keep(s)
		}
		s.sweepRelays()
		select {
		case <-ctx.Done():
			mm.stop()
			<-done
			return
		case <-tick.C:
		case <-s.relayPoke:
		}
	}
}

// meshRunner is the server's BoneMesh transport, as RunRelays keeps it.
type meshRunner struct {
	mesh    *relay.Mesh
	name    string
	renewAt time.Time
	said    string
}

// keep starts BoneMesh once there is a mesh root, and again when the root
// changes or the main node's certificate nears its end.
func (mm *meshRunner) keep(s *Server) {
	m, err := s.store.GetRelayMesh()
	if errors.Is(err, store.ErrNoRelayMesh) {
		mm.say("Relays over BoneMesh wait for a mesh root, which pairing the first relay makes")
		return
	}
	if err != nil {
		mm.say(fmt.Sprintf("Relays over BoneMesh: reading the mesh root: %v", err))
		return
	}
	if mm.mesh != nil && m.Mesh == mm.name && time.Now().Before(mm.renewAt) {
		return
	}
	mm.stop()
	mesh, err := relay.StartBoneMesh(s.relays, relay.BoneMeshOptions{Port: s.cfg.Relay.BoneMeshPort})
	if err != nil {
		mm.say(fmt.Sprintf("Relays over BoneMesh are off: %v", err))
		return
	}
	mm.mesh, mm.name, mm.renewAt = mesh, m.Mesh, time.Now().Add(mainCertFor-7*24*time.Hour)
	mm.say(fmt.Sprintf("Relays over BoneMesh are taken on port %d, in mesh %s", mesh.Port(), m.Mesh))
}

// say logs what changed, once.
func (mm *meshRunner) say(msg string) {
	if msg != mm.said {
		log.Print(msg)
		mm.said = msg
	}
}

func (mm *meshRunner) stop() {
	if mm.mesh != nil {
		mm.mesh.Close()
		mm.mesh = nil
	}
}

package api

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/relay"
	"github.com/google/uuid"
)

// Relays (docs/relays.md): BAM! run beside printers and devices on a local
// network, linked to this one over BoneMesh, WebSocket, or both, as
// [relay] turns them on. With both off, there is no bus, and relays are
// off on this server.

// newRelayBus makes the server's end of the relay bus, or nil when both
// transports are off. Each server has an ID of its own, so the servers
// sharing the database can tell which holds which relay.
func (s *Server) newRelayBus() *relay.Bus {
	if !s.cfg.Relay.BoneMesh && !s.cfg.Relay.WebSocket {
		return nil
	}
	return relay.New(relay.Options{Name: relay.Main, Server: uuid.NewString(), Trust: relay.Unpaired{}, Cluster: s.store})
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

// RunRelays runs the relay bus until ctx ends: BoneMesh, when it is on,
// and keeping this server's claims on the relays it holds.
func (s *Server) RunRelays(ctx context.Context) {
	if s.relays == nil {
		return
	}
	if s.cfg.Relay.BoneMesh {
		mesh, err := relay.StartBoneMesh(s.relays, relay.BoneMeshOptions{Port: s.cfg.Relay.BoneMeshPort})
		switch {
		case errors.Is(err, relay.ErrNotPaired):
			log.Printf("Relays over BoneMesh wait for a mesh identity, which pairing a relay makes")
		case err != nil:
			log.Printf("Relays over BoneMesh are off: %v", err)
		default:
			log.Printf("Relays over BoneMesh are taken on port %d", mesh.Port())
			defer mesh.Close()
		}
	}
	if s.cfg.Relay.WebSocket {
		log.Printf("Relays over WebSocket are taken at %s", relay.LivePath)
	}
	s.relays.Run(ctx)
}

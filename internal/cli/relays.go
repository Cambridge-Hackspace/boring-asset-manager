package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/relay"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/relayd"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Relays (docs/relays.md#pairing): adding one on the main node without
// the web interface, for instances a portal provisions, and pairing a
// relay from the machine it runs on.

// RelayCodeFor is how long a code made on the command line works, as one
// made under Admin › Relays.
const RelayCodeFor = time.Hour

// AdminRelayAdd adds a relay named name, and prints the code it pairs with
// and the command that pairs it.
func AdminRelayAdd(configPath, name string, out io.Writer) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("--name is required")
	}
	if !relay.ValidRelayName(name) {
		return fmt.Errorf("%q cannot name a relay: use lowercase letters, digits and hyphens, at most 63, and not main", name)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}
	if key, _ := cfg.Auth.Key(); key == nil {
		return errors.New("pairing relays needs [auth] secret_key in the configuration, which seals the mesh root")
	}
	st, err := store.Open(&cfg.Database)
	if err != nil {
		return fmt.Errorf("opening database (%s): %w", cfg.Database.Type, err)
	}
	defer st.Close()
	code := relay.NewCode()
	r, err := st.CreateRelay(name, store.RelayCodeKey(relay.NormalizeCode(code)), time.Now().Add(RelayCodeFor))
	if err != nil {
		return fmt.Errorf("adding the relay: %w", err)
	}
	fmt.Fprintf(out, "Added relay %s. Its code works once, until %s:\n\n  %s\n\nOn the relay, run:\n\n  bam relay join %s %s\n",
		r.Name, r.CodeExpiresAt.Local().Format("15:04 on 2 January"), code, cfg.Server.BaseURL, code)
	return nil
}

// RelayJoinEnv gives a relay the main node's address and its code, as
// "<url> <code>", for a relay provisioned by its environment.
const RelayJoinEnv = "BAM_RELAY_JOIN"

// RelayStateEnv names where a relay keeps its pairing.
const RelayStateEnv = "BAM_RELAY_STATE"

// RelayStatePath is where a relay keeps its pairing: state, or
// $BAM_RELAY_STATE, or relay.json.
func RelayStatePath(state string) string {
	switch {
	case state != "":
		return state
	case os.Getenv(RelayStateEnv) != "":
		return os.Getenv(RelayStateEnv)
	}
	return "relay.json"
}

// RelayJoin pairs this machine as a relay of the main node at mainURL,
// with its code, and keeps the pairing at state. Without mainURL and code
// it reads them from $BAM_RELAY_JOIN. It refuses to replace a pairing
// already kept.
func RelayJoin(ctx context.Context, mainURL, code, state, version string, out io.Writer) error {
	if mainURL == "" && code == "" {
		if fields := strings.Fields(os.Getenv(RelayJoinEnv)); len(fields) == 2 {
			mainURL, code = fields[0], fields[1]
		}
	}
	if mainURL == "" || code == "" {
		return fmt.Errorf("give the main node's address and the relay's code, as bam relay join https://bam.example.org <code>, or in $%s", RelayJoinEnv)
	}
	path := RelayStatePath(state)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already holds a pairing: remove it to pair again", path)
	}
	p, err := relay.Join(ctx, nil, mainURL, code, version)
	if err != nil {
		return err
	}
	if err := p.Save(path); err != nil {
		return fmt.Errorf("keeping the pairing in %s: %w", path, err)
	}
	var over []string
	if p.BoneMesh != "" {
		over = append(over, "BoneMesh at "+p.BoneMesh)
	}
	if p.WebSocket {
		over = append(over, "WebSocket")
	}
	fmt.Fprintf(out, "Paired as relay %s of %s, over %s. The pairing is kept in %s; its certificate is renewed before %s.\n",
		p.Name, p.Main, strings.Join(over, " and "), path, p.ExpiresAt.Local().Format("2 January 2006"))
	return nil
}

// RelayRun runs this machine as a relay (docs/relays.md), with the pairing
// kept at state, until ctx ends. The configuration at configPath, if
// there is one, and the environment give its allowlist ([relay] allow and
// [printers] devices). It links over the transports its pairing offers,
// less any turned off here.
func RelayRun(ctx context.Context, configPath, state string, bonemesh, websocket bool, version string) error {
	path := RelayStatePath(state)
	p, err := relay.LoadPairing(path)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no pairing is kept at %s: pair this machine first, with bam relay join", path)
	}
	if err != nil {
		return err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}
	allow, err := relayd.ParseAllowlist(cfg.Relay.Allow, cfg.Printers.Devices, cfg.Printers.Bluetooth)
	if err != nil {
		return err
	}
	if len(allow.Summary()) == 0 {
		log.Printf("The allowlist is empty, so relay %s may reach nothing: list what it may reach under [relay] allow, [printers] devices and [printers] bluetooth", p.Name)
	}
	transport := printers.NewDispatcher(cfg.Printers.Devices)
	transport.AllowedBluetooth = cfg.Printers.Bluetooth
	r, err := relayd.New(relayd.Config{Pairing: p, State: path, Version: version, BoneMesh: bonemesh, WebSocket: websocket,
		Allow: allow, Transport: transport})
	if err != nil {
		return err
	}
	return r.Run(ctx)
}

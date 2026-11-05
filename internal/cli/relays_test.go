package cli

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
)

func TestAdminRelayAdd(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	if err := Init(configPath); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database.DSN = filepath.Join(dir, "bam.db")
	cfg.Server.BaseURL = "https://bam.example.org"
	if err := cfg.SaveTOML(configPath); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(configPath); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := AdminRelayAdd(configPath, "front-desk", &out); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`bam relay join https://bam\.example\.org [0-9A-Z]{4}(-[0-9A-Z]{4}){3}\n`).MatchString(out.String()) {
		t.Fatalf("printed:\n%s", out.String())
	}
	if err := AdminRelayAdd(configPath, "front-desk", &out); err == nil {
		t.Fatal("a second relay of the same name was added")
	}
	for _, bad := range []string{"", "main", "Front Desk"} {
		if err := AdminRelayAdd(configPath, bad, &out); err == nil {
			t.Fatalf("a relay named %q was added", bad)
		}
	}
}

func TestRelayJoinArguments(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(RelayStateEnv, "")
	t.Setenv(RelayJoinEnv, "")
	if err := RelayJoin(context.Background(), "", "", filepath.Join(dir, "relay.json"), "test", os.Stdout); err == nil || !strings.Contains(err.Error(), RelayJoinEnv) {
		t.Fatalf("with nothing to join: %v", err)
	}

	// A pairing kept is not replaced.
	state := filepath.Join(dir, "kept.json")
	if err := os.WriteFile(state, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(RelayJoinEnv, "https://bam.example.org ABCD-EFGH-JKMN-PQRS")
	if err := RelayJoin(context.Background(), "", "", state, "test", os.Stdout); err == nil || !strings.Contains(err.Error(), "already holds a pairing") {
		t.Fatalf("over a pairing kept: %v", err)
	}

	if got := RelayStatePath(""); got != "relay.json" {
		t.Fatalf("the default state: %q", got)
	}
	t.Setenv(RelayStateEnv, "/var/lib/bam/relay.json")
	if got := RelayStatePath(""); got != "/var/lib/bam/relay.json" {
		t.Fatalf("the state from the environment: %q", got)
	}
	if got := RelayStatePath("here.json"); got != "here.json" {
		t.Fatalf("the state given: %q", got)
	}
}

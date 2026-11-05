package store

import (
	"testing"
	"time"
)

func TestRelayHolds(t *testing.T) {
	st := setupTestStore(t)
	recent := func() time.Time { return time.Now().Add(-time.Minute) }

	holder, err := st.RelayHolder("r1", recent())
	if err != nil || holder != "" {
		t.Fatalf("a relay no server holds is held by %q, %v", holder, err)
	}
	if err := st.HoldRelay("r1", "websocket", "a"); err != nil {
		t.Fatal(err)
	}
	if holder, _ := st.RelayHolder("r1", recent()); holder != "a" {
		t.Fatalf("held by %q, not a", holder)
	}
	// BoneMesh is preferred, wherever it is held.
	if err := st.HoldRelay("r1", "bonemesh", "b"); err != nil {
		t.Fatal(err)
	}
	if holder, _ := st.RelayHolder("r1", recent()); holder != "b" {
		t.Fatalf("held by %q, not b, which holds it over BoneMesh", holder)
	}

	// Another server takes it over a transport; the first finds it has
	// lost it.
	if err := st.HoldRelay("r1", "websocket", "c"); err != nil {
		t.Fatal(err)
	}
	if kept, err := st.KeepRelay("r1", "websocket", "a"); err != nil || kept {
		t.Fatalf("a kept a relay c took: %v, %v", kept, err)
	}
	if kept, err := st.KeepRelay("r1", "websocket", "c"); err != nil || !kept {
		t.Fatalf("c could not keep its relay: %v, %v", kept, err)
	}
	// Letting go of what another holds changes nothing.
	if err := st.ReleaseRelay("r1", "websocket", "a"); err != nil {
		t.Fatal(err)
	}
	if err := st.ReleaseRelay("r1", "bonemesh", "b"); err != nil {
		t.Fatal(err)
	}
	if holder, _ := st.RelayHolder("r1", recent()); holder != "c" {
		t.Fatalf("held by %q, not c", holder)
	}
	// A claim not kept lately was left by a stopped server.
	if holder, _ := st.RelayHolder("r1", time.Now().Add(time.Minute)); holder != "" {
		t.Fatalf("a stale claim still holds the relay, for %q", holder)
	}
}

func TestRelayMail(t *testing.T) {
	st := setupTestStore(t)
	big := make([]byte, 100<<10)
	for i := range big {
		big[i] = 'a' + byte(i%26)
	}
	for _, m := range [][]byte{[]byte(`{"n":1}`), big, []byte(`{"n":"ü"}`)} {
		if err := st.PostRelayMail("a", m); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	if err := st.PostRelayMail("b", []byte(`{"for":"b"}`)); err != nil {
		t.Fatal(err)
	}
	mail, err := st.TakeRelayMail("a", time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(mail) != 3 || string(mail[0]) != `{"n":1}` || string(mail[1]) != string(big) || string(mail[2]) != `{"n":"ü"}` {
		t.Fatalf("a took %d messages, not its three in order", len(mail))
	}
	if again, _ := st.TakeRelayMail("a", time.Now().Add(-time.Minute)); len(again) != 0 {
		t.Fatalf("a took %d messages twice", len(again))
	}
	// Mail no server takes is dropped once stale.
	if _, err := st.TakeRelayMail("c", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if left, _ := st.TakeRelayMail("b", time.Now().Add(-time.Minute)); len(left) != 0 {
		t.Fatalf("b's stale mail was kept: %d", len(left))
	}
}

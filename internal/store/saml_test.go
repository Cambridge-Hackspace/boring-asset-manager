package store

import (
	"errors"
	"testing"
	"time"
)

func TestASAMLAssertionSignsInOnce(t *testing.T) {
	st := setupTestStore(t)
	corp := testProvider(t, st, "corp")
	other := testProvider(t, st, "other")
	until := time.Now().Add(5 * time.Minute)
	if err := st.UseSAMLAssertion(corp.ID, "_a1", until); err != nil {
		t.Fatal(err)
	}
	if err := st.UseSAMLAssertion(corp.ID, "_a1", until); !errors.Is(err, ErrAssertionUsed) {
		t.Errorf("used again: %v", err)
	}
	// Another provider's assertion of the same ID is its own.
	if err := st.UseSAMLAssertion(other.ID, "_a1", until); err != nil {
		t.Errorf("another provider's: %v", err)
	}
	// One expired is forgotten, so the table does not grow.
	if err := st.UseSAMLAssertion(corp.ID, "_old", time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := st.UseSAMLAssertion(corp.ID, "_a2", until); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.QueryRow("SELECT COUNT(*) FROM saml_assertions WHERE assertion_id = ?", "_old").Scan(&n); err != nil || n != 0 {
		t.Errorf("expired kept: %d %v", n, err)
	}
	// Deleting the provider deletes its assertions.
	if err := st.DeleteProvider(corp.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.QueryRow("SELECT COUNT(*) FROM saml_assertions WHERE provider_id = ?", corp.ID).Scan(&n); err != nil || n != 0 {
		t.Errorf("a deleted provider's kept: %d %v", n, err)
	}
}

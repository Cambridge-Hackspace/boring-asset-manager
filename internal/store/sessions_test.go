package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

)

// setupTestStore opens a migrated, seeded store. It uses a temporary SQLite file
// unless BAM_TEST_DIALECT/BAM_TEST_DSN select a PostgreSQL or MariaDB server, in
// which case each test gets its own ephemeral schema/database.
func setupTestStore(t *testing.T) *Store {
	t.Helper()

	var st *Store
	if dialect := os.Getenv(TestDialectEnv); dialect != "" && dialect != "sqlite" {
		ephemeral, cleanup, err := OpenEphemeral(dialect, os.Getenv(TestDSNEnv))
		if err != nil {
			t.Fatalf("opening ephemeral %s store: %v", dialect, err)
		}
		t.Cleanup(cleanup)
		st = ephemeral
	} else {
		dbPath := filepath.Join(t.TempDir(), "test.db")
		opened, err := OpenMigratedSQLite(dbPath)
		if err != nil {
			t.Fatalf("opening store: %v", err)
		}
		st = opened
	}

	if err := st.Migrate(); err != nil {
		t.Fatalf("migrating store: %v", err)
	}

	if err := st.SeedDefaultPermissionsAndRoles(); err != nil {
		t.Fatalf("seeding store: %v", err)
	}

	return st
}

func TestSessionLifecycle(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	user, err := st.CreateUser("user@example.com", "Password123!", true)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// 1. Create Session
	sess, err := st.CreateSession(user.ID, 2*time.Hour)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if len(sess.ID) != 64 {
		t.Errorf("expected 64-hex session token, got length %d", len(sess.ID))
	}
	if sess.UserID != user.ID {
		t.Errorf("expected user ID %s, got %s", user.ID, sess.UserID)
	}

	// 2. Retrieve Active Session
	retrievedSess, retrievedUser, err := st.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if retrievedSess.ID != sess.ID {
		t.Errorf("expected session ID %s, got %s", sess.ID, retrievedSess.ID)
	}
	if retrievedUser.Email != user.Email {
		t.Errorf("expected email %s, got %s", user.Email, retrievedUser.Email)
	}

	// 3. Delete Session
	if err := st.DeleteSession(sess.ID); err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	// 4. Verify Session is gone
	_, _, err = st.GetSession(sess.ID)
	if err == nil {
		t.Fatal("expected error retrieving deleted session, got nil")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestExpiredSessions(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	user, err := st.CreateUser("user2@example.com", "Password123!", true)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Create an already-expired session (TTL: -1 hour)
	sess, err := st.CreateSession(user.ID, -1*time.Hour)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// GetSession should report expired/not found
	_, _, err = st.GetSession(sess.ID)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound for expired session, got %v", err)
	}

	// CleanExpiredSessions should delete it
	cleaned, err := st.CleanExpiredSessions()
	if err != nil {
		t.Fatalf("CleanExpiredSessions failed: %v", err)
	}
	if cleaned != 1 {
		t.Errorf("expected 1 cleaned session, got %d", cleaned)
	}
}

func TestDeleteUserSessions(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	user, err := st.CreateUser("user3@example.com", "Password123!", true)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Create two sessions for same user
	sess1, err := st.CreateSession(user.ID, 1*time.Hour)
	if err != nil {
		t.Fatalf("CreateSession 1 failed: %v", err)
	}
	sess2, err := st.CreateSession(user.ID, 1*time.Hour)
	if err != nil {
		t.Fatalf("CreateSession 2 failed: %v", err)
	}

	// Revoke all sessions for user
	if err := st.DeleteUserSessions(user.ID); err != nil {
		t.Fatalf("DeleteUserSessions failed: %v", err)
	}

	for _, token := range []string{sess1.ID, sess2.ID} {
		if _, _, err := st.GetSession(token); !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("expected session %s to be revoked, got %v", token, err)
		}
	}
}

func TestSessionTokensAreNotStored(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	u, err := st.CreateUser("hash@hackspace.org", "Pass1234!", true)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := st.CreateSession(u.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	var stored string
	if err := st.QueryRow("SELECT id FROM sessions WHERE user_id = ?", u.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == sess.ID {
		t.Fatal("the sessions table holds the token itself")
	}
	if stored != sessionKey(sess.ID) {
		t.Errorf("stored %q, want the token's SHA-256 %q", stored, sessionKey(sess.ID))
	}

	// The cookie token still works; what the table holds does not.
	if got, _, err := st.GetSession(sess.ID); err != nil || got.ID != sess.ID {
		t.Errorf("GetSession(token) = %+v, %v", got, err)
	}
	if _, _, err := st.GetSession(stored); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("a value copied from the sessions table signed in: %v", err)
	}

	if err := st.DeleteSession(sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.GetSession(sess.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("deleted session still valid: %v", err)
	}
}

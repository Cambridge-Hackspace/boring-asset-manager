package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
)

func setupTestStore(t *testing.T) *Store {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	cfg := &config.DatabaseConfig{
		Type: "sqlite",
		DSN:  dbPath + "?_journal=WAL&_busy_timeout=5000",
	}

	st, err := Open(cfg)
	if err != nil {
		t.Fatalf("opening store: %v", err)
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

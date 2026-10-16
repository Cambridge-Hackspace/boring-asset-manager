package integration

import (
	"os"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

// TestMain hashes passwords at bcrypt's lowest cost: at the real cost each
// hash or check takes seconds under the race detector (auth.BcryptCost).
func TestMain(m *testing.M) {
	auth.BcryptCost = bcrypt.MinCost
	os.Exit(m.Run())
}

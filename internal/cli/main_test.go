package cli

import (
	"os"
	"testing"
)

// TestMain clears the environment overrides config.Load applies, such as the
// BAM_DB_PATH reaper sets, so every command here runs on the configuration
// file its test wrote rather than on a database shared with other packages.
func TestMain(m *testing.M) {
	for _, name := range []string{"BAM_SERVER_HOST", "BAM_SERVER_PORT", "BAM_SERVER_BASE_URL", "BAM_DATABASE_TYPE", "BAM_DATABASE_DSN", "BAM_DB_PATH", "BAM_STORAGE_LOCAL_PATH", "BAM_STORAGE_PATH", "BAM_PRINTERS_DEVICES"} {
		os.Unsetenv(name)
	}
	os.Exit(m.Run())
}

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWarrantyNoticeDays: 30 by default, also for a file written before
// [purchases] existed, set by the file or BAM_PURCHASES_WARRANTY_NOTICE_DAYS,
// and from 0 to 365.
func TestWarrantyNoticeDays(t *testing.T) {
	if d := DefaultConfig().Purchases.WarrantyNoticeDays; d != 30 {
		t.Errorf("default: %d", d)
	}
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("[server]\nport = 8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if loaded, err := Load(configPath); err != nil || loaded.Purchases.WarrantyNoticeDays != 30 {
		t.Errorf("an older file: %+v, %v", loaded, err)
	}
	if err := os.WriteFile(configPath, []byte("[purchases]\nwarranty_notice_days = 14\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if loaded, err := Load(configPath); err != nil || loaded.Purchases.WarrantyNoticeDays != 14 {
		t.Errorf("from the file: %+v, %v", loaded, err)
	}
	t.Setenv("BAM_PURCHASES_WARRANTY_NOTICE_DAYS", "0")
	if loaded, err := Load(configPath); err != nil || loaded.Purchases.WarrantyNoticeDays != 0 {
		t.Errorf("from the environment: %+v, %v", loaded, err)
	}
	for _, bad := range []int{-1, 366} {
		cfg := DefaultConfig()
		cfg.Purchases.WarrantyNoticeDays = bad
		if err := cfg.Validate(); err == nil {
			t.Errorf("%d days was accepted", bad)
		}
	}
}

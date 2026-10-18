package cli

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/api"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/notify"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Init generates a default configuration file if it does not already exist.
func Init(configPath string) error {
	if configPath == "" {
		configPath = "config.toml"
	}

	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("configuration file %s already exists; refusing to overwrite", configPath)
	}

	cfg := config.DefaultConfig()
	if err := cfg.SaveTOML(configPath); err != nil {
		return fmt.Errorf("generating default configuration file: %w", err)
	}

	fmt.Printf("Initialized default configuration at %s\n", configPath)
	return nil
}

// Migrate connects to the configured database and runs pending embedded migrations.
func Migrate(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}

	st, err := store.Open(&cfg.Database)
	if err != nil {
		return fmt.Errorf("opening database (%s): %w", cfg.Database.Type, err)
	}
	defer st.Close()

	if err := st.Migrate(); err != nil {
		return fmt.Errorf("running database migrations: %w", err)
	}

	if err := st.SeedDefaultPermissionsAndRoles(); err != nil {
		return fmt.Errorf("seeding default permissions and roles: %w", err)
	}

	fmt.Printf("Database migrations successfully applied for dialect %s.\n", cfg.Database.Type)
	return nil
}

// AdminCreate provisions an initial administrator account, named name if
// it is not empty.
func AdminCreate(configPath, email, password, name string) error {
	if email == "" {
		return errors.New("--email is required")
	}
	if password == "" {
		return errors.New("--password is required")
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}

	st, err := store.Open(&cfg.Database)
	if err != nil {
		return fmt.Errorf("opening database (%s): %w", cfg.Database.Type, err)
	}
	defer st.Close()

	// Ensure migrations and roles are present before creating admin
	if err := st.Migrate(); err != nil {
		return fmt.Errorf("ensuring migrations applied: %w", err)
	}
	if err := st.SeedDefaultPermissionsAndRoles(); err != nil {
		return fmt.Errorf("ensuring roles seeded: %w", err)
	}

	user, err := st.CreateAdminUser(email, password)
	if err != nil {
		return fmt.Errorf("provisioning administrator account: %w", err)
	}
	if strings.TrimSpace(name) != "" {
		if user, err = st.SetUserName(user.ID, name); err != nil {
			return fmt.Errorf("naming the administrator: %w", err)
		}
	}

	fmt.Printf("Administrator account provisioned successfully: %s (ID: %s)\n", user.Email, user.ID)
	return nil
}

// AdminPassword sets the password of the account with an email, for
// someone locked out, and signs them out everywhere. It changes nothing
// else about the account.
func AdminPassword(configPath, email, password string) error {
	if email == "" {
		return errors.New("--email is required")
	}
	if password == "" {
		return errors.New("--password is required")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}
	st, err := store.Open(&cfg.Database)
	if err != nil {
		return fmt.Errorf("opening database (%s): %w", cfg.Database.Type, err)
	}
	defer st.Close()
	user, err := st.GetUserByEmail(email)
	if err != nil {
		return fmt.Errorf("finding %s: %w", email, err)
	}
	if err := st.SetPassword(user.ID, password, ""); err != nil {
		return fmt.Errorf("setting the password: %w", err)
	}
	fmt.Printf("Password set for %s; they are signed out everywhere.\n", user.Email)
	return nil
}

// Serve starts the BAM HTTP daemon with graceful shutdown.
func Serve(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}

	st, err := store.Open(&cfg.Database)
	if err != nil {
		return fmt.Errorf("opening database (%s): %w", cfg.Database.Type, err)
	}
	defer st.Close()

	// Ensure database schema is ready
	if err := st.Migrate(); err != nil {
		return fmt.Errorf("verifying database schema: %w", err)
	}
	if err := st.SeedDefaultPermissionsAndRoles(); err != nil {
		return fmt.Errorf("verifying default roles: %w", err)
	}

	server := api.NewServer(cfg, st)

	// Send queued notifications until the server stops.
	workerCtx, stopWorker := context.WithCancel(context.Background())
	defer stopWorker()
	go (&notify.Worker{Store: st, BaseURL: cfg.Server.BaseURL}).Run(workerCtx)
	// Print jobs from the spool until the server stops.
	go server.RunSpool(workerCtx)
	// Send live connections the events they ask for.
	go server.RunLive(workerCtx)

	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("BAM! daemon listening on http://%s:%d (Base URL: %s)", cfg.Server.Host, cfg.Server.Port, cfg.Server.BaseURL)
		if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("server error: %w", err)
	case sig := <-shutdownChan:
		log.Printf("Received termination signal %s; shutting down gracefully...", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			return fmt.Errorf("server graceful shutdown error: %w", err)
		}
		log.Println("BAM! daemon stopped cleanly.")
	}

	return nil
}

// Backup copies the configured SQLite database to dest (README §2.2). Other
// databases return store.ErrBackupUnsupported.
func Backup(configPath, dest string) error {
	if dest == "" {
		return errors.New("--to is required")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}
	if err := store.BackupSQLite(&cfg.Database, dest); err != nil {
		return err
	}
	fmt.Printf("Backed up the database to %s\n", dest)
	return nil
}

// Restore puts a backup made by Backup in place of the configured SQLite
// database. BAM must be stopped first.
func Restore(configPath, from string) error {
	if from == "" {
		return errors.New("--from is required")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}
	if err := store.RestoreSQLite(&cfg.Database, from); err != nil {
		return err
	}
	fmt.Printf("Restored the database from %s\n", from)
	return nil
}

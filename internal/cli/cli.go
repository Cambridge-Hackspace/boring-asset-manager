package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/api"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/archive"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
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
	// A store that cannot be reached or written is found now, rather than
	// at the first upload.
	checkCtx, cancelCheck := context.WithTimeout(context.Background(), 30*time.Second)
	where, err := server.CheckStorage(checkCtx)
	cancelCheck()
	if err != nil {
		return fmt.Errorf("checking the storage for uploaded files, %s: %w", where, err)
	}
	log.Printf("Uploaded files are kept in %s", where)

	// Send queued notifications until the server stops.
	workerCtx, stopWorker := context.WithCancel(context.Background())
	defer stopWorker()
	go (&notify.Worker{Store: st, BaseURL: cfg.Server.BaseURL}).Run(workerCtx)
	// Print jobs from the spool until the server stops.
	go server.RunSpool(workerCtx)
	// Send live connections the events they ask for.
	go server.RunLive(workerCtx)
	go server.RunIcons(workerCtx)
	// Keep the file store in step with the database.
	go server.RunSweep(workerCtx)

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

// StorageSweep sweeps the configured file store once (README §2): it
// removes the files no row names, older than a day, and lists the rows
// whose file is missing. A dry run removes nothing.
func StorageSweep(configPath string, dryRun bool) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}
	st, err := store.Open(&cfg.Database)
	if err != nil {
		return fmt.Errorf("opening database (%s): %w", cfg.Database.Type, err)
	}
	defer st.Close()
	server := api.NewServer(cfg, st)
	res, err := server.SweepStorage(context.Background(), dryRun)
	if err != nil {
		return err
	}
	verb := "Removed"
	if dryRun {
		verb = "Would remove"
	}
	fmt.Printf("%s %d files no row names.\n", verb, len(res.Removed))
	if dryRun {
		for _, k := range res.Removed {
			fmt.Println("  " + k)
		}
	}
	if len(res.Kept) > 0 {
		fmt.Printf("Kept %d files no row names yet, as uploads may be under way; they go once a day old.\n", len(res.Kept))
	}
	if len(res.Missing) > 0 {
		fmt.Printf("%d files rows name are missing from the store:\n", len(res.Missing))
		for _, k := range res.Missing {
			fmt.Println("  " + k)
		}
	}
	return nil
}

// StorageCopy copies every uploaded file from the configured store to the
// one the [storage] table of another configuration file names, reading each
// back to check it (README §2), so an instance can move from a directory to
// a bucket and back. Change the configuration to the new store after.
func StorageCopy(configPath, toPath string) error {
	if toPath == "" {
		return errors.New("--to is required: a configuration file whose [storage] names the store to copy to")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}
	toCfg, err := config.Load(toPath)
	if err != nil {
		return fmt.Errorf("loading %s: %w", toPath, err)
	}
	from, err := filestore.Open(cfg.Storage)
	if err != nil {
		return err
	}
	to, err := filestore.Open(toCfg.Storage)
	if err != nil {
		return err
	}
	if fmt.Sprint(from) == fmt.Sprint(to) {
		return fmt.Errorf("both configurations keep files in %s", from)
	}
	ctx := context.Background()
	if err := filestore.Check(ctx, to); err != nil {
		return fmt.Errorf("checking %s: %w", to, err)
	}
	fmt.Printf("Copying from %s to %s...\n", from, to)
	n, err := filestore.Copy(ctx, from, to, nil)
	if err != nil {
		return fmt.Errorf("after copying %d files: %w", n, err)
	}
	fmt.Printf("Copied and checked %d files.\n", n)
	return nil
}

// Backup writes an archive of the instance to dest (README §2.3): its rows,
// from any database, and the uploaded files they name, with authentication
// unless opts leave it out. A dest ending in .db is a copy of a SQLite
// database instead, as before archives (§2.2); other databases return
// store.ErrBackupUnsupported for it.
func Backup(configPath, dest string, opts BackupOptions) error {
	if dest == "" {
		return errors.New("--to is required")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}
	if strings.EqualFold(filepath.Ext(dest), ".db") {
		// Scripts written before archives keep the copy they asked for.
		if opts.WithoutSecrets || opts.Split > 0 {
			return errors.New("--without-secrets and --split are for archives; a --to ending in .db makes a copy of a SQLite database")
		}
		if err := store.BackupSQLite(&cfg.Database, dest); err != nil {
			return err
		}
		fmt.Printf("Copied the SQLite database to %s (a --to ending in .db makes a copy, not an archive)\n", dest)
		return nil
	}

	st, err := store.Open(&cfg.Database)
	if err != nil {
		return err
	}
	defer st.Close()
	blobs, err := filestore.Open(cfg.Storage)
	if err != nil {
		return err
	}
	var names []string
	open := func(part int) (io.WriteCloser, error) {
		name := PartName(dest, part, opts.Split > 0)
		names = append(names, name)
		return os.OpenFile(name+".partial", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	}
	sum, err := archive.Write(context.Background(), st, blobs, open, archive.Options{
		BamVersion: api.Version,
		Secrets:    !opts.WithoutSecrets,
		Split:      opts.Split,
	})
	if err != nil {
		for _, name := range names {
			_ = os.Remove(name + ".partial")
		}
		return err
	}
	// Each part takes its name once all are written, so a name never
	// holds a part of a backup that failed.
	for _, name := range names {
		if err := os.Rename(name+".partial", name); err != nil {
			return err
		}
	}

	rows := 0
	for _, n := range sum.Tables {
		rows += n
	}
	fmt.Printf("Backed up %d rows and %d files (%d bytes) to %s\n", rows, sum.Files, sum.FileBytes, strings.Join(names, ", "))
	if opts.WithoutSecrets {
		fmt.Println("Authentication is left out: restoring it keeps the instance's own users' passwords, sessions and tokens.")
	} else {
		fmt.Println("It holds password hashes, sessions and tokens: keep it as carefully as the database.")
	}
	if len(sum.Missing) > 0 {
		fmt.Printf("%d files rows name were not in the file store, and are listed in the manifest; bam storage sweep --dry-run lists them too.\n", len(sum.Missing))
	}
	return nil
}

// BackupOptions are bam backup's choices beyond where to write it.
type BackupOptions struct {
	// WithoutSecrets leaves authentication out.
	WithoutSecrets bool
	// Split is the most bytes a part may hold; 0 writes one archive.
	Split int64
}

// PartName is where part n of a backup to dest is written: dest itself,
// unless it is split, when .partN goes before its extension, as in
// bam.part2.bam.tar.gz.
func PartName(dest string, n int, split bool) string {
	if !split {
		return dest
	}
	part := ".part" + strconv.Itoa(n)
	for _, ext := range []string{".bam.tar.gz", ".tar.gz", ".tgz"} {
		if strings.HasSuffix(strings.ToLower(dest), ext) {
			return dest[:len(dest)-len(ext)] + part + dest[len(dest)-len(ext):]
		}
	}
	return dest + part
}

// ParseSize reads a size such as 500MB, 4GiB or 1048576 (bytes).
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	i := len(s)
	for i > 0 && (s[i-1] < '0' || s[i-1] > '9') {
		i--
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s[:i]), 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("size %q: a whole number of bytes, or of KB, MB, GB, TB, KiB, MiB, GiB or TiB", s)
	}
	units := map[string]int64{
		"": 1, "b": 1,
		"kb": 1e3, "mb": 1e6, "gb": 1e9, "tb": 1e12,
		"kib": 1 << 10, "mib": 1 << 20, "gib": 1 << 30, "tib": 1 << 40,
	}
	unit, ok := units[strings.ToLower(strings.TrimSpace(s[i:]))]
	if !ok || n > math.MaxInt64/unit {
		return 0, fmt.Errorf("size %q: a whole number of bytes, or of KB, MB, GB, TB, KiB, MiB, GiB or TiB", s)
	}
	return n * unit, nil
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

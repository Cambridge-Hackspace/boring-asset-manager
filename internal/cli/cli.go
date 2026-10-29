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
	"sort"
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
	// Remove archives uploaded and left unused for a day.
	go server.RunUploads(workerCtx)

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

// RestoreOptions are bam restore's choices beyond what to restore.
type RestoreOptions struct {
	// Only are the kinds of information to restore; nil for all.
	Only []archive.Kind
	// Check only checks the archive, and says what a restore would do.
	Check bool
	// Replace replaces the kinds chosen in an instance in use (§2.3), after
	// backing up what it replaces to BackupTo: by default, a file in the
	// staging directory.
	Replace  bool
	BackupTo string
	// Wipe empties the database first, to put an instance back as the
	// archive has it whatever was done since (bam-update, after an update
	// fails).
	Wipe bool
}

// Restore puts a backup made by Backup back. It tells the two kinds apart
// by their contents: a copy of a SQLite database replaces the configured
// one, as before archives (§2.2); an archive is restored, authentication
// included, into an instance not yet in use, on any database (§2.3). BAM
// must be stopped first.
func Restore(configPath, from string, opts RestoreOptions) error {
	if from == "" {
		return errors.New("--from is required")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}
	kind, err := backupKind(from)
	if err != nil {
		return err
	}
	if kind == "sqlite" {
		if opts.Only != nil || opts.Check || opts.Replace || opts.Wipe {
			return errors.New("--only, --check, --replace and --wipe are for archives; this is a copy of a SQLite database, put back whole")
		}
		if err := store.RestoreSQLite(&cfg.Database, from); err != nil {
			return err
		}
		fmt.Printf("Restored the database from %s\n", from)
		return nil
	}

	open := func() (io.ReadCloser, error) { return os.Open(from) }
	if opts.Check {
		f, err := open()
		if err != nil {
			return err
		}
		defer f.Close()
		c, err := archive.Check(f)
		if err != nil {
			return err
		}
		printChecked(c)
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
	if opts.Replace {
		path, err := backupBeforeReplacing(st, blobs, cfg.Storage.StagingDir(), opts)
		if err != nil {
			return fmt.Errorf("backing up what it replaces, so replacing nothing: %w", err)
		}
		fmt.Printf("Backed up what it replaces to %s.\n", path)
	}
	sum, err := archive.Restore(context.Background(), st, blobs, open, archive.RestoreOptions{Kinds: opts.Only, Secrets: true, Replace: opts.Replace, Wipe: opts.Wipe})
	if err != nil {
		return err
	}
	if err := keepOwners(st, cfg); err != nil {
		return fmt.Errorf("restored, but giving the files it made to their directories' owners: %w", err)
	}
	for _, w := range sum.Warnings {
		fmt.Println("Note: " + w + ".")
	}
	if sum.Header.Set.Part > 1 {
		fmt.Printf("Restored part %d of %d: %d files (%d bytes); %d already restored, %d named by no row.\n",
			sum.Header.Set.Part, sum.Header.Set.Parts, sum.Files, sum.FileBytes, sum.Already, sum.Unnamed)
		return nil
	}
	rows := 0
	for _, n := range sum.Tables {
		rows += n
	}
	fmt.Printf("Restored %d rows and %d files (%d bytes) of %s, from %s.\n", rows, sum.Files, sum.FileBytes, kindList(sum.Kinds), from)
	for _, removed := range []struct {
		what   string
		counts map[string]int
	}{{"the archive did not have", sum.Removed}, {"that named only what was removed", sum.Lost}} {
		for _, table := range sortedKeys(removed.counts) {
			fmt.Printf("Removed %d rows of %s %s.\n", removed.counts[table], table, removed.what)
		}
	}
	if sum.Migrated {
		fmt.Printf("The archive's schema, version %d, was migrated to this bam's.\n", sum.Header.SchemaVersion)
	}
	if parts := sum.Header.Set.Parts; parts > 1 {
		fmt.Printf("This is part 1 of %d: restore the others, in any order, for the rest of the files.\n", parts)
	}
	return nil
}

// backupKind tells a copy of a SQLite database ("sqlite") from an archive
// ("archive") by its first bytes.
func backupKind(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	head := make([]byte, 16)
	n, _ := io.ReadFull(f, head)
	switch {
	case n == 16 && string(head) == "SQLite format 3\x00":
		return "sqlite", nil
	case n >= 2 && head[0] == 0x1f && head[1] == 0x8b:
		return "archive", nil
	}
	return "", fmt.Errorf("%s is neither an archive nor a copy of a SQLite database", path)
}

// ParseKinds reads a list of kinds of information, such as assets,labels.
func ParseKinds(s string) ([]archive.Kind, error) {
	var out []archive.Kind
	for _, k := range strings.Split(s, ",") {
		k = strings.TrimSpace(k)
		found := false
		for _, known := range archive.Kinds {
			if string(known) == k {
				out = append(out, known)
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("%q is not a kind of information: %s", k, kindList(archive.Kinds))
		}
	}
	return out, nil
}

func kindList(kinds []archive.Kind) string {
	s := make([]string, len(kinds))
	for i, k := range kinds {
		s[i] = string(k)
	}
	return strings.Join(s, ", ")
}

// printChecked says what an archive that checks out holds.
func printChecked(c *archive.Checked) {
	h := c.Header
	fmt.Printf("The archive checks out: every member matches its manifest.\n")
	fmt.Printf("Written %s by bam %s from %s, schema version %d.\n", h.CreatedAt, h.BamVersion, h.Dialect, h.SchemaVersion)
	parts := c.Manifest.Set.Parts
	if h.Set.Part > 1 {
		fmt.Printf("Part %d of %d: %d files (%d bytes), which restoring it adds.\n", h.Set.Part, parts, c.Files, c.FileBytes)
		return
	}
	rows := 0
	for _, n := range c.Rows {
		rows += n
	}
	fmt.Printf("Holds %s: %d rows and %d files (%d bytes).\n", kindList(h.Kinds), rows, c.Files, c.FileBytes)
	if h.Secrets {
		fmt.Println("It holds authentication, which bam restore restores with it.")
	}
	if n := len(c.Manifest.Missing); n > 0 {
		fmt.Printf("%d files its rows name were missing when it was written.\n", n)
	}
	if parts > 1 {
		fmt.Printf("Part 1 of %d: restoring it restores the rows; the other parts add the rest of the files.\n", parts)
	}
	fmt.Println("Restoring it replaces those kinds in an instance not yet in use, and migrates it if its schema is older.")
}

// backupBeforeReplacing backs up the kinds a replace will replace, all of
// them unless some are chosen, authentication included, and returns where.
func backupBeforeReplacing(st *store.Store, blobs filestore.Blobs, stagingDir string, opts RestoreOptions) (string, error) {
	path := opts.BackupTo
	if path == "" {
		if err := os.MkdirAll(stagingDir, 0o700); err != nil {
			return "", err
		}
		path = filepath.Join(stagingDir, fmt.Sprintf("before-replace-%s.bam.tar.gz", time.Now().UTC().Format("20060102-150405")))
	}
	f, err := os.OpenFile(path+".partial", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	_, err = archive.Write(context.Background(), st, blobs, func(int) (io.WriteCloser, error) { return f, nil },
		archive.Options{BamVersion: api.Version, Kinds: opts.Only, Secrets: true})
	f.Close()
	if err == nil {
		err = os.Rename(path+".partial", path)
	}
	if err != nil {
		_ = os.Remove(path + ".partial")
		return "", err
	}
	return path, nil
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// keepOwners gives what a restore run as root made, the database's files
// and the uploaded files, the owners of the directories they are in, so the
// service, running as its own user, can use them (bam-update restores as
// root, README §2.2). The store is closed first, so SQLite's journals are.
func keepOwners(st *store.Store, cfg *config.Config) error {
	if os.Geteuid() != 0 {
		return nil
	}
	_ = st.Close()
	if path, err := store.SQLiteFile(&cfg.Database); err == nil {
		if err := store.OwnLike(filepath.Dir(path)); err != nil {
			return err
		}
	}
	if cfg.Storage.Backend == config.StorageLocal {
		return store.OwnLike(cfg.Storage.LocalPath)
	}
	return nil
}

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/api"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/cli"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

func printUsage() {
	fmt.Fprintf(os.Stderr, `BAM! (Boring Asset Manager) - Physical asset label generator and tracker

Usage:
  bam <command> [arguments]

Available Commands:
  init          Generate a default configuration file (config.toml)
  migrate       Execute pending database migrations and seed system roles
  admin create  Provision an initial superuser account (requires --email and --password; --name optional)
  admin password Set an account's password, signing it out everywhere (requires --email and --password)
  admin mfa reset Remove an account's second factors, signing it out everywhere (requires --email)
  admin relay add Add a relay, printing the one-time code it pairs with (requires --name)
  relay join    Pair this machine as a relay: bam relay join <url> <code> (or $BAM_RELAY_JOIN; --state <file>)
  relay         Run this machine as a relay, beside its printers (--state <file>, --config <file> with [relay] allow;
                --bonemesh=false or --websocket=false turns a transport off)
  serve         Start the HTTP daemon
  backup        Write an archive of the instance, files included (requires --to; --split <size>, --without-secrets,
                --without-documents);
                a --to ending in .db copies a SQLite database instead, and exits 3 for other databases
  storage sweep Remove uploaded files no row names, older than a day, and list rows whose file is missing (--dry-run removes nothing)
  storage copy  Copy every uploaded file to the store another config file's [storage] names, checking each (requires --to)
  restore       Restore an archive into an instance not yet in use, or put a .db copy back (requires --from;
                --only <kinds>, --check; --replace [--backup-to <file>] replaces kinds in one in use;
                --wipe empties the database first; stop BAM first)
  export        Write a package of part of the instance, with what it needs (requires --select and --to, and --name;
                --photos (their attachments), --links, --history with assets; --without-documents;
                --description, --author, --license, --version, --preview <image>;
                --plan says what it would hold, writing nothing)
  import        Merge a package, or kinds of a backup, into the instance (requires --from; --anchor <category>;
                --choose kind:name=existing|copy|skip for what clashes; --only <kinds> of a backup; --check changes nothing)
  email test    Send a test email through the [email] server in the config file (requires --to)
  update failed Record that an update to another release failed and was rolled back, for bam-update
                (requires --tried; --reason, --restored)
  mcp           Relay MCP between stdio and a server, for assistants (requires --url; API token in $BAM_TOKEN)
  version       Print version information

Global Flags:
  --config      Path to configuration TOML file (default: ./config.toml)

`)
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "init":
		initCmd := flag.NewFlagSet("init", flag.ExitOnError)
		configPath := initCmd.String("config", "config.toml", "Path to config file to create")
		_ = initCmd.Parse(os.Args[2:])

		if err := cli.Init(*configPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "migrate":
		migrateCmd := flag.NewFlagSet("migrate", flag.ExitOnError)
		configPath := migrateCmd.String("config", "config.toml", "Path to configuration file")
		_ = migrateCmd.Parse(os.Args[2:])

		if err := cli.Migrate(*configPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "admin":
		if len(os.Args) >= 4 && os.Args[2] == "relay" && os.Args[3] == "add" {
			addCmd := flag.NewFlagSet("admin relay add", flag.ExitOnError)
			name := addCmd.String("name", "", "The relay's name: lowercase letters, digits and hyphens")
			configPath := addCmd.String("config", "config.toml", "Path to configuration file")
			_ = addCmd.Parse(os.Args[4:])
			if err := cli.AdminRelayAdd(*configPath, *name, os.Stdout); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}
		mfaReset := len(os.Args) >= 4 && os.Args[2] == "mfa" && os.Args[3] == "reset"
		mfaRequire := len(os.Args) >= 4 && os.Args[2] == "mfa" && os.Args[3] == "require"
		if !mfaReset && !mfaRequire && (len(os.Args) < 3 || (os.Args[2] != "create" && os.Args[2] != "password")) {
			fmt.Fprintf(os.Stderr, "Usage: bam admin create --email <email> --password <password> [--name <name>] [--config <path>]\n")
			fmt.Fprintf(os.Stderr, "       bam admin password --email <email> --password <password> [--config <path>]\n")
			fmt.Fprintf(os.Stderr, "       bam admin mfa reset --email <email> [--config <path>]\n")
			fmt.Fprintf(os.Stderr, "       bam admin mfa require --role <name> [--off] [--config <path>]\n")
			fmt.Fprintf(os.Stderr, "       bam admin relay add --name <name> [--config <path>]\n")
			os.Exit(1)
		}
		if mfaRequire {
			requireCmd := flag.NewFlagSet("admin mfa require", flag.ExitOnError)
			role := requireCmd.String("role", "", "Role whose holders must sign in with a second factor, such as admin")
			off := requireCmd.Bool("off", false, "Stop requiring it instead")
			configPath := requireCmd.String("config", "config.toml", "Path to configuration file")
			_ = requireCmd.Parse(os.Args[4:])
			if err := cli.AdminRequireFactor(*configPath, *role, !*off); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}
		if mfaReset {
			resetCmd := flag.NewFlagSet("admin mfa reset", flag.ExitOnError)
			email := resetCmd.String("email", "", "Account email address")
			configPath := resetCmd.String("config", "config.toml", "Path to configuration file")
			_ = resetCmd.Parse(os.Args[4:])
			if err := cli.AdminResetFactors(*configPath, *email); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}
		adminCmd := flag.NewFlagSet("admin "+os.Args[2], flag.ExitOnError)
		email := adminCmd.String("email", "", "Account email address")
		password := adminCmd.String("password", "", "Password")
		configPath := adminCmd.String("config", "config.toml", "Path to configuration file")
		var name *string
		if os.Args[2] == "create" {
			name = adminCmd.String("name", "", "Name shown in history and lists")
		}
		_ = adminCmd.Parse(os.Args[3:])

		var err error
		if os.Args[2] == "create" {
			err = cli.AdminCreate(*configPath, *email, *password, *name)
		} else {
			err = cli.AdminPassword(*configPath, *email, *password)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "serve":
		serveCmd := flag.NewFlagSet("serve", flag.ExitOnError)
		configPath := serveCmd.String("config", "config.toml", "Path to configuration file")
		_ = serveCmd.Parse(os.Args[2:])

		if err := cli.Serve(*configPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "backup", "restore":
		cmd := flag.NewFlagSet(command, flag.ExitOnError)
		configPath := cmd.String("config", "config.toml", "Path to configuration file")
		to := cmd.String("to", "", "File to write the backup to: an archive, or a copy of a SQLite database if it ends in .db (backup)")
		from := cmd.String("from", "", "Backup to restore (restore)")
		withoutSecrets := cmd.Bool("without-secrets", false, "Leave authentication out: password hashes, sessions, tokens, OAuth and notification channels' secrets (backup)")
		withoutDocuments := cmd.Bool("without-documents", false, "Leave out attachments that are not images: documents and other files (backup)")
		split := cmd.String("split", "", "Write the archive in parts of at most this size, such as 4GiB (backup)")
		only := cmd.String("only", "", "Restore only these kinds of information, such as assets,catalog (restore)")
		check := cmd.Bool("check", false, "Check the archive and say what restoring it would do, changing nothing (restore)")
		replace := cmd.Bool("replace", false, "Replace the kinds chosen in an instance in use with the archive's, backing them up first (restore)")
		backupTo := cmd.String("backup-to", "", "Where --replace backs up what it replaces; by default, the staging directory (restore)")
		wipe := cmd.Bool("wipe", false, "Drop every table of the database first, putting the instance back as the archive has it (restore)")
		_ = cmd.Parse(os.Args[2:])

		var err error
		if command == "backup" {
			opts := cli.BackupOptions{WithoutSecrets: *withoutSecrets, WithoutDocuments: *withoutDocuments}
			if *split != "" {
				opts.Split, err = cli.ParseSize(*split)
			}
			if err == nil {
				err = cli.Backup(*configPath, *to, opts)
			}
		} else {
			opts := cli.RestoreOptions{Check: *check, Replace: *replace, BackupTo: *backupTo, Wipe: *wipe}
			if *only != "" {
				opts.Only, err = cli.ParseKinds(*only)
			}
			if err == nil {
				err = cli.Restore(*configPath, *from, opts)
			}
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			// The updater tells "back this database up yourself" apart from
			// a failure by this code.
			if errors.Is(err, store.ErrBackupUnsupported) {
				os.Exit(3)
			}
			os.Exit(1)
		}

	case "export":
		cmd := flag.NewFlagSet(command, flag.ExitOnError)
		configPath := cmd.String("config", "config.toml", "Path to configuration file")
		to := cmd.String("to", "", "File to write the package to")
		var opts cli.ExportOptions
		cmd.Func("select", "What to package, kind:name, repeated or separated by commas: "+cli.SelectKinds(), func(s string) error {
			opts.Select = append(opts.Select, s)
			return nil
		})
		cmd.BoolVar(&opts.Photos, "photos", false, "Bring the assets' attachments, photos among them")
		cmd.BoolVar(&opts.WithoutDocuments, "without-documents", false, "Leave out attachments that are not images: documents and other files")
		cmd.BoolVar(&opts.Links, "links", false, "Bring the links between the assets packaged")
		cmd.BoolVar(&opts.History, "history", false, "Bring the assets' history, without who did what")
		cmd.BoolVar(&opts.Comments, "comments", false, "Bring the categories' comments, without who wrote them")
		cmd.StringVar(&opts.Info.Name, "name", "", "The package's name")
		cmd.StringVar(&opts.Info.Description, "description", "", "What the package is for")
		cmd.StringVar(&opts.Info.Author, "author", "", "Who made it")
		cmd.StringVar(&opts.Info.License, "license", "", "Its license, such as CC-BY-4.0")
		cmd.StringVar(&opts.Info.Version, "version", "", "Its version")
		cmd.StringVar(&opts.PreviewFile, "preview", "", "A preview image, PNG, JPEG or WebP, of at most 1 MiB")
		cmd.BoolVar(&opts.Plan, "plan", false, "Say what the package would hold, writing nothing")
		_ = cmd.Parse(os.Args[2:])
		if err := cli.Export(*configPath, *to, opts); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "import":
		cmd := flag.NewFlagSet(command, flag.ExitOnError)
		configPath := cmd.String("config", "config.toml", "Path to configuration file")
		from := cmd.String("from", "", "Package, or backup, to import")
		only := cmd.String("only", "", "Merge only these kinds of a backup, such as catalog,assets")
		var opts cli.ImportOptions
		cmd.StringVar(&opts.Anchor, "anchor", "", "The category, by name or ID, its top categories go under; by default, the top level")
		cmd.Func("choose", "What to do with what clashes, kind:name=existing, copy or skip, repeated or separated by commas", func(s string) error {
			opts.Choose = append(opts.Choose, s)
			return nil
		})
		cmd.BoolVar(&opts.Check, "check", false, "Say what importing would do, changing nothing")
		_ = cmd.Parse(os.Args[2:])
		var err error
		if *only != "" {
			opts.Only, err = cli.ParseKinds(*only)
		}
		if err == nil {
			err = cli.Import(*configPath, *from, opts)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "storage":
		if len(os.Args) < 3 || (os.Args[2] != "sweep" && os.Args[2] != "copy") {
			fmt.Fprintf(os.Stderr, "Usage: bam storage sweep [--dry-run] [--config <path>]\n       bam storage copy --to <config> [--config <path>]\n")
			os.Exit(1)
		}
		cmd := flag.NewFlagSet("storage "+os.Args[2], flag.ExitOnError)
		configPath := cmd.String("config", "config.toml", "Path to configuration file")
		dryRun := cmd.Bool("dry-run", false, "List what would be removed, removing nothing (sweep)")
		to := cmd.String("to", "", "Configuration file whose [storage] names the store to copy to (copy)")
		_ = cmd.Parse(os.Args[3:])
		var err error
		if os.Args[2] == "sweep" {
			err = cli.StorageSweep(*configPath, *dryRun)
		} else {
			err = cli.StorageCopy(*configPath, *to)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "email":
		if len(os.Args) < 3 || os.Args[2] != "test" {
			fmt.Fprintf(os.Stderr, "Usage: bam email test --to <address> [--config <path>]\n")
			os.Exit(1)
		}
		cmd := flag.NewFlagSet("email test", flag.ExitOnError)
		configPath := cmd.String("config", "config.toml", "Path to configuration file")
		to := cmd.String("to", "", "Address to send the test email to")
		_ = cmd.Parse(os.Args[3:])
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		err := cli.EmailTest(ctx, *configPath, *to)
		stop()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "update":
		if len(os.Args) < 3 || os.Args[2] != "failed" {
			fmt.Fprintf(os.Stderr, "Usage: bam update failed --tried <version> [--reason <why>] [--restored] [--config <path>]\n")
			os.Exit(1)
		}
		cmd := flag.NewFlagSet("update failed", flag.ExitOnError)
		configPath := cmd.String("config", "config.toml", "Path to configuration file")
		tried := cmd.String("tried", "", "The release the update tried to install")
		reason := cmd.String("reason", "", "Why it failed, as a clause: it did not answer its health check")
		restored := cmd.Bool("restored", false, "The database was restored from the backup taken before the update")
		_ = cmd.Parse(os.Args[3:])
		if err := cli.UpdateFailed(*configPath, *tried, *reason, *restored); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "mcp":
		mcpCmd := flag.NewFlagSet("mcp", flag.ExitOnError)
		url := mcpCmd.String("url", "", "Address of the BAM! server, such as https://bam.example.org")
		tokenEnv := mcpCmd.String("token-env", "BAM_TOKEN", "Environment variable holding the API token")
		_ = mcpCmd.Parse(os.Args[2:])

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		err := cli.MCP(ctx, *url, os.Getenv(*tokenEnv), os.Stdin, os.Stdout)
		stop()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "relay":
		if len(os.Args) < 3 || os.Args[2] != "join" {
			runCmd := flag.NewFlagSet("relay", flag.ExitOnError)
			state := runCmd.String("state", "", "Where the pairing is kept (default: $BAM_RELAY_STATE, or relay.json)")
			configPath := runCmd.String("config", "", "Configuration file with [relay] allow and [printers] devices (optional)")
			bonemesh := runCmd.Bool("bonemesh", true, "Link over BoneMesh, where the main node offers it")
			websocket := runCmd.Bool("websocket", true, "Link over WebSocket, where the main node offers it")
			_ = runCmd.Parse(os.Args[2:])
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			err := cli.RelayRun(ctx, *configPath, *state, *bonemesh, *websocket, api.Version)
			stop()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}
		joinCmd := flag.NewFlagSet("relay join", flag.ExitOnError)
		state := joinCmd.String("state", "", "Where to keep the pairing (default: $BAM_RELAY_STATE, or relay.json)")
		_ = joinCmd.Parse(os.Args[3:])
		mainURL, code := joinCmd.Arg(0), joinCmd.Arg(1)
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		err := cli.RelayJoin(ctx, mainURL, code, *state, api.Version, os.Stdout)
		stop()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "version", "--version", "-v":
		fmt.Printf("BAM! version %s\n", api.Version)

	case "help", "--help", "-h":
		printUsage()

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

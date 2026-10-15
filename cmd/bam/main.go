package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

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
  admin create  Provision an initial superuser account (requires --email and --password)
  serve         Start the HTTP daemon
  backup        Copy a SQLite database (requires --to); exits 3 for other databases
  restore       Replace a SQLite database with a backup (requires --from; stop BAM first)
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
		if len(os.Args) < 3 || os.Args[2] != "create" {
			fmt.Fprintf(os.Stderr, "Usage: bam admin create --email <email> --password <password> [--config <path>]\n")
			os.Exit(1)
		}
		adminCmd := flag.NewFlagSet("admin create", flag.ExitOnError)
		email := adminCmd.String("email", "", "Admin email address")
		password := adminCmd.String("password", "", "Admin password")
		configPath := adminCmd.String("config", "config.toml", "Path to configuration file")
		_ = adminCmd.Parse(os.Args[3:])

		if err := cli.AdminCreate(*configPath, *email, *password); err != nil {
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
		to := cmd.String("to", "", "File to write the backup to (backup)")
		from := cmd.String("from", "", "Backup to restore (restore)")
		_ = cmd.Parse(os.Args[2:])

		var err error
		if command == "backup" {
			err = cli.Backup(*configPath, *to)
		} else {
			err = cli.Restore(*configPath, *from)
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

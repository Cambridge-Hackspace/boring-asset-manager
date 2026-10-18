package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"

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
  serve         Start the HTTP daemon
  backup        Copy a SQLite database (requires --to); exits 3 for other databases
  restore       Replace a SQLite database with a backup (requires --from; stop BAM first)
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
		if len(os.Args) < 3 || (os.Args[2] != "create" && os.Args[2] != "password") {
			fmt.Fprintf(os.Stderr, "Usage: bam admin create --email <email> --password <password> [--name <name>] [--config <path>]\n")
			fmt.Fprintf(os.Stderr, "       bam admin password --email <email> --password <password> [--config <path>]\n")
			os.Exit(1)
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

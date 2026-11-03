# Configuration

BAM! reads a small TOML file at startup, and is set up and maintained from the `bam` command line.
The file holds only what is needed to start: the listening address, the database connection, where
uploaded files are kept, which printer devices may be used, whether the MCP endpoint is on, and the
email server, if any.
Everything else lives in the database and is managed through the web interface or the API.

## The configuration file

`bam init` writes a default `config.toml`. Its schema:

```toml
[server]
host = "0.0.0.0"
port = 8080
base_url = "http://localhost:8080"
# Reverse proxies whose X-Forwarded-For and Forwarded headers say who the
# client is, as addresses or CIDR ranges. Empty trusts no header.
# Environment: BAM_SERVER_TRUSTED_PROXIES, comma-separated.
# trusted_proxies = ["127.0.0.1", "10.0.0.0/8"]

[database]
# Drivers: "sqlite", "postgres", "mariadb"
type = "sqlite"
# SQLite DSNs use modernc.org/sqlite `_pragma` parameters. Foreign key
# enforcement is always switched on by BAM regardless of the DSN.
dsn = "data/bam.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"

[storage]
# Where uploaded files (asset photos, label images) are kept: "local", a
# directory, or "s3", a bucket of an S3-compatible store (below).
# Environment: BAM_STORAGE_BACKEND, BAM_STORAGE_LOCAL_PATH.
backend = "local"
local_path = "./data/uploads"
# Where archives uploaded through the web wait until they are used (docs/backups.md),
# with the local backend; empty is a directory named staging beside
# local_path. With s3, they wait in the bucket. Environment:
# BAM_STORAGE_STAGING_PATH.
# staging_path = ""

# Only with backend = "s3". Environment: BAM_STORAGE_S3_ENDPOINT, _REGION,
# _BUCKET, _PREFIX, _PATH_STYLE (true or false), _ACCESS_KEY_ID and
# _SECRET_ACCESS_KEY.
# [storage.s3]
# endpoint = "https://s3.eu-west-2.amazonaws.com"
# region = "eu-west-2"
# bucket = "bam"
# prefix = ""
# path_style = false
# access_key_id = ""
# secret_access_key = ""

[printers]
# Device paths printers may be connected to directly (docs/printing.md), as
# path.Match patterns. Empty (the default) turns device connections off.
# Environment: BAM_PRINTERS_DEVICES, comma-separated.
devices = []

[mcp]
# The MCP endpoint, /api/v1/mcp, for assistants (docs/mcp.md). On unless turned
# off, including for files written before it existed.
# Environment: BAM_MCP_ENABLED (true or false).
enabled = true

[notifications]
# How many days finished deliveries stay in the delivery log, and when a
# channel that keeps failing is turned off: after failing every attempt for
# disable_after_hours, or, when set, after disable_after_failures failed
# attempts in a row (docs/notifications.md). 0 turns each off.
keep_days = 30
disable_after_hours = 24
disable_after_failures = 0

[email]
# The SMTP server email is sent through (docs/notifications.md). Email is off
# while host is empty. tls is "starttls" (the default, port 587), "implicit"
# (port 465) or "none", which sends in the clear. ca is a PEM certificate to
# trust, for a server whose certificate no public authority signed.
# Environment: BAM_EMAIL_HOST, _PORT, _TLS, _USERNAME, _PASSWORD, _FROM,
# _REPLY_TO and _CA.
host = ""
port = 0
tls = "starttls"
username = ""
password = ""
from = "BAM! <bam@example.org>"
reply_to = ""
ca = ""

[auth]
# Seals second factors' secrets in the database, so a copy of the database
# alone cannot make their codes (docs/access.md): 32 random bytes as base64.
# bam init writes one. Keep it, and keep it the same: with another key, apps
# set up before cannot be checked. Without it, two-factor sign-in cannot be
# set up. Environment: BAM_AUTH_SECRET_KEY.
secret_key = "…"

```

### Environment overrides

Several settings can also be given in the environment:

| Setting | Environment variable |
|---|---|
| `storage.backend` | `BAM_STORAGE_BACKEND` |
| `storage.local_path` | `BAM_STORAGE_LOCAL_PATH` |
| `storage.staging_path` | `BAM_STORAGE_STAGING_PATH` |
| `storage.s3.endpoint` | `BAM_STORAGE_S3_ENDPOINT` |
| `storage.s3.region` | `BAM_STORAGE_S3_REGION` |
| `storage.s3.bucket` | `BAM_STORAGE_S3_BUCKET` |
| `storage.s3.prefix` | `BAM_STORAGE_S3_PREFIX` |
| `storage.s3.path_style` | `BAM_STORAGE_S3_PATH_STYLE` (`true` or `false`) |
| `storage.s3.access_key_id` | `BAM_STORAGE_S3_ACCESS_KEY_ID` |
| `storage.s3.secret_access_key` | `BAM_STORAGE_S3_SECRET_ACCESS_KEY` |
| `server.trusted_proxies` | `BAM_SERVER_TRUSTED_PROXIES`, comma-separated |
| `printers.devices` | `BAM_PRINTERS_DEVICES`, comma-separated |
| `mcp.enabled` | `BAM_MCP_ENABLED` (`true` or `false`) |
| `email.host`, `email.port`, `email.tls` | `BAM_EMAIL_HOST`, `BAM_EMAIL_PORT`, `BAM_EMAIL_TLS` |
| `email.username`, `email.password` | `BAM_EMAIL_USERNAME`, `BAM_EMAIL_PASSWORD` |
| `email.from`, `email.reply_to`, `email.ca` | `BAM_EMAIL_FROM`, `BAM_EMAIL_REPLY_TO`, `BAM_EMAIL_CA` |
| `auth.secret_key` | `BAM_AUTH_SECRET_KEY` |

How to set them for an installed service is in [Installing](installing.md).

### Behind a reverse proxy

BAM! knows who a request comes from by the address that connected to it. Behind a reverse proxy,
that is always the proxy, so slowing down password guessing by address would hold back everyone at
once, and the live connections list would show only the proxy.

Name the proxies in `server.trusted_proxies`, as addresses (`127.0.0.1`) or CIDR ranges
(`10.0.0.0/8`). A request from one of them comes from the rightmost address in its
`X-Forwarded-For` header (or, without one, its `Forwarded` header) that is not itself a trusted
proxy. Addresses further left are whatever the client chose to send, and are ignored.

A request from anywhere else comes from its own address, whatever its headers say, so a client
cannot claim another address by sending them. Leave the list empty when BAM! is reached directly.

## Where uploaded files are kept

Asset photos, their thumbnails and label images are files. They are kept through one interface,
`internal/filestore`, so the rest of bam treats a file in a directory and an object in a bucket
alike.

Each file is stored by a key, `photos/<id>` or `images/<id>`. Files are always served by bam, which
checks who may see them. There are no links to the store that would work for anyone holding one.

### Backends

- **`local`** (the default): files in `storage.local_path`, each at its key below it, as they
  always were. Each is written to a temporary file and renamed into place.
- **`s3`:** objects in a bucket of any S3-compatible store (AWS, MinIO, Garage, Ceph, Backblaze
  B2, Cloudflare R2). Each object is named `prefix` followed by its key, so one bucket can hold
  more than bam's.

### S3 details

- Requests are signed with AWS Signature Version 4 by bam's own small client, with no SDK.
- `path_style` puts the bucket in the path (`endpoint/bucket/key`), as most stores but AWS expect.
  Otherwise the bucket goes in the host name.
- `region` is `us-east-1` when empty.
- The keys are never logged.

### Checking the store at startup

At startup, `bam serve` writes, reads back and deletes a small object. A store that cannot be
reached or written therefore stops it with a plain error, rather than failing the first upload.
Its log says which store it uses ("Uploaded files are kept in S3 bucket bam at https://…").

With PostgreSQL or MariaDB and `s3`, an instance keeps nothing on its own disk. It can move hosts,
or run as more than one copy, without copying a directory.

### Keeping files and rows in step

A file and its row cannot be written in one transaction. So a file is written before its row and
deleted after it, and a sweep tidies what a failure part way leaves.

The sweep:

- removes each file no row names (an asset photo, its thumbnail, a label image) once it is a day
  old, so as never to take an upload under way
- reports the rows whose file is missing

`bam serve` sweeps at startup and every six hours, logging what it finds. `bam storage sweep`
sweeps once. It lists what it removes (or, with `--dry-run`, would remove), what it keeps for now,
and what is missing.

### Moving between stores

`bam storage copy --to <file>` copies every uploaded file to the store named by the `[storage]`
table of another configuration file. It reads each file back to check it is the same, and refuses
to copy a store onto itself.

To move:

1. Stop BAM.
2. Copy.
3. Point the configuration at the new store.

## Command line

BAM does not use a web-based setup wizard for core security initialization. Provisioning is done
with the `bam` command:

```bash
# Generate a default configuration file
./bam init --config ./config.toml

# Run database migrations
./bam migrate --config ./config.toml

# Provision the initial administrator account (bypasses lockout guards)
./bam admin create \
  --email "admin@example.com" \
  --password "temporary-secure-password" \
  --name "Ada Admin" \
  --config ./config.toml

# Later, for anyone locked out: set their password, signing them out everywhere
./bam admin password --email "admin@example.com" --password "another-password" --config ./config.toml

# For anyone who has lost their authenticator app: remove their second factors,
# signing them out everywhere (docs/access.md)
./bam admin mfa reset --email "admin@example.com" --config ./config.toml

# Start the daemon
./bam serve --config ./config.toml

# Back up the instance, files and all, on any database (safe while running),
# and restore it into a new one (with BAM stopped); docs/backups.md. A --to ending in
# .db copies a SQLite database instead, which restore puts back (docs/installing.md)
./bam backup --config ./config.toml --to ./bam-backup.bam.tar.gz
./bam restore --config ./config.toml --from ./bam-backup.bam.tar.gz
./bam backup --config ./config.toml --to ./bam-backup.db
./bam restore --config ./config.toml --from ./bam-backup.db

# Package part of the instance to share (docs/packages.md), and merge one into another,
# under a category, saying first what it would do
./bam export --config ./config.toml --select subtree:Tools --name Tools --to ./tools.bam.tar.gz
./bam import --config ./config.toml --from ./tools.bam.tar.gz --anchor Lab --check

# Keep uploaded files in step with the database: remove those no row names,
# older than a day, and list rows whose file is missing (--dry-run removes
# nothing); and copy every uploaded file to the store another configuration
# file's [storage] names, as from a directory to a bucket (docs/configuration.md)
./bam storage sweep --config ./config.toml --dry-run
./bam storage copy --config ./config.toml --to ./config.s3.toml

# Relay an assistant that starts local programs to a server's MCP endpoint,
# as the API token in $BAM_TOKEN (docs/mcp.md)
BAM_TOKEN=bam_… ./bam mcp --url https://bam.example.org

# Send a test email through the [email] server (docs/notifications.md)
./bam email test --config ./config.toml --to you@example.org

```

### Commands at a glance

| Command | What it does | Read more |
|---|---|---|
| `bam init` | Writes a default configuration file, with a new `[auth] secret_key`. | |
| `bam migrate` | Runs database migrations. | |
| `bam admin create` | Provisions the first administrator, bypassing lockout guards. | [Access](access.md) |
| `bam admin password` | Sets a locked-out user's password, signing them out everywhere. | [Access](access.md) |
| `bam admin mfa reset` | Removes a user's second factors, admins included, signing them out everywhere. | [Access](access.md#two-factor-sign-in) |
| `bam serve` | Starts the server. | |
| `bam backup` | Backs up the instance as an archive, files and all, on any database, safely while running. With a `--to` ending in `.db`, copies a SQLite database instead. | [Backups](backups.md) |
| `bam restore` | Restores an archive into a new instance, with BAM stopped, or puts back a SQLite `.db` copy. | [Backups](backups.md), [Installing](installing.md) |
| `bam export` | Packages part of the instance to share. | [Packages](packages.md) |
| `bam import` | Merges a package into the instance under a category; `--check` says first what it would do. | [Packages](packages.md) |
| `bam storage sweep` | Removes uploaded files no row names, older than a day, and lists rows whose file is missing. `--dry-run` removes nothing. | [Above](#keeping-files-and-rows-in-step) |
| `bam storage copy` | Copies every uploaded file to the store another configuration file's `[storage]` names. | [Above](#moving-between-stores) |
| `bam mcp` | Relays an assistant that starts local programs to a server's MCP endpoint, as the API token in `$BAM_TOKEN`. | [MCP](mcp.md) |
| `bam email test` | Sends a test email through the `[email]` server, so a mistake shows before a notification is missed. | [Notifications](notifications.md#email) |

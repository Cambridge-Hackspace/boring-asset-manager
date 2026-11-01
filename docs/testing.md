# Testing

BAM! is tested at several levels, from pure unit tests and golden files to full-stack runs on
every supported database and a browser battery for the web interface. Before pushing, the whole
portfolio runs on a remote ephemeral virtual machine through
[`reaper`](https://forge.axonibyte.com/axonibyte/reaper). This page covers how to run the suites,
locally and through reaper, and what each one checks.

## The pre-push loop with reaper

BAM is onboarded as a tenant of reaper via [`.reaper.toml`](../.reaper.toml), for fast, isolated
pre-push validation on remote ephemeral Proxmox VE virtual machines.

```bash
reaper up        # Provisions remote VM, lays out ZFS pools, fetches pinned toolchains
reaper test      # Syncs uncommitted working tree -> builds -> ZFS state rollback -> runs test suite
reaper down      # Tears down remote VM (cull daemon provides automatic TTL backstop)
```

### What `reaper test` runs

`reaper test` builds the binary in the pinned Go image, then runs `tools/reaper/run.sh` on the
guest. The script, in order:

1. Fetches the icon compiler, as `wasm.lock` pins it ([Architecture](architecture.md)).
2. Runs the Go suites, with the SQLite database and uploads in `$REAPER_STATE`:
   - with `-race`;
   - with a timeout of 30 minutes for each package, rather than `go test`'s 10;
   - with `BAM_REQUIRE_ISOLEX=1`, under which the compiler's tests fail without it, rather than
     skip as a plain `go test` does.
3. Runs `svelte-check` and Vitest.
4. Runs the Playwright and `axe` battery ([Web interface tests](#web-interface-tests)).

Each step runs in its own pinned container. So the run verb executes on the guest
(`exec = "host"`) and starts them with its podman; a containerized verb cannot start containers.

The images are listed under `run.images`, so reaper pulls them first. `node_modules`, the npm cache
and Go's caches live in reaper's caches, rather than on the guest's small root disk. A test
(`tools/reaper`) checks that every image the scripts run is in `run.images`, in the digest-only form
reaper accepts.

The script runs the same way by hand, with `REAPER_STATE` and the `REAPER_CACHE_*` variables
pointing at directories of your own.

### State rollback

Ephemeral databases and uploads live under `$REAPER_STATE` (`tank/state`). This enables
near-instantaneous ZFS rollbacks to `@pristine` between test iterations.

## Web interface tests

The web interface has its own checks. They run in pinned containers, and need no Node on the host.

### `tools/web/build.sh check`

Runs `svelte-check` and the Vitest tests:

- `svelte-check` checks types and Svelte's accessibility warnings, which fail the run.
- The Vitest unit and component tests run against a fake API.

### `tools/web/e2e.sh`

Runs the Playwright battery in `web/e2e`. It builds the interface and a Linux binary, and starts
`bam serve` on a fresh SQLite database inside the Playwright container. It then drives Chromium
through:

- sign-in
- fields and categories
- assets and labels
- CSV export and import
- thresholds and notification channels
- the template editor, by mouse and keyboard
- administration

Printing goes to in-process fake TCP printers, whose jobs are checked byte by byte. The server's
outgoing HTTPS is pointed at a closed port, so notifications never leave the container.

Every view is audited with `axe-core` for WCAG 2.1 A and AA, in both light and dark mode.
Keyboard-only walks cover the main flows.

Arguments filter the specs:

```bash
tools/web/e2e.sh editor a11y
```

### Caches

reaper runs both scripts. The scripts keep `node_modules`, the npm cache and Go's caches in named
volumes. `BAM_WEB_NODE_MODULES`, `BAM_NPM_CACHE`, `BAM_GO_CACHE` and `BAM_GO_MOD` put them in
directories instead.

## Running the suites against PostgreSQL and MariaDB

By default, every test uses SQLite. Setting `BAM_TEST_DIALECT` and `BAM_TEST_DSN` reruns the store,
API, notification, CSV and integration suites against a live server.

Each test creates and drops its own schema (PostgreSQL) or database (MariaDB). So the DSN must
belong to a role allowed to do that. On PostgreSQL it must also be allowed to create databases, for
the encoding tests.

```bash
BAM_TEST_DIALECT=postgres BAM_TEST_DSN='postgres://postgres:secret@localhost:5432/postgres?sslmode=disable' go test ./internal/store/ ./internal/api/ ./internal/notify/ ./internal/assetcsv/ ./internal/integration/

BAM_TEST_DIALECT=mariadb BAM_TEST_DSN='root:secret@tcp(localhost:3306)/' go test ./internal/store/ ./internal/api/ ./internal/notify/ ./internal/assetcsv/ ./internal/integration/
```

### Hostile test databases

These namespaces are deliberately hostile, as a server set up without BAM in mind may be:

- On MariaDB, the database defaults to `latin1` with a case-insensitive collation. Each session
  starts with a Latin-1 connection, no SQL modes, and the time zone `+05:45`.
- On PostgreSQL, each session starts in `Pacific/Chatham` time, with
  `standard_conforming_strings` off.

Everything BAM needs, it sets for itself ([Data model](data-model.md)).

## Running against S3-compatible storage

Setting `BAM_TEST_S3_ENDPOINT`, with `BAM_TEST_S3_BUCKET`, `BAM_TEST_S3_ACCESS_KEY_ID` and
`BAM_TEST_S3_SECRET_ACCESS_KEY`, also runs:

- the file store's conformance suite on an S3-compatible store, as well as on a directory;
- the API suite, photos, images and labels included, with every test server keeping its files
  there, each under a prefix of its own.

Without these variables, those runs are skipped with a message.

MinIO no longer publishes images anyone can pull, so the S3 gateway of
[Versity](https://github.com/versity/versitygw) serves instead, keeping a bucket as a directory:

```bash
podman run -d --pod bamdb --name bams3 --entrypoint sh docker.io/versity/versitygw@sha256:2df780de109d16491cb99c25f4c0bc9085b286e77b5b47b56121b340094e70bb   -c 'mkdir -p /data/bam && exec versitygw --port :9000 --access bamtest --secret bamtest-secret-key posix /data'

BAM_TEST_S3_ENDPOINT=http://localhost:9000 BAM_TEST_S3_BUCKET=bam BAM_TEST_S3_ACCESS_KEY_ID=bamtest BAM_TEST_S3_SECRET_ACCESS_KEY=bamtest-secret-key   go test ./internal/filestore/ ./internal/api/
```

## The testing portfolio

Testing follows the "Portfolio of Oracles" methodology. The tiers:

### Tier 1: pure unit tests and golden vectors

- Vector math
- Micro QR bitpacking
- bcrypt password hashing
- Boundary decoders
- Cross-language barcode golden files: module matrices from Python's `segno`, and scan checks with
  `zxing-cpp`
- Byte-exact SVG, PNG, PDF and printer-stream goldens

### Tier 4: server contract tests

RBAC permission union resolution and anti-lockout invariants, tested as in-process pure functions
without database dependencies.

### Tier 6: full stack and migrations

These run on SQLite, PostgreSQL and MariaDB, with migration idempotence. The store and API suites
run against PostgreSQL and MariaDB by pointing them at a server
([Running the suites against PostgreSQL and MariaDB](#running-the-suites-against-postgresql-and-mariadb)).
The test databases there are deliberately hostile ([Hostile test databases](#hostile-test-databases)).

`internal/integration` checks text handling:

- Awkward text survives storage everywhere the API keeps text: four-byte characters, combining
  marks, right-to-left text, SQL quotes and wildcards, and the longest text allowed.
- NUL characters and overlong text are refused alike on every database.
- Text differing only in case or normalization stays distinct.
- Search treats its term literally.

The store suite:

- upgrades a MariaDB database created before migration `008`, data and all;
- resumes a conversion that stopped part way;
- checks that PostgreSQL databases in `LATIN1` and `SQL_ASCII` are refused.

The integration battery (`internal/integration`) drives a real server over HTTP on each database.
It covers:

- inheritance conflict resolution;
- administrators removing each other at the same moment;
- a template rendered before and after a database round trip: byte-identical SVG, and SVG and PDF
  goldens written on SQLite that every database must reproduce
  (`go test ./internal/integration/ -update` rewrites them);
- import atomicity;
- threshold crossings, including simultaneous archives;
- exactly-once delivery from several workers to several channels.

### Tier 7: seeded fuzzing

`TestSeededFuzz` (`internal/api`) sends 1,500 requests to every endpoint. Each is made by mutating
a valid one:

- values of the wrong type, out of range, or awkward
- missing and extra keys
- deep nesting
- broken JSON
- damaged forms and uploads
- junk and path-traversing IDs
- imports of generated CSV

Every response must be below `500`. Every API error must be a JSON object with a message, and every
JSON response must be valid. Afterwards, the server must still answer and still know the session.

`TestSeededImports` (`internal/assetcsv`) plans 300 generated files and holds the planner to its
word. A file without problems must import. Exporting everything afterwards, and planning that
again, must change nothing.

Both are deterministic, so a failure repeats. `BAM_FUZZ_SEED`, `BAM_FUZZ_REQUESTS` and
`BAM_FUZZ_IMPORTS` choose another seed and length. Both run on PostgreSQL and MariaDB with the other
database suites.

For longer, coverage-guided runs, Go's fuzzer drives the same checks:

```bash
go test -fuzz=FuzzAPI ./internal/api/
go test -fuzz=FuzzImport ./internal/assetcsv/
```

There are also `FuzzParse` for templates, and the generator fuzz targets in their packages.

Sending to printers and notification services is left out of fuzzing, as those requests would
leave the process.

### Tier 8: concurrency harnesses

Simultaneous write simulations for inventory threshold decrements and per-generator ID
allocations, asserting stored state consistency.

## Portability rules for schema and queries

- Write placeholders as `?`. `Store.Rebind` converts them to `$n` for PostgreSQL.
- Avoid reserved identifiers. `key` is reserved in MariaDB, which is why the corpus, permission and
  settings columns are `field_key`, `permission_key` and `setting_key`.
- Declare foreign keys at table level.
- Use `TIMESTAMP` in migrations. The runner rewrites it to `DATETIME(6)` on MariaDB, where
  `TIMESTAMP` columns silently gain `ON UPDATE CURRENT_TIMESTAMP`.
- The MariaDB driver is always opened with `parseTime=true` and `loc=UTC`, a `utf8mb4` connection,
  a strict SQL mode, and the session time zone `+00:00`.
- Use `TEXT` for unbounded text. The runner rewrites it to `LONGTEXT` on MariaDB, where `TEXT`
  holds only 65,535 bytes, and creates every table in `utf8mb4` with `utf8mb4_nopad_bin`.
- A change that depends on the dialect, or on what is in the database, can be a Go migration
  (`codeMigrations` in `internal/store/migrations`).
- Check text with `internal/textcheck` before it is stored.
- Do not match user input with `LIKE`, whose wildcards, escapes and case folding differ between
  databases.

# BAM! (Boring Asset Manager)

![BAM!](img/bam.png)

> Excruciatingly simple physical asset label generation and tracker.

BAM! is a single-tenant, API-first web application designed for tracking physical assets, managing hierarchical categories with dynamic field inheritance, and visually designing and batch-printing labels for dedicated thermal label printers (such as the Brother QL-810W, DYMO LabelWriter and Rollo) as well as standard sheet layouts.

---

## 1. System Architecture & Tech Stack

### Core Philosophy
- **Self-Contained Deployment:** Distributed as a single compiled binary for FreeBSD, Linux, and Windows with effortless `CGO_ENABLED=0` cross-compilation.
- **Embedded Frontend:** The Go backend serves the compiled Single Page Application (SPA) directly via `embed.FS`.
- **Strict Configuration Separation:** On-disk TOML handles bootstrap infrastructure (listening ports, database connection strings, storage paths, the printer device allowlist). All operational settings, network printer definitions, notification channels, and layouts live inside the database.

### Technology Choices
- **Backend:** Go 1.23+ (Standard library `net/http` or router such as `chi`).
- **Database Abstraction:** Dialect-agnostic SQL driver supporting **SQLite3** (via pure-Go `modernc.org/sqlite` with WAL mode for seamless cross-compilation without C toolchains), **PostgreSQL 14+**, and **MariaDB 10.6+**. Migrations managed via embedded SQL scripts (`golang-migrate` or custom embedded schema runner).
- **Frontend:** TypeScript + Svelte 5, built with Vite and styled with Tailwind CSS, embedded in the binary (see Frontend Architecture below). The template editor draws over the server's own SVG preview, so there is one layout engine, not two.
- **Label Generation & Printing:**
  - **Pure-Go Rendering:** One layout engine feeds SVG, PNG and PDF backends (§4). Fonts, glyph outlines and rasterization come from `golang.org/x/image` (the embedded Go fonts, OpenType parsing and its vector rasterizer); the PDF writer is a small in-house one so output stays deterministic.
  - **Direct Printing:** Each printer's own command stream (Brother QL, DYMO LabelWriter, TSPL) from pluggable print drivers, sent over raw TCP (port 9100), to a USB device on the server, to a raw IPP/CUPS queue, or through the Windows spooler, with no desktop driver or print dialog.
- **Barcode & Matrix Engine:** In-house QR Code, Micro QR and Code 128 encoders with per-generator payload and character set validation (§4.2).

### Frontend Architecture

The web interface is a Svelte 5 single-page application written in TypeScript, built with Vite and styled with Tailwind CSS. It talks only to the public `/api/v1` API, so anything it can do a script can do too, and the server remains the authority on every rule (validation, permissions, inheritance, layout).

* **Layout:** the app lives in `web/`. `package.json` pins exact versions and `package-lock.json` is committed. Dependencies stay few: Svelte, Vite, TypeScript, Tailwind, and the test tools. There is no SvelteKit; routing is a small in-house history router.
* **Build:** `tools/web/build.sh` runs `npm ci` and `vite build` in a pinned Node container and writes the output to `internal/webui/static/app/`, which is ignored by git. Release builds run it before `go build`. `tools/web/build.sh check` type-checks the app and runs its unit tests instead. For development, run `npm run dev` in `web/` next to `bam serve`; Vite forwards `/api` to port 8080.
* **Embedding:** package `internal/webui` embeds `static/` with `embed.FS`. If the app has not been built, the binary serves a committed placeholder page saying so, so a plain `go build`, the Go test suites and reaper never need Node.
* **Serving:** `/api/*`, `/healthz` and `/readyz` are unchanged. Every other `GET` serves a file from the build or, for client-side routes, `index.html`. Hashed assets are cached as immutable; `index.html` is never cached. Every page carries a strict Content Security Policy (`default-src 'self'`, with `data:` and `blob:` images for previews, inline style attributes for the editor's positioning, `frame-ancestors 'none'`), plus `X-Content-Type-Options: nosniff` and `Referrer-Policy: same-origin`.
* **Sessions:** the app uses the existing `bam_session` cookie (`HttpOnly`, `SameSite=Lax`). On load it asks `GET /api/v1/auth/me` for the user, their permissions, and the names of their roles (`roles`), which **Your account** lists with what each permission allows, in words. Any `401` returns to the sign-in page and then back to where the user was.
* **Permissions:** actions the user lacks are hidden or disabled, using the permission list from `/auth/me`. The server still checks every request.
* **Errors:** validation errors from the API are shown beside the field or template element they name (template errors carry a `path`).
* **Phones:** below 640 px the header is one row, with a **Menu** button opening every section, Help and the account; menus near the right edge open inward. Tables of users, loans, assets and the spool become a card per row, each cell under its column's heading (`lib/stack.ts`); the asset list's filters fold behind **Filters**, and help's topics behind **All help topics**, so the article comes first. On touch screens the template editor's handles are larger and its keyboard help gives way to touch's, and small buttons beside text are at least 32 px tall. The phone browser tests check that no page scrolls sideways at 390 px.
* **Accessibility:** the target is WCAG 2.1 AA.
  * Everything works from the keyboard, including the template editor: arrow keys nudge, Shift+arrow moves by a larger step, and Tab moves between elements.
  * Focus is always visible, and every input has a label.
  * Category colors are never the only signal; a name always accompanies them. Text on a category color is black or white, whichever contrasts more.
  * The interface follows `prefers-color-scheme` and `prefers-reduced-motion`.
* **Language:** American English throughout.

#### Template Editor

The editor never reimplements layout in the browser. The canvas shows the label as the server renders it, an SVG from `POST /api/v1/templates/preview`, requested at most a few times a second while the user works. Over it sits an interactive layer of element outlines. Text measurement, barcode module snapping and quiet zones therefore always match what prints.

* **Editing:** select, drag and resize elements on a grid in the template's units, with keyboard nudging, multi-select, z-order and undo/redo. A side panel edits the selected element's properties by type: binding, font, size, weight and alignment for text; symbology and error correction for barcodes; the image for images. Bindings are offered from the field corpus and asset placeholders.
* **Canvas:** units, size and DPI can be set directly or taken from a printer. Choosing a printer applies its loaded media's size and its DPI, and the fit check runs as the user edits.
* **Preview:** placeholder data or a chosen asset. Exports and test prints come straight from the editor.
* **Saving:** templates save through the existing API. Unsaved changes are kept in the browser in case the tab closes.

---

## 2. Configuration & Initialization

### TOML Schema (`config.toml`)
```toml
[server]
host = "0.0.0.0"
port = 8080
base_url = "http://localhost:8080"

[database]
# Drivers: "sqlite", "postgres", "mariadb"
type = "sqlite"
# SQLite DSNs use modernc.org/sqlite `_pragma` parameters. Foreign key
# enforcement is always switched on by BAM regardless of the DSN.
dsn = "data/bam.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"

[storage]
local_path = "./data/uploads"

[printers]
# Device paths printers may be connected to directly (README §4.4), as
# path.Match patterns. Empty (the default) turns device connections off.
# Environment: BAM_PRINTERS_DEVICES, comma-separated.
devices = []

```

### CLI Bootstrap Tooling

BAM does not use a web-based setup wizard for core security initialization. Provisioning is performed via the binary CLI:

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

# Start the daemon
./bam serve --config ./config.toml

# Back up a SQLite database (safe while running), and put a backup back
# (with BAM stopped); see §2.2
./bam backup --config ./config.toml --to ./bam-backup.db
./bam restore --config ./config.toml --from ./bam-backup.db

```

### 2.1 Installing as a Service

BAM is one static binary with the web interface built in. Each release on GitHub has binaries for Linux (amd64 and arm64) and FreeBSD (amd64) (§2.2); `tools/release/build.sh` builds the same from a checkout into `bin/`, needing only podman (or docker, with `CONTAINER=docker`); name other targets as arguments, such as `tools/release/build.sh freebsd/arm64`. The service files and example configurations are in `packaging/`. Both set BAM up the same way: a `bam` account that owns only the data directory, a configuration readable by root and that account alone, BAM listening on loopback behind a reverse proxy that terminates HTTPS, and a restart five seconds after any crash. Database migrations run each time the service starts.

#### Linux (systemd: Debian, Arch and the rest)

As root, from a checkout with the binary built:

```bash
install -m 0755 bin/bam-linux-amd64 /usr/local/bin/bam
install -D -m 0644 packaging/linux/bam.sysusers /etc/sysusers.d/bam.conf
systemd-sysusers                                   # creates the bam account
install -d -m 0750 -o root -g bam /etc/bam
install -m 0640 -o root -g bam packaging/linux/config.example.toml /etc/bam/config.toml
editor /etc/bam/config.toml                        # base_url, and the database if not SQLite
install -m 0644 packaging/linux/bam.service /etc/systemd/system/bam.service
systemctl daemon-reload
systemctl enable --now bam
runuser -u bam -- bam admin create --config /etc/bam/config.toml \
  --email admin@example.org --password 'change-me-at-first-sign-in'
```

The data lives in `/var/lib/bam`, which systemd creates for the service; logs go to the journal (`journalctl -u bam`). Environment overrides (§2) go in `/etc/bam/environment`, one `NAME=value` per line. The unit is sandboxed: BAM can reach the network and write to `/var/lib/bam`, and nothing else. To use printers on USB device files (§4.4), uncomment `SupplementaryGroups=lp` in the unit and list the devices under `[printers]`. To upgrade, replace `/usr/local/bin/bam` and run `systemctl restart bam`.

#### FreeBSD

As root, from a checkout with the binary built:

```sh
install -m 0555 bin/bam-freebsd-amd64 /usr/local/bin/bam
pw groupadd bam
pw useradd bam -g bam -d /var/db/bam -s /usr/sbin/nologin -c "BAM! (Boring Asset Manager)"
install -d -m 0750 -o root -g bam /usr/local/etc/bam
install -m 0640 -o root -g bam packaging/freebsd/config.example.toml /usr/local/etc/bam/config.toml
ee /usr/local/etc/bam/config.toml                  # base_url, and the database if not SQLite
install -m 0555 packaging/freebsd/bam /usr/local/etc/rc.d/bam
sysrc bam_enable=YES
service bam start
su -m bam -c "bam admin create --config /usr/local/etc/bam/config.toml \
  --email admin@example.org --password 'change-me-at-first-sign-in'"
```

The data lives in `/var/db/bam`, which the rc script creates. BAM runs under `daemon(8)`, which logs to syslog with the tag `bam` (`/var/log/messages`). The script's settings, all optional, are listed at its top (`bam_config`, `bam_user`, `bam_dbdir`, and `bam_env` for environment overrides); set them with `sysrc`. To use printers on device files, give the `bam` account write access to them (a `devfs.rules` entry) and list them under `[printers]`. To upgrade, replace `/usr/local/bin/bam` and run `service bam restart`.

#### Windows

Releases include `bam-windows-amd64.exe` (or build it with `tools/release/build.sh windows/amd64`). There is no service packaging or updater for Windows: from a folder of its own, run `bam init` to write a `config.toml` there, set `base_url` and the paths in it, then `bam migrate`, `bam admin create --email … --password …` and `bam serve`, under whatever keeps it running (Task Scheduler at startup, or a service wrapper such as NSSM). Printers can use the `windows` connection, through the spooler (§4.4).

#### The reverse proxy

The example configurations listen on `127.0.0.1:8080` and expect an HTTPS address in `base_url`, which also marks session cookies `Secure`. Any reverse proxy will do. With Caddy, which obtains a certificate itself:

```
bam.example.org {
    reverse_proxy 127.0.0.1:8080
}
```

With nginx, pass `X-Forwarded-Proto` and allow bodies up to 8 MB, for CSV imports (5 MB) and image uploads (2 MB):

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    client_max_body_size 8m;
}
```

To serve plain HTTP on the network without a proxy instead, set `host = "0.0.0.0"` and an `http://` `base_url`.

`tools/service/check-systemd.sh` follows the Linux steps above in a Debian container booted with systemd and checks that the service starts at boot, serves, signs the administrator in, comes back after it is killed and after a reboot, and stops cleanly. The FreeBSD script has no such check, as FreeBSD cannot run in a container; ShellCheck passes it.

### 2.2 Releases & Updates

Each version tag pushed to GitHub, such as `v1.2.0`, is built by `.github/workflows/release.yml`: the Go suites run, then `tools/release/build.sh` builds the binaries with the tag as their version (`bam version`), and they are published as the release's files with `SHA256SUMS`. A tag with a suffix, such as `v1.2.0-rc.1`, is published as a pre-release. The web app's footer, on every page and the sign-in page too, names the running version and links to its release; a build that is not a release says "development build", with the commit it was built from. `GET /api/v1/info`, which needs no sign-in, reports both (`version`, `commit` and whether the tree had changes, `modified`; the commit is recorded by Go when building in a git checkout). `.github/workflows/build.yml` runs the same build on every push and pull request, so a change that breaks it shows up before a tag does. Instead of building, an installation can start from a release (§2.1, using the downloaded file in place of `bin/bam-<os>-<arch>`):

```sh
curl -fLO https://github.com/Cambridge-Hackspace/boring-asset-manager/releases/latest/download/bam-linux-amd64
curl -fLO https://github.com/Cambridge-Hackspace/boring-asset-manager/releases/latest/download/SHA256SUMS
sha256sum -c --ignore-missing SHA256SUMS
# On FreeBSD, with bam-freebsd-amd64 downloaded (fetch works as well as curl):
# sha256 -c "$(awk '$2 == "bam-freebsd-amd64" { print $1 }' SHA256SUMS)" bam-freebsd-amd64
```

#### Updating automatically (optional)

`packaging/bam-update` installs the latest release when it is newer than the installed one, on both systems. It verifies the download against the release's `SHA256SUMS` and checks that the new binary reports the release's version. Then it stops the service, backs up the database, swaps the binary (keeping the old one as `bam.previous`), starts the service, and waits up to 30 seconds for it to answer its health check. If it does not, the service is stopped, the database restored from the backup, so that changes the new version made (such as its migrations) are undone, and the old binary started again. It leaves alone an installation that is up to date, newer than the latest release, or not a release at all (a build from source reports `git describe` or `dev`; `bam-update --force` replaces it). `bam-update --check` only reports. Each run logs to syslog with the tag `bam-update`, and cron mails what it prints when something goes wrong. Pre-releases are never installed.

**Backups before updating.** A SQLite database is copied with `bam backup` into `/var/backups/bam` (readable by root only), and the newest 5 backups are kept (`BAM_UPDATE_KEEP_BACKUPS`). BAM does not back up PostgreSQL or MariaDB itself, so for those the updater refuses to update until you give it commands for both directions in `update.conf`. Each command is run by `sh` with the backup's path in `$BAM_BACKUP_FILE`, for example:

```sh
BAM_UPDATE_BACKUP_COMMAND='pg_dump -Fc -f "$BAM_BACKUP_FILE" bam'
BAM_UPDATE_RESTORE_COMMAND='pg_restore --clean --if-exists -d bam "$BAM_BACKUP_FILE"'
```

`BAM_UPDATE_BACKUP=off` updates without a backup, which leaves a failed update's changes to the database in place. If the backup fails, nothing is changed and the old version is started again. Outside the updater, `bam backup --config <file> --to <file>` copies a SQLite database at any time, even while BAM runs, and `bam restore --config <file> --from <file>` puts one back while BAM is stopped. Neither copies uploaded files: label images and asset photos live under `storage.local_path`, so back that directory up as well.

On Linux (cron must be installed; on Arch, `cronie`):

```bash
install -m 0755 packaging/bam-update /usr/local/sbin/bam-update
install -m 0644 packaging/linux/bam-update.cron /etc/cron.d/bam-update
```

On FreeBSD:

```sh
install -m 0555 packaging/bam-update /usr/local/sbin/bam-update
install -d /usr/local/etc/cron.d
install -m 0644 packaging/freebsd/bam-update.cron /usr/local/etc/cron.d/bam-update
```

Both run it nightly at 04:17; change the minute in the cron file, so that every installation does not ask GitHub at once. Settings (the repository, the binary, the health check URL) are listed at the top of the script; put any in `/etc/bam/update.conf` (`/usr/local/etc/bam/update.conf` on FreeBSD) as `NAME=value` lines. If BAM listens on another port, set `BAM_UPDATE_HEALTH_URL` there.

`tools/service/check-update.sh` checks the updater in a Debian container booted with systemd, against fake releases served from files. It must update to a newer release, leave an up-to-date installation running, and ignore an older release. It must refuse a download that does not match its checksum, and roll back a release that does not come up, together with the change that release made to the database. It must refuse to update a database it cannot back up, run backup and restore commands when given them, and keep only the newest backups. It must leave a build that is not a release alone until forced.

---

## 3. Data Models & Relationship Schema

```
                     ┌──────────────────┐
                     │   Field Corpus   │
                     │  (Global Corpus) │
                     └────────┬─────────┘
                              │
               ┌──────────────┴──────────────┐
               ▼                             ▼
    ┌──────────────────────┐      ┌──────────────────────┐
    │   Category Fields    │      │  Asset Field Values  │
    │ (required / optional)│      │   (stored as text)   │
    └──────────┬───────────┘      └──────────┬───────────┘
               │                             │
               ▼                             ▼
    ┌──────────────────────┐      ┌──────────────────────┐
    │      Categories      │◄────►│        Assets        │
    │  (Single-Parent Tree)│ M:N  │   (UUID, Consumable) │
    └──────────┬───────────┘      └──────────┬───────────┘
               │ 1:M                         │ 1:M
               ▼                             ▼
    ┌──────────────────────┐      ┌──────────────────────┐
    │   Label Templates    │      │ Asset Generator IDs  │
    │  (Declarative Blocks)│      │  (Micro QR / QR IDs) │
    └──────────────────────┘      └──────────────────────┘

```

### 3.1 Field Corpus & Dynamic Inheritance

* **Global Field Corpus (`fields`):**
* Columns: `id (UUID)`, `field_key` (snake_case; exposed as `key` in the API), `label (string)`, `validation_type (string: text|number|date|url|select)`, `options (JSON nullable)`.
* **Options Schema (`options` JSON):** For `select` validation types, `options` stores a structured JSON schema with selectable choices and an optional default value:
  ```json
  {
    "choices": [
      { "label": "Operational", "value": "operational" },
      { "label": "Needs Repair", "value": "needs_repair" },
      { "label": "Decommissioned", "value": "decommissioned" }
    ],
    "default": "operational"
  }
  ```
  *(Enforces unique values per choice and verifies that `default`, if present, corresponds to an existing choice value).*


* **Category Hierarchy (`categories`):**
* Represents a single-parent tree: `parent_id` points to another category or `NULL` (root).
* Flags: `is_consumable (bool)`, `is_running_low (bool)`, `color_hex (string)`, and for consumables an optional `low_stock_threshold (int)` (§5.1).
* Associative table `category_fields`: `(category_id, field_id, is_required)`.


* **Inheritance Engine:**
* A category recursively inherits all fields of its ancestor chain.
* Assets can belong to **multiple** categories (`asset_categories` join table).
* An asset inherits the **union** of all fields defined across all its assigned categories and their respective ancestor trees.
* **Conflict Resolution Rule:** If Field $X$ is marked *Required* in Category $A$ and *Optional* in Category $B$, the *Required* constraint takes precedence.


* **Uncategorized State:**
* If an asset is detached from all categories, it automatically attaches to the virtual/system category `Uncategorized`.
* The `Uncategorized` category hides itself in navigation lists when it contains zero assets.
* **Data Retention Rule:** An asset **never** discards its populated field values upon category detachment. Orphaned field values remain persisted in `asset_field_values`.



### 3.2 Assets & Barcode Generator Mappings

* **Assets (`assets`):**
* Primary identifier: `id` (random UUIDv4).
* Audit timestamps: `created_at`, `updated_at`, `archived_at`.
* **Archiving and restoring:** `DELETE /api/v1/assets/{id}` (`assets.delete`) archives an asset and releases its codes for other assets. Archived assets cannot be edited, used or restocked, and are listed apart: `GET /api/v1/assets?archived=true`, most recently archived first, with the same category and search filters (search finds them by name, since their codes are released). `POST /api/v1/assets/{id}/restore` (`assets.delete`) brings one back with the codes archiving released, except any another asset has taken since; the response lists those as `skipped_codes`. Assets archived before archiving recorded their codes (§5.1) come back without codes. Restoring counts toward thresholds as creating does.
* Optional stock counted by quantity: `stock_quantity`, `stock_unit`, `low_stock_threshold`, `is_running_low` (§5.1).
* **Photos** (migration `018`): up to 10 per asset, in order, the first shown beside it in lists. `POST /api/v1/assets/{id}/photos` (`assets.write`) takes a JPEG or PNG of up to 15 MB, at most 12,000 pixels a side and 50 megapixels, as the `file` part of a multipart form. Every photo is decoded and encoded again, so it keeps no metadata from its file, such as where it was taken; a JPEG is first turned upright as its EXIF orientation says, and photos are scaled down to 2048 pixels on their longer side, with a thumbnail of 320. `GET /api/v1/assets/{id}/photos/{photo_id}` (`assets.read`) serves one, and `?size=thumb` its thumbnail; `DELETE` removes one and `PUT /api/v1/assets/{id}/photos` with `{"photo_ids": [...]}`, naming each once, orders them. Adding and removing are recorded in the asset's history. The files live under `storage.local_path/photos/`. Archived assets cannot be changed.
* **Condition** (migration `013`): `ok`, `needs_maintenance` or `out_of_service`, returned as `condition` with a `condition_note` of up to 255 characters saying what is wrong. `PUT /api/v1/assets/{id}/condition` with `{"condition", "note"}` changes it: reporting a problem needs `assets.report` or `assets.write`, and marking an asset `ok` again needs `assets.write`; an ok asset keeps no note. Each change records `asset.condition_changed` with the condition before and after and the note, which channels can subscribe to (§5.2); the same condition and note again records nothing. Archived assets cannot be changed. The list filters on it with `?condition=`, one of the three or `problem` for any asset that is not ok, and the home page lists assets needing attention.


* **Generator ID Mapping (`asset_generator_keys`):**
* Maps compact, deterministic, or custom constrained strings to an asset for constrained barcodes:
* `asset_id (UUID)`
* `generator_type (string: "qr" | "micro_qr" | "code128")`
* `assigned_code (string, unique per generator_type)`

* **Custom IDs Restricted Per-Generator:**
* Assets can be assigned custom, human-readable IDs or system-allocated short sequences that are strictly validated and constrained per generator type:
  * **Micro QR (`micro_qr`):** Constrained by physical matrix bounds and version capacity (M2–M4). Custom IDs are restricted to the QR alphanumeric character set (`0-9`, `A-Z`, space, `$%*+-./:`) and strictly capped to the largest symbol's (M4-L) payload limits: 35 characters for purely numeric codes, 21 characters otherwise.
  * **Standard QR (`qr`):** Supports arbitrary UTF-8 payloads up to 2953 encoded bytes (version 40-L byte mode) and dynamic URL templates (`{base_url}/a/{asset_uuid}` or custom slug).
  * **Code 128 (`code128`):** Restricted to printable ASCII (32–126) and at most 80 characters, so labels stay scannable.
* **Uniqueness Invariant:** The compound key `(generator_type, assigned_code)` must be unique across all active assets, guaranteeing that scanning any generated code resolves unambiguously to a single asset.




### 3.3 RBAC Security Matrix

* **Tables:** `users`, `roles`, `permissions`, `user_roles`, `role_permissions`, and for scopes `scopes`, `category_scopes` and `role_permission_scopes` (migration `019`).
* **Permissions:** Granular `resource.action` keys (`assets.read`, `assets.write`, `assets.delete`, `templates.manage`, `categories.manage`, `settings.manage`, `users.manage`, from Phase 3 `labels.print`, and from Phase 7 `stock.record`, which uses, restocks and flags stock without being able to edit assets, `assets.report`, which reports an asset needing maintenance or out of service, and `assets.borrow` and `assets.lend` (§5.3), `reports.manage` (§6.3), `assets.scan` (§6.4), `assets.track`, which sees the people around assets: who borrowed them, who changed them, and the loans list, `assets.act`, which presses actions, and `events.read`, which receives events live (§5.4)). Seeding grants every permission to the `admin` role, including permissions added by later releases. Migrations `011`, `013` and `014` grant `stock.record`, `assets.report`, `assets.borrow` and `assets.lend` to every role that had `assets.write`, so nobody loses what they could do, and `017` grants `reports.manage` to every role that had `templates.manage`. Migration `020` grants `assets.track` to every role that reads assets, in the scopes it reads them in, since reading showed people until then. Keys were written `resource:action` until migration `019` renamed them, so that a key can carry a scope.
* **Scopes:** a scope is a label, named by 1 to 64 lowercase letters, digits, `-` and `_`, put on categories and passed down to their subcategories. An asset is in the scopes of its categories and of every ancestor of theirs, or, when there are none, in the reserved `default` scope; so an asset leaves `default` when it joins a category with a scope, and returns to it when it leaves the last one. A role holds a permission in every scope (`assets.write`) or in some (`tools:assets.write`, `default:assets.write`); only the asset permissions are scopable: `assets.read`, `assets.scan`, `assets.act`, `events.read`, `assets.track`, `assets.write`, `assets.delete`, `assets.report`, `assets.borrow`, `assets.lend` and `stock.record`. A permission in any one of an asset's scopes is enough, and one held in every scope covers each (a role given both keeps the wider). Holding a permission in any scope opens its endpoints and pages; each then checks the asset: an asset the requester can neither read nor act on as asked is `404`, as if it did not exist, and one they can read but not act on is `403`. Lists, exports, reports, loans and labels hold only the assets the requester can read, and an asset's links and location leave out those it cannot. Creating an asset, or moving one to other categories, needs `assets.write` in the scopes it ends up in as well as those it was in; a link needs it on both assets. An import checks each row and link the same way and reports what it may not change as problems, and assets it cannot read are, to it, unknown. Flagging a category running low needs `stock.record` in the scopes its assets get from it. `GET /api/v1/auth/me` lists scoped keys as they are, so the web app shows each asset only what its scopes allow.
* **People:** without `assets.track` for an asset, its loan says only that it is lent out (and to the requester, if it is theirs), its history leaves out who did each thing and who borrowed it, and `{{loan.borrower}}` is blank; with it, people show by name. Emails never show on asset pages, lists, history or the loans list.
* **The public role:** everyone holds the `public` role, signed in or not, and API tokens too (migration `021`). It may only look (`assets.read` and `assets.scan`, in any scopes), keeps its name, and cannot be deleted (`409`); it is not given to users, who all have it already, and `GET /api/v1/roles` marks it `public`. It starts with `public:assets.read`: reading the assets in the `public` scope, a built-in scope that labels no categories until someone puts some in it, so nothing is public until then. Signed in, its permissions add to the user's own wherever they apply. Signed out, they apply only to an asset's own page: `GET /api/v1/assets/{id}`, its photos and history (without people, §5.3), and `GET /api/v1/generators`; the catalog (the asset list, categories, fields, templates, reports and the rest) answers `401`, and the web app opens only the asset page, with a link to sign in. `GET /api/v1/auth/me` answers signed-out visitors with `{"user": null}` and the public role's permissions. A role or scope already named `public` before `021` is renamed `public (renamed)` or `public-renamed`, so what it allowed never passes to everyone.
* **Managing scopes:** `GET /api/v1/scopes` (`users.manage` or `categories.manage`) lists them with how many categories they label and roles use them; `POST /api/v1/scopes` and `PUT /api/v1/scopes/{id}` with `{"name"}` create and rename one, and `DELETE /api/v1/scopes/{id}` deletes one that no category or role uses (`409` otherwise), all with `users.manage`, since scopes decide who reaches what. `GET` and `PUT /api/v1/categories/{id}/scopes` (`{"scope_ids": [...]}`; reading with `users.manage` or `categories.manage`, changing with `users.manage`) give a category's own scopes. `default` and `public` cannot be renamed or deleted, and `default` cannot be put on a category (`409`). Renaming a scope carries the roles' permissions in it along. Moving categories under others changes their assets' scopes, so `categories.manage` is trusted with that.
* **Roles:** `GET /api/v1/roles` lists roles, those made here first by name regardless of case and then the built-in `admin` and `public`, with their permissions, how many users who are not archived hold each (`user_count`), and which is `protected`; `POST /api/v1/roles` creates one and `PUT`/`DELETE /api/v1/roles/{id}` change or delete it, all with `users.manage` and a body of `{"name", "description", "permissions"}`. Names are unique and up to 64 characters; permissions must be known keys, each alone or, if scopable, as `scope:key` naming an existing scope. Deleting a role takes it from everyone who holds it. The `admin` role is protected: it cannot be renamed, edited or deleted (`409`), since seeding recreates it by name and grants it every permission at each start. Editing and deleting roles follow the anti-lockout rule below.
* **Union Principle:** When a user is assigned multiple roles, their effective permissions are the strict union of all granted rights across all assigned roles.
* **Anti-Lockout Rule:** System operations must reject any deletion, deactivation, role removal, or role edit that results in zero active users possessing the `users.manage` permission. Each such change runs in one transaction that first takes the `users_manage` row of the `app_locks` table (migration `004`), so concurrent changes are checked one after another and two admins cannot remove each other at the same time.

### 3.4 Sessions & Authentication

* **Local Credentials:** Email (case-insensitive, unique) + password hashed via bcrypt (cost 12).
* **Names:** each user has a display name (up to 255 characters, migration `010`), which is how asset pages show them: borrowers, history, the loans list, report and label bindings. Asset pages never show emails, so a user from before names were required, or the first admin made by `bam admin create` without `--name`, shows as "Unnamed user" until named; to the signed-in user, what they did themselves reads "you". The home page's setup checklist asks an admin with no name to give one. The header and the user list show the email when there is no name. It is for display only; signing in is always by email. A name is required: admins give it when creating a user (`name` in `POST /api/v1/users`) and change it later (`PUT /api/v1/users/{id}/name`, `users.manage`), and anyone signed in changes their own (`PUT /api/v1/auth/name`), each with `{"name": "..."}`; a blank one is `400`. The lending form names people by name, adding the email only to tell apart two of the same name. History keeps the name as it was when the change was made, and Discord messages name the person by it.
* **TOTP (RFC 6238):** Optional two-factor authentication flag per user.
* **Passwords:** stored as bcrypt hashes (cost 12). Every new password must be 8-72 bytes: 72 is the most bcrypt uses, so longer ones are refused rather than silently cut short. Whoever manages users sets someone's password for them, for one who has forgotten theirs, with `PUT /api/v1/users/{id}/password` (`users.manage`, `{"password"}`), which signs that person out everywhere (but for the session making the change, when it is their own); the sign-in page says whom to ask. On the command line, `bam admin password --email … --password …` does the same for anyone, such as an admin locked out, changing nothing else about the account (where `bam admin create` would also make them an admin).
* **Session Store:** Stateful sessions stored in the active database (`sessions` table) referenced via an `HttpOnly`, `SameSite=Lax`, `Secure` cookie. The cookie holds a random 256-bit token; the table stores only its SHA-256 hash (migration `005`), so a copy of the database cannot be used to sign in.
* **API tokens:** scripts and dashboards authenticate with `Authorization: Bearer <token>` instead of a session (migration `012`). A token has a name, roles whose permissions it holds (as a user holds theirs), an optional expiry and the time it was last used (written at most once a minute). Its secret, `bam_` and 64 hex digits (256 random bits), is returned once when the token is made; like a session token, only its SHA-256 hash is stored. `GET`, `POST /api/v1/tokens` and `DELETE /api/v1/tokens/{id}` list, make (`{"name", "role_ids", "expires_at"}`, an RFC 3339 time or `null` for never) and revoke tokens, with `users.manage` and a signed-in session: a token can never manage tokens, nor use the account endpoints (`/api/v1/auth/me`, `name`, `password` and `logout`), which answer it `403`. A revoked, expired or unknown token is answered `401` at once, even with a session cookie alongside. Revoked tokens stay listed, and changes a token makes are recorded in history under its name (`"actor": {"token": true, ...}`). Tokens do not count toward the anti-lockout rule. Deleting a role takes it from tokens too.

### 3.5 Stored Text

Text behaves the same on every database, whatever the server's defaults:

* **Rules:** text must be valid UTF-8 and may not contain a NUL character (PostgreSQL cannot store one). Names, labels, choices, codes and email addresses hold up to 255 characters, and field values up to 10,000; lengths count characters, not bytes. Text outside the rules is refused with `400` (or, in an import, a problem against its row and column), never cut short or altered. Uploaded image names are cleaned instead: control characters and invalid bytes are dropped, and the name is cut to 255 characters. A request with a NUL character or bytes that are not UTF-8 in its path, its query or any string of a JSON body is refused with `400` before it reaches a handler, since it cannot name anything stored.
* **Comparison is exact:** `Drill` and `drill`, or `é` written as one character and as `e` with a combining accent, are different codes and different channel names on every database.
* **Search** (`search` on the asset list, export and reports) finds the term anywhere in an asset's name or codes, or the name of a place it is in at any depth (so "shelf a" finds what is on Shelf A, and in the boxes on it), ignoring case for every letter, not just ASCII. `%`, `_` and `\` have no special meaning. Places count only as far as the requester may read them, so an asset is never found by the name of one hidden from them.
* **Suggesting codes:** `GET /api/v1/generators/{type}/next-code` (`assets.write`) answers with `code`, one more than the highest code of that type ending in a number, keeping its prefix and zero-padding and skipping any taken (after `HS-105`, `HS-106`; after `BIN-0099`, `BIN-0100`), or `""` when none ends in a number. The asset form's **Assign the next code** uses it.
* **Where things are** in the web app: the asset form's **Where is it?** finds the asset it is in and links it there when saved (moving it from wherever it was); its page always has a **Where** row, with **Move**; an asset that holds others lists them under **Contents**; and the asset list has a **Where** column, the place it is directly in.
* **PostgreSQL** databases must use the `UTF8` encoding; BAM refuses to start on any other, including `SQL_ASCII`.
* **MariaDB** needs nothing set up: BAM creates its tables in `utf8mb4` with the exact collation `utf8mb4_nopad_bin`, whatever the database's default, and sets each connection's character set, SQL mode (strict) and time zone (UTC) itself. Migration `008` converts tables created by earlier versions, which took the database's default (often `latin1` with a case-insensitive collation).

### 3.6 Asset Associations

Assets link to one another: a bin contains screws, a drill is used with its bits (migration `015`).

* **Relation types** (`relation_types`) are a shared list, like the field corpus, managed with `categories.manage` through `GET` (`assets.read`), `POST /api/v1/relation-types` and `PUT`/`DELETE /api/v1/relation-types/{id}`. Each has a `name` read from the first asset and an `inverse_name` read from the second (`contains` / `contained in`), up to 64 characters, and three flags: `is_symmetric` for one that reads the same both ways (`used with`; its inverse is its name), `is_single` when the second asset can have one only (a thing is in one place), and `is_location` for one whose links give where things are (it must be single). `contains` (a location), `has part` and `used with` are seeded. Names are also written as verbs in CSV files (§6.1): lowercased, with each run of characters other than letters and digits made `_` (`contained in` is `contained_in`), so a name whose verb another type already has, or one with no letters or digits, is refused (`409`). While links use a type only its names can change, and it cannot be deleted (`409`, with how many links use it).
* **Links** (`asset_links`) are stored once and read correctly from both assets. `POST /api/v1/assets/{id}/links` (`assets.write`) with `{"type_id", "asset_id", "outgoing"}` links the asset to another, reading from the asset: `outgoing` by the type's name (this bin contains those screws), otherwise by its inverse (these screws are contained in that bin). For a single type, linking the second asset anew moves it, ending its old link. A link that would make a loop of a single type (a bin inside its own contents), an asset linked to itself, a link that already exists (either way round for a symmetric type), and links to archived assets are refused. `DELETE /api/v1/assets/{id}/links/{link_id}` removes one. Changes serialize on the `asset_links` lock.
* **On the asset:** `GET /api/v1/assets/{id}` lists `links`, each with the `label` read from the asset, the other asset and whether it is `outgoing`, and `location`, the chain of containers through location links, outermost first (Shelf 2 › Bin A3).
* **History:** adding or removing a link records an `asset.edited` change on both assets (`"what": "link"`, the label read from that asset and the other asset's name); a move shows on the thing moved, its old container and its new one.
* **Archiving:** an asset that holds active assets through a location link cannot be archived until they are moved out (`409`). An archived asset keeps its links, marked as archived, so restoring it puts it back where it was.
* **Suggestions:** `GET` and `PUT /api/v1/categories/{id}/relation-types` (`{"relation_type_ids": [...]}`, `categories.manage` to change) name the types a category suggests; an asset's `suggested_relation_type_ids` gathers them from its categories and their ancestors, and the web app offers them first.

---

## 4. Label Template Engine & Print Subsystem

Every label passes through the same three stages, whichever format it ends up in:

```
template + asset ──► bind ──► layout (whole printer dots) ──► SVG │ PNG │ PDF │ printer raster
```

1. **Bind** resolves each element's `content_binding` against the asset, its field values and its generator keys.
2. **Layout** converts the template's physical units into dots at the template's DPI, snaps every barcode module to a whole number of dots, and sets text with the embedded fonts. Its output is a scene of rectangles, glyph outlines and images, measured in dots.
3. **Backends** draw that scene. Text is drawn as outlines rather than font references, so SVG, PDF and PNG agree at the template's DPI and never depend on the fonts installed where they are viewed. Every backend stacks images beneath text and text beneath barcodes, whatever the element order.

### 4.1 Declarative Template Specification

A template is stored in the `templates` table: `id`, `name`, `units`, `width`, `height` and `dpi` are columns, and `elements`, with `sizing` and `adapt` for an adaptive template, are kept in `layout_json`. The API reads and writes it as one JSON document:

```json
{
  "id": "3f6c2a9e-8b1d-4e57-a0c4-91d2b7e5f803",
  "name": "Standard Die-Cut 29mm x 90mm",
  "units": "mm",
  "width": 90.0,
  "height": 29.0,
  "dpi": 300,
  "elements": [
    {
      "type": "generator",
      "generator_type": "qr",
      "error_correction": "M",
      "x": 3.0,
      "y": 3.0,
      "width": 23.0,
      "height": 23.0,
      "content_binding": "{{base_url}}/a/{{asset.id}}"
    },
    {
      "type": "text",
      "x": 28.0,
      "y": 5.0,
      "width": 58.0,
      "height": 6.0,
      "font_size": 14,
      "font_weight": "bold",
      "content_binding": "{{asset.name}}"
    },
    {
      "type": "text",
      "x": 28.0,
      "y": 12.0,
      "width": 58.0,
      "height": 4.0,
      "font_size": 9,
      "font_family": "mono",
      "content_binding": "{{field.location}}"
    },
    {
      "type": "static_image",
      "x": 75.0,
      "y": 18.0,
      "width": 10.0,
      "height": 8.0,
      "image_id": "7c9e6679-7425-40de-944b-e07fc1f90ae7"
    }
  ]
}
```

* **ID:** a UUID the server assigns when the template is created; an `id` sent with `POST /api/v1/templates` is ignored, and the response carries the new one. A `PUT` may leave `id` out, but cannot change it. Templates created by earlier versions keep the slugs their authors chose.
* **Canvas:** `units` is `mm` or `in`; `width` and `height` are positive; `dpi` is 150–600 (Brother QL and DYMO LabelWriter printers print at 300, Rollo and most TSPL printers at 203). Positions and sizes use the template's units with the origin at the top-left corner, and every element must lie inside the canvas. `font_size` is always in points.
* **Content bindings:** `content_binding` is literal text with `{{...}}` placeholders:

  | Placeholder | Resolves to |
  |---|---|
  | `{{asset.id}}` | The asset's UUID |
  | `{{asset.name}}` | The asset's name |
  | `{{asset.code.<generator_type>}}` | The asset's assigned code for that generator, e.g. `{{asset.code.micro_qr}}` |
  | `{{field.<key>}}` | The asset's value for that corpus field (empty when unset) |
  | `{{base_url}}` | `server.base_url` from the configuration |
  | `{{asset.categories}}` | The asset's categories, by name, separated by commas |
  | `{{asset.location}}` | Where the asset is, through location links (§3.6): `Shelf 2 › Bin A3` |
  | `{{asset.condition}}` | `Working`, `Needs maintenance` or `Out of service` |
  | `{{stock.quantity}}`, `{{stock.unit}}` | Its stock counted by quantity, and the unit (empty when not counted) |
  | `{{loan.borrower}}`, `{{loan.due}}` | Who has it on loan, by name ("Unnamed user" for someone without one; empty without `assets.track`, §3.3), and the day it is due back as `YYYY-MM-DD` in UTC (empty when not lent) |
  | `{{scan_url}}` | The asset's scan page, `{{base_url}}/scan/<asset ID>` (§6.4): a QR code of it takes whoever scans the label there |

  Unknown placeholders and unknown field keys are rejected when the template is saved. A field deleted later simply resolves to an empty string. A `generator` element without a `content_binding` encodes `{{asset.code.<its generator_type>}}`, and rendering fails for an asset that has no code of that type. Resolved generator payloads go through the same validator as asset generator keys before they are encoded.
* **Element types:**
  * `generator`: `generator_type` (`qr`, `micro_qr` or `code128`); optional `error_correction` (QR `L`, `M` (default), `Q`, `H`; Micro QR `L` (default), `M`, `Q`); optional `quiet_zone` in modules (defaults: QR 4, Micro QR 2, Code 128 10). The symbol is drawn centered, at the largest whole number of dots per module that fits the box; rendering fails if that is under 2 dots. Code 128 bars fill the box height.
  * `text`: `font_size` (pt), `font_weight` (`normal`/`bold`), `font_family` (`sans`/`mono`), `align` (`left`/`center`/`right`), `wrap` (default `false`) and `min_font_size` (default 6). Lines break at newlines (and between words when wrapping). Text that overflows its box shrinks in 0.5 pt steps down to `min_font_size`, then is cut short with an ellipsis. The block is centered vertically in its box. The embedded Go fonts cover Latin, Greek and Cyrillic; other scripts render as empty boxes.
  * `static_image`: `image_id` names an uploaded PNG or JPEG, scaled to fit the box with its aspect ratio kept. Uploads are recorded in the `images` table and stored under `storage.local_path/images/`; `GET /api/v1/images/{id}` returns the metadata and `/content` the image itself. SVG images are not supported, since there is no dependable pure-Go SVG rasterizer.

* **Fixed and adaptive templates:** a template is fixed (no `sizing`, or `"fixed"`), printed only at its own size and DPI, dot for dot as previewed, or adaptive (`"sizing": "adaptive"`): designed at its own size, it is laid out again for the size of each label it prints on and rendered at the printer's DPI. `adapt` gives the sizes it takes, `min_width`–`max_width` by `min_height`–`max_height` in its units, which must include its own size, and how long its labels are on a continuous roll, whose width alone is given (`continuous_length`: `proportional` to the width, the default, or `fixed` as designed). On a printer, its width or its height runs across the roll, whichever leaves it nearest its own shape and fits. Each element says how it follows the label across and down (`anchor_x`, `anchor_y`): `scale` in proportion (the default), `start` or `end` at its distance from that edge, `both` at its distance from each (stretching), or `center` at its distance from the middle. Text scales its font with its box (by the lesser of its two scales) and still shrinks to fit; codes take the largest whole number of dots per module their box allows, as always, and images keep their proportions. Laid out (`render.Adapt`), it is an ordinary fixed template, so rendering, exports and the printers are unchanged. A label is refused, saying which element, when an element would run off it or have no room, text would fall below its `min_font_size`, two elements that do not overlap as designed would, or a code would not fit its box at the printer's DPI with the longest code assigned with its generator, or the longest value of a field it uses, among active assets. Downloads and previews of an adaptive template use its own size. The template editor switches a template between fixed and adaptive, sets its range and each element's anchors, limits and alternative, and shows it laid out on each printer as loaded and at any size typed in, with the sizes it takes in its own shape.
* **Limits in adaptive templates:** an element may set `min_size` and `max_size`, bounding each side of its box in the template's units; laid out larger, it is centered where its box would be at its maximum. A code may set `min_module_mm`, the smallest module it is drawn with for the scanners in use, on fixed templates too; two dots at the printer's DPI is always the least. An element that does not fit a label (no room, under its minimum size, text below its minimum, or a code too big for its box with the longest codes assigned) does as `when_too_small` says: `refuse` the label (the default for codes and text), `hide`, leaving it off (the default for images), or `alternative`, giving way to its `alternative`, a code or text element laid out in its box, such as a Micro QR code of the asset's `micro_qr` code or the code as text; an alternative that does not fit either refuses. What was left off or replaced is said in the print answer's and the spool's warnings. `POST /api/v1/templates/adapt` (`templates.manage`) lays a template out at a `width`, `height` and `dpi`, or as a printer would (`printer_id`), answering with it laid out and its `notices`, or the `problem`; `POST /api/v1/templates/range` works out the sizes it takes in its own shape at a `dpi` (its own by default), from `smallest` to `largest`, and what stops it going further (`smaller`, `larger`), when its declared range does not. Both use the longest codes and field values assigned among active assets. One asset's code can still be too long for a size the template otherwise takes: that label alone is not produced, and when its template came from its categories it moves to the first of theirs after it that suits the printer and holds it, to print next time (§6.2).

### 4.2 Generator Interfaces

```go
// Symbol is an encoded barcode as a grid of modules. Linear symbologies
// (Code 128) have a single row, which layout stretches to the element height.
type Symbol struct {
    Cols, Rows int
    QuietZone  int    // standard quiet zone, in modules
    Dark       []bool // row-major, len(Dark) == Cols*Rows
}

type Generator interface {
    Type() string
    // Validate applies the asset key rules; every payload it accepts encodes
    // at the lowest error correction level.
    Validate(payload string) error
    // ErrorCorrectionLevels lists accepted levels, default first (none for Code 128).
    ErrorCorrectionLevels() []string
    // Encode validates, then encodes; "" selects the default level.
    Encode(payload string, errorCorrection string) (Symbol, error)
}
```

Generators return modules, not images: sizing and drawing belong to layout, which is what keeps modules on whole dots.

* **QR Code (`qr`):** Model 2, versions 1–40, error correction L/M/Q/H. A payload is encoded as a single segment in the most compact of numeric, alphanumeric or byte mode; UTF-8 is written as plain bytes with no ECI header, which is what phone scanners expect. The smallest version that fits is used, and the mask is chosen by the standard's penalty rules.
* **Micro QR Code (`micro_qr`):** versions M2–M4 in numeric or alphanumeric mode, the smallest version that fits at the requested error correction, mask chosen by the Micro QR evaluation rule. M1 is never used: it detects errors but cannot correct them, so a scuffed label would stop scanning. Asset keys are validated against M4-L capacity, so a template asking for `M` or `Q` can reject a long code at render time.
* **Code 128 (`code128`):** code sets B and C with the modulo-103 check symbol. Following ISO/IEC 15417 Annex E, a symbol starts in C when it begins with four or more digits, and switches to C for a run of four digits at the end or six elsewhere.
* All three encoders are written in-house as standalone packages (`internal/generators/qr`, `microqr`, `code128`) that know nothing of the registry; the QR flavors share private helpers for Reed–Solomon, bit packing and matrix layout. Go QR libraries exist, but none implements Micro QR, and owning all three keeps output deterministic for golden tests.

### 4.3 Output Formats

1. **SVG:** the interchange format for the frontend editor preview. Adjacent dark modules merge into rectangles; text is glyph outlines.
2. **PNG:** rendered at the template's DPI, as 8-bit grayscale for previews or 1-bit black and white for printing, where text is thresholded at 50% coverage and images are Floyd–Steinberg dithered. Barcode modules are never anti-aliased.
3. **PDF:** one page per label, sized exactly to the label, with vector barcodes and text and images embedded as Flate-compressed bitmaps. Output is byte-for-byte deterministic: no timestamps or random document IDs.

Batch exports produce a multi-page PDF or a ZIP of PNGs named by asset ID (§6.2).

### 4.4 Printers & Direct Network Dispatch

Any number of printers can be configured through `/api/v1/printers` (`settings.manage`; anyone with `labels.print` or `templates.manage` can list them, and template designers can run a printer's fit check without printing through `POST /api/v1/printers/{id}/fit`). Each is stored in the `printers` table with a name, driver, model, connection, the media currently loaded, and driver `options` as a JSON object.

Printer families are supported by **drivers** (`internal/printers`), registered by name and chosen per printer:

```go
type Driver interface {
    Info() DriverInfo                                // models, media and options, for clients
    Validate(s Settings) error                       // model, media and options of a printer
    Plan(s Settings, label LabelSpec) (Plan, error)  // fit check; ErrDoesNotFit → 422
}

type Plan interface {
    Encode(labels []*image.Gray) ([]byte, error)     // 1-bit labels in, one print job out
}
```

The print endpoint only renders labels, asks the printer's driver to plan and encode them, and hands the job to a `Transport` for the printer's connection (see Delivery below). Adding a printer family means registering a driver; the API does not change. `GET /api/v1/printers/drivers` lists every registered driver with its models (and each model's DPI), media and options, and for drivers that take custom stock, how wide each model's labels may be and how long continuous ones (`custom_stock`).

* **The fit check** is the same for every driver (`printers.FitLabel`): a driver reports its printer's geometry as configured (the DPI templates must use, the roll's label size, the lengths it prints from a continuous roll, and how much of each label its head reaches) and keeps only encoding. A template fits when its DPI is the printer's, one side is the roll's width (±0.5 mm), and the other is the label length on die-cut rolls or within the printer's range on continuous ones (to half a dot); a template whose height is the roll's width is rotated 90° counterclockwise. Anything else is refused, with `422` when printing, giving every reason at once (its resolution and its size). A fit can also carry **warnings**, which never stop printing: part of the template (its elements' boxes) where the head does not print, such as the far edge of a 59 mm label on a first-generation LabelWriter or the outer 1.5 mm of Brother tape; content within 1 mm of a die-cut label's edge, which may be cut off as labels shift; and custom stock on a DYMO 550, 5XL or Wireless, which print only on labels they identify. `POST /api/v1/printers/{id}/fit` takes the template's size and DPI, and optionally its elements' boxes in millimeters (`content`), and answers `fits` with `warnings` or why not; when it fits, also with the part of the label the printer prints (`printable`, in the template's millimeters), how near a die-cut label's edge content may be cut off (`edge_mm`), and which of the boxes reach outside what it prints (`outside`) or near the edge (`near_edge`), which the editor shades and outlines on its canvas. `POST /api/v1/printers/{id}/test-print` (`labels.print` and `templates.manage`) prints one label of a template that need not be saved, as `/templates/preview` takes it, for an asset or with placeholders, laid out for the printer if adaptive; it is not recorded in history; printing answers with the warnings too, and so do the spool (`fit_warnings`, by template and destination) and printing from it.

* **Changing a printer:** `POST /api/v1/printers/{id}/changes` (`settings.manage`) takes a printer as `PUT` would save it and says, without saving, what the change would leave without a printer (`changes`): labels waiting in the spool for it, or for a pool it is in, that would no longer fit there (`stranded`, by template and destination, with how many); categories whose label template no printer would suit (`categories`); and pools whose printers would no longer be loaded alike (`pools`). `PUT` answers with the same. Waiting labels whose template came from their categories, and that the change leaves unfit, move to the first other template of their categories' that fits where they go, which `stranded` says as `move_to`; those whose template was chosen by hand stay, marked as not fitting. The web app asks before saving a change that affects anything. Templates are never changed by it: one that no longer fits is simply not printed there until a roll it suits is loaded again. `GET /api/v1/templates` says which printers suit each template as loaded (`suits`), to those who may see printers.

* **Custom label stock** (migrations `028` and `029`, which widens `printers.media` to hold its ID): a roll known by its size alone, a name, width, and the length of die-cut labels with the gap between them (none for a continuous roll), managed with the printers through `/api/v1/label-stock` (`settings.manage`; listed to those who may see printers, with the printers that have each loaded). A printer's `media` names either one of its driver's built-in rolls or custom stock by ID; printers answer with the stock (`stock`) and a description of whatever is loaded (`loaded`), so clients need not look either up. Drivers take custom stock only where a size is all the printer needs: TSPL printers and DYMO LabelWriters do, up to their head's width; Brother QL printers, which identify the DK roll loaded and need Brother's figures to place a label on it, take only their built-in rolls. Changing stock checks every printer with it loaded, and is refused, naming the printer, if one would no longer take it; stock loaded in a printer cannot be deleted (`409`).

Three drivers ship today: `brother_ql`, `dymo_labelwriter` and `tspl`. Option values are listed by the drivers endpoint, and unknown options are refused.

#### `brother_ql`

For the QL-810W and QL-820NWB. Its only option is `cut` (default `true`): cut each label off as it finishes, or let the tape feed on uncut.

* **Media catalog:** each supported roll records its width, whether it is die-cut (and if so its label length), its printable width in head dots and how many pins sit beside the tape. It covers every roll the QL-800 series takes, with the figures from Brother's *Raster Command Reference QL-800/810W/820NWB* (version 1.01, sections 2.3.2 and 2.3.5): continuous tape 12, 29, 38, 50, 54 and 62 mm wide; die-cut labels 17 × 54, 17 × 87, 23 × 23, 29 × 42, 29 × 90, 38 × 90, 39 × 48, 52 × 29, 54 × 29, 60 × 86, 62 × 29, 62 × 60, 62 × 75 and 62 × 100 mm; and round labels 12, 24 and 58 mm across. Rolls are named by their DK part number where Brother sells them under one. Two-color black/red media (DK-22251) is out of scope.
* **Fit check:** before any asset is laid out, the template must suit the loaded media (above), at 300 DPI; continuous labels are 25–1000 mm long. The head reaches the middle of the tape, and the middle of a die-cut label's length, by Brother's printable pins and lines.
* **Printable area:** the printer cannot reach the tape edges, so the rendered label is cropped to the printable area around its center: 306 of the 343 dots across 29 mm tape (about 1.5 mm lost on each side), 696 across 62 mm, and on DK-11201 991 of 1063 dots along the label (about 3 mm at each end). Keep elements clear of those margins.
* **Raster stream:** each label is rendered 1-bit at 300 DPI and encoded as described in Brother's QL-800 series raster command reference: invalidate, initialize, then per label raster mode, print information, auto-cut, margins and TIFF compression, PackBits-compressed raster lines (90 bytes for each line of the 720-pin head, mirrored because the first pin sits at the right of the tape) and a print command. With `cut` on, every label is cut off as it finishes.

#### `dymo_labelwriter`

For DYMO LabelWriter printers, which speak one of two raster languages. The 400, 400 Turbo, Twin Turbo, 450, 450 Turbo, 450 Twin Turbo, 450 Duo (label side) and 4XL speak the first. The 550, 550 Turbo, 5XL and LabelWriter Wireless speak the second. The first language is implemented from DYMO's *LabelWriter 450 Series Technical Reference Manual*, and its tests are worked out from that manual. DYMO has published no reference for the second; its command format was learned by reading DYMO's open-source (GPL) Linux drivers. No code or test data is taken from those drivers, so BAM stays MIT-licensed.

Options: `density` (`light`, `medium`, `normal` or `dark`; default `normal`) and, on twin-roll models only, `roll` (`auto`, `left` or `right`; default `auto`).

* **Media catalog:** the common die-cut rolls (30252/99010 address 28 × 89 mm, 30321/99012 large address 36 × 89 mm, 30256 shipping 59 × 102 mm, 30323/99014 shipping 54 × 101 mm, 30334/11354 57 × 32 mm, 30336/11352 25 × 54 mm, 30332/11353 25 × 25 mm, 30347 25 × 38 mm, 30330/11355 19 × 51 mm, 30345 19 × 64 mm), 30270 continuous 57 mm, and 1744907 4 × 6 in shipping labels, which need the 4XL or 5XL. Label sizes in dots follow from the nominal sizes at 300 DPI.
* **Fit check:** as above, at 300 DPI; continuous labels are 10–2700 mm long.
* **Printable area:** labels sit against the roll guide at the head's first dot. The render is centered on the label. On first-generation models it is then cut to the most the head takes in one line, 56 mm (672 dots) or 104 mm (1248 dots), so the far edge of a 59 mm shipping label does not print, which the fit check warns of when the template has anything there. Second-generation models receive the whole label.
* **Raster stream, first generation:** one ESC more than the longest raster line (85, or 157 on the 4XL) to resynchronize, text mode, density, the roll on twin models, and the label length (a little over the label on die-cut rolls, `0xFFFF` for continuous). Then each line is trimmed of blank bytes at both ends and sent run-length compressed when that is shorter, with dot tab and bytes per line sent only when they change and runs of blank lines as skips. Labels are separated by short form feeds, and a form feed after the last brings it to the tear bar. Blank lines at the end of a die-cut label are left to the form feed.
* **Raster stream, second generation:** a job header (random job ID, density, text quality, default media type). Then per label: the label length (0 for die-cut, `0xFFFF` for continuous), a label index, a print data header and uncompressed lines. After a final form feed, an end-of-job command closes the job. The 550 series only prints on genuine DYMO labels it can identify, whatever the driver sends.
* **Connection:** the LabelWriter 400 and 450 series and the 4XL have only a USB port, so they use a `device`, `ipp`, `windows` or print-server `tcp` connection (see Delivery). The 550 Turbo, 5XL and Wireless have network ports.

#### `tspl`

For printers that speak TSPL, the label language of TSC printers that many inexpensive 4-inch direct thermal printers also use. Models: Rollo X1038 and X1040, Munbyn ITPP941, and generic 203 DPI and 300 DPI TSPL printers. The driver follows TSC's *TSPL/TSPL2 Programming Manual*.

Options: `density` (0–15; unset keeps the printer's own setting) and `rotate_180` (default `false`) to print labels the other way up.

* **Media catalog:** these printers take any label size up to their head width, so the catalog lists common sizes rather than part numbers: 4 × 6, 4 × 4, 4 × 3, 4 × 2, 3 × 2, 3 × 1, 2.25 × 1.25, 2 × 1, 1.5 × 1 and 1 × 1 in die-cut labels with a 1/8 in gap, and 4 in and 2 in continuous. Any other size is added as custom stock.
* **Fit check:** as above, at the model's DPI (203 for the Rollo); continuous labels are 10–1000 mm long. A label wider than the head is printed around its middle.
* **Program:** `SIZE`, `GAP`, `DIRECTION`, `REFERENCE` and optionally `DENSITY`, then for each distinct label `CLS`, one `BITMAP` holding the whole label (a 0 bit prints a dot) centered across the label, and `PRINT 1,n` with `n` the copies. None of the printer's own fonts or barcode commands are used, so output matches the SVG and PDF exports.
* **Connection:** the X1038 has only a USB port (see Delivery); the X1040 is reached over the network.

#### Delivery

Each printer has a `connection`, `{"type": ..., "address": ...}`, saying how BAM reaches it:

| Type | Address | Use |
|---|---|---|
| `tcp` | `host` or `host:port` (default 9100) | Network printers, and USB printers shared by a raw print server such as `p910nd`. |
| `device` | A device path on the BAM server, such as `/dev/usb/lp0` (Linux) or `/dev/ulpt0` (FreeBSD) | A USB printer plugged into the BAM server. The path must match a pattern in the server's `[printers] devices` setting, which is empty by default: otherwise anyone who can manage printers could make BAM write to any file it can open. BAM never creates the file, and needs permission to write the device (on Linux, usually membership of the `lp` group). |
| `ipp` | `ipp://host[:port]/path`, or `ipps`, `http`, `https` | A queue on CUPS or another IPP server, such as a USB printer shared from another machine. The job is sent as `application/octet-stream`, so the queue must be raw (CUPS: `lpadmin -m raw`; CUPS 2.x still supports raw queues though it marks them deprecated). CUPS can also forward a raw queue to a Windows shared printer through its `smb://` backend. |
| `windows` | A Windows printer name or `\\host\share` | When BAM runs on Windows: a local or shared printer, printed through the spooler with the `RAW` datatype. Windows has no device paths for USB printers, so this covers both. |

* **Serialization:** jobs are sent one at a time per destination, so concurrent requests never interleave their streams.
* **Failures:** TCP has dial and write timeouts and IPP a 90-second limit. A connection the server may not use (a device outside the allowed patterns, or `windows` on another OS) is refused with `400` when the printer is saved and with `502` at print time. An unreachable printer also returns `502`.
* **Delivery:** a job counts as printed once the destination has accepted the whole stream. Reading back printer status is a later improvement.

---

## 5. Notifications & Consumables Tracking

### 5.1 Running Low

Stock is counted in one of two ways, for two kinds of things:

| | Counted by items | Counted by quantity |
| :--- | :--- | :--- |
| For | Things labeled one by one: filament spools, saw blades, batteries | Things kept in bulk: screws, zip ties, solder |
| Each unit is | An asset of its own | Not tracked; one asset stands for all of them |
| What is counted | Active assets in a consumable category and its subcategories | A quantity on the asset |
| The threshold is on | The category | The asset |
| Using some up | Archive the asset | Use or restock it (`POST /api/v1/assets/{id}/quantity`) |

The two meet in one place: things labeled one by one that are used up gradually, such as filament spools. A consumable category with a unit (below) weighs them rather than counting them, so a 1000 g spool and a 2000 g spool make 3000 g. Otherwise the two are never added together, since they count different things (3 spools and 500 g of solder make no total).

#### Counted by items

* Categories tagged with `is_consumable = true` support an `is_running_low` flag. Only consumable categories may carry it.
* **Manual:** `PUT /api/v1/categories/{id}/running-low` with `{"running_low": true|false}` sets or clears it. It needs only `stock.record`, so the people handling stock can flag it without being able to edit categories or assets. Editing the category (`categories.manage`) can set it too.
* **Inventory thresholds:** a consumable category may have an optional `low_stock_threshold` (a positive integer, or `null` for none). Its *active count* is the number of distinct active (not archived) assets in the category or any of its descendants; an asset in two subcategories counts once. `GET /api/v1/categories/{id}` reports it as `active_count`.
  * When the active count drops below the threshold, the category flags itself as running low. When it climbs back to the threshold or above, it clears itself.
  * The rule reacts to the count *crossing* the threshold, not to its level. A manual change stands until the count next crosses, so the automatic rule never undoes a person's decision.
  * Anything that changes an active count is checked: creating, archiving, deleting or recategorizing assets; moving or deleting categories; imports (checked once, after the whole file); and changing the threshold itself (setting it above the current count flags at once).
  * Changes that can move a count serialize on an application lock, so two simultaneous archives cannot both miss a crossing.
* **Weighing instead of counting:** a consumable category may have a `unit` (up to 32 characters, migration `016`). Its level is then the total stock, in that unit, of the distinct active assets in it and its descendants that count their stock by quantity in that unit (a spool of filament with 1000 g); other assets add nothing. The threshold is in the unit too, and `GET /api/v1/categories/{id}` reports the level as `total` beside `active_count`. Each category measures by its own unit, so totals carry up to parent categories only where the units match. Using or restocking such an asset, or checking a lent one back in with what is left (§5.3), is checked against the thresholds as any other change, and the category's events carry the unit. An asset whose stock is in the unit of one of its categories, or their ancestors, is `weighed`.
* **Events:** every change of the flag, manual or automatic, records an event (`category.running_low` or `category.restocked`) in the same transaction as the change: the category, the user whose action caused it, whether it was manual or automatic, and for automatic changes the count (or total) and threshold, with the unit of a category that weighs. A change that rolls back records nothing, and two people flagging at once produce one event.

#### Counted by quantity

* **Stock:** an asset may have `stock`, `{"quantity", "unit", "low_stock_threshold", "is_running_low"}`, or `null` when its stock is not counted this way. The quantity is a whole number from 0 to 1,000,000,000; for things measured in length or weight, pick a unit small enough (`mm`, `g`). The unit is optional text of up to 32 characters, and the threshold is optional, from 1 to 1,000,000,000.
* **Setting it:** creating an asset (`POST /api/v1/assets`) with a `stock` object starts counting; updating it (`PUT /api/v1/assets/{id}`) with an object replaces the unit and threshold, and with `null` stops counting and clears all four. An update without `stock` leaves it alone. An object without `quantity` keeps the current quantity (or starts at 0), so an edit cannot undo a use someone made while the form was open; the web form sends the quantity only when it was changed.
* **Using and restocking:** `POST /api/v1/assets/{id}/quantity` (`stock.record`) with `{"adjust": n}`, negative to use some and positive to restock, or `{"set": n}`. Using more than there is answers `409` (`not enough stock: 40 left`) and changes nothing. Changes run under the inventory lock, so people using stock at once each take their share.
* **Thresholds and the flag:** the same rule as categories. Dropping below the threshold flags the asset and climbing back clears it, reacting to crossings only, so a flag set by hand (`PUT /api/v1/assets/{id}/running-low`, `stock.record`) stands until the next crossing. Archived assets are not checked.
* **Events and history:** crossings and manual changes record `asset.running_low` and `asset.restocked`, with the asset, its quantity, threshold and unit, and who caused it; channels can subscribe to them. Every change of a quantity also records `asset.quantity_changed`, with how much it moved, for the asset's history. Channels cannot subscribe to it.
* **Asset history:** creating, editing, archiving and restoring an asset record `asset.created`, `asset.edited`, `asset.archived` and `asset.restored`, in the same transaction as the change, through the web app, the API and CSV imports alike (an import sets the trigger to `import`). Each lists its `changes`: the name, categories joined and left, field values, codes, and whether stock is counted, its unit and threshold, each with the value before and after and the field's label or the generator's name as they read then. A save that changes nothing records nothing. Archiving lists the codes it released, and restoring the codes it got back and those another asset had taken. Changes made elsewhere that affect an asset, such as deleting a field or a category, are not recorded on it, and assets created before this release have no record of earlier edits. Channels cannot subscribe to these either.
* **Reading the history:** `GET /api/v1/assets/{id}/history` (`assets.read`) returns `events`, newest first, 50 at a time, with the email and name of whoever made each change, and `more` when older events remain; `?before=<event id>` returns the page older than that event, and an event that is not the asset's is a 400.

### 5.2 Notifiers

Notifications go out through pluggable notifier drivers, in the same way printers use print drivers: Discord and MQTT.

* **Drivers** (`internal/notify`) declare their options, which options are secret, how to validate them and how to deliver an event. Events themselves are neutral; each driver decides how to present them. `GET /api/v1/notifiers/drivers` lists them.
* **Channels** (`notification_channels`) are configured through `/api/v1/notifiers` (`settings.manage`): a name, a driver, its options, an enabled switch and the event kinds the channel subscribes to. There can be any number. Secret options can be written but are never returned; reads show only whether they are set. `POST /api/v1/notifiers/{id}/test` sends a test message.
* **Delivery:** each event fans out to one delivery per enabled, subscribed channel, created in the same transaction as the event. A worker in `bam serve` sends them:
  * A delivery is claimed with a lease, so several servers sharing a database send it once.
  * Network errors and `5xx` responses are retried with exponential backoff; a `429` waits as long as the service asks. Other `4xx` responses fail at once, and anything still unsent after 24 hours is given up.
  * The outcome of recent deliveries is visible in `GET /api/v1/notifiers/deliveries`.
* **Discord** (`discord`): a webhook URL (secret), accepted only on Discord's own hosts (`discord.com`, `discordapp.com` and their `ptb.` and `canary.` variants), so the server cannot be pointed at arbitrary addresses. "Running low" is an embed in the category's color with the category name, a link to `{base_url}/categories/{id}`, the triggering user, the reason and a timestamp; "Restocked" is a shorter, neutral embed. An asset's messages link to `{base_url}/assets/{id}`, give its quantity and threshold with their unit, and are amber, since an asset has no color of its own. A change of condition is titled "Out of service" (red), "Needs maintenance" (amber) or "Working again" (neutral), with the note and who reported it. An overdue loan is "Overdue" (amber), naming the borrower and the day it was due (UTC).
* **MQTT** (`mqtt`), for those running a broker, such as Home Assistant's: each event is published as JSON, with people by name only and a link to what it is about, to `<topic prefix>/<kind>/<asset or category ID>` (a test message to `<prefix>/test`), at QoS 1, not retained. Options: the broker, `mqtts://host[:port]` for TLS (8883 unless given) or `mqtt://host[:port]` to send without encryption (1883), so sending in the clear is chosen explicitly; the topic prefix (no `+`, `#` or NUL); a client ID (`bam` unless given; at most 23 characters); a user name and password (secret); a CA certificate in PEM, to trust a broker whose certificate no public authority signed (the system's authorities otherwise); and a client certificate and key in PEM (the key secret), for brokers that know clients by certificate. Each delivery connects, publishes, waits for the broker's acknowledgment and disconnects, so no connection is kept alive between them. A broker refusing the credentials or the client, or one whose certificate is not trusted, fails the delivery at once; one not reachable, or not answering, is retried. The client is the part of MQTT 3.1.1 publishing needs, written here rather than taken from a library. Like the other channels, MQTT channels are set up by `settings.manage`, and their broker may be any address.

### 5.3 Lending

Some equipment is lent out: drills, multimeters, a ladder. Lending is turned on per category and tracked per asset (migration `014`).

* **Lendable categories:** `is_lendable` on a category lets its assets, and those of every category below it, be checked out. `GET /api/v1/assets/{id}` says whether an asset is `lendable`, and gives its open `loan`, if any: the borrower (`id`, `email`, `name`, and `archived` if they have left), when it went out, when it is due back, and whether it is `overdue`.
* **Who may lend:** `assets.borrow` checks an asset out to oneself and checks one's own loans back in; `assets.lend` checks an asset out to any active user and checks any loan back in. Migration `014` grants both to every role that had `assets.write`. Borrowers are always user accounts; `GET /api/v1/people` (`assets.lend` or `users.manage`) lists the active users a loan can name. An API token can lend with `assets.lend`, naming the borrower, but cannot borrow.
* **Checking out:** `POST /api/v1/assets/{id}/checkout` with `{"borrower_id", "due_at"}`, both optional: no borrower means whoever is signed in, and `due_at` is an RFC 3339 time (the web app sends the end of the chosen day in the browser's time zone, since the server has no local time zone of its own). It is refused (`409`) for an asset that is archived, in no lendable category, out of service, already lent (one borrower at a time), or counted by quantity without being weighed (§5.1): lending part of a pooled quantity is not supported, so label each item to lend it, but a weighed spool is lent like anything else.
* **Checking in:** `POST /api/v1/assets/{id}/checkin` ends the open loan. For an asset counted by quantity, such as a spool, `{"left": n}` says how much came back; the difference is recorded as use and checked against thresholds. A lent asset cannot be archived until it is checked in (`409`). A borrower who is archived keeps their loans, so it stays clear who had the drill.
* **Loans and the list:** `GET /api/v1/loans` (`assets.track`, §3.3) lists loans of the assets the requester tracks, newest first, by the borrower's name, returned ones too, with `?borrower_id=` and `?state=open` or `overdue`. The asset list filters with `?loan=open` or `overdue` and shows who has each asset. The Loans page shows lenders every open loan and borrowers their own, past and present.
* **History and notices:** checking out and in record `asset.checked_out` and `asset.checked_in` in the asset's history, with the borrower as they were named then and the due date. The notification worker in `bam serve` sweeps for open loans past due each time it polls and records `asset.overdue` once per loan, with the trigger `schedule`; channels can subscribe to it, and several servers sharing a database still record it once.

---

### 5.4 Actions

An action is a button people press on assets, such as Identify, which records an event for devices and notification channels to act on: a cabinet that lights the drawer an asset is in, say (migration `026`).

* **Defining them:** `categories.manage` defines actions through `/api/v1/actions`: a key devices listen for (1–64 lowercase letters, digits, `-` and `_`, starting with a letter, unique), a label people see, and optionally a question to confirm first. Anyone who reads assets lists them.
* **Offering them:** a category offers actions on its assets (`PUT /api/v1/categories/{id}/actions` with `{"actions": [{"action_id", "cascades"}]}`; `categories.manage`), and with `cascades` on its subcategories' assets too. An asset's `actions` are those its categories offer and those cascading from their ancestors, by label. Deleting an action takes it off every category.
* **Pressing one:** `POST /api/v1/assets/{id}/actions/{key}` (`assets.act`, scopable; migration `026` grants it to every role that reads assets, in the same scopes, except the public role) records `asset.action` on an active asset offering the action, and answers `201` with the event's ID. One person may press an asset's actions once every 2 seconds and 30 times a minute on each server (`429` with `Retry-After` otherwise). The web app shows them on the asset's page and, when its scan template includes the `actions` quick action, on its scan page; an action with a question asks it first.
* **The event** carries the action's key and label, the asset, and the containers it is in through location links (§3.4), outermost first, each with its codes by generator type and its field values by key, so a device can find the thing: give each drawer an `led` field, and identifying a resistor lights its drawer. It is kept in the asset's history, and notification channels can subscribe to it; Discord shows it as "Identify: Drawer A3", pressed by whom.

#### Live Connections

Devices dial in for events rather than being called: a cabinet on the hackspace network opens a WebSocket and is sent the events meant for it as they happen (migration `027`).

* **Connecting:** `GET /api/v1/events/live` upgrades to a WebSocket (RFC 6455, version 13; anything else is `426`), with an API token as `Authorization: Bearer …` or a signed-in session. It needs `events.read`, which is scopable and held by no role but `admin` until given: a device's token gets a role holding it in the scopes its cabinet covers. One token or user holds at most 5 connections at once (`429`). Nothing is set up for a connection in the admin pages: the token is what lets it in, and the tokens page lists each token's open connections (`GET /api/v1/tokens/connections`, `users.manage`), on every server.
* **What it is sent:** first `{"type": "hello", "connection_id", "filter"}`, then `{"type": "event", "event": …}` for each event recorded after it opened about an asset or category in a scope it holds `events.read` in, with people by name only, and only when it holds `assets.track` there too. It may narrow what it is sent when connecting, with comma-separated `?kinds=`, `?actions=` (action keys) and `?categories=` (each with its subcategories), or later by sending `{"type": "filter", "filter": {"kinds": […], "actions": […], "categories": […]}}`; empty lists do not narrow.
* **Answering actions:** a device may answer an `asset.action` event it was sent, once, with `{"type": "reply", "event_id", "ok": true, "message": "Lit drawer A3"}` (a message of up to 255 characters); it is acknowledged with `{"type": "replied"}`. The answer is kept with the device's token name, and `GET /api/v1/assets/{id}/actions/replies?event_id=` (`assets.act`) lists the answers to a press; the button that was pressed shows them for a few seconds, or that nothing answered. Anything else it sends is answered with `{"type": "error", "message"}`.
* **Delivery:** every server reads new events from the database while it holds connections: at once after a request that changed something, and every second otherwise, which brings what other servers sharing the database recorded. Clocks differ between servers, so each read looks back a few seconds and skips what was sent. Connections are live only: one that drops is forgotten, and what happened meanwhile is not replayed. A connection falling far behind is closed.
* **Upkeep:** the server pings each connection every 30 seconds, and every 10 checks its token or user still may read events and refreshes its permissions; one revoked, deactivated or without `events.read` is closed with a policy-violation status saying why, at once on the server that revoked it and within seconds on others. Each server lists its open connections in the database, so every server can count a token's, and those a stopped server left drop off the list after a minute.

## 6. Batch Processing Subsystems

### 6.1 CSV Import & Export

**Format.** One row per asset, UTF-8 (a leading byte order mark is written on export and accepted on import), with these columns:

| Column | Meaning |
| :--- | :--- |
| `id` | The asset's UUID. Blank on import creates a new asset. |
| `name` | The asset's name. |
| `categories` | Category references separated by `;`. A reference is a path of names from the root joined by ` > ` (`Tools > Drills`) or a category ID. Export writes the path unless it is ambiguous or contains a separator, and then the ID. |
| `code.<type>` | The assigned code for a generator type (`code.qr`, `code.micro_qr`, `code.code128`). |
| `quantity`, `unit`, `low_stock_threshold` | Stock counted by quantity (§5.1). Export writes them only when some asset's stock is counted that way. On import a blank cell keeps what the asset has; a unit or threshold for an asset whose stock is not counted needs a quantity too. An import cannot stop counting, or remove a threshold. |
| `field.<key>` | A field value, by corpus key. |
| `links` | The asset's links (§3.6), each `verb:asset`, separated by `;`: `contained_in:Bin A3;used_with:Drill`. The verb is a relation type's name or inverse name written as a verb, read from this row's asset (case and spacing do not matter on import). The asset is a name when it names one active asset, otherwise an ID or an assigned code. Export writes the name where that works, and the ID where it does not (two assets of that name, or a name containing `;`); it writes the column only when some exported asset has links, listing each link from both ends. |

Spreadsheet formula injection is defused: on export a cell beginning with `=`, `+`, `-`, `@`, a tab, a carriage return or `'` is prefixed with `'` (plain numbers such as `-5` are left alone), and on import a `'` followed by one of those characters is removed, so a file reads back exactly as it was written.

**Export.** `GET /api/v1/assets/export.csv` (`assets.read`) takes the asset list's filters (`category_id`, `search`) and writes every matching asset, with a column for every generator type and every field any of them has a value for.

**Import.** `POST /api/v1/assets/import/preview` and `POST /api/v1/assets/import` (`assets.write`) take the file (at most 5 MB and 5,000 rows) and a mapping from each column to `id`, `name`, `categories`, `links`, `quantity`, `unit`, `low_stock_threshold`, a generator type, a field key, or `ignore`.

1. **Mapping:** the preview suggests a mapping from the headers (the names above, and field labels as well as keys). Every column must be mapped or explicitly ignored, and the user confirms the mapping before importing.
2. **Pre-flight validation:** every referenced category must already exist; otherwise the import is refused with the list of unknown references. Each row is then checked as the API would check it: required fields from the asset's effective fields, field types and select choices, and generator codes (format, and uniqueness both within the file and against other assets). Duplicate IDs within the file, unknown IDs and archived assets are errors.
3. **Rows:** a row without an `id` creates an asset. A row whose `id` matches an asset updates it, and a blank cell leaves that value unchanged. The preview reports each problem with its row and column, and how many assets would be created, updated and left unchanged.
4. **Links:** names in a `links` column are read as they will be once the rows are applied, so a file can create a bin and fill it, and a link stated from both ends counts once. Links that do not exist yet are added, and for a single type (§3.6) a new link moves the asset, ending its old one. By default nothing else is removed. With `remove_links=true` (the form field, beside the file and mapping; the web app's "Also remove links that no row states"), the links of the file's assets that no row states are removed too, so an exported file, edited, imports back exactly; assets the file does not mention keep theirs. The preview lists each link added, moved or removed (up to 500, with counts of all), and reports a cell that is not `verb:asset`, an unknown verb, an asset named by nothing or by several, an asset linked to itself, a new link to an archived asset, an asset put in two places, and a loop. Link changes are recorded in history with the trigger `import`.
5. **Atomic processing:** the import validates everything again, without trusting the preview, and applies every row in one transaction. Any error means nothing is written. A link the preview planned to remove or move that has gone since, or one added since that the import would duplicate, refuses the import (`409`), asking for a new preview. Inventory thresholds (§5.1) are checked once afterwards, so each category records at most one event.



### 6.2 Bulk Label Printing & Network Dispatch

* **Export Mode:** `POST /api/v1/labels/export` takes a template ID, up to 500 distinct asset IDs and a format (`pdf` or `zip`, with `"mono": true` for 1-bit PNGs), and streams back a multi-page PDF or a ZIP of PNGs named `<asset_id>.png`.
* **Network Dispatch Mode:** `POST /api/v1/printers/{id}/print` (`labels.print`) takes a template ID, asset IDs and a copy count (1–20, at most 500 labels in all), checks the template against the printer's media (§4.4), and streams the job to the printer over TCP port 9100.
* **What can print:** `POST /api/v1/labels/options` (`assets.read`) takes up to 500 asset IDs and answers with the templates that can label them: their categories' templates first, in order of preference (marked `preferred`), then the rest, each with the `problems` of up to 50 of the assets it cannot label (an asset with no code of a type it prints, in a sentence), and `template_id`, the one to start with: the first of their categories' that labels them all and prints somewhere. For those with `labels.print` it adds the `printers`, and `fits`, by template and printer, whether its labels print there (`fits`), why not (`reason`, every reason at once, naming the template and printer), and `warnings`. The web app's Labels panel offers only the printers a template fits, listing the others with why not, and puts a template's problems in place of its preview.
* **All or nothing:** every asset is bound and validated before any output is produced, so a batch either renders completely or fails with `422` and a list of `{asset_id, path, message}` for the assets that could not be rendered. Archived assets are never labeled.
* **History:** labels sent to a printer record `asset.label_printed` in each asset's history, with the template and printer as they were named then and the copies. Exports are not recorded, since they may never be printed. Channels cannot subscribe to it.

#### The Spool

The spool (migration `023`), the **Print queue** in the web app, is a print queue that labels gather in over time, to be sorted onto templates and printers in bulk and printed together. There is one, shared by everyone with `labels.print`, which every spool endpoint needs; each entry also needs its asset readable in its scopes (§3.3), and entries for assets a requester cannot read are neither listed nor changed for them.

* **Label templates on categories** (migration `030`): a category names the label templates its assets join the spool with, in order of preference, up to 10 (`PUT /api/v1/categories/{id}/label-template` with `{"label_template_ids": [...]}`, or `{"label_template_id"}` for one, an empty list or null for none; `categories.manage`), and its subcategories take them unless they name their own. A category answers with `label_template_ids` and the first as `label_template_id`; the one template a category named before migration `030` becomes its first. The list is chosen as scan templates are (§6.4): the closest category, ties settled by depth, size and age, with no default; categories naming the same list in the same order agree. An asset's `label_template` gives the choice, with the list as `template_ids`; ties are marked as for scan templates, listed by `GET /api/v1/assets?conflict=label_template` and `GET /api/v1/categories/label-conflicts` (which takes `label_template_ids=a,b` to ask as if a category named them). A template a category names cannot be deleted (`409`).
* **Entries:** each is one asset's label: a template, a destination (`printer:<id>`, `pool:<id>`, `download`, or none yet), copies (1–20), who added it and when. An asset may wait more than once with different templates, but adding one already waiting with the same template adds nothing. Deleting a printer or a template leaves its entries without one. At most 5,000 labels wait at once; an add that would pass that adds nothing (`422`).
* **Adding:** `POST /api/v1/spool` takes exactly one of `asset_ids`, `category_id` (with `"subcategories": true` for those below it too) or `filter` (an asset filter as `GET /api/v1/assets` takes it, never archived). Each label takes `template_id` when given, else the first of its categories' templates that suits its `destination` (§4.4), or the first when it has none yet; `destination` when given, else the one printer whose fit check (§4.4) its template passes, if only one does; and `copies`, 1 by default. The answer lists the entries `added` (`201` when there are any), the assets `already_waiting`, and those `skipped` as archived or not found. The web app adds from the asset list (the selection, or everything the filters show), a category, an asset's page, its scan page, the new-asset form and CSV imports; the last two start ticked and remember each person's choice in their browser.
* **Sorting:** `GET /api/v1/spool` lists the waiting entries in the order added, with the templates and printers and which printers each template suits (`fits`), what to know before printing each template where it can go (`fit_warnings`, which never keep an entry from printing), whether each entry's template came from its categories (`template_from_category`; choosing one by hand clears it), and each entry's `problems`: no template, no destination, a template its printer does not suit, an archived asset, or why its label could not be produced last time. `?mine=true` keeps those the requester added. `PATCH /api/v1/spool` sets `template_id`, `destination` or `copies` on `entry_ids`, all or none, and clears their last problem; setting a template on entries with no destination sends them to the one printer it suits, if only one does. `POST /api/v1/spool/remove` takes entries out.
* **Printing:** `POST /api/v1/spool/print` makes print jobs (migration `024`) of `entry_ids`, or with none of every entry ready to print as it stands going to a printer, and answers at once with the jobs (`201`) and the entries left out with why. Each printer and template is a job of at most 500 labels, in the order the entries were added. An entry's last failure does not stop it printing again; its blockers do. Entries in a job show it (`job`, with its state and printer) until it is done, and cannot be changed, removed or printed again meanwhile (`409`).
* **The spooler** in `bam serve` sends jobs in the background, oldest first. Each printer takes one batch of a job at a time, and printers work in parallel; direct printing (`POST /api/v1/printers/{id}/print`) takes the same per-printer turn, so two jobs never interleave on a printer served by one server. The database keeps two servers sharing it from sending the same batch, or two batches to one printer. Labels are laid out as whoever asked for the job may see them when it is sent, and nothing is printed for an account deactivated or a token revoked meanwhile. A label that cannot be produced stays waiting with the reason while the rest print; a printer that cannot be reached fails the job, and its labels wait with the error. A batch being sent beats a heartbeat every few seconds; one without a beat for 30 seconds, as a stopped server leaves it, fails and its labels wait marked as perhaps printed, and a job whose printer is deleted before it starts fails too.
* **Printer pools** (migration `025`): a pool is a set of printers loaded alike that share print jobs, managed with the printers through `/api/v1/printer-pools` (`settings.manage`; listed to those who may see printers): a name, a batch size (1–100, 10 by default), whether shares are fixed throughout, and its printers, each with a share (1–100, 1 by default). An entry's destination may be `pool:<id>`, which a template's labels take by themselves when it is the one pool they suit and holds every printer they suit. A pool's job is cut into batches of about the batch size, a label's copies kept together. Its members are the printers whose fit check the template passes; the others sit the job out, said in the print answer's `warnings`. Members on a direct connection (`tcp`, `device`), where delivery tracks printing because the printer stops taking data when its buffer fills, take the next batch when they have sent their last, so a faster printer prints more. Members behind a spooler (`ipp`, `windows`), which accepts a whole job at once, are given their share of the batches up front, by share, interleaved; so is every member when shares are fixed. A member that cannot be reached, or whose media no longer suits, hands its batch, and any it was given, back to the rest and drops out of that job; the others take them, those behind a spooler too once no other is left, and the job fails only when every member has dropped out. Each member takes one batch at a time like any printer. Deleting a pool leaves its waiting labels with nowhere to go and fails its batches no member holds; deleting a printer takes it out of its pools.
* **Jobs:** `GET /api/v1/spool/jobs` lists them newest first, from the last 30 days unless `?older=true`: the template and printer as named then, the state (`waiting`, `sending`, `done`, `failed` or `canceled`), any error, and the labels in all and printed so far; a pool's job lists its `members`, each with the labels it printed, whether it is sending and whether it dropped out. `POST /api/v1/spool/jobs/{id}/cancel` cancels one not yet started (`409` after), its labels waiting again. `POST /api/v1/spool/jobs/{id}/resend` puts the labels a finished job printed back in the spool with their template and copies, going to `destination` when given and where the job went otherwise, and prints them at once when that is a printer or a pool.
* **Downloading:** `POST /api/v1/spool/download` lays out `entry_ids`, all of one template, each as many times as its copies, as one PDF or a ZIP of PNGs (`"mono": true` for 1-bit). Labels that cannot be produced stay waiting with the reason, and `X-Spool-Problems` says how many; the rest count as printed, recorded as a finished job to send again.
* **Printed:** printed entries stay, as the Printed list: `GET /api/v1/spool?printed=true`, most recent first, from the last 30 days unless `?older=true`, with the template and printer as named when printed. `POST /api/v1/spool/requeue` puts them back to print again with the template, destination and copies they had. Printing records `asset.label_printed` in each asset's history; a download names no printer and sets `download`.

### 6.3 Reports

Saved reports (migration `017`) gather assets by a filter and show them as tables whose columns are content bindings, the same `{{...}}` placeholders labels use (§4.1). Anyone with `assets.read` runs them; `reports.manage` creates, edits and deletes them, and migration `017` grants it to roles that manage templates. Two are seeded to start from and can be edited or copied: "Running low", a shopping list grouped by category, and "On loan".

* **The spec** is JSON, checked when saved as templates are, with unknown keys refused:
  ```json
  {
    "filter": {"category_ids": ["…"], "search": "drill", "archived": false, "running_low": true, "loan": "open", "condition": "problem"},
    "sections": [
      {"type": "table", "title": "Shopping list",
       "columns": [{"heading": "Asset", "binding": "{{asset.name}}"}, {"heading": "In stock", "binding": "{{stock.quantity}} {{stock.unit}}"}],
       "sort_by": 1, "descending": true, "group_by": "category"}
    ]
  }
  ```
  Every filter is optional. `category_ids` keeps assets in any of those categories or below them; `running_low` keeps assets flagged running low themselves or in a flagged category, or below one; `loan` (`open`, `overdue`) and `condition` (a condition, or `problem`) work as on the asset list (§5.3, §3.2). A report has 1 to 20 sections and a table 1 to 30 columns; `sort_by` names a column from 0, sorting numbers as numbers and text ignoring case, and `group_by: "category"` lists each row under every category its asset is in, groups by name, marking those running low.
* **Charts** are sections too: `{"type": "chart", "chart": "bar" | "donut", "group_by": …, "measure": …}` or `{"type": "chart", "chart": "line", "measure": …, "period": "week" | "month"}`. Bars and donuts group the assets by `category` (each it is in), `condition`, `borrower`, `location`, `unit` or `field:<key>`, and measure `assets` (how many), `quantity` (their stock, split by unit unless grouped by it, so units never add up) or `loans` (how many are lent out); the 20 largest groups are shown and the rest added up as `Other`. Lines run over the last 12 weeks (from Monday, UTC) or months, from the history of the assets the report finds: stock `used` or `restocked`, a line per unit, or `loans` made. The web app draws charts itself, as bars, a donut or lines, each described for screen readers and with its numbers as a table, and a chart downloads as CSV like a table: a row per group or period, a column per line.
* **Endpoints:** `GET /api/v1/reports` and `GET /api/v1/reports/{id}`; `POST /api/v1/reports` and `PUT`/`DELETE /api/v1/reports/{id}` with `{"name", "spec"}` (`reports.manage`). `GET /api/v1/reports/{id}/run` returns the result: how many assets matched and, per section, its headings and groups of rows, each with its asset's ID and cells. `POST /api/v1/reports/preview` with `{"spec"}` (`reports.manage`) runs an unsaved spec, for the editor. `GET /api/v1/reports/{id}/sections/{n}/csv` downloads table `n` (from 1) as CSV, written as the asset export writes its file (a byte order mark, and formula-like cells defused), with a first `Group` column when grouped, named after the report. A report naming a field deleted since it was saved answers `409`, asking to be edited.
* **In the web app**, the Reports page lists them; a report runs when opened, prints without the app's navigation, and links each row to its asset. The editor offers placeholders as you type and previews the report before it is saved.

### 6.4 Scanning

What scanning an asset's code shows is a scan template (migration `022`): a saved report of kind `scan`, kept and edited with reports (`reports.manage`), that lays out the one asset scanned.

* **The spec** has no filter, and 1 to 20 sections of three types: `{"type": "details", "columns": [...]}`, the asset's own values, a heading and binding each, as reports' columns; `{"type": "table", "source": "contents", "columns": [...], "sort_by": …, "descending": …}`, a table of what the asset holds through location links (§3.6), up to 500, only those the viewer can read; and `{"type": "actions", "actions": [...]}`, quick actions from `open`, `edit`, `use`, `borrow`, `return` and `report`, each listed once. Charts and groups are refused. One is seeded, "Scanned": the asset's name, location, condition and stock, what it holds, and its actions.
* **Choosing one:** a category names the scan template its assets show (`PUT /api/v1/categories/{id}/scan-template` with `{"scan_template_id"}`, null for none; `categories.manage`), and its subcategories show it too unless they name their own. An asset shows the template named by the closest category up from any of its categories, its own category before its parent, its parent before a grandparent, whichever tree each is in. Equally close categories naming different templates are a tie, settled by (b) the category with the longest line of ancestors, as the more specific, then (c) the one with the fewest subcategories, as the more granular, then (d) the oldest, so that rearranging other categories does not change it, then by ID. An asset in no category naming one shows the default template; `POST /api/v1/reports/{id}/default` (`reports.manage`) makes another the default. The default, and any template a category names, cannot be deleted (`409`).
* **Ties are marked**, since someone should settle them: an asset's `scan_template` gives the template, the category it came from and, when a tie between different templates decided it, `conflict` and the categories in it (`contenders`). `GET /api/v1/assets?conflict=scan_template` lists those assets, and `GET /api/v1/categories/scan-conflicts` the categories in ties, with how many active assets each; with `?category_id=…&scan_template_id=…` (empty for none) it answers as if that category named that template. The web app marks them with a yellow triangle on the asset, in the asset list and in the category tree, and the category form warns before saving a template that would make a new tie. Ordinary inheritance is never marked.
* **Endpoints:** scan templates are reports: `GET /api/v1/reports?kind=scan` lists them, and `POST /api/v1/reports` with `"kind": "scan"` creates one; a report keeps its kind. `POST /api/v1/reports/preview` with `{"kind": "scan", "asset_id", "spec"}` lays one out for an asset the editor can read. Running one as a report, or downloading its CSV, is `409`. The Reports page lists them apart, with the default marked.
* **`assets.scan`** looks an asset up by its code and sees its scan page. Migration `022` grants it to every role that reads assets, in the same scopes, the public role among them (§3.3).
* **Looking up:** `GET /api/v1/scan/{code}` (`assets.scan` for the asset; signed-out visitors too, with the public role) finds the active asset with that assigned code, of any generator type, or that ID, and answers with the asset's ID, name and stock unit, the template, and the template laid out for it. Its actions are narrowed to those the viewer may take now: `open` with `assets.read`, `edit` with `assets.write`, `use` for counted stock with `stock.record`, `borrow` for oneself when it is lendable, not lent and in service, with `assets.borrow`, `return` when lent, with `assets.lend` or as its borrower, `report` with `assets.report` or `assets.write`, and `actions`, the actions its categories offer (§5.4), with `assets.act`, each in the asset's scopes; with `actions` kept, the answer's asset lists them. An asset the viewer cannot scan, an archived one, or none is `404`; a code two assets have, as codes of different kinds, is `409`. A template naming a field deleted since is `409`, asking for it to be edited.
* **The scan page** in the web app, `/scan/{code}`, shows the template and takes its actions in place: using some stock, borrowing (with an optional due date), returning, and reporting a problem, with the same words and problem form as the asset's page; opening and editing go to the asset's pages. Details with no value and contents with nothing in them are left off. It is open to signed-out visitors for public assets (§3.3); a visitor who is not signed in is asked to sign in rather than told nothing has the code, since the server does not say which, and so is one opening an asset's page. Every asset's page links to its own. Labels reach it with `{{scan_url}}` (§4.1), which the template editor puts in new QR codes, warning about a QR code that is not a link.
* **Looking codes up** in the web app: the **Scan** page, `/scan` (in the menu for `assets.scan`), takes a code typed, pasted or typed by a USB scanner, or read by the camera where the browser has the Barcode Detection API, and opens its scan page; a scan link or an asset's address, from any of the server's addresses, opens that page. The home page has a box for it too.

### 6.5 Help

The web app carries its own user help: Markdown topics in `web/src/help/`, bundled with the app and rendered by a small renderer of its own (`web/src/lib/markdown.ts`: headings, paragraphs, lists, bold, italics, code, and links to the app's pages or https, everything else escaped). `/help` lists the topics and `/help/{topic}` shows one; both are open to everyone, signed in or not. Every page has one **Help** link, in the header, to the topic for that page; forms carry no help marks of their own. A topic is added by adding its file and, in `web/src/lib/help.ts`, its place in the list and the pages it covers; tests check that every page has a topic, that links between topics lead somewhere, and that the help says rather than asks.

---

## 7. Implementation Roadmap for Agents

When implementing this repository from scratch, complete the following milestones in sequential order:

```
[Phase 1: Foundation] ──► [Phase 2: Core Domain] ──► [Phase 3: Template & Print] ──► [Phase 4: Frontend UI] ──► [Phase 5: Automation] ──► [Phase 6: Operations]

```

### Phase 1: Foundation & Infrastructure

#### Completed Skeleton & Base Tooling
* [x] Establish Go module structure (`cmd/bam`, `internal/api`, `internal/models`, `internal/store`, `internal/config`, `internal/auth`, `internal/rbac`, `internal/cli`).
* [x] Implement TOML configuration loader with environment variable overrides (`BAM_SERVER_*`, `BAM_DATABASE_*`, `BAM_DB_PATH`, `BAM_STORAGE_*`, `BAM_PRINTERS_DEVICES`).
* [x] Set up database abstraction layer with pure-Go SQLite (`modernc.org/sqlite` WAL mode) and embedded migration runner with 16 core schema tables.
* [x] Implement CLI commands (`init`, `migrate`, `admin create`, `serve`, `version`).
* [x] Onboard to `reaper` ([`.reaper.toml`](.reaper.toml)) with remote execution proven on Proxmox VM.

#### Sub-Phase 1.1: Database-Backed Sessions & RBAC Store Engine
* [x] Implement session CRUD in store (`CreateSession`, `GetSession`, `DeleteSession`, `CleanExpiredSessions`).
* [x] Implement case-insensitive user lookup and user-role-permission SQL join for union resolution.
* [x] Implement store-level anti-lockout invariant enforcement on user deactivation, deletion, and admin role revocation.

#### Sub-Phase 1.2: Multi-Dialect Store Support (PostgreSQL & MariaDB)
* [x] Integrate pure-Go PostgreSQL (`github.com/lib/pq`) and MariaDB (`github.com/go-sql-driver/mysql`) drivers while preserving `CGO_ENABLED=0` cross-compilation.
* [x] Implement dialect-aware connection pooling and configuration in `store.Open()`.
* [x] Validate dialect compatibility across migrations and SQL operations for SQLite, PostgreSQL, and MariaDB (store and API suites run against each via `BAM_TEST_DIALECT`; see §8.3).

#### Sub-Phase 1.3: HTTP Cookie Management & RBAC Middleware
* [x] Implement stateful session cookie management (`bam_session`, `HttpOnly`, `SameSite=Lax`, secure detection).
* [x] Implement `Authenticate` middleware resolving sessions and injecting user + permissions map into request context.
* [x] Implement `RequireAuth` and `RequirePermission` authorization middlewares with `401 Unauthorized` and `403 Forbidden` responses.

#### Sub-Phase 1.4: Authentication & User Administration API
* [x] Implement session authentication endpoints: `POST /api/v1/auth/login`, `POST /api/v1/auth/logout`, `GET /api/v1/auth/me`.
* [x] Implement user administration endpoints (`GET /api/v1/users`, `POST /api/v1/users`, `PATCH /api/v1/users/{id}/status`, `DELETE /api/v1/users/{id}`).
* [x] Enforce anti-lockout rejection (`400 Bad Request`) when attempting to deactivate or delete the last active user with `users.manage`.
* [x] Prove complete Phase 1 test battery via `reaper test` in remote ephemeral VM.


### Phase 2: Core Domain & Schema

#### Sub-Phase 2.1: Global Field Corpus & Dynamic Select Schema Validation
* [x] Implement Field store operations (`CreateField`, `GetFieldByID`, `GetFieldByKey`, `ListFields`, `UpdateField`, `DeleteField`).
* [x] Implement validation logic for field types (`text`, `number`, `date`, `url`, `select`).
* [x] Implement JSON schema parser & validator for `select` options (`choices` uniqueness, non-empty, optional `default` constraint).
* [x] Implement Field corpus HTTP REST API (`/api/v1/fields`).

#### Sub-Phase 2.2: Category Hierarchy, Single-Parent Tree Traversal, Cycle Detection & Field Inheritance Engine
* [x] Implement Category store operations (`CreateCategory`, `GetCategoryByID`, `ListCategories`, `UpdateCategory`, `DeleteCategory`).
* [x] Implement cycle detection algorithm preventing circular references in category single-parent trees.
* [x] Implement `category_fields` associative bindings with requirement flags (`is_required`).
* [x] Implement recursive ancestor tree traversal and field inheritance engine with conflict resolution (Required overrides Optional).
* [x] Implement Category hierarchy HTTP REST API (`/api/v1/categories` and `/api/v1/categories/{id}/fields`).

#### Sub-Phase 2.3: Asset Registry, Generator ID Constraints & Uncategorized System State
* [x] Implement virtual/system `Uncategorized` category fallback for assets without user categories (hiding when asset count is 0).
* [x] Implement constrained generator key validators (`micro_qr` capacity/charset, `qr` UTF-8 payload, `code128` ASCII).
* [x] Implement compound uniqueness invariant `(generator_type, assigned_code)` across active assets.
* [x] Implement Asset store operations (`CreateAsset`, `GetAssetByID`, `ListAssets`, `UpdateAsset`, `DeleteAsset`) with multi-category join.
* [x] Implement data retention invariant ensuring field values are never discarded upon category detachment.

#### Sub-Phase 2.4: Dynamic Field Value Persistence, Full REST API & End-to-End Test Battery
* [x] Implement dynamic field value validation matching effective inherited category fields.
* [x] Implement Asset HTTP REST API (`/api/v1/assets`).
* [x] Build comprehensive integration test battery across fields, categories, inheritance, asset persistence, and generator keys.
* [x] Verify complete Phase 2 test suite via `reaper test` with remote ephemeral ZFS rollback.

### Phase 3: Generator & Label Rendering Engine

Sub-phases land in order, each with its tests, and leave the tree releasable. The only new module dependency is `golang.org/x/image`, held at v0.30.0, the last release that supports Go 1.23.

#### Sub-Phase 3.1: Symbology Encoders
* [x] Implement a shared bit buffer and GF(256) Reed–Solomon encoder.
* [x] Implement the QR Code encoder (versions 1–40, error correction L/M/Q/H, numeric/alphanumeric/byte modes, penalty-based mask selection).
* [x] Implement the Micro QR encoder (M2–M4, numeric/alphanumeric modes, Micro QR mask evaluation).
* [x] Implement the Code 128 encoder (code sets B and C, check symbol).
* [x] Replace the `ValidateGeneratorCode` switch with a generator registry implementing §4.2, keeping its current validation behavior and tests.
* [x] Check in golden module matrices generated by [`tools/goldens`](tools/goldens/README.md) with Python's `segno` (QR and Micro QR, with version and mask pinned). Valid Code 128 encodings are not unique, so Code 128 is verified by decoding with `zxing-cpp` instead.
* [x] Add Go fuzz targets asserting that every payload `Validate` accepts encodes at the lowest error correction level.

#### Sub-Phase 3.2: Template Specification, Storage & API
* [x] Implement the template parser and validator (§4.1, `internal/labels`), rejecting unknown properties, with errors that name the offending element and property.
* [x] Implement the content binding parser and resolver.
* [x] Implement template store operations and the REST API (`/api/v1/templates`): reads need `assets.read`, writes need `templates.manage`. Validation failures return `400` with a `details` list of `{path, message}`; a `PUT` cannot change a template's ID.
* [x] Add migration `002` for the `images` table and the image API (`/api/v1/images`): PNG or JPEG up to 2 MiB and 4096 × 4096 pixels, uploads need `templates.manage`, and deleting an image a template still uses is refused with `409`.

#### Sub-Phase 3.3: Layout & Rendering
* [x] Implement unit conversion, dot-grid layout, module snapping and quiet zones (`internal/render`).
* [x] Implement text layout with the embedded Go fonts (Regular, Bold, Mono, Mono Bold): measuring, wrapping, shrink-to-fit and ellipsis.
* [x] Implement the SVG, PNG (grayscale and 1-bit) and PDF backends.
* [x] Check in golden outputs for each backend, regenerated with `go test ./internal/render -update`; `tools/goldens/verify_renders.py` decodes the rendered barcodes with `zxing-cpp` to confirm they scan.

#### Sub-Phase 3.4: Export Endpoints
* [x] Implement `GET /api/v1/assets/{id}/label?template_id=…&format=svg|png|pdf` (`assets.read`; add `mono=true` for a 1-bit PNG). An asset the template cannot be filled for returns `422` naming the element.
* [x] Implement `POST /api/v1/templates/preview` (`templates.manage`), rendering an unsaved template (its `id` may be omitted) as SVG against `asset_id` or placeholder data, for the Phase 4 editor.
* [x] Implement `POST /api/v1/labels/export` (`assets.read`, §6.2).
* [x] Cap request body sizes on every endpoint: 1 MiB, refused with `413` when declared up front; image uploads keep their own limit.

#### Sub-Phase 3.5: Brother QL Network Printing
* [x] Add migration `003` for the `printers` table, and the `labels.print` permission.
* [x] Implement the media catalog, fit check, printable-area crop and automatic rotation (`internal/printers/brotherql`).
* [x] Implement the raster command encoder with PackBits compression.
* [x] Implement the TCP dispatcher with timeouts and per-printer job serialization (`internal/printers`).
* [x] Implement the printer REST API (`/api/v1/printers`, managed with `settings.manage`) and `POST /api/v1/printers/{id}/print` (`labels.print`).
* [x] Test against an in-process fake printer that decodes the command stream and rebuilds each label's bitmap for comparison with the 1-bit render, plus a golden byte stream per media.
* [ ] Check printing on a real QL-810W (manual; outside reaper).

#### Sub-Phase 3.6: DYMO LabelWriter Printing
* [x] Implement the `dymo_labelwriter` driver for both LabelWriter raster languages (`internal/printers/dymolw`), with models, media catalog, fit check, rotation, and `density` and `roll` options.
* [x] Test against expectations worked out from DYMO's technical reference manual, golden byte streams per protocol and head width, a stream decoder that rebuilds each label's bitmap, and an end-to-end print through the API to a fake printer.
* [ ] Check printing on a real LabelWriter 400 or 450 (manual; outside reaper).

#### Sub-Phase 3.7: TSPL Printing
* [x] Implement the `tspl` driver (`internal/printers/tspl`) for Rollo and other TSPL printers, with models, a media catalog of common sizes, fit check, rotation, copies as `PRINT` counts, and `density` and `rotate_180` options.
* [x] Test against expectations worked out from TSC's programming manual, golden programs, a decoder that rebuilds each bitmap, and an end-to-end print through the API to a fake printer.
* [ ] Check printing on a real Rollo X1038 (manual; outside reaper).

#### Sub-Phase 3.8: Printer Connections
* [x] Replace a printer's host and port with a `connection`: `tcp`, `device` (allow-listed by the server's `[printers] devices` setting), `ipp` (raw queues on CUPS and other IPP servers) and `windows` (the Windows spooler, RAW).
* [x] Test each transport: device writes and the allowlist, an in-process IPP server that decodes the Print-Job request, refusal of `windows` off Windows, and an end-to-end print to a device through the API.
* [ ] Check the Windows spooler path on Windows (manual: it compiles and vets for Windows, but the suites run on Linux).

### Phase 4: Frontend Application

Sub-phases land in order, each with its tests, and leave the tree releasable. Frontend checks (`svelte-check`, Vitest, Playwright) run in pinned Node and Playwright containers. The Go suites stay free of Node, and the frontend checks can be added to reaper once they exist.

#### Sub-Phase 4.1: Scaffolding, Embedding & Shell
* [x] Create the Svelte 5 + TypeScript + Vite + Tailwind project in `web/` with exact pinned versions, `svelte-check` and Vitest.
* [x] Add `tools/web/build.sh`, building in a pinned Node container into `internal/webui/static/app/`.
* [x] Implement `internal/webui`: the embedded file server, history fallback to `index.html`, caching headers, security headers and the not-built placeholder, with Go tests for each.
* [x] Implement the typed API client (models mirroring `internal/models`, uniform error handling, `401` to sign-in), the session store, the router, sign-in and sign-out, and the app shell with permission-aware navigation.

#### Sub-Phase 4.2: API Additions for the Interface
* [x] `POST /api/v1/categories/effective-fields` resolves the effective fields for a set of categories, for the asset form before the asset exists.
* [x] `GET /api/v1/roles` lists roles with their permissions; `PUT /api/v1/users/{id}/roles` sets a user's roles under the anti-lockout guard.
* [x] `PUT /api/v1/auth/password` lets a signed-in user change their own password, ending their other sessions.
* [x] Each with store and API tests across SQLite, PostgreSQL and MariaDB. New passwords (users, the CLI admin, password changes) must be 8-72 bytes, the most bcrypt can use.

#### Sub-Phase 4.3: Fields & Categories
* [x] Field corpus views: list, create, edit and delete, by type with select options.
* [x] Category tree with color swatches, consumable and running-low flags, and create, edit, move and delete.
* [x] Category field assignment, showing each effective field's source category and inherited constraints.

#### Sub-Phase 4.4: Assets & Labels
* [x] Asset list with search, category filter and multi-select.
* [x] Asset detail.
* [x] Asset form generated from the effective fields of the chosen categories, re-resolved as they change, with generator keys and server errors shown beside the fields they name.
* [x] Archive an asset.
* [x] Label preview, PNG/PDF download and printing (printer, copies) for one asset; batch export and printing for a selection.

#### Sub-Phase 4.5: Template Editor
* [x] Template list and editor shell: canvas settings, printer-derived size and DPI, and the fit check.
* [x] Server preview canvas with throttled requests, plus the interactive element layer: select, drag, resize, grid snapping, keyboard nudging, multi-select and z-order.
* [x] Property panels per element type, binding suggestions, image picking and upload.
* [x] Undo/redo, unsaved-change protection, and saving with server errors mapped to elements.
* [x] Preview against an asset, export, and test print from the editor.

#### Sub-Phase 4.6: Administration
* [x] Printers: forms generated from `GET /api/v1/printers/drivers` (models, media, options and their values) and the connection types, with a test print.
* [x] Users and roles: list, create, activate and deactivate, archive and role assignment, with anti-lockout refusals explained.
* [x] Image library: upload, browse, see which templates use an image, delete.
* [x] Own password change.

#### Sub-Phase 4.7: End-to-End & Accessibility Battery
* [x] Playwright tests in a pinned container against a real `bam serve` on a fresh SQLite database, covering sign-in, fields, categories, assets, the editor (including dragging and keyboard editing), exports, and printing to an in-process fake TCP printer whose received job is decoded and checked.
* [x] Automated accessibility checks (`axe-core`) on every view, plus keyboard-only walkthroughs of the main flows.

### Phase 5: Batch Operations & Notifications

Sub-phases land in order, each with its tests, and leave the tree releasable.

#### Sub-Phase 5.1: Running Low & Inventory Thresholds
* [x] `PUT /api/v1/categories/{id}/running-low` (`assets.write`), changing the flag only on a real transition.
* [x] `low_stock_threshold` on consumable categories, with active counts over the category's subtree, re-checked by every change that can move a count, under an application lock.
* [x] Running-low events recorded in the same transaction as the change.

#### Sub-Phase 5.2: Notifier Framework & Discord
* [x] The notifier driver registry, channels with secret options, and `/api/v1/notifiers`.
* [x] Deliveries fanned out with each event, and the delivery worker: leases, backoff, `429` handling and the 24-hour limit.
* [x] The Discord driver, tested against a fake Discord server.

#### Sub-Phase 5.3: CSV Export
* [x] `GET /api/v1/assets/export.csv` with category paths, generator codes, field values and formula-injection defusing.

#### Sub-Phase 5.4: CSV Import
* [x] Header mapping suggestions, preview with per-row and per-column problems, and the strict category pre-flight.
* [x] Create-or-update by ID in one transaction, with thresholds checked once afterwards.

#### Sub-Phase 5.5: Import & Export Screens
* [x] Export from the asset list; upload, mapping, preview and confirmation for imports.

#### Sub-Phase 5.6: Notification & Threshold Screens
* [x] Administration of notification channels from the driver descriptions, with a test message and the delivery log.
* [x] The threshold and active count on the category form; running low and restocked actions; running-low categories on the home page.

#### Sub-Phase 5.7: Integration Battery
* [x] HTTP-level suites on SQLite, PostgreSQL and MariaDB covering inheritance conflict resolution, anti-lockout under concurrency, template vector compilation across a database round trip, import atomicity, threshold crossings (including simultaneous archives) and exactly-once delivery to several channels.

### Phase 6: Operations

Running BAM for real, and keeping it honest. In priority order:

#### Sub-Phase 6.1: Service Scripts & Installation
* [x] A sandboxed systemd unit and a FreeBSD rc.d script, with example configurations and installation steps (§2.1), and release builds for Linux and FreeBSD.
* [x] A check that installs the systemd unit in a Debian container and boots, kills and reboots it.

#### Sub-Phase 6.2: Releases & Updates
* [x] Release builds on GitHub for each tag, and an optional scheduled updater for both service managers (§2.2), with a check of the updater against fake releases.

#### Sub-Phase 6.3: Web Checks in reaper
* [x] `svelte-check`, Vitest and Playwright in the reaper battery (§8.1).

#### Sub-Phase 6.4: Error Mapping
* [x] Every handler answers a caller's mistake with `4xx` and only a failure of the server with `5xx`: database failures are told apart by their driver errors (`store.IsDatabaseFailure`), a lost race on a unique value is `409`, and a session or permissions that cannot be read are `500`, not a sign-out. A `500` names what failed and leaves the database's message to the log. `TestDatabaseFailuresAreServerErrors` drops the tables behind every endpoint's working request and checks none answers `4xx` or passes SQL on.

#### Sub-Phase 6.5: Expensive Inputs
* [x] Requests whose cost is out of proportion to their size are bounded. Laying out text measures each line once rather than every candidate from scratch, splits it into words once, and stops breaking lines past the box: the worst a template allows (100 wrapped, shrinking elements bound to 10,000-character values) went from 25 seconds and 6 GB to under 0.1 second, with output unchanged (`TestTextLayoutMatchesTheReference` compares it with the old algorithm). Batches stop with `422` once their labels hold more than 5 million shapes.

### Phase 7: Inventory

#### Sub-Phase 7.1: Quantities on Assets
* [x] Stock counted by quantity on an asset, beside the category counts (§5.1): use and restock actions, a threshold and running-low flag per asset, asset events for notifications, a per-asset history, CSV columns, and the screens for all of it.

#### Sub-Phase 7.2: Change History
* [x] Every change to an asset recorded in its history, not only stock (§5.1): who created, renamed, recategorized, edited fields, codes or stock settings, or archived it, and when, paged on the asset's page.

#### Sub-Phase 7.3: Restoring Archived Assets
* [x] Archived assets listed apart and restored (§3.2), with the codes no other asset has taken since, recorded in their history.

#### Sub-Phase 7.4: User Names
* [x] Display names for users, set by admins and by each user for themselves, shown in place of emails (§3.4).

#### Sub-Phase 7.5: Role Management
* [x] Roles created, edited and deleted from the web app and the API under the anti-lockout rule, with `admin` protected, and `stock.record` so a role can record stock without editing assets (§3.3).

#### Sub-Phase 7.6: API Tokens
* [x] Hashed, revocable, optionally expiring tokens holding roles, made once from the web app and shown once, with their changes recorded under their name (§3.4).

#### Sub-Phase 7.7: Asset Condition
* [x] Assets reported as needing maintenance or out of service, with a note, by members holding `assets.report`, cleared by those who edit assets, recorded in history, filtered in the list and announced to channels (§3.2).

#### Sub-Phase 7.8: Lending
* [x] Lendable categories; assets checked out to user accounts, by themselves (`assets.borrow`) or by lenders (`assets.lend`), with optional due dates; loans shown on assets, in the list and on a Loans page, recorded in history, and announced once when overdue (§5.3).

#### Sub-Phase 7.9: Asset Associations
* [x] Relation types named from each end, single and symmetric, with containers giving locations; links stored once, moves, loops refused, recorded in both assets' history; containers that hold anything kept from archiving; categories suggesting types (§3.6).

#### Sub-Phase 7.10: Weights for Counted Items
* [x] Consumable categories with a unit total their assets' stock in it, with thresholds in it, cascading where units match; weighed spools can be lent, and come back with what is left (§5.1, §5.3).

#### Sub-Phase 7.11: Reports
* [x] Saved reports: a filter over assets and table sections whose columns are template bindings, sorted and grouped by category, run, previewed, printed and downloaded as CSV; "Running low" and "On loan" to start from; new placeholders for labels and reports (§6.3, §4.1).

#### Sub-Phase 7.12: Report Charts
* [x] Bar, donut and line chart sections in reports: grouped and measured from the assets a report finds, or over time from their history, drawn in the web app with their data as tables and as CSV (§6.3).

#### Sub-Phase 7.13: Photos on Assets
* [x] Up to 10 photos per asset, stripped of metadata, turned upright and scaled, with thumbnails in the asset list, ordered, and recorded in history (§3.2).

#### Sub-Phase 7.14: Version in the Footer
* [x] Every page names the running version, linked to its release, or the commit of a development build (§2.2).

#### Sub-Phase 7.15: Links in CSV Files
* [x] A `links` column of `verb:asset` entries, exported from both ends and imported with a preview, adding and moving links, and removing those no row states when asked (§6.1).

#### Sub-Phase 7.16: Scoped Permissions
* [x] Scopes on categories, passed down, with a reserved `default` scope; roles holding asset permissions in some scopes only, checked on every asset; keys renamed `resource.action` (§3.3).

#### Sub-Phase 7.17: Seeing People
* [x] `assets.track` for borrowers, history's people and the loans list; names instead of emails on asset pages, and names required (§3.3, §3.4).

#### Sub-Phase 7.18: The Public Role
* [x] A role everyone holds, signed in or not, reading the assets in a `public` scope; signed-out visitors open those assets' pages and nothing else (§3.3).

#### Sub-Phase 7.19: Scan Templates
* [x] Reports of kind scan, chosen by categories with inheritance and a default, ties settled by depth, size and age and marked for someone to settle (§6.4).

#### Sub-Phase 7.20: Scanning Codes
* [x] Looking an asset up by its code, a scan page with quick actions open to the public role, and `{{scan_url}}` for labels (§6.4).

#### Sub-Phase 7.21: Help
* [x] User help in the web app, a topic per part of it, reached by one Help link on every page (§6.5).

#### Sub-Phase 7.22: The Spool
* [x] A shared print queue: label templates on categories, adding from everywhere assets are, sorting in bulk, downloading, the Printed list, and labels printed in assets' histories (§6.2).
* [x] Printing from the spool in the background, one job at a time per printer and printers in parallel, with resending past jobs.
* [x] Printer pools sharing a job's labels: the first free printer takes the next batch, printers behind a spooler take fixed shares, and one that fails hands its batch back.

#### Sub-Phase 7.23: Actions and Live Events
* [x] Actions on assets, such as Identify, offered by categories with cascading, pressed from asset and scan pages, and recorded with what a device needs to find the thing (§5.4).
* [x] Live connections devices open with an API token, sent the events they may see across servers sharing a database, with answers to actions (§5.4).
* [x] An MQTT notifier, with user names and passwords, TLS with a CA of its own, and client certificates (§5.2).

#### Sub-Phase 7.24: Label Stock and Adaptive Templates
* [x] Custom label stock for printers that take any size, and every roll the Brother QL-800 series takes (§4.4).
* [x] One fit check for every driver, with warnings as well as refusals (§4.4).
* [x] Warnings when a printer's roll changes, and which printers each template suits (§4.4).
* [x] Several label templates per category, by preference, that waiting labels move between when a roll changes (§6.2).
* [x] Templates that adapt to the label and printer they print on, with limits on their elements and alternatives, and the editor's sizing, anchors and previews on each printer (§4.1).

#### Sub-Phase 7.25: Interface Review
* [x] Printing offers only the printers a template fits, starts with the asset's category's templates, and says why a label cannot print or be made, in sentences (§6.2).
* [x] The template editor checks against a printer as you design, shades what it does not print on the canvas, test-prints unsaved templates, and scales elements to a printer's label size (§4.4).
* [x] Labels whose QR codes open the scan page, a Scan page that looks codes up by camera or typed, and a sign-in prompt for visitors (§6.4).
* [x] Setting someone's password from Users and `bam admin password`, `bam admin create --name`, a setup checklist on the home page, and pages that say what someone may do and what a page needs (§3.4).
* [x] Where things are: a place in the asset form, the list and asset pages, search by place, and the next code suggested (§3.5).
* [x] Phones: a one-row header with a menu, tables as cards, folded filters and help topics, and touch-sized editor handles (§1).
* [x] Plainer words (the Print queue, Link types, Code type, the roll loaded, printer models by make), creating with New, the category page's unsaved changes and its assets, and empty lists that say what to do (§1).

### Upcoming Features

Agreed, not yet scheduled or designed in detail.

#### Deferred
* **QR codes linking to an asset's page:** a QR code's content is a binding template authors choose; `{{scan_url}}` (§6.4) gives the asset's scan page for it, so no generator of its own is needed.

---

## 8. Ephemeral Test Harness (`reaper`) & Testing Methodology

BAM is onboarded as a tenant of [`reaper`](https://forge.axonibyte.com/axonibyte/reaper) via [`.reaper.toml`](.reaper.toml) for fast, isolated pre-push validation on remote ephemeral Proxmox VE virtual machines.

### 8.1 The Pre-Push Loop
```bash
reaper up        # Provisions remote VM, lays out ZFS pools, fetches pinned toolchains
reaper test      # Syncs uncommitted working tree -> builds -> ZFS state rollback -> runs test suite
reaper down      # Tears down remote VM (cull daemon provides automatic TTL backstop)
```

`reaper test` builds the binary in the pinned Go image, then runs `tools/reaper/run.sh` on the guest: the Go suites with `-race` (the SQLite database and uploads in `$REAPER_STATE`), then `svelte-check` and Vitest, then the Playwright and `axe` battery (§8.4). Each runs in its own pinned container, so the run verb executes on the guest (`exec = "host"`) and starts them with its podman; a containerized verb cannot start containers. The images are listed under `run.images` so reaper pulls them first, and `node_modules`, the npm cache and Go's caches live in reaper's caches rather than on the guest's small root disk. A test (`tools/reaper`) checks that every image the scripts run is in `run.images`, in the digest-only form reaper accepts. The script runs the same way by hand, with `REAPER_STATE` and the `REAPER_CACHE_*` variables pointing at directories of your own.

### 8.2 Testing Portfolio
Following the "Portfolio of Oracles" methodology:
1. **Tier 1 — Pure Unit & Golden Vectors:** Vector math, Micro QR bitpacking, bcrypt password hashing, boundary decoders, and cross-language barcode golden files (module matrices from Python's `segno`, scan checks with `zxing-cpp`) alongside byte-exact SVG, PNG, PDF and printer-stream goldens.
2. **Tier 4 — Server Contract Tests:** RBAC permission union resolution and anti-lockout invariants tested as in-process pure functions without database dependencies.
3. **Tier 6 — Full Stack & Migrations:** SQLite, PostgreSQL, and MariaDB, with migration idempotence. The test databases on PostgreSQL and MariaDB are deliberately hostile (§8.3), and `internal/integration` checks that awkward text (four-byte characters, combining marks, right-to-left text, SQL quotes and wildcards, the longest text allowed) survives storage everywhere the API keeps text, that NUL characters and overlong text are refused alike on every database, that text differing only in case or normalization stays distinct, and that search treats its term literally. The store suite upgrades a MariaDB database created before migration `008`, data and all, and resumes a conversion that stopped part way, and checks that PostgreSQL databases in `LATIN1` and `SQL_ASCII` are refused. The store and API suites run against PostgreSQL and MariaDB by pointing them at a server (see §8.3). The integration battery (`internal/integration`) drives a real server over HTTP on each database: inheritance conflict resolution, administrators removing each other at the same moment, a template rendered before and after a database round trip (byte-identical SVG, and SVG and PDF goldens written on SQLite that every database must reproduce; `go test ./internal/integration/ -update` rewrites them), import atomicity, threshold crossings including simultaneous archives, and exactly-once delivery from several workers to several channels.
4. **Tier 7 — Seeded Fuzzing:** `TestSeededFuzz` (`internal/api`) sends 1,500 requests to every endpoint, each made by mutating a valid one: values of the wrong type, out of range or awkward, missing and extra keys, deep nesting, broken JSON, damaged forms and uploads, junk and path-traversing IDs, and imports of generated CSV. Every response must be below `500`, every API error a JSON object with a message, and every JSON response valid; the server must still answer and still know the session afterwards. `TestSeededImports` (`internal/assetcsv`) plans 300 generated files and holds the planner to its word: a file without problems must import, and exporting everything afterwards and planning that again must change nothing. Both are deterministic, so a failure repeats; `BAM_FUZZ_SEED`, `BAM_FUZZ_REQUESTS` and `BAM_FUZZ_IMPORTS` choose another seed and length, and both run on PostgreSQL and MariaDB with the other database suites (§8.3). For longer, coverage-guided runs, Go's fuzzer drives the same checks: `go test -fuzz=FuzzAPI ./internal/api/`, `go test -fuzz=FuzzImport ./internal/assetcsv/`, and `FuzzParse` for templates and the generator fuzz targets in their packages. Sending to printers and notification services is left out, as those requests would leave the process.
5. **Tier 8 — Concurrency Harnesses:** Simultaneous write simulations for inventory threshold decrements and per-generator ID allocations, asserting stored state consistency.
6. **State Rollback:** Ephemeral databases and uploads live under `$REAPER_STATE` (`tank/state`), enabling near-instantaneous ZFS rollbacks to `@pristine` between test iterations.

### 8.4 Web Interface Tests

The web interface has its own checks, which run in pinned containers and need no Node on the host:

* `tools/web/build.sh check`: `svelte-check` (types and Svelte's accessibility warnings, which fail the run) and the Vitest unit and component tests, which run against a fake API.
* `tools/web/e2e.sh`: the Playwright battery in `web/e2e`. It builds the interface and a Linux binary, starts `bam serve` on a fresh SQLite database inside the Playwright container, and drives Chromium through sign-in, fields and categories, assets and labels, CSV export and import, thresholds and notification channels, the template editor (mouse and keyboard), and administration. Printing goes to in-process fake TCP printers whose jobs are checked byte by byte, and the server's outgoing HTTPS is pointed at a closed port, so notifications never leave the container. Every view is audited with `axe-core` for WCAG 2.1 A and AA in both light and dark mode, and keyboard-only walks cover the main flows. Arguments filter the specs: `tools/web/e2e.sh editor a11y`.

reaper runs both (§8.1). The scripts keep `node_modules`, the npm cache and Go's caches in named volumes; `BAM_WEB_NODE_MODULES`, `BAM_NPM_CACHE`, `BAM_GO_CACHE` and `BAM_GO_MOD` put them in directories instead.

### 8.3 Running the Suites Against PostgreSQL and MariaDB

By default every test uses SQLite. Setting `BAM_TEST_DIALECT` and `BAM_TEST_DSN` reruns the store, API, notification, CSV and integration suites against a live server. Each test creates and drops its own schema (PostgreSQL) or database (MariaDB), so the DSN must belong to a role allowed to do that; on PostgreSQL it must also be allowed to create databases, for the encoding tests.

These namespaces are deliberately hostile, as a server set up without BAM in mind may be: on MariaDB the database defaults to `latin1` with a case-insensitive collation, and each session starts with a Latin-1 connection, no SQL modes and the time zone `+05:45`; on PostgreSQL each session starts in `Pacific/Chatham` time with `standard_conforming_strings` off. Everything BAM needs, it sets for itself (§3.5).

```bash
BAM_TEST_DIALECT=postgres BAM_TEST_DSN='postgres://postgres:secret@localhost:5432/postgres?sslmode=disable' go test ./internal/store/ ./internal/api/ ./internal/notify/ ./internal/assetcsv/ ./internal/integration/

BAM_TEST_DIALECT=mariadb BAM_TEST_DSN='root:secret@tcp(localhost:3306)/' go test ./internal/store/ ./internal/api/ ./internal/notify/ ./internal/assetcsv/ ./internal/integration/
```

Portability rules for schema and queries:
* Write placeholders as `?`; `Store.Rebind` converts them to `$n` for PostgreSQL.
* Avoid reserved identifiers (`key` is reserved in MariaDB, which is why the corpus, permission and settings columns are `field_key`, `permission_key` and `setting_key`).
* Declare foreign keys at table level.
* Use `TIMESTAMP` in migrations; the runner rewrites it to `DATETIME(6)` on MariaDB, where `TIMESTAMP` columns silently gain `ON UPDATE CURRENT_TIMESTAMP`.
* The MariaDB driver is always opened with `parseTime=true` and `loc=UTC`, a `utf8mb4` connection, a strict SQL mode and the session time zone `+00:00`.
* Use `TEXT` for unbounded text; the runner rewrites it to `LONGTEXT` on MariaDB, where `TEXT` holds only 65,535 bytes, and creates every table in `utf8mb4` with `utf8mb4_nopad_bin`. A change that depends on the dialect or on what is in the database can be a Go migration (`codeMigrations` in `internal/store/migrations`).
* Check text with `internal/textcheck` before it is stored, and do not match user input with `LIKE`, whose wildcards, escapes and case folding differ between databases.

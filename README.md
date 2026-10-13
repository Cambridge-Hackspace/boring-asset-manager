# BAM! (Boring Asset Manager)

![BAM!](img/bam.png)

> Excruciatingly simple physical asset label generation and tracker.

BAM! is a single-tenant, API-first web application designed for tracking physical assets, managing hierarchical categories with dynamic field inheritance, and visually designing and batch-printing labels for dedicated thermal label printers (such as the Brother QL-810W) as well as standard sheet layouts.

---

## 1. System Architecture & Tech Stack

### Core Philosophy
- **Self-Contained Deployment:** Distributed as a single compiled binary for FreeBSD, Linux, and Windows with effortless `CGO_ENABLED=0` cross-compilation.
- **Embedded Frontend:** The Go backend serves the compiled Single Page Application (SPA) directly via `embed.FS`.
- **Strict Configuration Separation:** On-disk TOML handles bootstrap infrastructure (listening ports, database connection strings, master secrets). All operational settings, network printer definitions, webhooks, and layouts live inside the database.

### Technology Choices
- **Backend:** Go 1.23+ (Standard library `net/http` or router such as `chi`).
- **Database Abstraction:** Dialect-agnostic SQL driver supporting **SQLite3** (via pure-Go `modernc.org/sqlite` with WAL mode for seamless cross-compilation without C toolchains), **PostgreSQL 14+**, and **MariaDB 10.6+**. Migrations managed via embedded SQL scripts (`golang-migrate` or custom embedded schema runner).
- **Frontend:** TypeScript + Svelte (or React/Vite) with a lightweight 2D canvas/SVG label layout engine and Tailwind CSS for accessible, high-contrast, colorful category UI components.
- **Label Generation & Printing:**
  - **Pure-Go Rendering:** One layout engine feeds SVG, PNG and PDF backends (§4). Fonts, glyph outlines and rasterization come from `golang.org/x/image` (the embedded Go fonts, OpenType parsing and its vector rasterizer); the PDF writer is a small in-house one so output stays deterministic.
  - **Direct Network Printing:** Raw TCP dispatch (port 9100) of Brother QL raster command streams to network thermal printers such as the Brother QL-810W, with no desktop driver or print dialog.
- **Barcode & Matrix Engine:** In-house QR Code, Micro QR and Code 128 encoders with per-generator payload and character set validation (§4.2).

---

## 2. Configuration & Initialization

### TOML Schema (`config.toml`)
```toml
[server]
host = "0.0.0.0"
port = 8080
base_url = "http://localhost:8080"
session_secret = "replace-with-a-secure-random-64-char-string"

[database]
# Drivers: "sqlite", "postgres", "mariadb"
type = "sqlite"
# SQLite DSNs use modernc.org/sqlite `_pragma` parameters. Foreign key
# enforcement is always switched on by BAM regardless of the DSN.
dsn = "data/bam.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"

[storage]
local_path = "./data/uploads"

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
  --config ./config.toml

# Start the daemon
./bam serve --config ./config.toml

```

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
* Flags: `is_consumable (bool)`, `is_running_low (bool)`, `color_hex (string)`.
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

* **Tables:** `users`, `roles`, `permissions`, `user_roles`, `role_permissions`.
* **Permissions:** Granular string keys (`assets:read`, `assets:write`, `assets:delete`, `templates:manage`, `categories:manage`, `settings:manage`, `users:manage`, and from Phase 3 `labels:print`). Seeding grants every permission to the `admin` role, including permissions added by later releases.
* **Union Principle:** When a user is assigned multiple roles, their effective permissions are the strict union of all granted rights across all assigned roles.
* **Anti-Lockout Rule:** System operations must reject any deletion, deactivation, or role removal that results in zero active users possessing the `users:manage` permission.

### 3.4 Sessions & Authentication

* **Local Credentials:** Email (case-insensitive, unique) + password hashed via bcrypt (cost 12).
* **TOTP (RFC 6238):** Optional two-factor authentication flag per user.
* **Session Store:** Stateful sessions stored in the active database (`sessions` table) referenced via an `HttpOnly`, `SameSite=Lax`, `Secure` cookie.

---

## 4. Label Template Engine & Print Subsystem

Every label passes through the same three stages, whichever format it ends up in:

```
template + asset ──► bind ──► layout (whole printer dots) ──► SVG │ PNG │ PDF │ Brother QL raster
```

1. **Bind** resolves each element's `content_binding` against the asset, its field values and its generator keys.
2. **Layout** converts the template's physical units into dots at the template's DPI, snaps every barcode module to a whole number of dots, and sets text with the embedded fonts. Its output is a scene of rectangles, glyph outlines and images, measured in dots.
3. **Backends** draw that scene. Text is drawn as outlines rather than font references, so SVG, PDF and PNG agree at the template's DPI and never depend on the fonts installed where they are viewed.

### 4.1 Declarative Template Specification

A template is stored in the `templates` table: `id`, `name`, `units`, `width`, `height` and `dpi` are columns, and `elements` is kept in `layout_json`. The API reads and writes it as one JSON document:

```json
{
  "id": "brother_ql_29x90",
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

* **ID:** a slug matching `^[a-z0-9][a-z0-9_-]{0,63}$`, chosen by the author.
* **Canvas:** `units` is `mm` or `in`; `width` and `height` are positive; `dpi` is 150–600 (Brother QL printers print at 300). Positions and sizes use the template's units with the origin at the top-left corner, and every element must lie inside the canvas. `font_size` is always in points.
* **Content bindings:** `content_binding` is literal text with `{{...}}` placeholders:

  | Placeholder | Resolves to |
  |---|---|
  | `{{asset.id}}` | The asset's UUID |
  | `{{asset.name}}` | The asset's name |
  | `{{asset.code.<generator_type>}}` | The asset's assigned code for that generator, e.g. `{{asset.code.micro_qr}}` |
  | `{{field.<key>}}` | The asset's value for that corpus field (empty when unset) |
  | `{{base_url}}` | `server.base_url` from the configuration |

  Unknown placeholders and unknown field keys are rejected when the template is saved. A field deleted later simply resolves to an empty string. A `generator` element without a `content_binding` encodes `{{asset.code.<its generator_type>}}`, and rendering fails for an asset that has no code of that type. Resolved generator payloads go through the same validator as asset generator keys before they are encoded.
* **Element types:**
  * `generator`: `generator_type` (`qr`, `micro_qr` or `code128`); optional `error_correction` (QR `L`, `M` (default), `Q`, `H`; Micro QR `L` (default), `M`, `Q`); optional `quiet_zone` in modules (defaults: QR 4, Micro QR 2, Code 128 10). The symbol is drawn centered, at the largest whole number of dots per module that fits the box; rendering fails if that is under 2 dots. Code 128 bars fill the box height.
  * `text`: `font_size` (pt), `font_weight` (`normal`/`bold`), `font_family` (`sans`/`mono`), `align` (`left`/`center`/`right`), `wrap` (default `false`) and `min_font_size` (default 6). Text that overflows its box shrinks in 0.5 pt steps down to `min_font_size`, then is cut short with an ellipsis.
  * `static_image`: `image_id` names an uploaded PNG or JPEG, scaled to fit the box with its aspect ratio kept. Uploads are recorded in the `images` table and stored under `storage.local_path/images/`; `GET /api/v1/images/{id}` returns the metadata and `/content` the image itself. SVG images are not supported, since there is no dependable pure-Go SVG rasterizer.

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
2. **PNG:** rendered at the template's DPI, as 8-bit grayscale for previews or 1-bit black and white for printing. Barcode modules are never anti-aliased.
3. **PDF:** one page per label, sized exactly to the label, with vector barcodes and text and images embedded as Flate-compressed bitmaps. Output is byte-for-byte deterministic: no timestamps or random document IDs.

Batch exports produce a multi-page PDF or a ZIP of PNGs named by asset ID (§6.2).

### 4.4 Printers & Direct Network Dispatch

Printers are configured through the API and stored in the `printers` table: name, driver, model, host, port (default 9100) and the media currently loaded. Phase 3 ships one driver, `brother_ql`, targeting the QL-810W:

* **Media catalog:** each supported roll records its width, whether it is die-cut (and if so its label length), and its printable width and offset in head dots. Phase 3 covers DK-22210 (29 mm continuous), DK-22205 (62 mm continuous) and DK-11201 (29 × 90 mm die-cut). Two-color black/red media (DK-22251) is out of scope.
* **Fit check:** before anything is sent, the template must suit the loaded media. Its DPI must be 300, one side must match the media width (landscape templates are rotated 90° automatically), and on die-cut media the other side must match the label length.
* **Raster stream:** each label is rendered 1-bit at 300 DPI and encoded as described in Brother's QL-800 series raster command reference: invalidate, initialize, raster mode, print information, auto-cut and margins, then PackBits-compressed raster lines (90 bytes for each line of the 720-dot head) and the print command, cutting after the job's last label.
* **Transport:** raw TCP to port 9100 with dial and write timeouts. Jobs are serialized per printer so concurrent requests never interleave their streams. A job counts as printed once the printer has accepted the whole stream; reading back printer status is a later improvement.

---

## 5. Webhooks & Consumables Tracking

* **Consumable & Bin Management:**
* Categories tagged with `is_consumable = true` support an active `is_running_low` flag.
* Users can toggle this state manually via dashboard actions or inventory thresholds.


* **Discord Notification Engine:**
* Configurable webhook URL stored in the DB settings table.
* When a category transitions from `is_running_low = false` to `is_running_low = true`, a background job dispatches a formatted Discord Embed:
* Category name and visual color bar (`color_hex`).
* Timestamp and triggering user.
* Quick link to the category asset view.





---

## 6. Batch Processing Subsystems

### 6.1 CSV Import Pipeline

1. **Schema Pre-flight Validation:**
* Headers are parsed against the global field corpus and category lists.
* **Strict Validation Constraint:** All referenced categories in the CSV **must already exist** in the system. The importer aborts if unmapped or non-existent categories are detected.


2. **Field Mapping Interface:**
* Users confirm column-to-corpus mappings before finalizing execution.


3. **Atomic Processing:**
* Ingestion runs inside a database transaction to prevent partial state corruption on failure.



### 6.2 Bulk Label Printing & Network Dispatch

* **Export Mode:** `POST /api/v1/labels/export` takes a template ID, up to 500 asset IDs and a format (`pdf` or `zip`), and streams back a multi-page PDF or a ZIP of PNGs.
* **Network Dispatch Mode:** `POST /api/v1/printers/{id}/print` takes a template ID, asset IDs and a copy count, checks the template against the printer's media (§4.4), and streams the job to the printer over TCP port 9100.
* **All or nothing:** every asset is bound and validated before any output is produced, so a batch either renders completely or fails with a list of the assets that could not be rendered.

---

## 7. Implementation Roadmap for Agents

When implementing this repository from scratch, complete the following milestones in sequential order:

```
[Phase 1: Foundation] ──► [Phase 2: Core Domain] ──► [Phase 3: Template & Print] ──► [Phase 4: Frontend UI] ──► [Phase 5: Automation]

```

### Phase 1: Foundation & Infrastructure

#### Completed Skeleton & Base Tooling
* [x] Establish Go module structure (`cmd/bam`, `internal/api`, `internal/models`, `internal/store`, `internal/config`, `internal/auth`, `internal/rbac`, `internal/cli`).
* [x] Implement TOML configuration loader with environment variable overrides (`BAM_SERVER_*`, `BAM_DATABASE_*`, `BAM_DB_PATH`, `BAM_STORAGE_*`).
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
* [x] Enforce anti-lockout rejection (`400 Bad Request`) when attempting to deactivate or delete the last active user with `users:manage`.
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

Sub-phases land in order, each with its tests, and leave the tree releasable. The only new module dependency planned is `golang.org/x/image`.

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
* [x] Implement template store operations and the REST API (`/api/v1/templates`): reads need `assets:read`, writes need `templates:manage`. Validation failures return `400` with a `details` list of `{path, message}`; a `PUT` cannot change a template's ID.
* [x] Add migration `002` for the `images` table and the image API (`/api/v1/images`): PNG or JPEG up to 2 MiB and 4096 × 4096 pixels, uploads need `templates:manage`, and deleting an image a template still uses is refused with `409`.

#### Sub-Phase 3.3: Layout & Rendering
* [ ] Implement unit conversion, dot-grid layout, module snapping and quiet zones.
* [ ] Implement text layout with the embedded Go fonts (Regular, Bold, Mono): measuring, wrapping, shrink-to-fit and ellipsis.
* [ ] Implement the SVG, PNG (grayscale and 1-bit) and PDF backends.
* [ ] Check in golden outputs for each backend, regenerated with `go test ./internal/render -update`; `tools/goldens` decodes the rendered barcodes with `zxing-cpp` to confirm they scan.

#### Sub-Phase 3.4: Export Endpoints
* [ ] Implement `GET /api/v1/assets/{id}/label?template_id=…&format=svg|png|pdf`.
* [ ] Implement `POST /api/v1/templates/preview`, rendering an unsaved template as SVG against an asset or placeholder data, for the Phase 4 editor.
* [ ] Implement `POST /api/v1/labels/export` (§6.2).
* [ ] Cap request body sizes on every endpoint (1 MiB for JSON; uploads set their own limit).

#### Sub-Phase 3.5: Brother QL Network Printing
* [ ] Add migration `003` for the `printers` table, and the `labels:print` permission.
* [ ] Implement the media catalog, fit check and automatic rotation.
* [ ] Implement the raster command encoder with PackBits compression.
* [ ] Implement the TCP dispatcher with timeouts and per-printer job serialization.
* [ ] Implement the printer REST API (`/api/v1/printers`, managed with `settings:manage`) and `POST /api/v1/printers/{id}/print` (`labels:print`).
* [ ] Test against an in-process fake printer that parses the command stream and rebuilds each label's bitmap for comparison with the 1-bit render, plus a golden byte stream per media.
* [ ] Check printing on a real QL-810W (manual; outside reaper).

### Phase 4: Frontend Application

* [ ] Initialize TypeScript + Vite SPA inside `frontend/`.
* [ ] Embed compiled assets into the Go binary using Go 1.16+ `embed.FS`.
* [ ] Build category navigation views with accessible color coding.
* [ ] Implement asset management views with dynamic input forms based on inherited fields and generator ID validation.
* [ ] Implement visual template editor (canvas-based node positioning and binding configuration) with print preview and direct network print triggers.

### Phase 5: Batch Operations & Webhooks

* [ ] Implement CSV parser with strict category pre-flight checks and column mapper.
* [ ] Implement Discord webhook dispatcher for consumables marked running low.
* [ ] Write integration test suites covering inheritance conflict resolution, anti-lockout logic, and template vector compilation.

---

## 8. Ephemeral Test Harness (`reaper`) & Testing Methodology

BAM is onboarded as a tenant of [`reaper`](https://forge.axonibyte.com/axonibyte/reaper) via [`.reaper.toml`](.reaper.toml) for fast, isolated pre-push validation on remote ephemeral Proxmox VE virtual machines.

### 8.1 The Pre-Push Loop
```bash
reaper up        # Provisions remote VM, lays out ZFS pools, fetches pinned toolchains
reaper test      # Syncs uncommitted working tree -> builds -> ZFS state rollback -> runs test suite
reaper down      # Tears down remote VM (cull daemon provides automatic TTL backstop)
```

### 8.2 Testing Portfolio
Following the "Portfolio of Oracles" methodology:
1. **Tier 1 — Pure Unit & Golden Vectors:** Vector math, Micro QR bitpacking, bcrypt password hashing, boundary decoders, and cross-language barcode golden files (module matrices from Python's `segno`, scan checks with `zxing-cpp`) alongside byte-exact SVG, PNG, PDF and printer-stream goldens.
2. **Tier 4 — Server Contract Tests:** RBAC permission union resolution and anti-lockout invariants tested as in-process pure functions without database dependencies.
3. **Tier 6 — Full Stack & Migrations:** SQLite, PostgreSQL, and MariaDB drivers initialized against hostile environments (e.g. non-UTF8 charsets), verifying migration idempotence. The store and API suites run against PostgreSQL and MariaDB by pointing them at a server (see §8.3).
4. **Tier 7 — Seeded Fuzzing:** Deterministic input fuzzing asserting no 500 crashes, valid JSON envelopes, and daemon liveness.
5. **Tier 8 — Concurrency Harnesses:** Simultaneous write simulations for inventory threshold decrements and per-generator ID allocations, asserting stored state consistency.
6. **State Rollback:** Ephemeral databases and uploads live under `$REAPER_STATE` (`tank/state`), enabling near-instantaneous ZFS rollbacks to `@pristine` between test iterations.

### 8.3 Running the Suites Against PostgreSQL and MariaDB

By default every test uses SQLite. Setting `BAM_TEST_DIALECT` and `BAM_TEST_DSN` reruns the store and API suites against a live server. Each test creates and drops its own schema (PostgreSQL) or database (MariaDB), so the DSN must belong to a role allowed to do that:

```bash
BAM_TEST_DIALECT=postgres BAM_TEST_DSN='postgres://postgres:secret@localhost:5432/postgres?sslmode=disable' go test ./internal/store/ ./internal/api/

BAM_TEST_DIALECT=mariadb BAM_TEST_DSN='root:secret@tcp(localhost:3306)/' go test ./internal/store/ ./internal/api/
```

Portability rules for schema and queries:
* Write placeholders as `?`; `Store.Rebind` converts them to `$n` for PostgreSQL.
* Avoid reserved identifiers (`key` is reserved in MariaDB, which is why the corpus, permission and settings columns are `field_key`, `permission_key` and `setting_key`).
* Declare foreign keys at table level.
* Use `TIMESTAMP` in migrations; the runner rewrites it to `DATETIME(6)` on MariaDB, where `TIMESTAMP` columns silently gain `ON UPDATE CURRENT_TIMESTAMP`.
* The MariaDB driver is always opened with `parseTime=true` and `loc=UTC`.

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
- **Backend:** Go 1.22+ (Standard library `net/http` or router such as `chi`).
- **Database Abstraction:** Dialect-agnostic SQL driver supporting **SQLite3** (via pure-Go `modernc.org/sqlite` with WAL mode for seamless cross-compilation without C toolchains), **PostgreSQL 14+**, and **MariaDB 10.6+**. Migrations managed via embedded SQL scripts (`golang-migrate` or custom embedded schema runner).
- **Frontend:** TypeScript + Svelte (or React/Vite) with a lightweight 2D canvas/SVG label layout engine and Tailwind CSS for accessible, high-contrast, colorful category UI components.
- **Label Generation & Printing:**
  - **Native Go Vector/Raster Engines:** SVG template compilation, high-DPI PNG rasterization (e.g., `fogleman/gg`, `srwiley/oksvg`), and PDF vector document creation (e.g., `jung-kurt/gofpdf`, `signintech/gopdf`).
  - **Direct Network Printing:** Direct raw TCP socket dispatch (Port 9100) supporting Brother ESC/P and raster command streams directly to network-connected thermal printers (such as Brother QL-810W) without desktop driver or print dialog dependencies.
- **Barcode & Matrix Engine:** Go-native QR Code and Micro QR generation interfaces with per-generator payload and character set constraint validation.

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
dsn = "data/bam.db?_journal=WAL&_busy_timeout=5000"

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
* Columns: `id (UUID)`, `key (snake_case)`, `label (string)`, `validation_type (string: text|number|date|url|select)`, `options (JSON nullable)`.
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
  * **Micro QR (`micro_qr`):** Constrained by physical matrix bounds and version capacity (M1–M4). Custom IDs are restricted to allowed character sets (e.g., uppercase alphanumeric) and strictly capped to the version's payload limits (e.g., 10–35 alphanumeric characters).
  * **Standard QR (`qr`):** Supports arbitrary UTF-8 payloads and dynamic URL templates (`{base_url}/a/{asset_uuid}` or custom slug).
  * **Code 128 (`code128`):** Restricted to standard 128 ASCII character set with linear density constraints.
* **Uniqueness Invariant:** The compound key `(generator_type, assigned_code)` must be unique across all active assets, guaranteeing that scanning any generated code resolves unambiguously to a single asset.




### 3.3 RBAC Security Matrix

* **Tables:** `users`, `roles`, `permissions`, `user_roles`, `role_permissions`.
* **Permissions:** Granular string keys (`assets:read`, `assets:write`, `assets:delete`, `templates:manage`, `categories:manage`, `settings:manage`, `users:manage`).
* **Union Principle:** When a user is assigned multiple roles, their effective permissions are the strict union of all granted rights across all assigned roles.
* **Anti-Lockout Rule:** System operations must reject any deletion, deactivation, or role removal that results in zero active users possessing the `users:manage` permission.

### 3.4 Sessions & Authentication

* **Local Credentials:** Email (case-insensitive, unique) + password hashed via Argon2id or bcrypt.
* **TOTP (RFC 6238):** Optional two-factor authentication flag per user.
* **Session Store:** Stateful sessions stored in the active database (`sessions` table) referenced via an `HttpOnly`, `SameSite=Lax`, `Secure` cookie.

---

## 4. Label Template Engine & Print Subsystem

### 4.1 Declarative Template Specification

Templates are stored as structured JSON/YAML definitions specifying physical canvas bounds and responsive placement nodes:

```json
{
  "id": "tpl_brother_ql_standard_29x90",
  "name": "Standard Die-Cut 29mm x 90mm",
  "units": "mm",
  "width": 90.0,
  "height": 29.0,
  "dpi": 300,
  "elements": [
    {
      "type": "generator",
      "generator_type": "qr",
      "x": 3.0,
      "y": 3.0,
      "width": 23.0,
      "height": 23.0,
      "content_binding": "url_prefix:https://bam.local/a/{{asset.id}}"
    },
    {
      "type": "text",
      "x": 28.0,
      "y": 5.0,
      "width": 58.0,
      "height": 6.0,
      "font_size": 14,
      "font_weight": "bold",
      "content_binding": "field:name"
    },
    {
      "type": "text",
      "x": 28.0,
      "y": 12.0,
      "width": 58.0,
      "height": 4.0,
      "font_size": 9,
      "content_binding": "asset:id"
    },
    {
      "type": "static_image",
      "x": 75.0,
      "y": 18.0,
      "width": 10.0,
      "height": 8.0,
      "asset_path": "/static/branding/logo.svg"
    }
  ]
}

```

### 4.2 Generator Interfaces

```go
type Generator interface {
    Type() string
    Validate(code string) error
    GenerateVector(code string, width, height float64) ([]byte, error)
    GenerateRaster(code string, dpi int) ([]byte, error)
}

```

* **Micro QR Code:** Enforces tight payload budgets (alphanumeric/byte limits based on version M1–M4).
* **Standard QR Code:** Dynamic URL prefixes (`{base_url}/a/{asset_uuid}`) with selectable error correction levels (L, M, Q, H).

### 4.3 Output Formats & Direct Network Dispatch

1. **SVG:** Base interchange format used by the frontend canvas preview editor.
2. **PNG:** High-resolution 300+ DPI rasterization for Brother QL raster drivers / CUPS thermal queues.
3. **PDF:** Vector-accurate standard page layouts for printing batch sheets or continuous rolls.
4. **Direct Network Printing (Raw Socket / Port 9100):** BAM communicates directly over the local network with IP-enabled thermal label printers (e.g., Brother QL-810W) via raw TCP sockets on port 9100. The backend compiles the template into native printer raster/ESC-P commands and streams the payload directly to the device, enabling one-click hardware label dispensing without client-side print dialogs or local printer drivers.


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

* **Export Mode:** Endpoint accepts an array of asset IDs and a target template ID, streaming back a multi-page PDF or a stitched raster archive matching the template's dimensions and roll constraints.
* **Network Dispatch Mode:** Endpoint allows streaming generated batch print jobs directly to a configured IP thermal printer over raw TCP (Port 9100) for rapid hardware dispensing.

---

## 7. Implementation Roadmap for Agents

When implementing this repository from scratch, complete the following milestones in sequential order:

```
[Phase 1: Foundation] ──► [Phase 2: Core Domain] ──► [Phase 3: Template & Print] ──► [Phase 4: Frontend UI] ──► [Phase 5: Automation]

```

### Phase 1: Foundation & Infrastructure

* [ ] Establish Go module structure (`cmd/bam`, `internal/api`, `internal/models`, `internal/store`, `internal/config`).
* [ ] Implement TOML configuration loader using environment variable fallback.
* [ ] Set up database abstraction layer with support for pure-Go SQLite (`modernc.org/sqlite` for CGO-free cross-compilation), PostgreSQL, and MariaDB.
* [ ] Implement CLI commands (`init`, `migrate`, `admin create`, `serve`).
* [ ] Implement database-backed sessions and RBAC middleware with anti-lockout checks.

### Phase 2: Core Domain & Schema

* [ ] Implement global field corpus CRUD with JSON schema validation for `select` options.
* [ ] Implement category hierarchy (tree traversal, cycle detection, field inheritance calculator).
* [ ] Implement asset registry (UUID generation, multi-category assignment, per-generator custom ID validation, fallback to Uncategorized).
* [ ] Implement dynamic field value persistence.

### Phase 3: Generator & Label Rendering Engine

* [ ] Implement barcode generator interface with QR and Micro QR engines (enforcing per-generator character set and payload limits).
* [ ] Build the declarative template parser (JSON/YAML schema validation).
* [ ] Implement SVG/PNG/PDF rendering pipelines matching physical dimensions (mm/in).
* [ ] Implement single and batch label export endpoints (downloadable streams).
* [ ] Implement direct network printer dispatch client (Raw TCP socket / Port 9100 for Brother QL raster / ESC-P streams).

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
1. **Tier 1 — Pure Unit & Golden Vectors:** Vector math, Micro QR bitpacking, Argon2id auth, boundary decoders, and deterministic cross-language barcode golden files.
2. **Tier 4 — Server Contract Tests:** RBAC permission union resolution and anti-lockout invariants tested as in-process pure functions without database dependencies.
3. **Tier 6 — Full Stack & Migrations:** SQLite, PostgreSQL, and MariaDB drivers initialized against hostile environments (e.g. non-UTF8 charsets), verifying migration idempotence.
4. **Tier 7 — Seeded Fuzzing:** Deterministic input fuzzing asserting no 500 crashes, valid JSON envelopes, and daemon liveness.
5. **Tier 8 — Concurrency Harnesses:** Simultaneous write simulations for inventory threshold decrements and per-generator ID allocations, asserting stored state consistency.
6. **State Rollback:** Ephemeral databases and uploads live under `$REAPER_STATE` (`tank/state`), enabling near-instantaneous ZFS rollbacks to `@pristine` between test iterations.



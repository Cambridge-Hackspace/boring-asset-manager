# BAM! (Boring Asset Manager)

![BAM!](img/bam.png)

> Excruciatingly simple physical asset label generation and tracker.

BAM! is a single-tenant, API-first web application designed for tracking physical assets, managing hierarchical categories with dynamic field inheritance, and visually designing and batch-printing labels for dedicated thermal label printers (such as the Brother QL-810W) as well as standard sheet layouts.

---

## 1. System Architecture & Tech Stack

### Core Philosophy
- **Self-Contained Deployment:** Distributed as a single compiled binary for FreeBSD and Linux (cross-compilable to Windows).
- **Embedded Frontend:** The Go backend serves the compiled Single Page Application (SPA) directly via `embed.FS`.
- **Strict Configuration Separation:** On-disk TOML handles bootstrap infrastructure (listening ports, database connection strings, master secrets). All operational settings, webhooks, and layouts live inside the database.

### Technology Choices
- **Backend:** Go 1.22+ (Standard library `net/http` or router such as `chi`).
- **Database Abstraction:** Dialect-agnostic SQL driver supporting **SQLite3** (with WAL mode), **PostgreSQL 14+**, and **MariaDB 10.6+**. Migrations managed via embedded SQL scripts (`golang-migrate` or custom embedded schema runner).
- **Frontend:** TypeScript + Svelte (or React/Vite) with a lightweight 2D canvas/SVG label layout engine and Tailwind CSS for accessible, high-contrast, colorful category UI components.
- **Label Generation:** Native Go vector rendering:
  - **SVG:** Native vector template compilation.
  - **PNG:** High-DPI rasterization via Go vector rasterizers (e.g., `fogleman/gg` or `srwiley/oksvg`).
  - **PDF:** Vector document creation (e.g., `jung-kurt/gofpdf` or `signintech/gopdf`).
- **Barcode & Matrix Engine:** Go-native QR Code and Micro QR generation interfaces.

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
* Allows mapping compact, deterministic, or constrained strings to an asset for constrained barcodes:
* `asset_id (UUID)`
* `generator_type (string: "qr" | "micro_qr" | "code128")`
* `assigned_code (string, unique per generator_type)`


* Generator engines enforce character set and min/max length constraints during code generation (e.g., Micro QR code size limits).



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
      "content_binding": "url_prefix:[https://bam.local/a/](https://bam.local/a/){{asset.id}}"
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

### 4.3 Output Formats

1. **SVG:** Base interchange format used by the frontend canvas preview editor.
2. **PNG:** High-resolution 300+ DPI rasterization for Brother QL raster drivers / CUPS thermal queues.
3. **PDF:** Vector-accurate standard page layouts for printing batch sheets or continuous rolls.

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



### 6.2 Bulk Label Printing

* Endpoint accepts an array of asset IDs and a target template ID.
* Streams back a multi-page PDF or a stitched raster archive matching the template's dimensions and roll constraints.

---

## 7. Implementation Roadmap for Agents

When implementing this repository from scratch, complete the following milestones in sequential order:

```
[Phase 1: Foundation] ──► [Phase 2: Core Domain] ──► [Phase 3: Template & Print] ──► [Phase 4: Frontend UI] ──► [Phase 5: Automation]

```

### Phase 1: Foundation & Infrastructure

* [ ] Establish Go module structure (`cmd/bam`, `internal/api`, `internal/models`, `internal/store`, `internal/config`).
* [ ] Implement TOML configuration loader using environment variable fallback.
* [ ] Set up database abstraction layer with support for SQLite, PostgreSQL, and MariaDB.
* [ ] Implement CLI commands (`init`, `migrate`, `admin create`, `serve`).
* [ ] Implement database-backed sessions and RBAC middleware with anti-lockout checks.

### Phase 2: Core Domain & Schema

* [ ] Implement global field corpus CRUD.
* [ ] Implement category hierarchy (tree traversal, cycle detection, field inheritance calculator).
* [ ] Implement asset registry (UUID generation, multi-category assignment, fallback to Uncategorized).
* [ ] Implement dynamic field value persistence.

### Phase 3: Generator & Label Rendering Engine

* [ ] Implement barcode generator interface with QR and Micro QR engines.
* [ ] Build the declarative template parser (JSON/YAML schema validation).
* [ ] Implement SVG/PNG/PDF rendering pipelines matching physical dimensions (mm/in).
* [ ] Implement single and batch label export endpoints.

### Phase 4: Frontend Application

* [ ] Initialize TypeScript + Vite SPA inside `frontend/`.
* [ ] Embed compiled assets into the Go binary using Go 1.16+ `embed.FS`.
* [ ] Build category navigation views with accessible color coding.
* [ ] Implement asset management views with dynamic input forms based on inherited fields.
* [ ] Implement visual template editor (canvas-based node positioning and binding configuration).

### Phase 5: Batch Operations & Webhooks

* [ ] Implement CSV parser with strict category pre-flight checks and column mapper.
* [ ] Implement Discord webhook dispatcher for consumables marked running low.
* [ ] Write integration test suites covering inheritance conflict resolution, anti-lockout logic, and template vector compilation.

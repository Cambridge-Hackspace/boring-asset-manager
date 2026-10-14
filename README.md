# BAM! (Boring Asset Manager)

![BAM!](img/bam.png)

> Excruciatingly simple physical asset label generation and tracker.

BAM! is a single-tenant, API-first web application designed for tracking physical assets, managing hierarchical categories with dynamic field inheritance, and visually designing and batch-printing labels for dedicated thermal label printers (such as the Brother QL-810W, DYMO LabelWriter and Rollo) as well as standard sheet layouts.

---

## 1. System Architecture & Tech Stack

### Core Philosophy
- **Self-Contained Deployment:** Distributed as a single compiled binary for FreeBSD, Linux, and Windows with effortless `CGO_ENABLED=0` cross-compilation.
- **Embedded Frontend:** The Go backend serves the compiled Single Page Application (SPA) directly via `embed.FS`.
- **Strict Configuration Separation:** On-disk TOML handles bootstrap infrastructure (listening ports, database connection strings, storage paths, the printer device allowlist). All operational settings, network printer definitions, webhooks, and layouts live inside the database.

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
* **Sessions:** the app uses the existing `bam_session` cookie (`HttpOnly`, `SameSite=Lax`). On load it asks `GET /api/v1/auth/me` for the user and their permissions. Any `401` returns to the sign-in page and then back to where the user was.
* **Permissions:** actions the user lacks are hidden or disabled, using the permission list from `/auth/me`. The server still checks every request.
* **Errors:** validation errors from the API are shown beside the field or template element they name (template errors carry a `path`).
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
* **Anti-Lockout Rule:** System operations must reject any deletion, deactivation, or role removal that results in zero active users possessing the `users:manage` permission. Each such change runs in one transaction that first takes the `users_manage` row of the `app_locks` table (migration `004`), so concurrent changes are checked one after another and two admins cannot remove each other at the same time.

### 3.4 Sessions & Authentication

* **Local Credentials:** Email (case-insensitive, unique) + password hashed via bcrypt (cost 12).
* **TOTP (RFC 6238):** Optional two-factor authentication flag per user.
* **Passwords:** stored as bcrypt hashes (cost 12). Every new password must be 8-72 bytes: 72 is the most bcrypt uses, so longer ones are refused rather than silently cut short.
* **Session Store:** Stateful sessions stored in the active database (`sessions` table) referenced via an `HttpOnly`, `SameSite=Lax`, `Secure` cookie. The cookie holds a random 256-bit token; the table stores only its SHA-256 hash (migration `005`), so a copy of the database cannot be used to sign in.

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
* **Canvas:** `units` is `mm` or `in`; `width` and `height` are positive; `dpi` is 150–600 (Brother QL and DYMO LabelWriter printers print at 300, Rollo and most TSPL printers at 203). Positions and sizes use the template's units with the origin at the top-left corner, and every element must lie inside the canvas. `font_size` is always in points.
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
  * `text`: `font_size` (pt), `font_weight` (`normal`/`bold`), `font_family` (`sans`/`mono`), `align` (`left`/`center`/`right`), `wrap` (default `false`) and `min_font_size` (default 6). Lines break at newlines (and between words when wrapping). Text that overflows its box shrinks in 0.5 pt steps down to `min_font_size`, then is cut short with an ellipsis. The block is centered vertically in its box. The embedded Go fonts cover Latin, Greek and Cyrillic; other scripts render as empty boxes.
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
2. **PNG:** rendered at the template's DPI, as 8-bit grayscale for previews or 1-bit black and white for printing, where text is thresholded at 50% coverage and images are Floyd–Steinberg dithered. Barcode modules are never anti-aliased.
3. **PDF:** one page per label, sized exactly to the label, with vector barcodes and text and images embedded as Flate-compressed bitmaps. Output is byte-for-byte deterministic: no timestamps or random document IDs.

Batch exports produce a multi-page PDF or a ZIP of PNGs named by asset ID (§6.2).

### 4.4 Printers & Direct Network Dispatch

Any number of printers can be configured through `/api/v1/printers` (`settings:manage`; anyone with `labels:print` can list them). Each is stored in the `printers` table with a name, driver, model, connection, the media currently loaded, and driver `options` as a JSON object.

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

The print endpoint only renders labels, asks the printer's driver to plan and encode them, and hands the job to a `Transport` for the printer's connection (see Delivery below). Adding a printer family means registering a driver; the API does not change. `GET /api/v1/printers/drivers` lists every registered driver with its models, media and options.

Three drivers ship today: `brother_ql`, `dymo_labelwriter` and `tspl`. Option values are listed by the drivers endpoint, and unknown options are refused.

#### `brother_ql`

For the QL-810W and QL-820NWB. Its only option is `cut` (default `true`): cut each label off as it finishes, or let the tape feed on uncut.

* **Media catalog:** each supported roll records its width, whether it is die-cut (and if so its label length), its printable width in head dots and how many pins sit beside the tape. Phase 3 covers DK-22210 (29 mm continuous), DK-22205 (62 mm continuous) and DK-11201 (29 × 90 mm die-cut). Two-color black/red media (DK-22251) is out of scope.
* **Fit check:** before any asset is laid out, the template must suit the loaded media, or the request fails with `422`. Its DPI must be 300 and one side must be the tape width (±0.5 mm). A template whose height is the tape width is rotated 90° counterclockwise. On die-cut media the other side must be the label length; continuous labels must be 25–1000 mm long.
* **Printable area:** the printer cannot reach the tape edges, so the rendered label is cropped to the printable area around its center: 306 of the 343 dots across 29 mm tape (about 1.5 mm lost on each side), 696 across 62 mm, and on DK-11201 991 of 1063 dots along the label (about 3 mm at each end). Keep elements clear of those margins.
* **Raster stream:** each label is rendered 1-bit at 300 DPI and encoded as described in Brother's QL-800 series raster command reference: invalidate, initialize, then per label raster mode, print information, auto-cut, margins and TIFF compression, PackBits-compressed raster lines (90 bytes for each line of the 720-pin head, mirrored because the first pin sits at the right of the tape) and a print command. With `cut` on, every label is cut off as it finishes.

#### `dymo_labelwriter`

For DYMO LabelWriter printers, which speak one of two raster languages. The 400, 400 Turbo, Twin Turbo, 450, 450 Turbo, 450 Twin Turbo, 450 Duo (label side) and 4XL speak the first. The 550, 550 Turbo, 5XL and LabelWriter Wireless speak the second. The first language is implemented from DYMO's *LabelWriter 450 Series Technical Reference Manual*, and its tests are worked out from that manual. DYMO has published no reference for the second; its command format was learned by reading DYMO's open-source (GPL) Linux drivers. No code or test data is taken from those drivers, so BAM stays MIT-licensed.

Options: `density` (`light`, `medium`, `normal` or `dark`; default `normal`) and, on twin-roll models only, `roll` (`auto`, `left` or `right`; default `auto`).

* **Media catalog:** the common die-cut rolls (30252/99010 address 28 × 89 mm, 30321/99012 large address 36 × 89 mm, 30256 shipping 59 × 102 mm, 30323/99014 shipping 54 × 101 mm, 30334/11354 57 × 32 mm, 30336/11352 25 × 54 mm, 30332/11353 25 × 25 mm, 30347 25 × 38 mm, 30330/11355 19 × 51 mm, 30345 19 × 64 mm), 30270 continuous 57 mm, and 1744907 4 × 6 in shipping labels, which need the 4XL or 5XL. Label sizes in dots follow from the nominal sizes at 300 DPI.
* **Fit check:** as for `brother_ql`: 300 DPI, one side the label width (±0.5 mm), the other the label length on die-cut rolls or 10–2700 mm on continuous; a template whose height is the label width is rotated 90° counterclockwise.
* **Printable area:** labels sit against the roll guide at the head's first dot. The render is centered on the label. On first-generation models it is then cut to the most the head takes in one line, 56 mm (672 dots) or 104 mm (1248 dots), so the far edge of a 59 mm shipping label does not print. Second-generation models receive the whole label.
* **Raster stream, first generation:** one ESC more than the longest raster line (85, or 157 on the 4XL) to resynchronize, text mode, density, the roll on twin models, and the label length (a little over the label on die-cut rolls, `0xFFFF` for continuous). Then each line is trimmed of blank bytes at both ends and sent run-length compressed when that is shorter, with dot tab and bytes per line sent only when they change and runs of blank lines as skips. Labels are separated by short form feeds, and a form feed after the last brings it to the tear bar. Blank lines at the end of a die-cut label are left to the form feed.
* **Raster stream, second generation:** a job header (random job ID, density, text quality, default media type). Then per label: the label length (0 for die-cut, `0xFFFF` for continuous), a label index, a print data header and uncompressed lines. After a final form feed, an end-of-job command closes the job. The 550 series only prints on genuine DYMO labels it can identify, whatever the driver sends.
* **Connection:** the LabelWriter 400 and 450 series and the 4XL have only a USB port, so they use a `device`, `ipp`, `windows` or print-server `tcp` connection (see Delivery). The 550 Turbo, 5XL and Wireless have network ports.

#### `tspl`

For printers that speak TSPL, the label language of TSC printers that many inexpensive 4-inch direct thermal printers also use. Models: Rollo X1038 and X1040, Munbyn ITPP941, and generic 203 DPI and 300 DPI TSPL printers. The driver follows TSC's *TSPL/TSPL2 Programming Manual*.

Options: `density` (0–15; unset keeps the printer's own setting) and `rotate_180` (default `false`) to print labels the other way up.

* **Media catalog:** these printers take any label size up to their head width, so the catalog lists common sizes rather than part numbers: 4 × 6, 4 × 4, 4 × 3, 4 × 2, 3 × 2, 3 × 1, 2.25 × 1.25, 2 × 1, 1.5 × 1 and 1 × 1 in die-cut labels with a 1/8 in gap, and 4 in and 2 in continuous.
* **Fit check:** the template's DPI must match the model (203 for the Rollo), one side must be the label width (±0.5 mm) and, on die-cut rolls, the other the label length; continuous labels must be 10–1000 mm. A template whose height is the label width is rotated 90° counterclockwise.
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

* **Export Mode:** `POST /api/v1/labels/export` takes a template ID, up to 500 distinct asset IDs and a format (`pdf` or `zip`, with `"mono": true` for 1-bit PNGs), and streams back a multi-page PDF or a ZIP of PNGs named `<asset_id>.png`.
* **Network Dispatch Mode:** `POST /api/v1/printers/{id}/print` (`labels:print`) takes a template ID, asset IDs and a copy count (1–20, at most 500 labels in all), checks the template against the printer's media (§4.4), and streams the job to the printer over TCP port 9100.
* **All or nothing:** every asset is bound and validated before any output is produced, so a batch either renders completely or fails with `422` and a list of `{asset_id, path, message}` for the assets that could not be rendered. Archived assets are never labeled.

---

## 7. Implementation Roadmap for Agents

When implementing this repository from scratch, complete the following milestones in sequential order:

```
[Phase 1: Foundation] ──► [Phase 2: Core Domain] ──► [Phase 3: Template & Print] ──► [Phase 4: Frontend UI] ──► [Phase 5: Automation]

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
* [x] Implement template store operations and the REST API (`/api/v1/templates`): reads need `assets:read`, writes need `templates:manage`. Validation failures return `400` with a `details` list of `{path, message}`; a `PUT` cannot change a template's ID.
* [x] Add migration `002` for the `images` table and the image API (`/api/v1/images`): PNG or JPEG up to 2 MiB and 4096 × 4096 pixels, uploads need `templates:manage`, and deleting an image a template still uses is refused with `409`.

#### Sub-Phase 3.3: Layout & Rendering
* [x] Implement unit conversion, dot-grid layout, module snapping and quiet zones (`internal/render`).
* [x] Implement text layout with the embedded Go fonts (Regular, Bold, Mono, Mono Bold): measuring, wrapping, shrink-to-fit and ellipsis.
* [x] Implement the SVG, PNG (grayscale and 1-bit) and PDF backends.
* [x] Check in golden outputs for each backend, regenerated with `go test ./internal/render -update`; `tools/goldens/verify_renders.py` decodes the rendered barcodes with `zxing-cpp` to confirm they scan.

#### Sub-Phase 3.4: Export Endpoints
* [x] Implement `GET /api/v1/assets/{id}/label?template_id=…&format=svg|png|pdf` (`assets:read`; add `mono=true` for a 1-bit PNG). An asset the template cannot be filled for returns `422` naming the element.
* [x] Implement `POST /api/v1/templates/preview` (`templates:manage`), rendering an unsaved template (its `id` may be omitted) as SVG against `asset_id` or placeholder data, for the Phase 4 editor.
* [x] Implement `POST /api/v1/labels/export` (`assets:read`, §6.2).
* [x] Cap request body sizes on every endpoint: 1 MiB, refused with `413` when declared up front; image uploads keep their own limit.

#### Sub-Phase 3.5: Brother QL Network Printing
* [x] Add migration `003` for the `printers` table, and the `labels:print` permission.
* [x] Implement the media catalog, fit check, printable-area crop and automatic rotation (`internal/printers/brotherql`).
* [x] Implement the raster command encoder with PackBits compression.
* [x] Implement the TCP dispatcher with timeouts and per-printer job serialization (`internal/printers`).
* [x] Implement the printer REST API (`/api/v1/printers`, managed with `settings:manage`) and `POST /api/v1/printers/{id}/print` (`labels:print`).
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
* [ ] Field corpus views: list, create, edit and delete, by type with select options.
* [ ] Category tree with color swatches, consumable and running-low flags, and create, edit, move and delete.
* [ ] Category field assignment, showing each effective field's source category and inherited constraints.

#### Sub-Phase 4.4: Assets & Labels
* [ ] Asset list with search, category filter and multi-select.
* [ ] Asset detail.
* [ ] Asset form generated from the effective fields of the chosen categories, re-resolved as they change, with generator keys and server errors shown beside the fields they name.
* [ ] Archive an asset.
* [ ] Label preview, PNG/PDF download and printing (printer, copies) for one asset; batch export and printing for a selection.

#### Sub-Phase 4.5: Template Editor
* [ ] Template list and editor shell: canvas settings, printer-derived size and DPI, and the fit check.
* [ ] Server preview canvas with throttled requests, plus the interactive element layer: select, drag, resize, grid snapping, keyboard nudging, multi-select and z-order.
* [ ] Property panels per element type, binding suggestions, image picking and upload.
* [ ] Undo/redo, unsaved-change protection, and saving with server errors mapped to elements.
* [ ] Preview against an asset, export, and test print from the editor.

#### Sub-Phase 4.6: Administration
* [ ] Printers: forms generated from `GET /api/v1/printers/drivers` (models, media, options and their values) and the connection types, with a test print.
* [ ] Users and roles: list, create, activate and deactivate, archive and role assignment, with anti-lockout refusals explained.
* [ ] Image library: upload, browse, see which templates use an image, delete.
* [ ] Own password change.

#### Sub-Phase 4.7: End-to-End & Accessibility Battery
* [ ] Playwright tests in a pinned container against a real `bam serve` on a fresh SQLite database, covering sign-in, fields, categories, assets, the editor (including dragging and keyboard editing), exports, and printing to an in-process fake TCP printer whose received job is decoded and checked.
* [ ] Automated accessibility checks (`axe-core`) on every view, plus keyboard-only walkthroughs of the main flows.

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

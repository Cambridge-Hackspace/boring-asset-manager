# Architecture

BAM! is a single compiled program that holds the whole application: the API, the web interface,
the label renderer and the print drivers. This page describes how it is built and why, the web
interface that sits on top of the API, how that interface looks and stays accessible, and the
help system it carries.

## Core philosophy

BAM! is one compiled binary for FreeBSD, Linux and Windows. It is cross-compiled with
`CGO_ENABLED=0`, and it serves its own web interface from files embedded with `embed.FS`.

A TOML file on disk holds only what is needed to start:

- the listening address
- the database connection
- where files are kept
- which printer devices may be used

Everything else lives in the database, from settings and printers to notification channels and
label layouts. See [Configuration](configuration.md) for the file itself.

## Technology choices

| Area | Choice |
|---|---|
| Backend | Go 1.26+, serving with the standard library's `net/http` and its `ServeMux`. |
| Database | A dialect-agnostic SQL driver supporting **SQLite3**, **PostgreSQL 14+** and **MariaDB 10.6+**. SQLite runs through the pure-Go `modernc.org/sqlite` in WAL mode, so BAM! cross-compiles without a C toolchain. Migrations are BAM!'s own: SQL scripts embedded in the binary, and Go code where a change depends on the dialect or the data (`internal/store/migrations`). |
| Frontend | TypeScript and Svelte 5, built with Vite, styled with Tailwind CSS, and embedded in the binary (see [Frontend](#frontend)). |
| Label rendering | Pure Go. One layout engine feeds SVG, PNG and PDF backends (see [Labels](labels.md)). |
| Printing | Pluggable print drivers produce each printer's own command stream. |
| Barcodes and matrix codes | In-house encoders. |
| Icons | The isolex compiler (see [Icons](icons.md)). |

### Label rendering

Fonts, glyph outlines and rasterization come from `golang.org/x/image`: the embedded Go fonts,
OpenType parsing and its vector rasterizer. The PDF writer is a small in-house one, so output
stays deterministic.

The template editor draws labels with the server's own renderer, compiled to WebAssembly. There is
one layout engine, not two (see [Template editor](template-editor.md)).

### Direct printing

Each printer gets its own command stream (Brother QL, DYMO LabelWriter, TSPL, ZPL, EPL2) from a
pluggable print driver. The stream is sent in one of these ways, with no desktop driver or print
dialog:

- over raw TCP (port 9100)
- to a USB device on the server
- to a raw IPP/CUPS queue
- through the Windows spooler

### Barcode and matrix engine

QR Code, Micro QR and Code 128 encoders are written in-house. Each generator validates its
payload and character set (see [Labels](labels.md)).

### Icons

Icons are compiled by isolex, run in-process from WebAssembly. How it is pinned, contained and
drawn is described in [Icons](icons.md).

### SAML

SAML sign-in (docs/access.md#saml-20) checks XML signatures with
[goxmldsig](https://github.com/russellhaering/goxmldsig), not code of BAM!'s own, and not a whole
SAML library. The choice weighed:

- **XML signatures are where SAML's serious bugs have been:** canonicalization, namespaces, and
  signature wrapping, where a signature that checks is moved around something else. goxmldsig is
  maintained, is fuzzed, is what most Go SAML code uses, and had its own such bugs found and fixed
  in public. Writing BAM!'s own would start that history again.
- **The rest of SAML is narrow,** and BAM! writes it itself (`internal/idp/saml.go`): the request,
  the metadata, the conditions, and opening encrypted assertions. A whole library, such as
  `crewjam/saml`, brings its own sessions, middleware and routes, which BAM!'s provider framework
  already has, and more code to keep up with.
- **goxmldsig answers the element it checked,** and BAM! reads only from that. Other defenses
  against wrapping come from BAM!'s side: one assertion per response, no DOCTYPE, and the IDs and
  conditions read from the signed element alone.

The tests sign their answers with goxmldsig too, through a test identity provider
(`internal/idp/testsaml.go`), and try wrapped signatures, other keys and replays against it.

### Relays

A relay is the same binary, run beside printers and devices on a local network, linked to the main
node by the relay bus (see [Relays](relays.md)). The bus's design follows from a few choices:

- **One protocol, two transports.** BoneMesh and WebSocket carry bytes between peers. Messages,
  addressing, acknowledgments, retries, parts and delivering once are the bus's, in
  `internal/relay`, so delivery behaves the same over either, and over both.
- **The protocol owns delivery.** BoneMesh has acknowledgments, retries and splitting of its own, but
  WebSocket has none, and the bus must fail over between them. So the bus acknowledges end to end
  and splits large messages itself, into parts that fit a BoneMesh segment.
- **Signed over BoneMesh.** BoneMesh trusts every member, and does not tell a node which peer sent a
  message. A relay on a desk is a member that may be lost or tampered with, so each message over
  BoneMesh is signed with its sender's identity key and checked against pairing's record of it.
  Over WebSocket the credential and TLS already say who is at the other end.
- **BoneMesh on a port of its own.** Its sessions are not HTTP, and the Go node takes connections
  only from a listener of its own, so it cannot share the HTTPS port. WebSocket covers networks
  where only 443 is open.
- **Several servers through the database.** Servers that share a database need not reach each
  other. Each claims the relays it holds there, and leaves messages there for the others, as live
  connections already work from the database.

## Frontend

The web interface is a Svelte 5 single-page application written in TypeScript, built with Vite
and styled with Tailwind CSS. It talks only to the public `/api/v1` API. Anything it can do, a
script can do too, and the server remains the authority on every rule: validation, permissions,
inheritance and layout.

### Source and dependencies

The app lives in `web/`. `package.json` pins exact versions, and `package-lock.json` is
committed.

Dependencies stay few: Svelte, Vite, TypeScript, Tailwind, and the test tools. There is no
SvelteKit. Routing is a small in-house history router.

### Building

`tools/web/build.sh` builds the app in two steps:

1. It builds the editor's live renderer (`cmd/bamlive`) to WebAssembly with TinyGo, in a pinned
   container, into `web/src/lib/live/generated/`.
2. It runs `npm ci` and `vite build` in a pinned Node container and writes the output to
   `internal/webui/static/app/`, with a Brotli and a gzip copy of each asset worth compressing.

Both outputs are ignored by git. Release builds run the script before `go build`.

`tools/web/build.sh check` builds the live renderer, then type-checks the app and runs its unit
tests instead of building it.

### Developing

Run `tools/web/build.sh check` once, for the live renderer. Then run `npm run dev` in `web/` next
to `bam serve`. Vite forwards `/api` to port 8080.

### Embedding

Package `internal/webui` embeds `static/` with `embed.FS`. If the app has not been built, the
binary serves a committed placeholder page saying so. A plain `go build`, the Go test suites and
reaper therefore never need Node.

### Serving

`/api/*`, `/healthz` and `/readyz` are served by the API. Every other `GET` serves a file from the
build or, for client-side routes, `index.html`.

- Hashed assets are cached as immutable.
- Hashed assets are sent Brotli- or gzip-compressed to browsers that accept it, from the copies
  the build wrote.
- `index.html` is never cached.

Every page carries a strict Content Security Policy:

- `default-src 'self'`
- `data:` and `blob:` images, for previews
- inline style attributes, for the editor's positioning
- `'wasm-unsafe-eval'`, so the app may compile its own WebAssembly (it never evaluates script)
- `frame-ancestors 'none'`

Every page also carries `X-Content-Type-Options: nosniff` and `Referrer-Policy: same-origin`.

### Signing in and permissions

The app signs in with the `bam_session` cookie (`HttpOnly`, `SameSite=Lax`). On load it asks
`GET /api/v1/auth/me` for the user, their permissions, and the names of their roles (`roles`).
**Your account** lists those roles, with what each permission allows, in words. Any `401` returns
to the sign-in page, and then back to where the user was.

Actions the user lacks are hidden or disabled, using the permission list from `/auth/me`. The
server still checks every request.

Validation errors from the API are shown beside the field or template element they name.
Template errors carry a `path` for this.

## Look

The look is paper and ink. Red is kept for the one primary action on a screen, and yellow is only
ever behind ink.

`web/src/app.css` holds the colors, in light and dark, and the shared classes the views are built
from. Every view uses Tailwind's zinc for its neutrals; `app.css` redraws zinc in warm tones, at the
lightness Tailwind gives it.

Fonts are served from the app itself, from `web/src/fonts/`, beside their SIL Open Font License.
The Content Security Policy allows fonts only from the app.

| Use | Font |
|---|---|
| Headings | Space Grotesk |
| Text | IBM Plex Sans |
| Codes | IBM Plex Mono |

### Checks

- `src/theme.test.ts` checks that each color meets its contrast against what it sits on.
- `e2e/gate.spec.ts` checks every view at 1440 and 390 px, in both color schemes. Each view must
  keep its controls at least 44 px (32 px in the editors' dense controls), show at most one
  primary button, and never scroll sideways.

Every view passes, so the gate's list of known failures is empty.

## Phones and touch screens

Below 640 px, on a phone:

- The header is one row, with a **Menu** button opening every section, Help and the account.
- Tables of users, loans, assets and the spool become a card per row, each cell under its
  column's heading (`lib/stack.ts`).
- The asset list's filters fold behind **Filters**.
- Help's topics fold behind **All help topics**, so the article comes first.

On touch screens the template editor's handles are larger, and its keyboard help gives way to
help for touch. Small buttons beside text are at least 32 px tall.

The phone browser tests check that no page scrolls sideways at 390 px.

## Accessibility

The target is WCAG 2.1 AA.

- Everything works from the keyboard, including the template editor. The arrow keys nudge,
  Shift+arrow moves by a larger step, and Tab moves between elements.
- Focus is always visible, and every input has a label.
- Category colors are never the only signal; a name always accompanies them. Text on a category
  color is black or white, whichever contrasts more.
- The interface follows `prefers-color-scheme`, unless the person chooses Light or Dark in the
  footer. It also follows `prefers-reduced-motion`.

The Light or Dark choice is kept on the person's profile and in the browser
(`lib/theme.svelte.ts`). It is set as `data-theme` on `<html>`, which Tailwind's `dark:` variant
and the icon palette read.

The interface is written in American English throughout.

## Help

The web app carries its own user help, as Markdown topics in `internal/help/topics/`. The topics
are bundled with the app, and embedded in the server too, for assistants (see [MCP](mcp.md)).

### Reading help

- `/help` lists the topics, and `/help/{topic}` shows one. Both are open to everyone, signed in
  or not.
- Every page has one **Help** link, in the header, to the topic for that page.
- Forms carry no help marks of their own.

### Rendering

Topics are rendered by a small renderer of the app's own, `web/src/lib/markdown.ts`. It supports:

- headings
- paragraphs
- lists
- bold and italics
- code
- links to the app's pages or to https addresses

Everything else is escaped.

### Adding a topic

Add its file to `internal/help/topics/`. Then, in `web/src/lib/help.ts`, give it its place in the
list and name the pages it covers.

Tests check that:

- every page has a topic
- links between topics lead somewhere
- the help says rather than asks

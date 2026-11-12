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

Each printer gets its own command stream (Brother QL, TD and P-touch, DYMO LabelWriter and
LabelManager, Epson LabelWorks, TSPL, ZPL, EPL2, DPL, SBPL, EZPL, ESC/POS, Phomemo, Niimbot) from a
pluggable print driver. The stream is sent in one of these ways, with no desktop driver or print
dialog:

- over raw TCP (port 9100)
- to a USB device, or a Bluetooth serial one, on the server
- to a raw IPP/CUPS queue
- through the Windows spooler
- to a Bluetooth LE printer, through BlueZ on Linux

A Niimbot answers each command, so its job is sent in a conversation, each command waiting for the
answer, over Bluetooth LE or a serial device.

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

Dependencies stay few: Svelte, Vite, TypeScript, Tailwind, the test tools, and the
[code reader](#the-code-reader) for browsers without their own. There is no SvelteKit. Routing is
a small in-house history router.

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

`/api/*`, `/healthz` and `/readyz` are served by the API, as are `/manifest.webmanifest` and the
icons under `/app-icons/`, which are drawn from the installed app's settings. Every other `GET`
serves a file from the build or, for client-side routes, `index.html`.

- Hashed assets are cached as immutable.
- Hashed assets are sent Brotli- or gzip-compressed to browsers that accept it, from the copies
  the build wrote.
- `index.html` is never cached by the browser. The service worker keeps it, with the rest of the
  release's shell, until a new release takes over ([Installing](installing.md#offline)).
- `/sw.js`, the service worker, is never cached, and claims the whole app as its scope.

Every page carries a strict Content Security Policy:

- `default-src 'self'`
- `data:` and `blob:` images, for previews
- inline style attributes, for the editor's positioning
- `'wasm-unsafe-eval'`, so the app may compile its own WebAssembly, the editor's and the code
  reader's (it never evaluates script)
- `frame-ancestors 'none'`

Every page also carries `X-Content-Type-Options: nosniff` and `Referrer-Policy: same-origin`.

### The code reader

The camera scanner (`src/components/CodeScanner.svelte`) reads codes with the browser's own
`BarcodeDetector`, where there is one, and otherwise with the `barcode-detector` package's, over
`zxing-wasm`, which is zxing-cpp built as WebAssembly (#314). Both packages are pinned to exact
versions in `package.json`. `src/lib/codeReader.ts` chooses between them, and imports
`src/lib/codeReaderPackaged.ts` only where the browser has none, the first time someone scans.

That module points the package at the build's own copy of `zxing_reader.wasm`, imported with
`?url`, so the WebAssembly is an asset of the build like the editor's: hashed, compressed,
embedded and served by the binary, and kept by the service worker once fetched. Left alone, the
package would fetch it from jsDelivr, which the Content Security Policy refuses.

- The WebAssembly is 0.95 MB, 0.32 MB Brotli-compressed; the script, 43 KB, 15 KB compressed.
- It reads QR, Micro QR and Code 128. The unit tests read every barcode golden through it
  (`src/lib/codeReaderPackaged.test.ts`), and `e2e/scan-camera.spec.ts` reads a label of each
  from a fake camera with `BarcodeDetector` removed, failing on any request that leaves the
  instance.
- The licenses, MIT for both packages, Apache 2.0 for zxing-cpp, and MIT for the Emscripten
  runtime and musl built into it, are in `internal/webui/` (`NOTICE`).

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

## Installing on a phone

BAM! installs from the instance as a web app (#213): a manifest, icons drawn from its settings, and
a service worker that keeps the shell. [Installing](installing.md#on-a-phone) describes it.

The worker is `src/sw/worker.ts`. `vite.config.ts` builds it after the app, as a classic script,
to `sw.js` at the root of the build, given the build's files to keep and a name drawn from them.
What it answers and how is `src/lib/install/shell.ts`, which the unit tests read, and whose list of
the server's own paths `internal/api`'s tests check against the server's routes. The Playwright
specs block service workers, so that `page.route` sees every request, except `install.spec.ts`,
which tests the worker itself, and `offline.spec.ts`.

The offline copy (#214, [Installing](installing.md#the-offline-copy)) is the page's, not the
worker's, which still leaves the API to the network. `src/lib/offline/` holds it:

- `copy.ts`: its shape, and the plain functions that read it: a code to its asset, the search, and
  the shopping list's groups;
- `queue.ts`: changes waiting to be sent, and how each answer settles them;
- `storage.ts`: IndexedDB and the thumbnails' cache, or memory, for the unit tests and for browsers
  without IndexedDB;
- `offline.svelte.ts`: when the copy is kept, refreshed and cleared.

Opened with no connection, `App.svelte` takes who is signed in from the copy (`session.offline`),
and opens only the routes marked `offline`.

### Findings: an installed web app beside a native one

These were checked against what each platform documents, as of late 2026, not yet on phones.

- **Camera scanning.** `getUserMedia` works in an installed app's window on Android and on iOS
  (since iOS 13.4), so the Scan page can show the camera in both. Chrome on Android has the
  Barcode Detection API, which the Scan page uses. Safari on iOS has none, in the browser or
  installed, so there the Scan page reads codes with the code reader BAM! packages ([The code reader](#the-code-reader)).
  The Camera app opens a label's link in Safari rather than in the installed app. On Android,
  links within the app's scope open in the installed app.
- **Signing in on iOS.** An app added to the home screen keeps its own cookies and storage, apart
  from Safari's, so people sign in again in it. On Android it shares Chrome's.
- **Background sync.** Background Sync and Periodic Background Sync are Chrome's alone; iOS has
  neither. Offline changes (#214) are sent when the app is next open and online, not in the
  background.
- **Storage.** The Cache API and IndexedDB are kept per origin, within a share of the disk.
  Safari removes a site's script-written storage after seven days without a visit in the browser,
  but an app on the home screen is spared that while it is used. `navigator.storage.persist()`
  asks for storage that is not evicted under pressure; the offline copy asks for it, and
  stays small, since iOS can still remove it.
- **Notifications.** Web Push reaches installed web apps on iOS 16.4 and later, once the person
  allows it from a tap inside the app, and Chrome on Android in or out of the app. It needs VAPID
  keys, a subscription per device, and the server sending encrypted messages (RFC 8291) to each
  browser's push service, which the worker shows. That fits a **notifier driver**
  ([Notifications](notifications.md)): a channel that sends to the devices of the people
  subscribed. Safari's Declarative Web Push shows a notification without the worker.
- **What else differs.** iOS offers no install prompt (hence Share › Add to Home Screen), no Web
  Bluetooth and no Web NFC, which Chrome on Android has. A Bluetooth printer the server or a relay
  reaches prints from any phone, but one reached through the browser's own Bluetooth
  ([Printing](printing.md#from-a-browser)) can't be printed to from an iPhone; reading NFC tags
  would be Android's alone. App badges work on both.

### Decision: no app-store wrapper, for now (#215)

Nothing is in the app stores. #215 asked for a wrapper only if the installed web app fell short,
and of what it falls short in above, a wrapper fixes little, at a cost every instance pays:

- **One app serves every instance, which a store app can't do well.** A wrapper bundles a release
  of the web app, but each instance runs its own release, so the app would have to speak every
  release's API, where the installed web app is always its own instance's. Its pages would be
  another origin from the instance's API, so it couldn't sign in with the session cookie, and would
  need OAuth with PKCE and tokens (the authorization server MCP already has would serve).
- **Links can't open the app for every instance.** Universal links on iOS and verified app links on
  Android are listed per domain, in the app's build and in a file on each domain, so a label
  scanned with the Camera opens a self-hosted instance in the browser either way. A Trusted Web
  Activity is verified by a file on its one origin too, and without it shows the browser's address
  bar, so it fits a single hosted instance, not an app for all of them.
- **On Android, the installed web app has nothing to add.** Chrome gives it the camera, barcode
  detection, Web Bluetooth, Web NFC, background sync and push.
- **On iOS, the gaps are Bluetooth, NFC and background sync.** A Capacitor app with a Bluetooth
  plugin would print to a browser-reached printer from an iPhone. Apple's review refuses apps that
  are little more than a website, a developer account costs a yearly fee, and each release would
  wait on review.

Revisit this if an organization needs a store listing or device management, if printing from
iPhones over Bluetooth is wanted where no server or relay can reach the printer, or if one hosted
BAM! comes to serve many organizations, where a single origin would make links and a Trusted Web
Activity work. A wrapper would then be Capacitor around the same Svelte app, as #215 set out, never
a separate Swift or Kotlin one.

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

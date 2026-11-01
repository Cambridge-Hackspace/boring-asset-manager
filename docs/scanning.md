# Scanning

Scanning an asset's code opens its scan page: a short page about that one asset, with buttons for
what people most often do with it, such as using some stock, borrowing it, returning it or
reporting a problem. What the page shows is set by a scan template, which categories choose for
their assets.

## The scan page

The scan page in the web app is `/scan/{code}`. It shows the asset's scan template and takes its
actions in place:

- using some stock
- borrowing, with an optional due date
- returning
- reporting a problem

These use the same words and problem form as the asset's page. Opening and editing go to the
asset's own pages.

Details with no value, and contents with nothing in them, are left off.

The page is open to signed-out visitors for public assets ([Access](access.md)). A visitor who is
not signed in is asked to sign in rather than told nothing has the code, since the server does not
say which is the case. The same goes for a signed-out visitor opening an asset's page.

Every asset's page links to its own scan page. Labels reach it with `{{scan_url}}`
([Labels](labels.md)). The template editor puts it in new QR codes, and warns about a QR code that
is not a link.

## Looking codes up

The **Scan** page, `/scan`, looks codes up. It is in the menu for those with `assets.scan`. It
takes a code:

- typed or pasted,
- typed by a USB scanner, or
- read by the camera, where the browser has the Barcode Detection API.

It then opens that code's scan page. A scan link, or an asset's address from any of the server's
addresses, opens that page too. The home page has a box for it as well.

## Scan templates

A scan template is a saved report of kind `scan` ([Reports](reports.md)) that lays out the one
asset scanned. Scan templates are kept and edited with reports, under `reports.manage`. The
**Reports** page lists them apart from other reports, with the default marked.

One is seeded, **Scanned**. It shows the asset's name, location, condition and stock, what it
holds, and its actions. Migration `045` adds `flows` to its actions if they are still as seeded.

### The spec

A scan template's spec has no filter. It has 1 to 20 sections, of three types:

- `{"type": "details", "columns": [...]}`: the asset's own values, each column a heading and a
  binding, as reports' columns are.
- `{"type": "table", "source": "contents", "columns": [...], "sort_by": …, "descending": …}`: a
  table of what the asset holds through location links ([Links](data-model.md)). It lists up to
  500, and only those the viewer can read.
- `{"type": "actions", "actions": [...]}`: quick actions, each listed once.

Charts and groups are refused.

### Quick actions

The actions a template may list, and when a viewer is offered each:

| Action | What it does | Offered when |
| :--- | :--- | :--- |
| `open` | Opens the asset's page. | The viewer has `assets.read`. |
| `edit` | Edits the asset. | The viewer has `assets.write`. |
| `use` | Uses some stock. | The stock is counted, and the viewer has `stock.record`. |
| `borrow` | Borrows it, for oneself. | It is lendable, not lent and in service, and the viewer has `assets.borrow`. |
| `return` | Returns it. | It is lent, and the viewer has `assets.lend` or is its borrower. |
| `report` | Reports a problem. | The viewer has `assets.report` or `assets.write`. |
| `hooks` | Triggers the hooks its categories offer ([Hooks](hooks.md)). | The viewer has `assets.hook`. |
| `add_similar` | Adds a new asset like it ([Data model](data-model.md)). | The viewer has `assets.write`. |
| `add_inside` | Adds a new asset inside it ([Data model](data-model.md)). | The viewer has `assets.write`, and a link type makes locations. |
| `flows` | Starts a flow from it ([Flows](flows.md)). | There are flows the viewer may start from it. |

Each permission is checked in the asset's scopes.

## Which template an asset shows

A category names the scan template its assets show, or none. Its subcategories show it too,
unless they name their own.

An asset shows the template named by the closest category up from any of its categories, in
whichever tree each is in. Its own category comes before its parent, and its parent before a
grandparent.

Equally close categories naming different templates are a tie. A tie is settled by, in order:

1. the category with the longest line of ancestors, as the more specific;
2. then the one with the fewest subcategories, as the more granular;
3. then the oldest, so that rearranging other categories does not change it;
4. then by ID.

An asset in no category that names one shows the default template.

The default template, and any template a category names, cannot be deleted (`409`).

### Ties are marked

Ties are marked, since someone should settle them. Ordinary inheritance is never marked.

- An asset's `scan_template` gives the template and the category it came from. When a tie between
  different templates decided it, it also gives `conflict` and the categories in the tie
  (`contenders`).
- The web app marks ties with a yellow triangle on the asset, in the asset list, and in the
  category tree.
- The category form warns before saving a template that would make a new tie.

## Permissions

`assets.scan` looks an asset up by its code and sees its scan page. Migration `022` grants it to
every role that reads assets, in the same scopes, the public role among them
([Access](access.md)).

## API

| Method | Route | Permission | Purpose |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/scan/{code}` | `assets.scan` for the asset; signed-out visitors too, with the public role | Look a code up and lay out its scan template. |
| `PUT` | `/api/v1/categories/{id}/scan-template` | `categories.manage` | Name a category's scan template, with `{"scan_template_id"}` (null for none). |
| `POST` | `/api/v1/reports/{id}/default` | `reports.manage` | Make this scan template the default. |
| `GET` | `/api/v1/assets?conflict=scan_template` | | List assets whose template was decided by a tie. |
| `GET` | `/api/v1/categories/scan-conflicts` | | List the categories in ties. |
| `GET` | `/api/v1/reports?kind=scan` | | List scan templates. |
| `POST` | `/api/v1/reports` | `reports.manage` | Create a scan template, with `"kind": "scan"`. |
| `POST` | `/api/v1/reports/preview` | `reports.manage` | Lay a scan template out for an asset, with `{"kind": "scan", "asset_id", "spec"}`. |

### Looking up a code

`GET /api/v1/scan/{code}` finds the active asset with that assigned code, of any generator type,
or with that ID. It answers with:

- the asset's ID, name and stock unit
- the template
- the template laid out for the asset

Its actions are narrowed to those the viewer may take now, as the [quick actions](#quick-actions)
table describes. With `hooks` kept, the answer's asset lists the hooks as `hooks`. It lists the
flows the viewer may start as `flows`.

| Status | When |
| :--- | :--- |
| `404` | The viewer cannot scan the asset, it is archived, or no asset has the code. |
| `409` | Two assets have the code, as codes of different kinds. |
| `409` | The template names a field deleted since; it asks for the template to be edited. |

### Conflicts

`GET /api/v1/categories/scan-conflicts` lists the categories in ties, with how many active assets
each has. With `?category_id=…&scan_template_id=…` (empty for none), it answers as if that category
named that template.

### Scan templates as reports

A report keeps its kind. Preview lays a scan template out only for an asset the editor can read.
Running a scan template as a report, or downloading its CSV, is `409`.

## Storage

Scan templates were added by migration `022`, which also added `assets.scan`. Migration `045` adds
the `flows` action to the seeded **Scanned** template.

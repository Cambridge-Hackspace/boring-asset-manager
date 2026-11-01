# Packages

A **package** is part of a BAM! instance, chosen to share with another. You export categories,
templates, reports, flows, icons and assets from one instance and import them into another, where
they are merged with what is already there.

A package is an archive like a backup (see [Backups and archives](backups.md)), of `type`
`"package"`. It holds the rows of what was chosen and of what that needs, and the files they name.
A package is imported into an instance, merged with what is there. `bam restore` refuses one.

## What a package is

A package's header and manifest say what it is, under `package`, as a marketplace would list it:

- `name`
- `description`
- `author`
- `license`
- `version`
- `preview`: the member holding its preview image (`preview.png`, `.jpg` or `.webp`, at most 1 MiB).

## What can be chosen

Each of these can be chosen, by ID:

- categories, single or with every category under them;
- fields;
- hooks;
- link and relation types;
- label templates;
- scan templates;
- reports;
- flows;
- scopes;
- the icon library, with the palette and icon settings;
- assets: those in the categories chosen, or every one, with or without their photos, the links
  between them, and their history.

Archived assets are left out.

### What comes with a choice

What is chosen brings what it needs:

| Chosen | Brings |
|---|---|
| A category | Its fields, the hooks it offers, the link types it suggests, its label templates and scan template, its icon and the fields its icon draws, and the flows that file into it or take from it |
| An icon | What it `extends`, and the library files it `insert`s, `badge`s, takes `axes` from or sets text in, which are read from the sources |
| A label template | Its images, the fields it binds, and the icons it draws by name |
| A report or scan template | The fields it shows or groups by, and the categories it looks in |
| A flow | Its categories, label templates, hooks, the fields it asks or sets, and the flow whose filings it finds |
| Assets | Their categories, the icons they draw as their own, and the link types of their links |

A category whose parent is not in the package keeps what it inherits as its own: its parent's
fields, and its parent's label templates and scan template when it names none.

What comes is listed as **needed by** what needs it, transitively.

### What stays behind

Some things a choice needs stay behind, and are listed with why:

- The printers and pools a flow prints to, which are this instance's own. Once imported, a flow
  that printed adds its labels to the print queue instead.
- The places a flow puts things, which are this instance's assets.

A package never holds:

- people;
- password and token hashes;
- sessions;
- role membership;
- notification channels and their secrets;
- printers;
- loans;
- what flows filed;
- icon revisions;
- this instance's settings.

Who made a flow, and who did what in an asset's history, are left out (`created_by`,
`actor_user_id`, and the `actor` and `borrower` of events). Drafts are not packaged.

### Permissions to export

A package holds only what the requester may read:

| What | Needs |
|---|---|
| Categories and the rest | `assets.read` |
| Flows | `flows.manage` or `flows.run` |
| Scopes | `users.manage` or `categories.manage` |
| Assets | `assets.read` in their scopes |
| Assets' history | `events.read` in their scopes |

- Choosing what cannot be read is refused.
- What something needs that cannot be read stays behind, and is listed.
- Assets that cannot be read are counted and left out.

## Exporting a package

**Admin › Import & export**, for whoever holds `settings.manage`, lists what can be chosen the way
an installer lists its components:

- a tree of categories, where ticking one ticks those under it;
- a group for each other kind.

What the choice needs is ticked and marked *needed by…*. It stays ticked until what needs it is
unticked. The browser downloads the package as it is written, at any size.

From the command line, `--select` takes `kind:name`, by name, key or flow address, or by ID:

```bash
# What a selection would hold, writing nothing
./bam export --config ./config.toml --select subtree:Tools --select assets --plan
# The tools, their assets with photos, and the 3D printing flow
./bam export --config ./config.toml --select subtree:Tools,flow:print-a-part --select assets --photos \
  --name "Workshop tools" --license CC-BY-4.0 --version 1.0 --to ./workshop.bam.tar.gz
```

## Importing a package

A package, or the kinds chosen of a backup, is merged into an instance in use. It is checked
first, saying what it would do, then imported as checked, all or nothing.

### The check

Every member's SHA-256 is verified before anything else. An archive from a newer bam is refused.

The plan lists each thing the archive holds and what importing does with it:

- **create** it;
- use the **existing** one here;
- import it as a **copy** under a new name;
- **skip** it.

It counts the assets, photos, links and history that come. It lists **problems** that stop the
import, such as something needed that is skipped, or an icon that would not compile with this
instance's lexicon.

### Matching what is already here

What has a key that is the same on every instance is matched against what is here:

| What | Matched by |
|---|---|
| Fields and hooks | Key |
| Link types, scopes, label templates, reports and scan templates | Name |
| Flows | Address |
| Label images | Their content |
| Library files | Path, and the names they define |
| A category's icon | Its name, and the names it defines |

What clashes defaults to the existing one when it is the same, and to a copy when it differs. A
category's icon that clashes is copied or skipped.

A copy is renamed (`serial_2`, `Tool label (2)`, `lend-a-tool-2`, `shared/crates-2.ilx`).
Everything the archive holds that names it is rewritten: categories, assets' values, templates'
bindings, reports' columns, flows' steps, and icon sources, whose definitions are renamed with
them.

### Where it goes

The archive's top categories go under the category chosen (the anchor), or at the top level.
Everything gets new IDs. One package can therefore be imported twice under two categories, as two
copies, sharing what is the same.

### All or nothing

The rows go in in one transaction. Files are stored as they are read, before the transaction
commits, and removed if it does not.

Each asset's history records that it was created by an import, by whoever imported it. It keeps
what the archive's history held, without who did what.

A check's `fingerprint` is passed to the import. The import is refused if it would now do
something else.

### What comes in and what does not

What comes in is the catalog, labels, reports, icons, flows and assets.

People, roles, settings, printers, notification channels and authentication are never merged:
they are an instance's own. An archive holding authentication is imported without it, with a
warning, through the web and from the command line alike, as merging never brings it. (`bam
restore` does.)

Also left behind:

- loans, what flows filed, icon revisions and drafts;
- this instance's icon settings, and palette colors it already has;
- codes another asset here already has, which are left out and counted.

A flow is changed as it comes in where it must be:

- What it printed goes to the print queue.
- A place it named that did not come is asked for.
- A flow that moved things to such a place is turned off.

### Permissions to import

The permissions needed follow what the archive holds:

| What | Needs |
|---|---|
| Categories, fields, hooks, link types and scopes | `categories.manage` |
| Label templates and images | `templates.manage` |
| Reports and scan templates | `reports.manage` |
| The library, categories' icons and palette colors | `icons.manage` |
| Flows | `flows.manage` |
| Assets | `assets.write` in the scopes the assets will be in, under the category chosen |
| Uploading | `settings.manage` |

What needs a permission the importer lacks is refused at check, before anything changes.

### In the web interface

**Admin › Import & export** uploads the file in parts, at any size, as backups are (see
[Uploads through the web](backups.md#uploads-through-the-web)). It shows the plan, with a choice
for each clash and the category to import under. It checks again when these change, so what is
imported is what was shown.

### From the command line

```bash
# What importing would do, changing nothing
./bam import --config ./config.toml --from ./workshop.bam.tar.gz --anchor "Lab" --check
# Under Lab, using this instance's serial field and copying its label template
./bam import --config ./config.toml --from ./workshop.bam.tar.gz --anchor "Lab" \
  --choose field:serial=existing --choose "label-template:Tool label=copy"
# The catalog and assets of a backup, and nothing else of it
./bam import --config ./config.toml --from ./bam-backup.bam.tar.gz --only catalog,assets
```

`--choose` takes `kind:name=existing|copy|skip`:

- `kind` is `category`, `field`, `hook`, `link-type`, `label-template`, `scan-template`, `report`,
  `flow`, `scope`, `image`, `icon-file` or `category-icon`.
- `name` is its name, key or path, or its ID in the archive.

## Icon sets

A package of the icons alone (`--select icons`, or only the icons ticked) is not an archive. It is
a zipped isolex library folder, as the Icons page exports: `lexicon.ilx`, `library/`,
`categories/` and `icons.json`. You can edit it in isolex studio, keep it in git, or share it.

Such a zip, made here or in the studio, imports as an icon set. A folder zipped whole works, with
or without `icons.json`.

It is checked like any import:

- Each library file is created, or **replaces** the one at its path, or is the same.
- Each category's icon replaces that of the category it is, by `icons.json`, or else of the
  category whose icon has its name.
- Each palette color is added or replaced.

A definition that would clash with one here that it does not replace, or anything that would not
compile with this instance's lexicon, is a problem that stops it.

Only the palette is taken from `lexicon.ilx`. The tokens, stroke widths, reserved tokens and limits
are this instance's icon settings.

An icon set goes under no category and has no choices. Imported, it is one icon change, each file
it changed with its revision. What it does not change is left alone. It needs `icons.manage`.

## API

### Exporting

`POST /api/v1/exports/plan` with `{"selection"}` answers the plan:

- its `items`, each `chosen` or not, with what it is `needed_by`;
- the `assets` counted;
- what is `left_out`, with why.

`POST /api/v1/exports` with the selection, `name`, `description`, `author`, `license`, `version`
and a `preview` (base64) answers `201` with the export's `id` and where to `download` it.

`GET /api/v1/exports/{id}` downloads it:

- only by whoever posted it, within the hour;
- streamed as it is written;
- holding what they may read at that time. The selection is worked out again then, so what was
  removed meanwhile is not in it.

### Importing

`POST /api/v1/import/check` with `{"upload_id", "anchor", "choices", "kinds"}` starts a job.
`GET /api/v1/import/jobs/{id}` answers it with its `plan`:

- `items`, each with its `action`, `as`, the `existing` one it clashes with, whether `identical`,
  and its `choices`, keyed `kind:id`;
- the `assets`;
- the `permissions` it needs, and those `missing`;
- `warnings`;
- `problems`;
- the `fingerprint`.

`POST /api/v1/import`, with the same and the `fingerprint`, imports it. One import or replace runs
at a time.

| Method and route | Purpose |
|---|---|
| `POST /api/v1/exports/plan` | Plans an export, writing nothing. |
| `POST /api/v1/exports` | Starts an export. Answers `201` with its `id` and `download`. |
| `GET /api/v1/exports/{id}` | Downloads an export, by whoever posted it, within the hour. |
| `POST /api/v1/import/check` | Checks an upload for import. Starts a job. |
| `GET /api/v1/import/jobs/{id}` | Answers a check job with its `plan`. |
| `POST /api/v1/import` | Imports, given the `fingerprint`. |

### MCP

Over MCP, `export_package` exports as `POST /api/v1/exports` does. `check_import` and
`import_package` check and import, with an upload of the person's.

Memory use, for packages as for backups, does not grow with the size of an instance (see
[Memory](backups.md#memory)).

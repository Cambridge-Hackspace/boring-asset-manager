# Backups and archives

A backup is an **archive** of a whole BAM! instance: every row that is information, and the
uploaded files those rows name. It works the same on SQLite, PostgreSQL and MariaDB. Use it to
keep a copy of an instance, to restore one into a new database (including on another kind of
database), or to put back part or all of an instance in use.

Archives are also the form a package takes, for sharing part of an instance with another. See
[Packages](packages.md).

## Making a backup

`bam backup --to <file>` writes an archive of the whole instance. It includes the uploaded files
that rows name (asset photos and their thumbnails, label images), wherever they are kept (see
[Configuration](configuration.md)).

It is safe to run while BAM! runs:

- The rows are read in one transaction, so they are as they were at one moment, however much
  changes meanwhile.
- The files are copied after the rows. A file removed by then is listed as missing rather than
  failing the backup.
- The archive is readable only by its owner.

```bash
# Everything, authentication included
./bam backup --config ./config.toml --to ./bam-2026-10-29.bam.tar.gz
# Leaving authentication out
./bam backup --config ./config.toml --to ./bam.bam.tar.gz --without-secrets
# In parts of at most 4 GiB: bam.part1.bam.tar.gz, bam.part2.bam.tar.gz, ...
./bam backup --config ./config.toml --to ./bam.bam.tar.gz --split 4GiB
```

A `--to` ending in `.db` still makes a copy of a SQLite database, as before archives existed (see
[Installing](installing.md)). Scripts written for that keep working.

The schema must be current (`bam migrate`), because a backup describes the tables this version of
bam makes.

### In parts

With `--split <size>` (such as `500MB` or `4GiB`), a backup is a **set** of archives. Each part is
complete, with its own header and manifest:

- The first part holds the rows and the files that fit.
- The rest hold the rest of the files, none over the size. A single file larger than the size has
  a part of its own.

Parts are named with `.partN` before the extension. A very large instance can then be moved, and
uploaded, a part at a time.

## Authentication

Authentication is:

- password hashes and second factors (`users.password_hash`, `users.totp_secret`);
- sessions;
- API tokens;
- OAuth;
- notification channels' options, which hold their passwords and webhook addresses.

It is kept apart in the archive, under `auth/`. That holds the tables that are nothing but
authentication, whole, and the secret columns of the other tables with their key. So it can be
left out, or skipped whole.

Only the command line writes authentication, and `--without-secrets` leaves it out. A backup from
the web never holds it. Keep an archive that holds it as carefully as the database.

## Kinds of information

An archive holds these kinds of information, and a restore chooses among them. Each kind has a key
(used with `--only` and in the `kinds` of an archive) and its tables.

| Kind | Key | Tables |
|---|---|---|
| Reports | `reports` | `reports` |
| The catalog | `catalog` | `scopes`, `relation_types`, `fields`, `asset_actions`, `categories`, `category_fields`, `category_actions`, `category_relation_types`, `category_scopes` |
| Labels | `labels` | `templates`, `images` and their files, `category_label_templates` |
| Icons | `icons` | `icon_palette`, `icon_settings`, `icon_library`, `category_icons`, `icon_revisions`, `icon_param_bindings`, `icon_text_bindings` |
| People and roles | `people` | `roles`, `role_permissions`, `role_permission_scopes`, `users`, `user_roles`, `user_role_scopes` |
| Assets | `assets` | `assets`, `asset_categories`, `asset_field_values`, `asset_generator_keys`, `asset_links`, `asset_placements`, `asset_loans`, `asset_photos` and their files, and their history, `events` |
| Flows | `flows` | `flows`, `flow_filings` |
| Settings | `settings` | `settings`, `label_stock`, `printers`, `printer_pools`, `printer_pool_members`, `notification_channels`, `spool_jobs`, `spool_batches`, `spool_job_members`, `spool_entries` |
| Authentication | `auth` | `sessions`, `oauth_clients`, `oauth_codes`, `api_tokens`, `api_token_roles`, `api_token_role_scopes`, and the secret columns described under [Authentication](#authentication) |

The tables are listed, and restored, in that order: each after the tables its rows name.

### Tables left out

These tables are left out, because they are not information or are made again:

| Table | Why it is left out |
|---|---|
| `schema_migrations` | The schema's version is in the header, and a restore migrates. |
| `app_locks` | Rows that serialize changes. |
| `permissions` | The same on every instance by key; grants are archived by key. |
| `icon_build` | Made again from the icon sources when the server starts. |
| `live_connections` | Connections open now, gone when the server stops. |
| `flow_runs` | Runs under way, dropped when left 12 hours. |
| `action_replies` | Devices' answers to a hook, for the moment it is triggered. |
| `notification_deliveries` | The queue of notices to send, which a restore must not send again. |
| `uploads` and `upload_parts` | Archives being uploaded through the web, staged apart from the files and removed after a day. |

`internal/archive/tables.go` lists the tables, their kinds, columns and keys.
`tools/archive/tables.py` writes it from a migrated database. A test fails, on every dialect, when
a migration changes the schema without updating it.

## Restoring into a new instance

`bam restore --from <file>` runs with BAM! stopped. It tells an archive from a copy of a SQLite
database by its contents, not its name, and puts a copy back as before (see
[Installing](installing.md)).

An archive is restored into an instance not yet in use: one with no users and no assets. It works
on any database, and a backup from one dialect restores into another. Restoring is therefore also
the way to move between databases.

```bash
# Check an archive, and say what restoring it would do, changing nothing
./bam restore --config ./config.toml --from ./bam.bam.tar.gz --check
# Restore it all, authentication included
./bam restore --config ./config.toml --from ./bam.bam.tar.gz
# Restore some kinds of information only
./bam restore --config ./config.toml --from ./bam.bam.tar.gz --only labels,reports
# A set in parts: the first, then the others in any order, now or later
./bam restore --config ./config.toml --from ./bam.part1.bam.tar.gz
./bam restore --config ./config.toml --from ./bam.part3.bam.tar.gz
```

### Checking the archive

The archive is checked first, read through once before anything changes. The check covers:

- every member's size and SHA-256, against the manifest;
- the order of its members;
- the rows of each table;
- its versions.

An archive that is corrupted, cut short, padded or reordered, or has a member the manifest does not
list, is refused, saying what is wrong. Because the archive is read twice, it must be a file.

### All or nothing

The rows of each kind restored replace the instance's in one transaction, with every ID kept. A
kind not chosen is left as it is.

Files go into the configured store (see [Configuration](configuration.md)) as they are read,
before the rows naming them are committed. A restore that fails therefore leaves at most some files
for `bam storage sweep` to remove.

A restore is refused, changing nothing, if it would change a kind not chosen, or if its rows name
what the instance does not have (assets without their catalog, say).

### Authentication and people

From the command line, authentication is restored with the rest, sessions included, so people stay
signed in as the archive has them. Restoring people without authentication leaves them with no
password. Set one with `bam admin password`.

### Versions

An archive from an older bam is restored into an empty database. The database is migrated first
to the archive's schema, then to this bam's. An archive from a newer bam, or in a newer format, is
refused.

### Sets in parts

A set's first part restores the rows. Each later part adds the files its rows name that the store
does not yet have:

- A part applied twice changes nothing.
- A part from another set is refused.
- Until every part is in, `bam storage sweep --dry-run` lists the files still to come.

## Replacing an instance in use

`bam restore --replace`, or **Admin › Backups** in the web interface, puts back what a backup of
this instance holds. It puts back all of it, or only the kinds chosen (`--only assets`). Someone
restoring their assets gets their assets back and keeps everything else.

```bash
# Put the assets back as the backup has them, backing them up first
./bam restore --config ./config.toml --from ./bam.bam.tar.gz --replace --only assets
# Everything but authentication, the backup of what it replaces written where asked
./bam restore --config ./config.toml --from ./bam.bam.tar.gz --replace --backup-to ./before.bam.tar.gz
```

### What changes

Each kind chosen ends as the backup has it. Its rows are matched by key:

- Rows the backup has are updated or added.
- Rows it lacks are removed, together with what named only them (a flow's record of an asset
  removed goes with it).
- Kinds not chosen are left as they are.

### People and sign-ins

People are never removed. The backup's users and roles are put back as it had them, and the
instance's other users are kept, with their roles.

Passwords, sessions and tokens are authentication. The web never replaces them, and the CLI does
only when `auth` is among the kinds. So no one is signed out unless that is chosen.

A replace is refused if it would leave no one active who can manage users, or leave the person
replacing unable to.

### A backup first

A backup of what is being replaced is taken first, and the replace is refused if that fails:

- **From the CLI:** written to `--backup-to`, or to a file in the staging directory, with
  authentication included.
- **From the web:** written into the staging, without authentication, and kept a day like any
  upload. It can be downloaded, or put back with one click.

### Which backups can replace

Only a backup of this instance, at this version, can replace it:

- A backup from another version of bam is restored into a new instance instead.
- A backup of another instance clashes with this one's (its roles and people have other IDs under
  the same names), so it is refused, changing nothing. Moving between instances is what restoring
  into a new database is for.

## Backups in the web interface

**Admin › Backups** needs both `users.manage` and `settings.manage`.

**Download a backup** streams one to the browser. It never holds authentication or notification
channels' passwords, so it is not as sensitive as the database, though it holds people's names and
emails.

**Replace from a backup** does the following:

1. Uploads a backup (see [Uploads through the web](#uploads-through-the-web)).
2. Checks every byte of it.
3. Lists the kinds it holds, each ticked, and says what replacing each does.
4. Replaces those still ticked, once the instance's host (from `server.base_url`) is typed to
   confirm.

An archive holding authentication is replaced without it, saying so. A set's later parts each add
their files once uploaded.

## Uploads through the web

Archives of any size move over ordinary connections that drop. Neither the browser nor the server
holds one in memory.

### In the browser

One upload control does the work:

- It hashes the file a slice at a time.
- It sends the parts in turn, showing their progress.
- It pauses and resumes, and retries a part that fails with growing waits.
- Leaving the page asks first while an upload is under way.
- An unfinished upload is remembered. Choosing its file again carries on where it stopped.

The parts of a split backup are each an upload of their own.

### How uploads work

Uploads arrive in parts, each its own request, and resume where they stopped.

1. `POST /api/v1/uploads` with the file's `filename`, `size` and `sha256` begins one. The browser
   works out the SHA-256 from slices of the file. The answer holds the upload's `id` and
   `part_size`: 8 MiB, or more for an upload so large that it would need more than 10,000 parts.
2. `PUT /api/v1/uploads/{id}` with `Content-Range: bytes <first>-<last>/<size>` and
   `Content-Type: application/octet-stream` sends the next part. It must start where the upload
   has got to, and be `part_size` long, except for the last.
3. When the last part is in, the whole file is checked against its SHA-256. One that differs is
   removed (`422`).

`GET` or `HEAD` on `/api/v1/uploads/{id}` says how much has arrived (`received`, and the
`Upload-Offset` header). `DELETE` cancels an upload.

| Problem with a part | Answer |
|---|---|
| Out of order, sent again, or overlapping | `409`, with where to go on from |
| Wrong length, or past the end | `400` |
| Whole file does not match its SHA-256 | `422`, and the upload is removed |
| Will not fit in the staging free space (`local`) | `507`, when the upload begins |

Each upload route needs `settings.manage`. An upload belongs to its own person alone: anyone else
is told it is not found.

### Where uploads are staged

An upload is staged where the files are kept:

- **`local` backend:** in `storage.staging_path`. An upload that will not fit in its free space is
  refused when it begins (`507`).
- **`s3` backend:** in the bucket, as a multipart upload under `staging/`, so nothing is kept on
  bam's own disk.

An upload neither added to nor used for a day is removed, finished or not.

### Deadlines

Deadlines last while bytes move. Every route gives a request 15 seconds to arrive and its answer
15 seconds to be written, and takes bodies of at most 1 MiB. The routes that move archives are the
exception: their deadlines run two minutes from the last bytes moved, however long the whole takes.

## API

| Method and route | Purpose |
|---|---|
| `GET /api/v1/backup` | Streams a backup. |
| `POST /api/v1/backup/check` | With `{"upload_id"}`, checks an upload. Answers a job. |
| `POST /api/v1/backup/replace` | With `{"upload_id", "kinds", "confirm"}` (and `"part"` for a later part), replaces. Answers a job. |
| `GET /api/v1/backup/jobs/{id}` | Reports on a job until it is `done`, with its `result` or `error`. |
| `POST /api/v1/uploads` | Begins an upload. |
| `PUT /api/v1/uploads/{id}` | Sends the next part. |
| `GET`/`HEAD /api/v1/uploads/{id}` | Says how much has arrived. |
| `DELETE /api/v1/uploads/{id}` | Cancels an upload. |
| `GET /api/v1/uploads/{id}/content` | Downloads an upload of one's own, such as the backup a replace took first. |

One replace runs at a time.

None of this is offered over MCP. Replacing an instance stays with a signed-in person, as making
tokens does.

## The archive format

An archive is a gzipped tar, readable with `tar` and any JSON parser. Nothing in it needs bam to
read. Its members, in order:

- **`header.json`**, first. Its fields:
  - `format`: `"bam-archive"`.
  - `format_version`: 2. This is the format's own version, apart from bam's. bam reads 1 as well.
  - `type`: `"backup"`, or `"package"` (see [Packages](packages.md)).
  - `bam_version`.
  - `schema_version`: the last migration the database had run.
  - `created_at`.
  - `dialect`: the database it was written from.
  - `kinds`: the kinds of information it holds.
  - `secrets`: whether it holds authentication.
  - `set`: its `id`, this `part`, and, in parts after the first, how many `parts` there are.
- **`README.txt`**: what each member is, for whoever opens it without bam.
- **`data/<table>/0001.jsonl`, `0002.jsonl`, …**: each table's rows as JSON Lines, one object a
  row, in the order of the table's key, at most 8 MiB a member. A table with no rows has no
  members.
- **`auth/<table>/…`**: authentication, in the same form (see [Authentication](#authentication)).
- **`icons/` and `icons.json`**, when the archive holds icons (see [Icons in archives](#icons-in-archives)).
- **`files/photos/<id>` and `files/images/<id>`**: the uploaded files, as stored.
- **`manifest.json`**, last, once everything else is written. It holds:
  - the header again (with `set.parts`);
  - the rows of each table (`tables`);
  - the files and their bytes;
  - the files rows name that the store did not have (`missing_files`);
  - the size and SHA-256 of every other member (`members`);
  - whether this is the last part of its set (`last`).

### Values

Values are written one way, whatever the dialect:

| Value | Written as |
|---|---|
| Text | Strings |
| Numbers | Numbers |
| Booleans | `true` and `false` |
| Times | RFC 3339 in UTC to the microsecond (`2026-10-29T12:00:00.123456Z`) |
| Columns that hold JSON | JSON, not strings of it |
| NULL | `null` |

Grants of permissions (`role_permissions`, `role_permission_scopes`) name a permission by its
`permission_key`, which is the same on every instance. The database has its ID, which is not.

### Icons in archives

With icons, the archive holds the icons' published sources as an isolex library folder, which
`isolex check` and isolex studio open as it is:

- `icons/lexicon.ilx`: the lexicon block bam writes, and the palette as a `palette` block.
- `icons/library/<path>`: the library files.
- `icons/categories/<icon>.ilx`: each category's own icon.

The rows (`icon_library`, `category_icons`) keep everything else (IDs, drafts, when) but not the
source. A restore, replace or import takes the source from the folder. A row with no file has no
source.

`icons.json` says:

- which category each icon in `categories/` is, and for which rendition (`categories`: `icon`,
  `category_id`, `category`, `rendition`);
- which assets are drawn with an icon of their own (`assets`: `asset_id`, `asset`, `icon`,
  `rendition`).

An `icons.json` with no `rendition` means `screen`.

`lexicon.ilx` is written so the folder compiles on its own. The palette and icon settings are
restored from their rows, which keep colors with transparency.

Format 1, before this, kept the sources in the rows and had no folder.

## Memory

Memory does not grow with the size of an instance:

- Rows are written as they are read, and files streamed.
- Only the list of files to copy is kept, a few dozen bytes for each.
- A restore inserts rows in batches as it reads them, and streams files into the store.

Tests write and restore 256 MiB of files, generated as they go, and check the heap.
`BAM_TEST_ARCHIVE_BYTES=21474836480 go test ./internal/archive -run Memory` does 20 GiB.

An upload of 256 MiB goes over HTTP in parts the same way. `BAM_TEST_UPLOAD_BYTES` sends more
(`go test ./internal/api -run LargeUpload`).

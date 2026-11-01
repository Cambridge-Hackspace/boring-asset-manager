# CSV import and export

BAM! reads and writes assets as CSV files, one row per asset. Export a filtered list to edit in a
spreadsheet or keep as a record; import a file to create many assets at once, or to update existing
ones. An exported file, edited and imported again, changes exactly what was edited.

## The file format

Files are UTF-8. Export writes a leading byte order mark, and import accepts one.

Each row is one asset, with these columns:

| Column | Meaning |
| :--- | :--- |
| `id` | The asset's UUID. Blank on import creates a new asset. |
| `name` | The asset's name. |
| `categories` | Category references, separated by `;`. See [Categories](#categories). |
| `code.<type>` | The assigned code for a generator type: `code.qr`, `code.micro_qr`, `code.code128`. |
| `quantity`, `unit`, `low_stock_threshold` | Stock counted by quantity ([Stock](stock-and-lending.md)). See [Stock](#stock). |
| `icon` | The asset's own icon ([Icons](icons.md)). See [Icons](#icons). |
| `field.<key>` | A field value, by corpus key. |
| `links` | The asset's links ([Links](data-model.md)), each `verb:asset`, separated by `;`. See [Links](#links). |

### Categories

A category reference is either a path of names from the root, joined by ` > ` (`Tools > Drills`),
or a category ID.

Export writes the path. It writes the ID instead when the path is ambiguous or contains a
separator.

### Stock

Export writes `quantity`, `unit` and `low_stock_threshold` only when some asset's stock is counted
by quantity.

On import:

- A blank cell keeps what the asset has.
- A unit or threshold for an asset whose stock is not counted needs a quantity too.
- An import cannot stop counting, or remove a threshold.

### Icons

Export writes the `icon` column only when some asset has an icon of its own. The cell is blank for
assets drawn with their categories' icon.

On import, a blank cell gives the asset its categories' icon again. A name must be one the build
draws, unless it is the one the asset already has.

### Links

A `links` cell lists links as `verb:asset`, separated by `;`, for example
`contained_in:Bin A3;used_with:Drill`.

- The **verb** is a relation type's name or inverse name, written as a verb and read from this
  row's asset. Case and spacing do not matter on import.
- The **asset** is a name when it names exactly one active asset. Otherwise it is an ID or an
  assigned code.

Export writes the name where that works. It writes the ID where it does not: two assets share the
name, or the name contains `;`. Export writes the column only when some exported asset has links,
and lists each link from both ends.

### Formula injection

Spreadsheet formula injection is defused, and a file reads back exactly as it was written:

- On export, a cell beginning with `=`, `+`, `-`, `@`, a tab, a carriage return or `'` is prefixed
  with `'`. Plain numbers such as `-5` are left alone.
- On import, a `'` followed by one of those characters is removed.

## Exporting

Export writes every asset matching the asset list's filters (`category_id`, `search`). The file has
a column for every generator type, and for every field any of the exported assets has a value for.

## Importing

An import has two stages: a preview, then the import itself. The file may be at most 5 MB and
5,000 rows.

### Mapping columns

Each column is mapped to one of:

- `id`, `name`, `categories`, `links`
- `quantity`, `unit`, `low_stock_threshold`
- a generator type
- a field key
- `ignore`

The preview suggests a mapping from the headers. It recognizes the column names above, and field
labels as well as field keys. Every column must be mapped or explicitly ignored, and the user
confirms the mapping before importing.

### Creating and updating

A row without an `id` creates an asset. A row whose `id` matches an asset updates it, and a blank
cell leaves that value unchanged.

### Validation

Every referenced category must already exist. Otherwise the import is refused, with the list of
unknown references.

Each row is then checked as the API would check it:

- required fields, from the asset's effective fields
- field types and select choices
- generator codes: their format, and uniqueness both within the file and against other assets

Duplicate IDs within the file, unknown IDs, and archived assets are errors.

The preview reports each problem with its row and column. It also says how many assets would be
created, updated and left unchanged.

### Importing links

Names in a `links` column are read as they will be once the rows are applied. So a file can create
a bin and fill it in the same import. A link stated from both ends counts once.

Links that do not exist yet are added. For a single type ([Links](data-model.md)), a new link moves
the asset, ending its old one. By default, nothing else is removed.

With `remove_links=true`, the links of the file's assets that no row states are removed too. This
is a form field, sent beside the file and mapping; the web app calls it "Also remove links that no
row states". With it, an exported file, edited, imports back exactly. Assets the file does not
mention keep their links.

The preview lists each link added, moved or removed (up to 500, with counts of all). It reports:

- a cell that is not `verb:asset`
- an unknown verb
- an asset named by nothing, or by several
- an asset linked to itself
- a new link to an archived asset
- an asset put in two places
- a loop

Link changes are recorded in history with the trigger `import`.

### Applying the import

The import validates everything again, without trusting the preview. It applies every row in one
transaction: any error means nothing is written.

The import is refused with `409`, asking for a new preview, when:

- a link the preview planned to remove or move has gone since, or
- a link has been added since that the import would duplicate.

Inventory thresholds ([Stock](stock-and-lending.md)) are checked once afterwards, so each category
records at most one event.

## API

| Method | Route | Permission | Purpose |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/assets/export.csv` | `assets.read` | Export matching assets. Takes `category_id` and `search`. |
| `POST` | `/api/v1/assets/import/preview` | `assets.write` | Preview an import: suggested mapping, problems, counts and link changes. |
| `POST` | `/api/v1/assets/import` | `assets.write` | Import the file. |

Both import routes take the file, the mapping, and optionally `remove_links`.

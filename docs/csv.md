# CSV and Excel import and export

BAM! reads and writes assets as CSV files, one row per asset. It also writes them as Excel workbooks
(see [Exporting to Excel](#exporting-to-excel)), and reads workbooks a sheet at a time (see
[Excel workbooks](#excel-workbooks)). Export a filtered list to edit in a
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
| `vendor`, `order_date`, `received_date`, `price`, `currency`, `order_number`, `warranty_end` | Where it came from ([Purchases](data-model.md#purchases)). See [Purchases](#purchases). |
| `field.<key>` | A field value, by corpus key. A person field's people by name. See [People](#people). |
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

### Purchases

Export writes `vendor`, `order_date`, `received_date` and `warranty_end` only when some asset has
purchase details, and `price`, `currency` and `order_number` only when some asset shows any. The
vendor is written by name, dates as `YYYY-MM-DD`, and the price as a decimal in its currency, such
as `12.50`. In a workbook, dates are dates and prices numbers, where each reads back as written.
The price, currency and order number of assets whose prices the exporter may not see are blank
(see [Seeing prices](access.md#seeing-prices)). The invoice is an attachment, and stays out.

On import:

- A blank cell keeps what the asset has, so an export of hidden prices imports back unchanged.
- A vendor is named by its name, ignoring case, or its ID, among those the importer sees. Two
  vendors of the same name need the ID.
- A price needs a currency, in the row or the asset's own, and no more places than it has.
- Giving the price, currency or order number of an asset whose prices the importer may not see is
  a problem.

### People

A person field's cell ([Person fields](data-model.md#person-fields)) holds the people it names,
separated by `;`. Export writes their names, never their emails, and only those the exporter sees.

On import:

- A person is named by their email, or by a name only one person has, ignoring case, among those
  the importer may choose for the asset. Without `assets.track` for it, that is only themselves.
- A name several people have is a problem: give their email instead. So is one no one has.
- With `people.add`, the preview lists the names matching no one in `new_people`, with
  `can_add_people`, and offers to add them as people without accounts before checking again.
- The people a field names that the importer does not see stay, as they do in the form.

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

### Exporting to Excel

**Export Excel** writes the same assets as an Excel workbook (`.xlsx`):

- **An *All* sheet** has every asset, with the CSV's columns.
- **Then a sheet per category** any of them is in, in order of their paths. Each has only that
  category's fields as its field columns, those it inherits included. An asset in several
  categories is on each of their sheets, and one in none is only on *All*.
- **Cells are typed:** quantities and number fields are numbers, and date fields are dates, where
  each reads back as written. So `5` is a number, but `007` and `1.50` stay text, and so does a date
  with a time zone, which a spreadsheet cannot keep. Everything else is text.
- **Each sheet's header row** is bold and frozen, with filters, and the columns are fitted to what
  they hold.
- **Text is always written as text, never as a formula,** so a cell such as `=HYPERLINK(...)` cannot
  run when the workbook is opened. Unlike CSV, nothing needs defusing.
- **It imports again unchanged:** import its *All* sheet, or any category's, as an
  [Excel workbook](#excel-workbooks). So that it can, a workbook has at most 50 sheets. Categories
  past the first 49 are only on *All*.

BAM! writes workbooks with a small writer of its own (`internal/xlsx`), the reader's counterpart. The
workbook in `internal/xlsx/testdata/written.xlsx` is what it writes, and a test keeps it so; it was
checked by opening it in LibreOffice.

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
- field types, select choices, and the people in person fields
- generator codes: their format, and uniqueness both within the file and against other assets

Duplicate IDs within the file, unknown IDs, and archived assets are errors.

The preview reports each problem with its row and column. It also says how many assets would be
created, updated and left unchanged.

A link naming no asset is a problem too. Its `ref` is the name, code or ID it gave, and `unresolved`
counts these among `problem_count`, for [several files](#importing-several-files).

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

### Importing several files

The web app's import page takes up to 20 files at once, chosen or dropped together. Each keeps the
limits above, and is its own import: its own mapping, preview and transaction. One file's preview
does not change another's.

- **Order:** the files are imported in the order listed, which can be changed. A file whose links
  name assets that an earlier file creates must come after it. Its preview reports those links as
  problems (`unresolved`). Only those problems are allowed, and only when it is not first. They
  are checked again when it is imported.
- **A refused file** stops the ones after it. The page says which were imported, and checks the
  rest again, since what was imported may resolve their links.
- **Mappings** are remembered in the browser for each person and set of headers. A later file with
  the same headers starts from the last mapping used for them, with a way back to the suggested
  one.

There is no separate route: the page calls the preview and import routes once per file, so
scripts are unaffected.

### Excel workbooks

An Excel workbook (`.xlsx`) is read a sheet at a time, each sheet as if it were a CSV file. On the
import page, a workbook becomes one import per sheet, named "Workbook › Sheet" in the list, and any
can be left out. Each sheet counts toward the 20.

- **Only values are read.** A formula imports the value it last showed, as saved in the file. A
  formula saved without one is an error naming the cell: open the workbook, save it again, and
  import that. Formatting, comments and macros are ignored.
- **Numbers** import as they show, to 15 significant digits: `0.10` is `0.1`, and `5.0` is `5`.
  **Dates** import as ISO dates (`2024-03-05`), with the time when there is one
  (`2024-03-05T12:00:00`). **True and false** import as `true` and `false`. **Text** imports as
  text.
- **The first row with anything in it is the header row.** Empty columns at the sheet's edges are
  left out. Problems name the sheet's own rows. Merged cells in the header row are an error, since
  each column needs its own name.
- **Excel 97-2003 workbooks (`.xls`) and OpenDocument spreadsheets (`.ods`)** are not taken: save
  them as `.xlsx`, or as CSV.

A workbook is a ZIP of XML, so a hostile one is refused before it costs much:

- more than 50 MB uncompressed in all, or a part compressed past 100 to 1;
- more than 50 sheets, or more than 5,000 rows in a sheet;
- a DTD, which is where entity expansion and external entities live. A workbook never needs one.

BAM! reads workbooks with a small reader of its own (`internal/xlsx`): shared strings, sheets, cell
types, the styles that say which numbers are dates, and cached values. It takes no library.

## API

| Method | Route | Permission | Purpose |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/assets/export.csv` | `assets.read` | Export matching assets. Takes `category_id` and `search`. |
| `GET` | `/api/v1/assets/export.xlsx` | `assets.read` | Export them as an Excel workbook, with the same filters. |
| `GET` | `/api/v1/assets/export.pdf` | `assets.read` | Export them as a PDF table of the columns chosen (see [Reports](reports.md#pdf)). |
| `POST` | `/api/v1/assets/import/preview` | `assets.write` | Preview an import: suggested mapping, problems, counts and link changes. |
| `POST` | `/api/v1/assets/import` | `assets.write` | Import the file. |

Both import routes take the file, the mapping, and optionally `remove_links`. For a workbook,
`sheet` names the sheet; a preview without it answers the workbook's `{"sheets"}`, and an import
without it is refused.

## Vendor catalogs

A vendor's [catalog](vendors.md#catalogs) is read and written as CSV of its own, one row per entry,
from its **Catalog** tab. The columns are those an export writes, in its order:

| Column | Meaning |
| :--- | :--- |
| `id` | The entry's ID. Blank on import makes a new entry. |
| `category` | The category it is for, as in an asset's `categories` cell: a path of names, or an ID. |
| `asset` | Or the asset it is for, as a `links` cell names one: by name when no other active asset has it, or by ID or code. |
| `part_number`, `description`, `url` | The vendor's part number, a description and the product page. |
| `pack_size`, `pack_unit` | What a pack holds, such as `100` and `screws`. |
| `price`, `currency`, `price_checked` | The price of a pack, as a decimal in its currency, such as `4.50`, and when it was checked, `YYYY-MM-DD`. |
| `preferred` | `yes` for the one to reorder from, or blank. |

An export writes the entries the exporter sees, with formulas defused as an asset export's are,
and the price columns blank where they may not see prices.

An import is checked first (`check=true`), and then applied all or nothing:

- A row with an `id` replaces that entry of this vendor's; one without makes an entry. A file's
  header names the columns it has, in any order, and a column it leaves out keeps what an entry has.
- Without `purchases.read` for the vendor, the price columns keep what entries have, and filling
  one in is a problem.
- A row names a category or an asset, not both, and needs a part number or a description.
- Two rows preferring entries for the same category or asset are a problem. A row preferred makes
  the others for the same not, those not in the file among them.
- An exported file imported again changes nothing. Entries the file does not mention stay.
- The file may be at most 5 MB and 5,000 rows, and may be an Excel workbook's sheet, named in
  `sheet`, as an asset import's may.

| Method | Route | Permission | Purpose |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/vendors/{id}/catalog.csv` | `assets.read`, and seeing the vendor | Export its catalog. |
| `POST` | `/api/v1/vendors/{id}/catalog.csv` | `vendors.manage` for the vendor | With `check=true`, what importing the `file` would do, as `rows`, `create`, `update`, `unchanged` and `problems`; without it, import it, or answer `422` with the problems. |

# Reports

A report is a saved view of assets: it gathers assets by a filter and shows them as tables and
charts. Use reports for shopping lists, loan lists, and summaries of stock and condition. Table
columns are content bindings, the same `{{...}}` placeholders labels use
([Labels](labels.md)).

## Who can use them

- Anyone with `assets.read` runs reports.
- `reports.manage` creates, edits and deletes them. Migration `017` grants it to roles that manage
  templates.

## Starting reports

Two reports are seeded to start from. Both can be edited or copied.

- **Running low**: a shopping list, grouped by category.
- **On loan**.

## In the web app

The **Reports** page lists the reports. A report runs when opened, prints without the app's
navigation, and links each row to its asset.

The editor offers placeholders as you type, and previews the report before it is saved.

## The report spec

A report's spec is JSON. It is checked when saved, as templates are, and unknown keys are refused.

```json
{
  "filter": {"category_ids": ["…"], "search": "drill", "archived": false, "running_low": true, "loan": "open", "condition": "problem"},
  "sections": [
    {"type": "table", "title": "Shopping list",
     "columns": [{"heading": "Asset", "binding": "{{asset.name}}"}, {"heading": "In stock", "binding": "{{stock.quantity}} {{stock.unit}}"}],
     "sort_by": 1, "descending": true, "group_by": "category"}
  ]
}
```

### Filter

Every filter is optional.

| Filter | Keeps |
| :--- | :--- |
| `category_ids` | Assets in any of those categories, or below them. |
| `search` | Assets matching a search term, such as `"drill"`. |
| `archived` | `true` or `false`. |
| `running_low` | Assets flagged running low themselves, or in a flagged category, or below one. |
| `loan` | `open` or `overdue`, as on the asset list ([Lending](stock-and-lending.md)). |
| `condition` | A condition, or `problem`, as on the asset list ([Data model](data-model.md)). |
| `vendor` | Assets bought from this vendor, by ID, as the reader may see it ([Purchases](data-model.md#purchases)). |
| `purchased_between` | Assets ordered within `{"from": "2026-01-01", "to": "2026-03-31"}`, both days included, either left out for no limit. An asset with no order date counts by the day it was received; one with neither is left out. |
| `warranty_ends_before` | Assets whose warranty ends before this day, `YYYY-MM-DD`. |
| `contract` | Assets a contract covers, by ID, as the reader may see it ([Contracts](contracts.md)): those it names, and those in the categories it names or below them. The report then binds `{{asset.contract.*}}` to that contract. |

### Sections

A report has 1 to 20 sections.

### Tables

A table has 1 to 30 columns, each a `heading` and a `binding`.

- `sort_by` names a column, counting from 0. Numbers sort as numbers, and text sorts ignoring case.
  `descending` reverses the order.
- A binding is any label template binding (see [Labels](labels.md)), and also, for where the asset
  came from: `{{asset.purchase.vendor}}`, `{{asset.purchase.order_date}}`,
  `{{asset.purchase.received_date}}`, `{{asset.purchase.price}}`, `{{asset.purchase.currency}}`,
  `{{asset.purchase.order_number}}` and `{{asset.purchase.warranty_end}}`. Dates are
  `YYYY-MM-DD`, and a price a decimal, which sorts as a number. The price, currency and order
  number are blank for assets whose prices the reader may not see (see
  [Seeing prices](access.md#seeing-prices)).
- The contract covering the asset binds as on labels: `{{asset.contract.name}}`, `kind`, `vendor`,
  `reference`, `start_date`, `end_date`, `cost`, `currency` and `billing`, the contract filtered
  by when the filter names one, and otherwise the one in force covering it longest (see
  [Contracts](contracts.md#reports-and-labels)).
- `group_by: "category"` lists each row under every category its asset is in. Groups are ordered
  by name, and those running low are marked.

### Charts

Charts are sections too. There are two shapes:

- `{"type": "chart", "chart": "bar" | "donut", "group_by": …, "measure": …}`
- `{"type": "chart", "chart": "line", "measure": …, "period": "week" | "month"}`

Bars and donuts group the assets by one of:

- `category` (each category the asset is in)
- `condition`
- `borrower`
- `location`
- `unit`
- `field:<key>`

They measure one of:

- `assets`: how many.
- `quantity`: their stock, split by unit unless grouped by unit, so units never add up.
- `loans`: how many are lent out.

The 20 largest groups are shown, and the rest are added up as `Other`.

Lines run over the last 12 weeks (from Monday, UTC) or the last 12 months. They come from the
history of the assets the report finds, measuring either stock `used` or `restocked` (a line per
unit), or `loans` made.

The web app draws charts itself, as bars, a donut or lines. Each chart is described for screen
readers and shows its numbers as a table. A chart downloads as CSV like a table: a row per group or
period, and a column per line.

Each table and chart also downloads as an Excel workbook, and **Download all as Excel** gives the
whole report, a sheet per table or chart, named by its title. A cell is a number or a date when it
reads back as itself as one (so `12` is a number but `007` is text), and otherwise text, never a
formula.

## PDF

A report, or the asset list as its filters show it, downloads as a PDF: **PDF…** beside its other
downloads. It is made on the server, for printing, insurance, audits, board meetings and handovers.

- **A report** has every section: its tables, with their groups, and its charts, drawn as vectors.
- **The asset list** is a table of the columns chosen, from the CSV export's (see
  [CSV](csv.md#the-file-format)): name, categories and quantity unless others are.
- **Page size,** Letter or A4, is the instance's unless another is chosen. **Orientation** is
  portrait or landscape.
- **Photos and label previews:** each row can have its asset's photo, and its label as its
  category's template draws it. Both are off unless asked for, and drawn for at most 500 rows.
- **Every page is headed** with the instance's name, the document's title, and when it was made and
  by whom, and numbered at its foot.
- **Tables break across pages,** repeating their heading row on each. A group's title is kept with
  its first rows, and repeated, as continued, on a page its rows run onto.

The instance's name and page size are set on **Admin › Import & export** by those who manage
settings: `BAM!` and Letter until they are.

**How it is drawn:** with the label renderer, in the fonts labels embed, so a PDF looks the same
wherever it was made, with no second PDF library. Its text is in a font made of those fonts' own
glyphs, each defined once, so a long document stays small and its text can be searched and copied.
The same document is the same to the byte, everywhere.

## Asset sheets

Two more documents, made the same way. Each is on Letter or A4, upright or turned, as for the
others.

**An asset's sheet** is a page about one asset, for insurance records, handovers and a machine's
binder. **Sheet PDF** on its page downloads it, and **PDF…** on the asset list makes a sheet for
each asset the filters show, one to a page, up to 500 at once. A sheet has:

- **Pictures:** the asset's photo, each of its codes drawn as its QR code or barcode with the code
  under it, and its label as its category's template draws it, where it has each.
- **Facts:** its categories, where it is (its places, outermost first), its quantity, its condition
  and what is wrong, its current loan, its codes, its fields, where it came from as the reader may see it, and its
  links.
- **Recent history:** its last 10 events, with when and who, where the reader may see who.

**A stocktake checklist** is the assets the list's filters show, to walk the rooms with and count.
**PDF…** on the asset list makes one.

- **Grouped by where they are,** places in order of their names, outermost first, so a place comes
  just before those inside it. Assets in no place come last.
- **Each asset** has a box to tick, its codes, the quantity expected (its count for an asset
  counted by quantity, else one), and a blank for the count.
- **Bringing the count back** is for an import ([CSV](csv.md)) or the phone, not the checklist.

**Who sees what:** like the asset list, sheets and checklists hold only the assets the reader may
see, in their scopes. An asset outside them has no sheet for them (`404`), and the places and links
they may not see are left out.

## API

| Method | Route | Permission | Purpose |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/reports` | `assets.read` | List reports. |
| `GET` | `/api/v1/reports/{id}` | `assets.read` | Read one report. |
| `POST` | `/api/v1/reports` | `reports.manage` | Create a report, with `{"name", "spec"}`. |
| `PUT` | `/api/v1/reports/{id}` | `reports.manage` | Change a report, with `{"name", "spec"}`. |
| `DELETE` | `/api/v1/reports/{id}` | `reports.manage` | Delete a report. |
| `GET` | `/api/v1/reports/{id}/run` | `assets.read` | Run a report. |
| `POST` | `/api/v1/reports/preview` | `reports.manage` | Run an unsaved spec, with `{"spec"}`, for the editor. |
| `GET` | `/api/v1/reports/{id}/sections/{n}/csv` | `assets.read` | Download table `n` (counting from 1) as CSV. |
| `GET` | `/api/v1/reports/{id}/sections/{n}/xlsx` | `assets.read` | Download table `n` as an Excel workbook. |
| `GET` | `/api/v1/reports/{id}/xlsx` | `assets.read` | Download every table and chart as an Excel workbook, a sheet each. |
| `GET` | `/api/v1/reports/{id}/export.pdf` | `assets.read` | Download the report as a PDF. Takes `page_size` (`letter` or `a4`), `orientation` (`portrait` or `landscape`), `photos` and `labels`. |
| `GET` | `/api/v1/assets/export.pdf` | `assets.read` | Download the asset list as a PDF, with the CSV export's filters, `columns` (the export's, separated by commas), and the same options. |
| `GET` | `/api/v1/assets/{id}/sheet.pdf` | `assets.read` | Download an asset's sheet. Takes `page_size` and `orientation`. |
| `GET` | `/api/v1/assets/sheets.pdf` | `assets.read` | Download a sheet for each asset in the list, with its filters, up to 500 (`400` beyond). |
| `GET` | `/api/v1/assets/stocktake.pdf` | `assets.read` | Download a stocktake checklist of the list, with its filters. |
| `GET` | `/api/v1/settings/documents` | `assets.read` | The instance's name on PDFs, and their page size: `{"instance_name", "page_size"}`. |
| `PUT` | `/api/v1/settings/documents` | `settings.manage` | Set them, with the same body. |

The result of a run gives how many assets matched and, for each section, its headings and its
groups of rows. Each row has its asset's ID and its cells.

A section's CSV is written as the asset export writes its file ([CSV](csv.md)): with a byte order
mark, and with formula-like cells defused. It has a first `Group` column when the table is
grouped, and the file is named after the report.

A report naming a field deleted since it was saved answers `409`, asking to be edited.

## Storage

Saved reports were added by migration `017`. Scan templates are reports too, of kind `scan`
([Scanning](scanning.md)).

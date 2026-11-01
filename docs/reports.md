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

### Sections

A report has 1 to 20 sections.

### Tables

A table has 1 to 30 columns, each a `heading` and a `binding`.

- `sort_by` names a column, counting from 0. Numbers sort as numbers, and text sorts ignoring case.
  `descending` reverses the order.
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

The result of a run gives how many assets matched and, for each section, its headings and its
groups of rows. Each row has its asset's ID and its cells.

A section's CSV is written as the asset export writes its file ([CSV](csv.md)): with a byte order
mark, and with formula-like cells defused. It has a first `Group` column when the table is
grouped, and the file is named after the report.

A report naming a field deleted since it was saved answers `409`, asking to be edited.

## Storage

Saved reports were added by migration `017`. Scan templates are reports too, of kind `scan`
([Scanning](scanning.md)).

# Data model

BAM! keeps track of assets: the things you own, lend, use up and label. Each asset belongs to one
or more categories, carries values for the fields those categories define, has codes that
barcodes and QR codes resolve to, and can be linked to other assets, such as the bin it sits in.
This page describes those pieces, how they relate, and how BAM! stores them.

Who may see and change each piece is covered in [Access](access.md).

## Overview

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

- **Fields** live in one global corpus. Categories pick fields from it, and assets store a value
  for each field they have, as text.
- **Categories** form a single-parent tree. An asset can belong to several categories at once.
- **Label templates** belong to categories (see [Labels](labels.md)).
- **Codes** (generator IDs) belong to assets, so that scanning a label finds the asset.

## Fields

Every field is defined once, in a global corpus, and reused by any category that needs it. A
field has a key, a label and a validation type: `text`, `number`, `date`, `url` or `select`.

A `select` field offers a fixed set of choices, and may name one of them as the default. Each
choice's value must be unique, and a default, if present, must be one of the choice values.

On the Fields page you give each choice a label only. BAM! makes the choice's value from the
label, the same way it makes field keys, and shows it under the label with **Change** to set a
different one. A value that is already stored never changes by itself.

## Categories

Categories form a single-parent tree: each category has at most one parent, and a category with
none is a root. A category can be:

- **Consumable**, counting stock that gets used up, with an optional low-stock threshold (see
  [Stock and lending](stock-and-lending.md)).
- **Running low**, a flag set by hand or by the threshold.
- **Colored**, with a hex color.

Each category chooses fields from the corpus and marks each one required or optional.

### Inheritance

A category inherits every field of its parent, its parent's parent, and so on up to the root.

An asset can belong to several categories at once. It has the union of the fields defined across
all of its categories and all of their ancestors. When a field is required in one of those
categories and optional in another, required wins.

### Uncategorized

An asset removed from every category goes into the virtual system category **Uncategorized**.
That category hides itself in navigation lists while it holds no assets.

An asset never loses its field values when it leaves a category. Values for fields it no longer
has stay stored.

### Duplicating a category

The category page's **Duplicate** copies the category as saved, beside the original under the
same parent. A copy is named "<name> copy" unless you give it a name. The copy takes:

- its color, and its consumable, lendable, unit and threshold settings;
- its own fields, and which of them are required;
- the link types it suggests;
- the actions it offers, and whether they cascade;
- its scopes;
- its scan and label templates. These are the same templates, shared, not copies of them.

The copy does not take the category's subcategories or assets, nor a running-low flag set by
hand. A copy with a threshold flags itself as running low, being empty, as any new category
does.

The scopes are copied even when the person duplicating has no `users.manage` and so could not set
scopes themselves. Without them, the copy would put its assets where the original's scopes keep
them from being seen.

**Uncategorized** cannot be copied.

## Assets

An asset is identified by a random UUIDv4 and records when it was created, last updated and
archived. It may count its stock by quantity, in a unit, with its own low-stock threshold and
running-low flag (see [Stock and lending](stock-and-lending.md)). It may have an icon of its own,
drawn in place of its categories' icons (see [Icons](icons.md)).

### Archiving and restoring

Archiving takes an asset out of use without deleting it. An archived asset cannot be edited, used
or restocked, and its codes are released so other assets can take them.

Archived assets are listed apart from the rest, most recently archived first, with the same
category and search filters. Search finds them by name only, since their codes are released.

Restoring an asset brings it back with the codes that archiving released, except any that another
asset has taken since. The restore response lists those as `skipped_codes`. Assets archived
before BAM! recorded the codes it released come back with no codes (see
[Stock and lending](stock-and-lending.md)). Restoring counts toward low-stock thresholds as
creating does.

An asset that holds active assets through a location link cannot be archived until they are
moved out (`409`). See [Associations](#associations).

### Duplicating an asset

The asset page's **Duplicate…** opens the new-asset form (`/assets/new?like={id}`), filled in
from the asset, so you change only what differs. The draft takes:

- its name;
- its categories (but not Uncategorized), and the values of the fields they give it;
- its stock, including the quantity;
- its own icon, while the build still draws asset icons;
- for each of its codes, the next code in that code's own series. After `SP-014` comes `SP-015`,
  however high the `HS-` codes go. A code that does not end in a number gets no successor.
- its links, each with its type, label, other asset, direction, and whether it is a location.

In the form, where the asset is goes under **Where is it?**. The other links are listed, and each
is made when the asset is created unless you untick it. The photos are listed the same way, and
copied unless unticked.

A link is left out of the draft when:

- the copy taking it would take the other asset away from the original. A single type's far end
  can have only one such link (a box contains a thing, and a thing is in one box).
- the other asset is archived.
- the person duplicating may not change the other asset.

Condition, loans and history are not copied: the new asset starts its own. Photos are not part of
the draft itself; they are copied when the new asset is created.

### Adding assets from elsewhere

The new-asset form starts from wherever you opened it, as its query string says. These options
combine:

| Query | Opened from | Effect |
|---|---|---|
| `?like={id}` | an asset page's **Duplicate…** | Fills the form in from that asset, as above. |
| `?in={id}` | an asset page's **Add something inside…**, where a link type makes locations | Puts the new asset in that asset. |
| `?category={id}` | a category page's **Add an asset to it** | Puts the new asset in that category. |
| `?link={type id}:{out\|in}&to={id}` | the **Links** panel's **New asset…** | Links the new asset to that asset, read from the new asset: `out` by the type's name, `in` by its inverse, as the link chosen there would read. |

A location link read by its inverse says where the new asset is. Each link is made after the asset
is created, as the one under **Where is it?** is. **Cancel** goes back to where the form was
opened.

### Creating several in a row

**Create and add another** does what **Create asset** does: it creates the asset, links it, and
queues its label. Then it keeps the form as it was, ready for the next one:

- each code is replaced by the next in that code's series, as `GET /api/v1/assets/{id}/copy`
  suggests from the asset just made;
- the name is selected, so you can type over it;
- the assets made so far are listed, linked.

### Photos

An asset can have up to 10 photos, in order. The first is shown beside the asset in lists.

You can upload a JPEG or PNG of up to 15 MB, at most 12,000 pixels on a side and 50 megapixels.
BAM! decodes every photo and encodes it again, so it keeps none of its file's metadata, such as
where it was taken. A JPEG is first turned upright, as its EXIF orientation says. Photos are
scaled down to 2048 pixels on their longer side, with a 320-pixel thumbnail.

You can also copy another asset's photos onto this one. The copies go after this asset's own
photos, in their order, as files of their own, so removing one leaves the other. Either all of
them are copied or none are: if that would make more than 10 photos, the request fails with
`409`.

Adding and removing photos is recorded in the asset's history. An archived asset's photos cannot
be changed.

### Condition

An asset's condition says whether it can be used:

- `ok`
- `needs_maintenance`
- `out_of_service`

A condition other than `ok` comes with a note of up to 255 characters saying what is wrong. An ok
asset keeps no note.

Reporting a problem needs `assets.report` or `assets.write`. Marking an asset `ok` again needs
`assets.write`. An archived asset's condition cannot be changed.

Each change records an `asset.condition_changed` event, with the condition before and after and
the note. Notification channels can subscribe to it (see [Notifications](notifications.md)).
Setting the same condition and note again records nothing.

The asset list filters on condition with `?condition=`: one of the three conditions, or `problem`
for any asset that is not ok. The home page lists the assets that need attention.

### Codes

An asset can carry codes for barcodes and QR codes: custom, human-readable codes, or short
sequences that BAM! allocates. Each code belongs to a generator type and is checked against that
type's limits:

| Generator type | Key | Allowed codes |
|---|---|---|
| Micro QR | `micro_qr` | Constrained by physical matrix bounds and version capacity (M2–M4). Custom IDs use only the QR alphanumeric character set (`0-9`, `A-Z`, space, `$%*+-./:`) and are capped at the largest symbol's (M4-L) payload: 35 characters for purely numeric codes, 21 otherwise. |
| Standard QR | `qr` | Arbitrary UTF-8 payloads up to 2953 encoded bytes (version 40-L byte mode), and dynamic URL templates (`{base_url}/a/{asset_uuid}` or a custom slug). |
| Code 128 | `code128` | Printable ASCII (32–126) only, at most 80 characters, so labels stay scannable. |

The pair of generator type and code must be unique across all active assets, so scanning any
generated code resolves to a single asset. Archiving an asset releases its codes.

Codes are compared exactly. `Drill` and `drill` are different codes, and so are `é` written as
one character and `e` followed by a combining accent, on every database (see
[Stored text](#stored-text)).

#### Next code

The asset form's **Assign the next code** asks BAM! to suggest a code. The suggestion is one more
than the highest code of that type that ends in a number, keeping its prefix and zero-padding and
skipping any code already taken: after `HS-105` comes `HS-106`, and after `BIN-0099` comes
`BIN-0100`. When no code of that type ends in a number, the suggestion is empty (`""`).

## Associations

Assets link to one another: a bin contains screws, and a drill is used with its bits. Links also
say where things are.

### Relation types

Relation types are a shared list, like the field corpus. Each type has:

- a **name**, read from the first asset (`contains`);
- an **inverse name**, read from the second asset (`contained in`);
- **symmetric**, for a type that reads the same both ways (`used with`). Its inverse name is its
  name.
- **single**, when the second asset can have only one such link (a thing is in one place).
- **location**, for a type whose links say where things are. A location type must be single.

Names and inverse names are up to 64 characters. BAM! seeds three types: `contains` (a location
type), `has part` and `used with`.

Names are also written as verbs in CSV files (see [CSV](csv.md)): lowercased, with each run of
characters other than letters and digits made `_`, so `contained in` becomes `contained_in`. A
name whose verb another type already has is refused (`409`), and so is a name with no letters or
digits.

While links use a type, only its names can change, and it cannot be deleted (`409`, with how many
links use it).

A category can suggest relation types. An asset gathers the suggestions of its categories and
their ancestors, and the web app offers those types first.

### Links

A link is stored once and reads correctly from both assets. You make a link from one asset,
reading it from that asset's side: outgoing by the type's name ("this bin contains those
screws"), or incoming by its inverse ("these screws are contained in that bin").

For a single type, linking the second asset to something new moves it, ending its old link. BAM!
refuses:

- a link that would make a loop of a single type, such as a bin inside its own contents;
- an asset linked to itself;
- a link that already exists, either way round for a symmetric type;
- links to archived assets.

An asset's page lists its links, and its location: the chain of containers through location
links, outermost first (Shelf 2 › Bin A3).

Adding or removing a link records an `asset.edited` change on both assets (`"what": "link"`),
with the label read from that asset and the other asset's name. A move shows on the thing moved, on its old container
and on its new one.

An asset that holds active assets through a location link cannot be archived until they are
moved out (`409`). An archived asset keeps its links, marked as archived, so restoring it puts it
back where it was.

### Where things are

In the web app:

- the asset form's **Where is it?** finds the asset this one is in, and links it there when saved,
  moving it from wherever it was;
- an asset's page always has a **Where** row, with **Move**;
- an asset that holds others lists them under **Contents**;
- the asset list has a **Where** column, showing the place each asset is directly in.

## Stored text

Text behaves the same on every database, whatever the server's defaults.

### Rules

- Text must be valid UTF-8 and may not contain a NUL character, since PostgreSQL cannot store one.
- Names, labels, choices, codes and email addresses hold up to 255 characters.
- Field values hold up to 10,000 characters.
- Lengths count characters, not bytes.

Text that breaks these rules is refused with `400`, or, in an import, reported as a problem
against its row and column. It is never cut short or altered.

Uploaded image names are the exception: they are cleaned instead. Control characters and invalid
bytes are dropped, and the name is cut to 255 characters.

A request with a NUL character, or bytes that are not UTF-8, in its path, its query or any string
of a JSON body is refused with `400` before it reaches a handler, since it cannot name anything
stored.

### Comparison

Comparison is exact on every database. `Drill` and `drill`, or `é` written as one character and
as `e` with a combining accent, are different codes and different channel names.

### Search

Search (`search` on the asset list, the export and reports) finds the term anywhere in:

- an asset's name;
- its codes;
- the name of a place it is in, at any depth. "shelf a" finds what is on Shelf A, and what is in
  the boxes on it.

Search ignores case for every letter, not just ASCII. `%`, `_` and `\` have no special meaning.
Places count only as far as the requester may read them, so an asset is never found by the name
of a place hidden from them.

## Databases

PostgreSQL databases must use the `UTF8` encoding. BAM! refuses to start on any other, including
`SQL_ASCII`.

MariaDB needs nothing set up. BAM! creates its tables in `utf8mb4` with the exact collation
`utf8mb4_nopad_bin`, whatever the database's default. It sets each connection's character set,
SQL mode (strict) and time zone (UTC) itself. Migration `008` converts tables created by earlier
versions, which took the database's default (often `latin1`, with a case-insensitive collation).

## API

| Method and route | Permission | Purpose |
|---|---|---|
| `POST /api/v1/categories/{id}/clone` | `categories.manage` | Duplicate a category. Optional body `{"name"}`. Answers `201` with the copy. |
| `GET /api/v1/categories/{id}/relation-types` | `assets.read` | The relation types a category suggests. |
| `PUT /api/v1/categories/{id}/relation-types` | `categories.manage` | Set them, with `{"relation_type_ids": [...]}`. |
| `DELETE /api/v1/assets/{id}` | `assets.delete` | Archive an asset and release its codes. |
| `GET /api/v1/assets?archived=true` | `assets.read` | List archived assets. |
| `POST /api/v1/assets/{id}/restore` | `assets.delete` | Restore an archived asset. Answers `skipped_codes`. |
| `GET /api/v1/assets/{id}/copy` | `assets.write` | A draft of a new asset like this one, as `copy`. |
| `POST /api/v1/assets/{id}/photos` | `assets.write` | Add a photo, as the `file` part of a multipart form. |
| `GET /api/v1/assets/{id}/photos/{photo_id}` | `assets.read` | Serve a photo; `?size=thumb` serves its thumbnail. |
| `DELETE /api/v1/assets/{id}/photos/{photo_id}` | `assets.write` | Remove a photo. |
| `PUT /api/v1/assets/{id}/photos` | `assets.write` | Order the photos, with `{"photo_ids": [...]}`, naming each once. |
| `POST /api/v1/assets/{id}/photos/copy` | `assets.write` on this asset, `assets.read` on the other | Copy another asset's photos, with `{"from_asset_id": "..."}`. |
| `PUT /api/v1/assets/{id}/condition` | `assets.report` or `assets.write` to report a problem; `assets.write` to mark `ok` | Change the condition, with `{"condition", "note"}`. |
| `GET /api/v1/generators/{type}/next-code` | `assets.write` | Suggest the next code, as `code`. |
| `GET /api/v1/relation-types` | `assets.read` | List relation types. |
| `POST /api/v1/relation-types` | `categories.manage` | Create a relation type. |
| `PUT /api/v1/relation-types/{id}` | `categories.manage` | Change a relation type. |
| `DELETE /api/v1/relation-types/{id}` | `categories.manage` | Delete a relation type no link uses. |
| `POST /api/v1/assets/{id}/links` | `assets.write` | Link to another asset, with `{"type_id", "asset_id", "outgoing"}`. |
| `DELETE /api/v1/assets/{id}/links/{link_id}` | `assets.write` | Remove a link. |

The asset list (`GET /api/v1/assets`) also takes the category and search filters, and
`?condition=`.

`GET /api/v1/assets/{id}` returns, among the rest:

- `condition` and `condition_note`;
- `links`, each with the `label` read from this asset, the other asset, and whether it is
  `outgoing`;
- `location`, the chain of containers, outermost first;
- `suggested_relation_type_ids`, gathered from its categories and their ancestors.

The copy draft's `links` give each link's type, label, other asset, direction and whether it is a
location.

## Storage

| Table | Holds | Columns |
|---|---|---|
| `fields` | The field corpus | `id` (UUID), `field_key` (snake_case; `key` in the API), `label` (string), `validation_type` (string: `text`, `number`, `date`, `url` or `select`), `options` (JSON, nullable) |
| `categories` | The category tree | `parent_id` (another category, or `NULL` for a root), `is_consumable` (bool), `is_running_low` (bool), `color_hex` (string), `low_stock_threshold` (int, optional, for consumables) |
| `category_fields` | Which fields each category has | `category_id`, `field_id`, `is_required` |
| `asset_categories` | Which categories each asset is in | |
| `assets` | Assets | `id` (UUIDv4), `created_at`, `updated_at`, `archived_at`, `stock_quantity`, `stock_unit`, `low_stock_threshold`, `is_running_low`, `icon_name` |
| `asset_field_values` | Field values, stored as text, including values orphaned when an asset leaves a category | |
| `asset_generator_keys` | Asset codes | `asset_id` (UUID), `generator_type` (string: `"qr"`, `"micro_qr"` or `"code128"`), `assigned_code` (string, unique per `generator_type`) |
| `relation_types` | Relation types | `name`, `inverse_name`, `is_symmetric`, `is_single`, `is_location` |
| `asset_links` | Links between assets | |

For the `select` validation type, `options` holds the choices and an optional default:

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

`asset_generator_keys` maps compact, deterministic or custom constrained strings to an asset. The
compound key `(generator_type, assigned_code)` is unique across all active assets.

Photo files live under `storage.local_path/photos/` (see [Configuration](configuration.md)).

Changes to links serialize on the `asset_links` lock.

### Migrations

| Migration | Change |
|---|---|
| `008` | Converts MariaDB tables created by earlier versions to `utf8mb4` with `utf8mb4_nopad_bin`. |
| `013` | Asset condition. |
| `015` | Asset links and relation types. |
| `018` | Asset photos. |

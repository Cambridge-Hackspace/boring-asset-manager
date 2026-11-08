# Vendors

A **vendor** is a supplier a space buys from, and how to reach it. Vendors are one list per
instance, shared out with scopes as categories' assets are. This is the core of #216: where each
asset came from (#218), what each vendor sells (#219), contracts (#221) and
[purchase orders](purchase-orders.md) (#222) build on it.

## What a vendor has

| Field | What |
|---|---|
| `name` | Required, up to 255 characters. Names need not be unique. |
| `website` | Empty, or an `http://` or `https://` address with a host, up to 2048 characters. It is where things are reordered, and shown as a link, so no other scheme is taken. |
| `account_number` | The space's account with the vendor, up to 255 characters. |
| `address` | Up to 1000 characters. |
| `notes` | Markdown, up to 20000 characters (see [Notes](#notes)). |
| `contacts` | Up to 50, in order, each with a `name`, a `role` (such as `sales` or `support`), an `email` and a `phone`. A contact needs at least a name, an email or a phone; an email must be an address. |
| `scope_ids` | The scopes it is in (see [Who sees a vendor](#who-sees-a-vendor)). |

Text is trimmed and held to the [rules for stored text](data-model.md#stored-text). A vendor also
has [attachments](data-model.md#attachments), such as price lists and terms, and a history.

### Notes

Notes are Markdown, rendered by the web app's own small renderer (`web/src/lib/markdown.ts`).
[Comments](comments.md#rendering) are rendered on the server instead, with tables, images and task
lists. The web renderer knows headings, paragraphs, lists, bold, italics, code and links, and
nothing else:

- All text is escaped, so HTML in notes shows as typed and never renders.
- A link goes only to a page of the app (a path starting with one `/`), to an `https://` address,
  or to a `mailto:` address. Anything else, such as `javascript:`, `data:` or `//elsewhere`, shows
  as its label alone.

## Who sees a vendor

A vendor's scopes decide who sees it, as a category's scopes decide who sees its assets (see
[Access](access.md#scopes)):

- Someone sees a vendor if they hold `assets.read`, or `vendors.manage`, in one of its scopes.
- A vendor in no scope is seen by everyone who holds `assets.read` anywhere.
- The `public` role holds `public:assets.read` from the start, so a vendor in the `public` scope
  is seen by everyone signed in. Vendors are part of the catalog, so signed out, as for
  categories, the vendor routes answer `401`.

A vendor someone cannot see answers `404`, as if it did not exist, and is left out of lists, of
the vendors categories name, and of packages.

The `default` scope, where assets in no other scope are, cannot be put on a vendor (`409`): a
vendor meant for everyone is left in no scope instead. A scope on a vendor cannot be deleted
(`409`), since taking it off would show the vendor to everyone.

Which scopes a vendor is in is shown only to those who manage users, categories or vendors.

## Who changes a vendor

`vendors.manage` creates, edits and deletes vendors, and changes their attachments, and their
[contracts](contracts.md#who-changes-a-contract). It is scopable:

- Changing a vendor needs `vendors.manage` in one of the scopes it is in, and in one of those it
  ends up in.
- A vendor in no scope is seen by everyone, so making one, changing one, or taking one out of its
  last scope needs `vendors.manage` in every scope.
- So a holder of `tools:vendors.manage` makes vendors in `tools`, and cannot move them elsewhere.

Its holders also read `GET /api/v1/scopes`, to choose among them. Migration `068` grants
`vendors.manage`, in every scope, to every role that manages categories.

A vendor with contracts is not deleted (`409`): delete them, or move them to another vendor,
first. Nor is one with [purchase orders](purchase-orders.md) (`409`): delete them first.

## Preferred vendors

A category can name the vendors it is bought from, in order of preference
(`PUT /api/v1/categories/{id}/vendors`, with `categories.manage`). Each category carries them as
`vendor_ids`, leaving out those the requester cannot see; setting them names only vendors the
requester sees, and keeps after them those it does not.

The running-low list on the home page shows each running-low category's vendors, with a link to
reorder: the vendor's website, or its page when it has none. It shows the
[preferred catalog entry](#catalogs) above them. The phone's shopping list (#214) will show them the
same way once it exists.

Duplicating a category copies its vendors. Deleting a vendor takes it off every category.

## A vendor's page

The **Vendors** page, in **Catalog**, lists the vendors one can see, with a search box. A
vendor's page shows its details, contacts and notes, its [catalog](#catalogs), the assets
bought from it, its contracts, its attachments, its history, its scopes for those who see them,
and the categories bought from it.
## Catalogs

A vendor's **catalog** is what it sells (#219), one **entry** for each thing. The scope is kept
small on purpose: prices are typed in, never fetched from vendors' sites or APIs, and only the last
price checked is kept.

| Field | What |
|---|---|
| `vendor_id` | The vendor that sells it. |
| `category_id` or `asset_id` | What it is for: a category, for things counted by items (a box of screws is an asset of the category *Screws*), or an asset, for things counted by quantity (a reel of solder, whose stock is a number). One of them, never both. |
| `part_number` | The vendor's part number, up to 255 characters. An entry needs a part number or a description. |
| `description` | Up to 2000 characters. |
| `url` | The product page: empty, or an `http://` or `https://` address with a host, as a vendor's website. |
| `pack_size`, `pack_unit` | What a pack holds, such as `100` `screws`: a whole number of at least 1, or none, and up to 64 characters. |
| `price`, `currency`, `price_checked` | The price of a pack, kept exactly as a purchase's is (see [Purchases](data-model.md#purchases)), and when it was last checked, `YYYY-MM-DD`. A price needs its currency. |
| `preferred` | The one to reorder from. |

Answers also carry `vendor_name`, `linked_name` (the category's or asset's name) and `can_manage`.

### Several vendors, one preferred

A category or an asset may have entries from several vendors, and several from one vendor (in
different packs, say). At most one of them is **preferred**: saving one as preferred makes the
others not. The running-low list on the home page shows the preferred entry of each running-low
category and asset: its vendor, part number, price and pack, and a **Reorder** link to its product
page, or else its vendor's website, or else the vendor's page here.

This is apart from a category's [preferred vendors](#preferred-vendors), its list of vendors in
order, which the running-low list still shows below it.

### Who sees and changes entries

- An entry is seen by those who see its vendor and what it is for: any category, or an asset they
  may read. Others never learn of it.
- Its price, currency and when it was checked need `purchases.read` in one of the vendor's
  scopes, or anywhere for a vendor in none, as seeing the vendor is decided (see
  [Seeing prices](access.md#seeing-prices)). Without it they are left out, and the entry says
  `"prices_hidden": true`.
- Changing an entry takes `vendors.manage` for its vendor, as changing the vendor does, and for the
  vendor it moves to. An edit by someone who cannot see its price keeps the price.

### Where it shows

- A vendor's **Catalog** tab lists its entries, by what they are for. Those who manage the vendor
  add, edit and remove them, and export and import the catalog as [CSV](csv.md#vendor-catalogs).
- A category's **Catalog** tab, and the page of an asset counted by quantity, list what each vendor
  sells of it, and offer to make one preferred.
- Entries go with their vendor, and with their category or asset. Archiving an asset keeps its
  entries. Duplicating a category or an asset copies none.
- Each change is recorded in the vendor's history as `catalog`, naming the entry by its part number
  (or description) and what it is for, never its price, since the history is shown to those who
  may not see prices.

## Assets bought from it

An asset's [purchase details](data-model.md#purchases) name the vendor it was bought from. The
vendor's **Assets** part lists them, by name, with when each was ordered (or received), its price
and when its warranty ends. It lists only the assets the viewer may read, and their prices only
where they hold `purchases.read` (see [Access](access.md#seeing-prices)).

Deleting a vendor forgets it as the vendor of every asset bought from it; the rest of their
purchase details stay. An asset's vendor is shown only to those who see the vendor: to others, it
has none, and their edits leave it as it is.

## History

Making a vendor records a `vendor.created` event, and each change a `vendor.edited` event, with the
changes:

| `what` | `from` and `to` |
|---|---|
| `name`, `website`, `account_number`, `address` | The values. |
| `notes` | Neither: notes may be long, so the history says only that they changed. |
| `contact` | The contact as `name (role)`, keyed by its ID: `to` alone when added, `from` alone when removed, both when changed. |
| `scope` | The scope's name, keyed by its ID. |
| `photo`, `attachment` | Attachments added and removed, as an asset's. |

The events name the vendor in `vendor_id`, and are for its history only: no notification channel
or live connection receives them. Who made each change is shown to those who hold `assets.track`
in one of the vendor's scopes, or anywhere for one in none.

## API

| Method and route | Permission | Purpose |
|---|---|---|
| `GET /api/v1/vendors` | `assets.read` | The vendors the requester sees, by name, as `vendors`. `?q=` keeps those with every word of it in their name, website, account number, address, notes, or a contact's name, role, email or phone, regardless of case. |
| `POST /api/v1/vendors` | `vendors.manage` | Make one, with `{"name", "website", "account_number", "address", "notes", "contacts", "scope_ids"}`. Answers `201` with `vendor`. |
| `GET /api/v1/vendors/{id}` | `assets.read` | One, as `vendor`, with `can_manage`, whether the requester may change it. |
| `PUT /api/v1/vendors/{id}` | `vendors.manage` | Replace its details and contacts. A contact sent back with its `id` keeps it. `scope_ids` left out stay as they are. |
| `DELETE /api/v1/vendors/{id}` | `vendors.manage` | Delete it, with its contacts and attachments' rows. Its files stay until `bam storage sweep`. One with contracts answers `409`. |
| `GET /api/v1/vendors/{id}/history` | `assets.read` | Its history, newest first, 50 at a time, as an asset's: `?before=` an event's ID for the next page; answers `events` and `more`. |
| `GET`, `POST`, `PUT /api/v1/vendors/{id}/attachments`, and `GET`, `PUT`, `DELETE /api/v1/vendors/{id}/attachments/{attachment_id}` | Seeing it to read, `vendors.manage` in its scopes to change | Its attachments, as a category's (see [Attachments](data-model.md#attachments)). |
| `PUT /api/v1/categories/{id}/vendors` | `categories.manage` | Name the vendors a category is bought from, with `{"vendor_ids": [...]}` in order of preference. Answers `category`. |
| `GET /api/v1/assets?vendor_id={id}` | `assets.read`, and seeing the vendor | The assets bought from it that the requester may read, as the asset list (`404` for a vendor they cannot see). |
| `GET /api/v1/catalog` | `assets.read` | The catalog entries the requester sees, as `entries`, by what they are for, then vendor. `?vendor_id=`, `?category_id=` and `?asset_id=` narrow them, and `?preferred=true` keeps the preferred ones. |
| `POST /api/v1/catalog` | `vendors.manage` for its vendor | Make one, with every field above. Answers `201` with `entry`. |
| `GET /api/v1/catalog/{id}` | `assets.read` | One, as `entry`. |
| `PUT /api/v1/catalog/{id}` | `vendors.manage` for its vendor, and the one it moves to | Replace it, every field. |
| `DELETE /api/v1/catalog/{id}` | `vendors.manage` for its vendor | Delete it. |
| `GET /api/v1/vendors/{id}/catalog.csv` | `assets.read`, and seeing the vendor | Its catalog as CSV (see [CSV](csv.md#vendor-catalogs)). |
| `POST /api/v1/vendors/{id}/catalog.csv` | `vendors.manage` for it | Import its catalog from CSV, or a workbook's sheet: with `check=true`, only what it would do. |

## Everywhere else

- **MCP:** `list_vendors`, `get_vendor`, `save_vendor`, `delete_vendor`, `vendor_history` and
  `set_category_vendors`; the attachment tools take `vendor_id`. Catalogs have `list_catalog`,
  `get_catalog_entry`, `save_catalog_entry`, `delete_catalog_entry`, `export_vendor_catalog` and
  `import_vendor_catalog` (see [MCP](mcp.md)).
- **Backups:** vendors are part of the catalog, with categories' catalog entries; assets' entries
  are part of the assets (see [Backups](backups.md#kinds-of-information)).
- **Packages:** vendors can be chosen, and a category brings the vendors it is bought from and
  those that sell it, with their entries; assets bring those that sell them (see
  [Packages](packages.md)).
- **CSV:** categories have no CSV import or export of their own, so neither do vendors' details.
  Assets' `vendor` column names theirs, by name, and a vendor's catalog is read and written as CSV
  of its own (see [CSV](csv.md#vendor-catalogs)).
- **Reports:** `vendor` filters by it, and `{{asset.purchase.vendor}}` shows it (see
  [Reports](reports.md)).
- **Packages:** an asset brings the vendor it was bought from.
- **Contracts:** warranties, service and maintenance agreements, leases and licenses are with a
  vendor, and follow its scopes; its page has a **Contracts** tab (see [Contracts](contracts.md)).
- **Purchase orders** are to a vendor, follow its scopes, and draw their lines from its catalog;
  its page has an **Orders** tab, and the running-low list starts orders from preferred entries
  (see [Purchase orders](purchase-orders.md)).

## Storage

Migration `068` adds `vendors`, `vendor_contacts`, `vendor_scopes` and `vendor_attachments`, and
`category_vendors` for categories' preferred vendors, on every database. Migration `073` adds
`category_catalog_entries` and `asset_catalog_entries`, alike but for what they are for, so that
each goes with its kind of information in a backup. Their IDs are unique across both. It adds `vendor_id` to
`events`, which, like `category_id` and `asset_id`, is deliberately not a foreign key, so a
vendor's events read as they were after it is deleted.

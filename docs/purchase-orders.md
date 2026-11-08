# Purchase orders

A **purchase order** is an order to a [vendor](vendors.md): lines drawn from the vendor's
[catalog](vendors.md#catalogs) or written freely, with how many packs of each and what a pack
costs. It is placed with the vendor, sent as a PDF, and received line by line, in as many
deliveries as it takes. Receiving restocks what is counted by quantity, and creates the assets of
what is counted by items, with their [purchase details](data-model.md#purchases) filled in. This
is part of #216, after vendors (#217), purchases (#218), catalogs (#219) and PDF exports (#206).

Approvals, budgets and accounting integrations are out of scope, as are emailing an order to its
vendor, ordering through vendors' sites, and changing the catalog from an order.

## What an order has

| Field | What |
|---|---|
| `number` | Its own whole number, one more than the highest when it is made, shown as `reference` (see [Numbers](#numbers)). A deleted order's number can come back only if it was the highest. |
| `vendor_id` | Required: the vendor it is to. Its scopes decide who sees the order (see [Who sees an order](#who-sees-an-order)). |
| `state` | `draft`, `ordered`, `partly_received`, `received` or `canceled` (see [States](#states)). |
| `currency` | The ISO 4217 currency of all its prices: one for the order, as on the vendor's invoice. |
| `vendor_reference` | The vendor's order or confirmation number, up to 255 characters. |
| `ordered_on`, `expected_on`, `received_on` | Dates, `YYYY-MM-DD`: when it was placed, when it is expected, and when the last of it came. |
| `deliver_to` | Where it is delivered, up to 1000 characters. A new order starts with the newest order's. |
| `notes` | Markdown, up to 20000 characters, rendered as a vendor's are, and printed on the order. |
| `lines` | Up to 500 (see below). |
| `total` | In answers only: the sum of its lines' totals, where the requester sees prices. |

It also has [attachments](data-model.md#attachments), such as the vendor's quote, confirmation and
invoice, and a history.

### Lines

| Field | What |
|---|---|
| `catalog_entry_id` | The catalog entry it was drawn from, while it exists. |
| `category_id` or `asset_id` | What receiving it acts on: a category it makes assets in, or an asset counted by quantity it restocks. One of them, or neither. |
| `part_number`, `description` | Its text: a part number, a description, or both. |
| `pack_size`, `pack_unit` | What a pack holds, such as `100` `screws`. For an asset's line, how much one pack restocks it by. |
| `quantity` | Packs ordered, from 1 to 1,000,000. |
| `quantity_received` | Packs come so far. |
| `unit_price` | A pack's price, kept exactly as a [purchase's price](data-model.md#purchases) is, in the order's currency. |
| `total` | In answers only: `quantity` times `unit_price`. |

So a line is one of three kinds:

1. **From the catalog.** It names an entry, and takes the entry's category or asset, and whatever
   text and pack it is not given. A new line takes the entry's price when it is in the order's
   currency, and a new order with no currency takes the entry's; an entry priced in another
   currency comes without its price.
2. **Free, for something kept**, naming a category or an asset but no entry.
3. **Free text**, such as delivery charges: receiving it only counts it in.

Deleting an entry leaves its lines as free lines for the same thing; deleting a category or an
asset leaves its lines as free text. A line's text is its own once added, so an order reads as it
was sent after the catalog changes. A line's or order's total beyond fifteen digits is refused.

## States

```
draft ──place──▶ ordered ──receive some──▶ partly_received ──receive the rest──▶ received
                    │  └──────────────────receive all────────────────────────────────▲
                    └──cancel──▶ canceled ◀──cancel── partly_received
```

- **Placing** (`POST …/place`) marks a draft as sent to the vendor. It needs a line, and sets
  `ordered_on`, today unless given.
- **Receiving** makes an order `partly_received` or `received`, as anything is still to come or
  not. So does changing its lines: ordering only what came of the last line receives it.
- **Canceling** (`POST …/cancel`) stops an order placed or partly received: what came stays, and
  the rest is not expected. It is final. A draft is not canceled, but deleted, or left unplaced.
- **Deleting** works in any state, for those who manage the vendor. What it received stays
  received, and the assets' histories still name it. A vendor with orders is not deleted (`409`):
  delete them first.
- Nothing returns an order to draft, reopens a canceled one, or undoes a receipt: a receipt
  wrongly made is put right on the assets, as any change to stock is.

What can change in each state:

| State | What changes |
|---|---|
| `draft` | Everything. |
| `ordered` | All but its vendor and currency: lines added, changed and removed, prices, its dates, reference, delivery address and notes. |
| `partly_received` | As `ordered`, but a line something has come of is not removed, orders at least what came, and stays for what it was for. |
| `received`, `canceled` | Only its reference and notes, and its attachments. |

A change a state does not allow answers `409`, saying why.

## Receiving

`POST /api/v1/purchase-orders/{id}/receive` receives a delivery against an order placed or partly
received, on `received_on` (today unless given), with each line that came:

| Field | What |
|---|---|
| `line_id`, `quantity` | The packs that came, from 1 to what is still to come; more answers `409`. |
| `restock` | An asset's line: how much its quantity goes up; `quantity` times the pack's size unless given, which it must be when the pack's size is not known. |
| `assets` | A category's line: how many assets to make, from 0 to 500; one per pack unless given, as a box of screws is one asset. 0 makes none. |
| `like` | A category's line: an asset to make them like, as **Duplicate…** does: its name, categories, fields, location and other links, stock settings, and the next code in each of its codes' series for each. Attachments, condition and loans are not copied. |
| `name`, `fields` | A category's line made like none: the assets' name (the line's description, part number, or the category's name, unless given) and field values. The category's required fields must be given, as for any new asset. |
| `each` | A category that weighs (with a unit): each asset counts its stock in that unit, holding this much; the pack's size unless given, when the pack is in the category's unit and one asset is made per pack. |
| `warranty_end` | A category's line: when the assets' warranty ends. |

A receipt is all or nothing, every line given or none, in one change under the inventory lock, so
thresholds are rechecked once and `asset.restocked` and `category.restocked` are recorded as for
any change to stock. At most 500 assets are made at once.

- **Restocking** adds to the asset's quantity as `POST /api/v1/assets/{id}/quantity` with
  `adjust` does, recording `asset.quantity_changed` with the trigger `purchase_order`. An archived
  asset, or one not counted by quantity, is refused (`409`).
- **Assets made** record `asset.created` with the trigger `purchase_order`, and have the order's
  purchase details: its vendor, `ordered_on` as their order date, `received_on` as their received
  date, the order's number as shown then as their order number, and the line's price for the
  packs that came, shared between the assets made and rounded half up to the currency's minor
  unit. The price is written whoever receives it: it is the order's.
- **Labels:** the answer lists `created_asset_ids`. The receive page adds them to the
  [print queue](printing.md#the-spool) when **Add the new assets' labels to the print queue** is
  ticked, as the new-asset form does, and remembers the choice; MCP's `receive_purchase_order`
  takes `queue_labels`. Restocked assets are not queued: their bins are labeled already.
- **History:** the order records `purchase_order.received`, with each line's packs, how much it
  restocked by and the assets made, never a price. Every change a receipt makes to assets and
  categories names the order, in `purchase_order_id` and in its payload's `purchase_order`.

## Starting from the running-low list

`GET /api/v1/purchase-orders/suggestions` (`vendors.manage` somewhere) suggests draft orders of
what the requester sees running low:

- each consumable category flagged, and each asset counted by quantity flagged, with a preferred
  catalog entry from a vendor the requester may order from, is a line drawn from it, grouped by
  vendor in `vendors`;
- with enough packs to bring it back to its threshold, at least one: for an asset, the shortfall
  divided by the pack's size, rounded up; for a category counted by items, the shortfall, a pack
  being an asset; for a category that weighs, the shortfall divided by the pack's size when the pack
  is in its unit, else one; and one for something flagged by hand, with no threshold;
- what is already on an open order (`draft`, `ordered` or `partly_received`) is listed apart, in
  `on_order`, with the order, and what has no preferred entry from such a vendor in
  `without_entry`.

The home page's **Running low** list offers **Start orders…** to those who manage vendors, and
says *On order: PO-0012*, or *In draft PO-0013*, beside each thing on an open order, as do a
category's Catalog tab and a stock-counted asset's page. **Create drafts** makes a draft for each
vendor with something ticked. The phone's shopping list (#214) is to show the same.

## Numbers

An order's `number` is kept as a whole number; how it is shown, as `reference`, is the instance's
setting: a **prefix**, `PO-` unless set, and at least how many **digits**, padded with zeros, 4
unless set, so `PO-0012`, wider once it grows past them (`PO-12345`).

- The prefix may be empty, and is up to 16 letters A to Z, digits, hyphens and underscores, so that
  it is safe in a file's name and on a label. Padding is from 0 (none) to 9.
- Those who manage settings set it on **Admin › Import & export**, or with
  `PUT /api/v1/settings/purchase-orders` (`{"prefix", "padding"}`); everyone who reads assets reads
  it with `GET`. It is kept in the instance's settings, so it travels in backups with the
  **settings** kind.
- Changing it changes how every order is shown at once, on its page, its PDF and in events
  recorded after. What was written with the old form stays as it was: assets received already keep
  the order number they were given, and are not rewritten.

## Who sees an order

An order follows its vendor (see [Vendors](vendors.md#who-sees-a-vendor)):

- Whoever sees the vendor sees its orders. An order whose vendor someone cannot see answers `404`,
  and is left out of lists.
- A line for an asset the requester may not read keeps its own text, which is the order's, without
  the asset.
- Its prices, totals and currency need `purchases.read` in the vendor's scopes, or in every scope
  for a vendor in none, as contracts' costs do (see [Seeing prices](access.md#seeing-prices)): an
  order is what the space spends, not a list price. Without it they are left out of answers, the
  PDF, its history's price changes and MCP, and `prices_hidden` says so.

## Who changes an order

- **Making, changing, placing, canceling and deleting** take `vendors.manage` for its vendor, or in
  every scope for a vendor in none, as its catalog and contracts do. A line naming an asset, or
  drawn from an asset's entry, needs the asset to be one the editor reads (`400`). An edit by
  someone who cannot see prices keeps them: lines keep theirs, and new catalog lines take the
  entry's. Moving a draft to another vendor needs it for both.
- **Receiving** is recording stock, where the stock is, not managing the vendor: an asset's line
  needs `stock.record` on the asset; a category's line that makes assets needs `assets.write` in
  the scopes they get from the category, and, made like an asset, `assets.write` on it, linking
  only to assets the receiver may change; any other line needs `stock.record` somewhere. The person
  unpacking the box is seldom the one who chose the vendor.
- **Who made each change** in its history is shown to those who hold `assets.track` in the vendor's
  scopes, or anywhere for one in none, as for vendors and contracts.

No new permission is needed.

## The PDF

`GET /api/v1/purchase-orders/{id}/export.pdf` (seeing it) makes an order a PDF to send, drawn as
[reports' PDFs](reports.md#pdf) are, on the instance's page size unless `page_size` and
`orientation` say otherwise:

- headed with the instance's name, *Purchase order PO-0012* (*Draft purchase order PO-0012* before
  it is placed), and when it was made and by whom;
- to whom: the vendor's name, address, account number and first contact; where to deliver it: the
  instance's name and `deliver_to`; and its number, dates, the vendor's reference and currency;
- its lines: number, part number, description, pack, packs, price a pack and total, with a total
  row, and then its notes.

It leaves out what lines are for here, what came, and its state. Prices are left out, not zeroed,
for someone who cannot see them. Sending it is up to the person.

## History

Making an order records `purchase_order.created`, each change `purchase_order.edited`, placing it
`purchase_order.ordered`, each delivery `purchase_order.received`, and canceling it
`purchase_order.canceled`, with the changes:

| `what` | `from` and `to` |
|---|---|
| `vendor` | The vendors' names, keyed by the new one's ID. |
| `currency`, `vendor_reference`, `ordered_on`, `expected_on`, `deliver_to` | The values. |
| `notes` | Neither: history says only that they changed. |
| `line` | The line as `3 × M5-100 M5 bolts (100 pcs)`, keyed by its ID: `to` alone when added, `from` alone when removed. |
| `price` | A line's price with the currency, keyed by the line, labeled with its text. Only `purchases.read` sees it, and `currency`. |
| `photo`, `attachment` | Attachments added and removed, as a vendor's. |

The events name the order in `purchase_order_id`, which, like `contract_id`, is not a foreign key,
so they read as they were after it is deleted. An order's history lists its own events; the changes
a receipt made to assets are in theirs.

## Notifications

Channels can subscribe to `purchase_order.ordered` (*A purchase order is placed*) and
`purchase_order.received` (*A delivery against a purchase order is received*), each naming the
order's number and vendor, and each line's text and packs, never a price, since channels do not
check `purchases.read` (see [Notifications](notifications.md)). MQTT publishes them under
`<prefix>/<kind>/<order ID>`, and their links go to the order's page. The others are for the
order's history. The events list and live connections, which follow assets and categories, do not
carry order events; the asset events a receipt records reach them as usual.

## API

| Method and route | Permission | Purpose |
|---|---|---|
| `GET /api/v1/purchase-orders` | `assets.read` | The orders the requester sees, newest first, as `orders`. `?vendor_id=`, `?state=` (a state, or `open` for those still to come), `?category_id=` and `?asset_id=` (those with a line for it) and `?q=` (every word in its number, the vendor's reference, the vendor's name or its lines' text) narrow them. |
| `POST /api/v1/purchase-orders` | `vendors.manage` for the vendor | Make a draft, with `{"vendor_id", "currency", "vendor_reference", "ordered_on", "expected_on", "deliver_to", "notes", "lines"}`. Answers `201` with `order`. |
| `GET /api/v1/purchase-orders/suggestions` | `vendors.manage` | Draft orders of what is running low (see above). |
| `GET /api/v1/purchase-orders/{id}` | Seeing it | One, as `order`, with `can_manage`. |
| `PUT /api/v1/purchase-orders/{id}` | `vendors.manage` | Replace what its state lets change. `lines` replace its lines; a line with the `id` of one of them keeps it, and what came of it; `lines` left out stay as they are. |
| `DELETE /api/v1/purchase-orders/{id}` | `vendors.manage` | Delete it, with its lines and attachments' rows. Its files stay until `bam storage sweep`. |
| `POST /api/v1/purchase-orders/{id}/place` | `vendors.manage` | Place a draft, with an optional `ordered_on`. |
| `POST /api/v1/purchase-orders/{id}/cancel` | `vendors.manage` | Cancel it. |
| `POST /api/v1/purchase-orders/{id}/receive` | As in [Who changes an order](#who-changes-an-order) | Receive a delivery. Answers `order`, `created_asset_ids` and `restocked`. |
| `GET /api/v1/purchase-orders/{id}/history` | Seeing it | Its history, newest first, 50 at a time, as a contract's. |
| `GET /api/v1/purchase-orders/{id}/export.pdf` | Seeing it | Its PDF. |
| `GET`, `POST`, `PUT /api/v1/purchase-orders/{id}/attachments`, and `GET`, `PUT`, `DELETE /api/v1/purchase-orders/{id}/attachments/{attachment_id}` | Seeing it to read, `vendors.manage` in its vendor's scopes to change | Its attachments, as a contract's (see [Attachments](data-model.md#attachments)). |
| `GET /api/v1/settings/purchase-orders` | `assets.read` | How numbers are shown: `{"prefix", "padding"}`. |
| `PUT /api/v1/settings/purchase-orders` | `settings.manage` | Set it. |

## The web app

- **Orders**, in **Catalog**, lists the orders one can see, newest first, with their vendor, state,
  dates (one past its expected date marked *overdue*) and total, a state filter and a search box.
- An order's page has, for its state, **Place order**, **Receive…**, **PDF**, **Edit**, **Cancel
  order** and **Delete**, and tabs for its lines, details, attachments and history. Editing adds
  lines **From the vendor's catalog**, or **Add a line** for anything else.
- **Receive…** opens `/orders/{id}/receive`: each line still to come with the packs that came, all
  of them unless changed, and what that does, such as *Adds 200 pcs to M5 bolts*.
- A vendor's page has an **Orders** tab, with **New order**.

## Everywhere else

- **MCP:** `list_purchase_orders`, `get_purchase_order`, `save_purchase_order`,
  `delete_purchase_order`, `place_purchase_order`, `cancel_purchase_order`,
  `receive_purchase_order`, `suggest_purchase_orders`, `purchase_order_history`,
  `purchase_order_pdf`, `get_order_numbering` and `set_order_numbering`; the attachment tools take
  `purchase_order_id` (see [MCP](mcp.md)).
- **Backups:** orders, their lines and attachments are part of the assets, after what vendors sell
  of them, since lines name assets and their catalog entries; how numbers are shown is part of the
  settings (see [Backups](backups.md#kinds-of-information)).
- **Packages** never hold purchase orders: they are this instance's dealings, as loans are. The
  events a package carries name none (see [Packages](packages.md)).
- **Reports, labels, sheets and CSV** have nothing of their own for orders. The assets an order
  brings carry its number as their order number, which they already show and filter.

## Storage

Migration `076` adds `purchase_orders`, `purchase_order_lines` and `purchase_order_attachments` on
every database, `purchase_order_id` to `events`, and the lock new orders' numbers are allocated
under. (`075` is #280's.) An order's vendor is a foreign key that cascades, so that what replaces
vendors wholesale, such as restoring a backup, takes their orders with them; BAM! itself refuses to
delete a vendor with orders. A line's entries, category and asset are foreign keys set to null when
what they name is deleted.

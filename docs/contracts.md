# Contracts

A **contract** is an agreement with a [vendor](vendors.md) about assets: a warranty, a service
or maintenance agreement, a lease or a license. It says when it starts and ends, whether it
renews automatically and with how much notice, what it costs, and which assets it covers. BAM!
reminds channels before it ends, or before its notice deadline, and on the day it ends. This is
part of #216, after [vendors](vendors.md) (#217) and [attachments](data-model.md#attachments)
(#220).

## What a contract has

| Field | What |
|---|---|
| `vendor_id` | Required: the vendor it is with. Its scopes decide who sees the contract (see [Who sees a contract](#who-sees-a-contract)). |
| `kind` | Required: `warranty`, `service`, `maintenance`, `lease`, `license` or `other`. |
| `name` | Required, up to 255 characters. Names need not be unique. |
| `reference` | The vendor's reference for it, such as an agreement number, up to 255 characters. |
| `start_date`, `end_date` | Dates, `YYYY-MM-DD`, each optional. It cannot end before it starts. One with no end runs until it is ended, and is never reminded of. |
| `auto_renews` | Whether it renews by itself at its end unless notice is given. One that does needs an end date. |
| `notice_days` | Its notice period: how many days before its end notice must be given, from 0 to 3650. |
| `notice_deadline` | In answers only: for one that renews automatically, `notice_days` before its end. |
| `cost`, `currency` | What it costs, kept exactly as a [purchase's price](data-model.md#purchases) is: a decimal with no more places than its ISO 4217 currency has, which it needs. |
| `billing` | How often the cost is paid: `once`, `monthly`, `quarterly`, `yearly`, or empty when not said. |
| `notes` | Markdown, up to 20000 characters, rendered as a vendor's are (see [Vendors](vendors.md#notes)). |
| `asset_ids`, `category_ids` | What it covers, up to 5000 of each (see [What a contract covers](#what-a-contract-covers)). Answers name them in `assets` and `categories`. |

Text is trimmed and held to the [rules for stored text](data-model.md#stored-text). A contract
also has [attachments](data-model.md#attachments), such as the contract itself, and a history.

## What a contract covers

A contract covers the assets it names, and every asset in the categories it names or in the
categories below them. Each asset carries the contracts covering it, as `contracts` in its
answers: its own first, then those through a category, each once, by when they end, those with
no end last. One covering it through a category names it in `category_id`. The asset's page says
so, as *Under maintenance contract with Acme until June 2027*, or, for one that has ended,
*Maintenance contract with Acme ended May 2025*.

Deleting an asset or a category takes it off the contracts covering it; deleting a contract
leaves what it covered.

### Warranties

An asset's own `warranty_end`, in its [purchase details](data-model.md#purchases), is the
warranty it came with. A contract of the kind `warranty` is one bought or extended separately,
such as three years' cover from a reseller. The two are kept apart, and neither changes the
other: the asset shows both, `warranty.expiring` reminds of its own warranty, and
`contract.expiring` of the contract. A contract may cover many assets, or whole categories,
where an asset's warranty end is its alone.

## Who sees a contract

A contract follows its vendor (see [Vendors](vendors.md#who-sees-a-vendor)):

- Whoever sees the vendor sees its contracts. A contract whose vendor someone cannot see answers
  `404`, and is left out of lists and of the assets it covers.
- Of the assets a contract covers, each requester sees those they may read. Changing what it
  covers keeps those the editor may not read, and an asset named must be one they read (`400`).
- Its `cost`, `currency` and `billing` need `purchases.read` in the vendor's scopes, or in every
  scope for a vendor in none, which everyone sees (see [Access](access.md#seeing-prices));
  without it they are left out of answers, the assets it covers, its history's changes,
  reports, labels and packages, and `costs_hidden` says so. Editing it leaves them as they are.

## Who changes a contract

`vendors.manage` in the vendor's scopes creates, changes and deletes its contracts, and changes
their attachments; a vendor in no scope needs it in every scope. Moving a contract to another
vendor needs it for both. No new permission is needed.

A vendor with contracts is not deleted (`409`): delete them, or move them to another vendor,
first. Deleting the vendor would delete them too.

## Reminders

Notification channels can subscribe to two events (see [Notifications](notifications.md)):

| Event | When | `due_at` |
|---|---|---|
| `contract.expiring` | A number of days before the date it warns of, 30 unless configured (`[contracts] reminder_days`, 0 for the day itself; see [Configuration](configuration.md)), up to that date: its end, or for one that renews automatically, its notice deadline. | That date. |
| `contract.expired` | On the day it ends. | Its end. |

Each names the contract in `contract`: its ID, name, kind, vendor, end, and whether it renews
automatically with how much notice. Each is recorded once for each contract, kind and date,
claimed by inserting it into `contract_notices`, so servers sharing a database record it once,
as warranties' are; a new end, or a new notice period, gives new ones. A date already past when
the contract is first seen is not announced, and nor is a contract with no end.

A contract that renews automatically keeps the dates it was given: `contract.expired` says its
term has ended and that it renews, and its end should then be moved on by hand.

They are recorded in the contract's history, not an asset's or a category's, so the events list
and live connections, which follow assets and categories, do not carry them; channels do. MQTT
publishes them under `<prefix>/<kind>/<contract ID>`, and their links go to the contract's page.

## Reports and labels

A report's filter takes `contract`, a contract's ID, keeping the assets it covers as the
requester sees them (see [Reports](reports.md)). Labels and report columns bind the contract
covering an asset:

| Placeholder | What |
|---|---|
| `{{asset.contract.name}}`, `{{asset.contract.kind}}`, `{{asset.contract.vendor}}`, `{{asset.contract.reference}}` | Its name, kind, vendor's name and reference. |
| `{{asset.contract.start_date}}`, `{{asset.contract.end_date}}` | Its dates, `YYYY-MM-DD`. |
| `{{asset.contract.cost}}`, `{{asset.contract.currency}}`, `{{asset.contract.billing}}` | Its cost, currency and billing, blank for those who may not see them. |

A report filtering by a contract binds that one. Otherwise, and on labels, an asset binds the
contract in force that covers it longest: of those that have started, or have no start, and have
not ended, one with no end first, then the one ending last. One with none in force binds blanks.

## The Contracts page

The **Contracts** page, in **Catalog**, lists the contracts one can see, soonest-ending first,
with a search box; its **Ends** heading turns the order around. A contract's page shows its
details, what it covers, its attachments and its history. A vendor's page has a **Contracts**
tab, with a link to make one with it; a category's page lists the contracts naming it; an
asset's page lists those covering it.

## History

Making a contract records `contract.created`, and each change `contract.edited`, with the
changes:

| `what` | `from` and `to` |
|---|---|
| `vendor` | The vendors' names, keyed by the new one's ID. |
| `kind`, `name`, `reference`, `start_date`, `end_date`, `billing` | The values. |
| `auto_renews` | `yes` or `no`. |
| `notice_days` | The numbers. |
| `cost` | The cost with its currency. Only `purchases.read` sees it, and `billing`. |
| `notes` | Neither: history says only that they changed. |
| `asset`, `category` | What it covers, by name, keyed by ID: `to` alone when added, `from` alone when removed. |
| `photo`, `attachment` | Attachments added and removed, as a vendor's. |

Its reminders are in it too. The events name the contract in `contract_id`, which, like
`vendor_id`, is not a foreign key, so they read as they were after it is deleted. Who made each
change is shown to those who hold `assets.track` in the vendor's scopes, or anywhere for one in
none.

## API

| Method and route | Permission | Purpose |
|---|---|---|
| `GET /api/v1/contracts` | `assets.read` | The contracts the requester sees, by when they end, soonest first, as `contracts`. `?vendor_id=` keeps those with a vendor, `?category_id=` those naming a category, `?asset_id=` those covering an asset the requester reads (`404` for one they do not), and `?q=` those with every word of it in their name, kind, reference, vendor's name or notes, regardless of case. |
| `POST /api/v1/contracts` | `vendors.manage` in the vendor's scopes | Make one, with `{"vendor_id", "kind", "name", "reference", "start_date", "end_date", "auto_renews", "notice_days", "cost", "currency", "billing", "notes", "asset_ids", "category_ids"}`. Answers `201` with `contract`. |
| `GET /api/v1/contracts/{id}` | `assets.read`, and seeing its vendor | One, as `contract`, with `can_manage`, whether the requester may change it. |
| `PUT /api/v1/contracts/{id}` | `vendors.manage` in its vendor's scopes | Replace it. `asset_ids` and `category_ids` left out stay as they are. |
| `DELETE /api/v1/contracts/{id}` | `vendors.manage` in its vendor's scopes | Delete it, with its attachments' rows. Its files stay until `bam storage sweep`. |
| `GET /api/v1/contracts/{id}/history` | `assets.read`, and seeing its vendor | Its history, newest first, 50 at a time, as a vendor's. |
| `GET`, `POST`, `PUT /api/v1/contracts/{id}/attachments`, and `GET`, `PUT`, `DELETE /api/v1/contracts/{id}/attachments/{attachment_id}` | Seeing it to read, `vendors.manage` in its vendor's scopes to change | Its attachments, as a vendor's (see [Attachments](data-model.md#attachments)). |

## Everywhere else

- **MCP:** `list_contracts`, `get_contract`, `save_contract`, `delete_contract` and
  `contract_history`; the attachment tools take `contract_id` (see [MCP](mcp.md)).
- **Backups:** contracts, what they cover of categories, their attachments and the reminders
  recorded are part of the catalog; the assets they cover go with the assets (see
  [Backups](backups.md#kinds-of-information)).
- **Packages:** a vendor brings its contracts, with their attachments and what they cover of the
  package, but not the reminders recorded; their costs only from someone who may see them.
  Imported, a contract comes with its vendor when the vendor is imported, and not when an
  instance's own vendor of the same name is used (see [Packages](packages.md)).
- **Sheets:** an asset's sheet lists the contracts covering it.
- **CSV:** contracts have no CSV import or export of their own.

## Storage

Migration `072` adds `contracts`, `contract_categories`, `contract_assets`,
`contract_attachments` and `contract_notices` on every database, and `contract_id` to `events`.
(`071` is #224's.) A contract's vendor is a foreign key that cascades, so that what replaces
vendors wholesale, such as restoring a backup, takes their contracts with them; BAM! itself
refuses to delete a vendor with contracts.

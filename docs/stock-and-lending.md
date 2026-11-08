# Stock and lending

BAM! keeps track of consumables, so you know when something is running low, and of equipment that
people borrow, so you know who has the drill. It also keeps each asset's history: who created,
edited, archived, lent or printed it, and what changed.

Running low and overdue loans are events that [notification channels](notifications.md) can
subscribe to.

## Running low

Stock is counted in one of two ways, for two kinds of things:

| | Counted by items | Counted by quantity |
| :--- | :--- | :--- |
| For | Things labeled one by one: filament spools, saw blades, batteries | Things kept in bulk: screws, zip ties, solder |
| Each unit is | An asset of its own | Not tracked; one asset stands for all of them |
| What is counted | Active assets in a consumable category and its subcategories | A quantity on the asset |
| The threshold is on | The category | The asset |
| Using some up | Archive the asset | Use or restock it (`POST /api/v1/assets/{id}/quantity`) |

The two meet in one place: things labeled one by one that are used up gradually, such as filament
spools. A consumable category with a unit ([below](#weighing-instead-of-counting)) weighs them
rather than counting them, so a 1000 g spool and a 2000 g spool make 3000 g.

Otherwise the two are never added together, since they count different things (3 spools and 500 g
of solder make no total).

### How the flag works

Both kinds use the same rule. When the level drops below the threshold, the thing flags itself as
running low. When it climbs back to the threshold or above, it clears itself.

The rule reacts to the level *crossing* the threshold, not to the level itself. A manual change
stands until the level next crosses, so the automatic rule never undoes a person's decision.

### Counted by items

Categories tagged with `is_consumable = true` support an `is_running_low` flag; only consumable
categories may carry it.

`PUT /api/v1/categories/{id}/running-low` with `{"running_low": true|false}` sets or clears it by
hand. It needs only `stock.record`, so the people handling stock can flag it without being able to
edit categories or assets. Editing the category (`categories.manage`) can set it too.

A consumable category may have an optional `low_stock_threshold` (a positive integer, or `null` for
none). Its *active count* is the number of distinct active (not archived) assets in the category or
any of its descendants, and in the categories implying it ([Pools](#pools)). An asset in two
subcategories counts once. `GET /api/v1/categories/{id}` reports it as `active_count`.

Anything that changes an active count is checked against the threshold:

- creating, archiving, deleting or recategorizing assets;
- moving or deleting categories;
- linking categories, or taking links away ([Pools](#pools));
- imports, checked once, after the whole file;
- changing the threshold itself: setting it above the current count flags at once.

Changes that can move a count serialize on an application lock, so two simultaneous archives cannot
both miss a crossing.

### Weighing instead of counting

A consumable category may weigh instead of counting, when it has a `unit` (up to 32 characters).

Its level is then the total stock, in that unit, of the distinct active assets in it and its
descendants that count their stock by quantity in that unit (a spool of filament with 1000 g).
Other assets add nothing. The threshold is in the unit too, and `GET /api/v1/categories/{id}`
reports the level as `total` beside `active_count`.

Each category measures by its own unit, so totals carry up to parent categories only where the
units match.

These changes are checked against the thresholds as any other change, and the category's events
carry the unit:

- using or restocking such an asset;
- checking a lent one back in with what is left (see [Checking in](#checking-in)).

An asset whose stock is in the unit of one of its categories, or their ancestors, is `weighed`.

### Category events

Every change of a category's flag, manual or automatic, records an event (`category.running_low` or
`category.restocked`) in the same transaction as the change. The event records:

- the category;
- the user whose action caused it;
- whether it was manual or automatic;
- for automatic changes, the count (or total) and threshold, with the unit of a category that
  weighs.

A change that rolls back records nothing, and two people flagging at once produce one event.

### Counted by quantity

An asset may have `stock`, `{"quantity", "unit", "low_stock_threshold", "is_running_low"}`, or
`null` when its stock is not counted this way.

| Field | Values |
|---|---|
| `quantity` | A whole number from 0 to 1,000,000,000. For things measured in length or weight, pick a unit small enough (`mm`, `g`). |
| `unit` | Optional text of up to 32 characters |
| `low_stock_threshold` | Optional, from 1 to 1,000,000,000 |
| `is_running_low` | The flag |

#### Starting and stopping counting

- Creating an asset (`POST /api/v1/assets`) with a `stock` object starts counting.
- Updating it (`PUT /api/v1/assets/{id}`) with an object replaces the unit and threshold.
- Updating it with `null` stops counting and clears all four fields.
- An update without `stock` leaves it alone.

An object without `quantity` keeps the current quantity (or starts at 0). So an edit cannot undo a
use someone made while the form was open; the web form sends the quantity only when it was changed.

#### Using and restocking

`POST /api/v1/assets/{id}/quantity` (`stock.record`) uses and restocks:

- `{"adjust": n}`: negative to use some, positive to restock;
- `{"set": n}`: set the quantity.

Using more than there is answers `409` (`not enough stock: 40 left`) and changes nothing. Changes
run under the inventory lock, so people using stock at once each take their share.

#### Thresholds

Thresholds and the flag follow [the same rule](#how-the-flag-works) as categories. Dropping below
the threshold flags the asset and climbing back clears it, reacting to crossings only. So a flag set
by hand (`PUT /api/v1/assets/{id}/running-low`, `stock.record`) stands until the next crossing.
Archived assets are not checked.

#### Asset events

Crossings and manual changes record `asset.running_low` and `asset.restocked`, with the asset, its
quantity, threshold and unit, and who caused it. Channels can subscribe to them.

Every change of a quantity also records `asset.quantity_changed`, with how much it moved, for the
asset's history. Channels cannot subscribe to it.

## Pools

The same thing is often kept in more than one place: M5 bolts in a drawer in the workshop and a bin
by the laser cutter, or zip ties in three toolboxes. Each place is its own asset, with its own count.
A category can say that its members **also count in** another category, anywhere in the tree, so
that the places pool.

### What it does in practice

1. **Bolts in two places.** The workshop drawer's category and the laser bay's both imply
   *M5 bolts*, a consumable category with the unit `pcs` and a threshold of 200. One threshold
   watches the total of both, 440. An empty bin says where else to look: "140 here, 300 in the laser
   bay". Only a real shortage alerts: the drawer emptying while the laser bay holds 300 raises
   nothing.
2. **Views across the tree.** *PPE* gathers gloves, goggles and ear defenders from wherever they
   are filed, for one report, without moving anything.
3. **What it doesn't do.** No scopes, fields, label or scan templates, or suggested link types come
   through a link, and assets it brings in are not made [weighed](#weighing-instead-of-counting).
   Who can see an asset, and what its form asks for, are unchanged by it.

### Linking categories

A link says **the members of X are also members of Y**: the assets in X, and in the categories below
X, count in Y's totals and thresholds, and are found when filtering by Y and in reports on Y. They
count in the categories above Y too, as Y's own assets do.

- **Either side edits it.** On X's page, under **Counts in**, *Also counts in…* lists the categories
  it implies; on Y's, *Also includes…* lists those implying it. Each page lists its links both ways.
- **The API.** Categories carry `implies` and `included_by`, the IDs of each side.
  `PUT /api/v1/categories/{id}/links` with `{"implies": [...]}`, `{"included_by": [...]}` or both
  (`categories.manage`) replaces that side, leaving one not given as it is. MCP's
  `set_category_links` does the same.
- **Computed, not stored.** Membership through a link is worked out when it is needed, as a
  category's total includes its subcategories. Taking an asset out of X takes it out of Y at once. An
  asset put in Y directly stays there.
- **Chains, never cycles.** If X implies Y and Y implies Z, X's assets count in Z. A link leading back
  to where it starts, directly or through the tree (X implying a category inside X), is refused
  (`409`), and so is moving a category under one whose links lead back to it. An asset reached by
  more than one path counts once.
- **Rechecked.** Changing links goes through the same check as moving a category: a link that drops
  Y below its threshold flags it, and one that lifts it back clears it, recording
  `category.running_low` or `category.restocked`.
- **Shown.** An asset's page lists the categories it counts in through links, with the category each
  comes through: "Counts in Stock › M5 bolts (through Workshop › Fasteners › M5)". The API gives
  them as the asset's `counts_in`.
- **Copied and deleted.** Duplicating a category copies what it counts in, not what counts in it.
  Deleting either category deletes the link.

Links are not in asset CSV or Excel files, since they belong to categories. They travel in
[backups](backups.md) and [packages](packages.md) with the rest of the categories.

### Units

A pool is a consumable category with a unit ([above](#weighing-instead-of-counting)). Any link is
allowed, but its total includes only the assets whose stock is in its unit, as subcategories' do.
Other assets still count as members, for filters and reports.

A link warns when it is made, and on Y's page, when none of X's members count their stock in Y's
unit: "None of Workshop › Fasteners › M5's assets count in pcs, so they add nothing to M5 bolts'
total." `GET /api/v1/categories/{id}` and the links answer give these as `link_warnings`.

### Where else to look

An asset counted by quantity lists, on its page and its scan page, each pool it counts in, with the
pool's total and threshold and the other assets in it that have stock, with where they are and how
many. The API gives them as the asset's `pools`, on `GET /api/v1/assets/{id}` and the scan, each
listing every place, empty ones too, so stock can be moved to them.

### Quiet bins

An asset's own running-low flag holds off while the total of a pool it counts in is at or above
that pool's threshold. Only pools whose unit is the asset's, and that have a threshold, hold it off.
So an empty bin raises nothing while there is plenty elsewhere. When the pool runs short, the pool
flags itself, and so does each of its bins below its own threshold; when the pool is restocked, from
any bin, they clear.

### Moving stock

**Move to another place**, on an asset counted by quantity, takes some from it and adds it to
another in one step: `POST /api/v1/assets/{id}/move` with `{"to": "<asset id>", "amount": n}`
(`stock.record` in both assets' scopes), or MCP's `move_stock`.

- Both must be counted in the same unit.
- Moving more than there is answers `409` and changes nothing.
- Each asset's history records `asset.quantity_changed` with `move`, naming where it came from and
  went.
- A pool both count in keeps its total, so it does not alert on the way. Each asset's own flag is
  checked as usual.

### Partial visibility

People see only what they may:

- **Where else to look** lists only the assets the reader can see, besides the one they are on.
- **Any pool total shown to them**, on a category's page, an asset's or in a report, sums only
  those, and so does a category's `active_count`.
- **Alerts use the real total**, since they go to channels, not to that reader.

## Lending

Some equipment is lent out: drills, multimeters, a ladder. Lending is turned on per category and
tracked per asset.

`is_lendable` on a category lets its assets, and those of every category below it, be checked out.

### Who can lend and borrow

| Permission | Allows |
|---|---|
| `assets.borrow` | Checking an asset out to oneself, and checking one's own loans back in |
| `assets.lend` | Checking an asset out to any active user, and checking any loan back in |

Migration `014` grants both to every role that had `assets.write`.

Borrowers are always user accounts. `GET /api/v1/people` (`assets.lend` or `users.manage`) lists the
active users a loan can name. An API token can lend with `assets.lend`, naming the borrower, but
cannot borrow.

### Seeing a loan

`GET /api/v1/assets/{id}` says whether an asset is `lendable`, and gives its open `loan`, if any:

- the borrower: `id`, `email`, `name`, and `archived` if they have left;
- when it went out;
- when it is due back;
- whether it is `overdue`.

### Checking out

`POST /api/v1/assets/{id}/checkout` checks an asset out, with `{"borrower_id", "due_at"}`, both
optional:

- No borrower means whoever is signed in.
- `due_at` is an RFC 3339 time. The web app sends the end of the chosen day in the browser's time
  zone, since the server has no local time zone of its own.

It is refused (`409`) for an asset that is:

- archived;
- in no lendable category;
- out of service;
- already lent (one borrower at a time);
- counted by quantity without being [weighed](#weighing-instead-of-counting).

Lending part of a pooled quantity is not supported, so label each item to lend it. A weighed spool
is lent like anything else.

### Checking in

`POST /api/v1/assets/{id}/checkin` ends the open loan. For an asset counted by quantity, such as a
spool, `{"left": n}` says how much came back. The difference is recorded as use and checked against
thresholds.

A lent asset cannot be archived until it is checked in (`409`). A borrower who is archived keeps
their loans, so it stays clear who had the drill.

### Listing loans

`GET /api/v1/loans` (`assets.track`, see [Access](access.md)) lists loans of the assets the requester
tracks, newest first, by the borrower's name, returned ones too. It filters with `?borrower_id=` and
with `?state=open` or `overdue`.

The asset list filters with `?loan=open` or `overdue`, and shows who has each asset. The Loans page
shows lenders every open loan, and borrowers their own, past and present.

### Loan events and overdue loans

Checking out and in record `asset.checked_out` and `asset.checked_in` in the asset's history, with
the borrower as they were named then and the due date.

The notification worker in `bam serve` sweeps for open loans past due each time it polls. It records
`asset.overdue` once per loan, with the trigger `schedule`. Channels can subscribe to it, and several
servers sharing a database still record it once.

It does the same for things left through a flow past their date to collect by, as
`asset.flow_overdue` (see [Flows](flows.md)).

### Loan reminders

Borrowers can be emailed about what they borrowed: before it is due, the day it becomes overdue,
and weekly after. They are off until turned on under **Admin › Notifications**, by those who manage
settings, and need email set up with the server's `base_url` (see
[Notifications](notifications.md#email)).

- **Before it is due:** a reminder the set number of days before, one by default. Zero sends none
  before.
- **Once overdue:** a reminder the moment it is due, then a week later, and so on, up to the set
  number, four by default. Zero sends none.
- **What it says:** what was borrowed, from whom and when, when it was due, and links to return it
  by its scan page, or to its page when it has no code.
- **Who gets them:** the borrower, when their account is active, until they turn reminders off
  under **Your account**. A loan with no due date gets none, and nor does one returned.
- **Once each:** the notification worker sends them as it polls. Each reminder is claimed in the
  database before it is sent (`loan_reminders`), so servers sharing it send it once, and given back
  if sending fails, to be tried again at the next poll. A server stopped for a while sends only the
  reminder due now, not each it missed.
- **Backups:** the reminders sent are kept with their loans, so a restored instance does not send
  them again.

## Asset history

Creating, editing, archiving and restoring an asset record `asset.created`, `asset.edited`,
`asset.archived` and `asset.restored`. They are recorded in the same transaction as the change,
through the web app, the API and CSV imports alike. An import sets the trigger to `import`.

Each event lists its `changes`, each with the value before and after, and the field's label or the
generator's name as they read then:

- the name;
- categories joined and left;
- field values;
- codes;
- whether stock is counted, its unit and threshold.

A save that changes nothing records nothing. Archiving lists the codes it released. Restoring lists
the codes it got back, and those another asset had taken.

Changes made elsewhere that affect an asset, such as deleting a field or a category, are not
recorded on it. Assets created before this release have no record of earlier edits. Channels cannot
subscribe to these events either.

Other events also appear in an asset's history, including stock changes, loans, label printing (see
[Printing](printing.md)) and hooks (see [Hooks](hooks.md)).

`GET /api/v1/assets/{id}/history` (`assets.read`) returns `events`, newest first, 50 at a time, with
the email and name of whoever made each change, and `more` when older events remain.
`?before=<event id>` returns the page older than that event; an event that is not the asset's is a
`400`.

## API

| Method and route | Permission | Purpose |
|---|---|---|
| `GET /api/v1/categories/{id}` | `assets.read` | Includes `active_count`, `total` for a category that weighs, both of the assets the requester sees, and `link_warnings` |
| `PUT /api/v1/categories/{id}/links` | `categories.manage` | Set the categories it implies, or that imply it ([Pools](#pools)) |
| `PUT /api/v1/categories/{id}/running-low` | `stock.record` | Set or clear a category's flag by hand |
| `POST /api/v1/assets` | `assets.write` | Create an asset; a `stock` object starts counting |
| `PUT /api/v1/assets/{id}` | `assets.write` | Update an asset's `stock` |
| `POST /api/v1/assets/{id}/quantity` | `stock.record` | Use or restock |
| `PUT /api/v1/assets/{id}/running-low` | `stock.record` | Set or clear an asset's flag by hand |
| `POST /api/v1/assets/{id}/move` | `stock.record` | Move stock to another asset ([Moving stock](#moving-stock)) |
| `GET /api/v1/assets/{id}` | `assets.read` | Includes `lendable`, the open `loan`, `counts_in` and `pools` |
| `POST /api/v1/assets/{id}/checkout` | `assets.borrow` or `assets.lend` | Check out |
| `POST /api/v1/assets/{id}/checkin` | `assets.borrow` or `assets.lend` | Check in |
| `GET /api/v1/loans` | `assets.track` | List loans |
| `GET /api/v1/people` | `assets.lend` or `users.manage` | Active users a loan can name |
| `GET /api/v1/assets/{id}/history` | `assets.read` | An asset's history |
| `GET /api/v1/settings/loan-reminders` | `settings.manage` | Loan reminders: `on`, `days_before` and `overdue_times`, with `can_email` |
| `PUT /api/v1/settings/loan-reminders` | `settings.manage` | Set them; turning them on without email set up is a `409` |
| `PUT /api/v1/auth/loan-reminders` | signed in | Turn one's own reminders on or off: `{"reminders": true}` |

## Events

| Event | Recorded when | Channels can subscribe |
|---|---|---|
| `category.running_low`, `category.restocked` | A category's flag changes | Yes |
| `asset.running_low`, `asset.restocked` | An asset's flag changes | Yes |
| `asset.quantity_changed` | An asset's quantity changes, with `move` when stock moved between assets | No |
| `asset.created`, `asset.edited`, `asset.archived`, `asset.restored` | An asset is created, edited, archived or restored | No |
| `asset.checked_out`, `asset.checked_in` | An asset is lent or returned | No |
| `asset.overdue` | A loan passes its due date | Yes |
| `asset.flow_overdue` | Something left through a flow passes its date to collect by | Yes |

## Storage

| Migration | Adds |
|---|---|
| `014` | Lending; grants `assets.borrow` and `assets.lend` to every role that had `assets.write` |
| `016` | A category's `unit`, for weighing |
| `064` | Loan reminders sent (`loan_reminders`), and whether each user gets them (`users.loan_reminders`) |
| `075` | Categories that imply other categories (`category_implies`), for [pools](#pools) |

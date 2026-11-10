# Review states

When many people are asked to record what is in their area, or an assistant drafts records for
someone, a space wants to tell what has been checked from what has not, and to have records checked
again before they go stale. Review states do that. They are off until a category turns them on, and
an asset in no category that uses them has none: everything works as it did before.

## In practice

The wood shop asks its volunteers to record every tool on the shelves. It turns review states on for
its **Tools** category, and asks for review every 12 months:

1. A volunteer adds the band saw. It is a **draft**: recorded, not yet checked.
2. The shop's lead, who holds `assets.confirm` in the shop's scope, looks it over and **confirms**
   it: someone responsible for it says it is right.
3. Whoever keeps the inventory, holding `assets.verify`, checks it again and **verifies** it. Had
   something been wrong, they would have **returned it to draft**, saying why.
4. Eleven and a half months later the band saw's review falls **due**. `asset.review_due` is
   recorded once, and the channel the shop subscribed to it announces it. The home page shows it to
   those who can confirm it, and the asset list finds it under **Review due**. Confirming it again
   starts the next 12 months.

Editing the band saw's fields or links returns it to draft, unless the category says editing keeps
its state.

## The states

| State | Means | Reached by |
|---|---|---|
| `draft` | Recorded, not yet checked. | Being added; returning to draft; an edit, where the category says so; a change through an assistant. |
| `confirmed` | Someone responsible for it says it is right. | `assets.confirm`, from any state. |
| `verified` | Checked again by someone else, such as whoever keeps the inventory. | `assets.verify`, from confirmed or verified. |

An asset added to a category that uses review states is a draft, unless whoever adds it holds
`assets.confirm` in its scopes and chooses to confirm it. An asset that was there before its category
turned review states on reads as a draft, since no one has checked it.

## Category settings

Each category has four settings, each either its own or taken from its parent, as fields are passed
down to subcategories (`PUT /api/v1/categories/{id}/review`, `categories.manage`; `null` takes the
parent's):

| Setting | Default at the top | Means |
|---|---|---|
| `enabled` | off | Review states for its assets and those below it; off below a category that has them on. |
| `months` | 0 | Ask for review that many months (up to 120) after an asset is last confirmed or verified; 0 asks for none. |
| `on_edit` | `draft` | `draft` returns a confirmed or verified asset to draft when its fields or links are edited; `keep` leaves its state. |
| `self_verify` | off | Lets whoever confirmed an asset verify it too. |

An asset in several categories that use review states takes, of those: the shortest time between
reviews any asks for, a return to draft on editing if any asks for it, and verifying one's own
confirmation only if all allow it. Categories carry `review`, their own settings, and
`effective_review`, those in effect.

What counts as editing is anything an asset's history records as an edit: its name, categories,
fields, codes, links, stock settings, icons, purchase details, attachments and place. Its quantity,
condition, loans and labels printed are not edits of its record.

## Moves

`POST /api/v1/assets/{id}/review` with `{"move", "note"}`:

- **`confirm`** needs `assets.confirm` in the asset's scopes. It moves any state to confirmed, and
  starts the time until its next review again.
- **`verify`** needs `assets.verify`. It moves a confirmed or verified asset to verified, starting
  the time again. Someone cannot verify what they confirmed themselves (`403`) unless the category
  allows it.
- **`return`** needs `assets.verify`. It moves a confirmed or verified asset back to draft, with a
  note saying why, which it needs.

Each move is recorded in the asset's history as `asset.review_changed`, with who made it, the note,
and when the next review is due. A move the state does not allow, or on an asset whose categories do
not use review states, is refused (`409`).

Assets carry `review` while one of their categories uses review states: the `state`, who set it and
when, who last confirmed it and when, when it was last confirmed or verified (`reviewed_at`), when it
is next due (`due_at`), whether it is `due` or `overdue`, the `rules` in effect, and any `proposal`
(see [Assistants](#assistants)). People in it are shown as asset history shows them: by name, and only
to those with `assets.track`, though everyone sees their own.

## Review dates

A category asking for review every so many months makes each asset's review due that long after it
was last confirmed or verified. A draft has no review date: it is waiting to be confirmed.

An asset's review is **due** from a number of days before its date (14 by default, set per instance
under **Admin → Notifications**, in **Review**, or `PUT /api/v1/settings/review` with `due_days`, 0 to 365), and **overdue**
from the date. When it falls due, `asset.review_due` is recorded once for that date, as running low
is: the date is claimed on the asset, so servers sharing a database record it once, and a new date,
after the asset is confirmed or verified again, is a new event. Notification channels and hooks can
subscribe to it (see [Notifications](notifications.md) and [Hooks](hooks.md)).

Changing a category's settings, or moving it in the tree, works out every asset's date again.

## Finding what to review

- **The asset list** filters by `?review=` `draft`, `confirmed`, `verified`, `due`, `overdue` or
  `proposed` (changes from an assistant waiting).
- **The home page** shows those who can confirm how many drafts and reviews due or overdue there are,
  those who can verify how many confirmed assets wait for them, and those who can edit how many
  assistants' changes wait (`GET /api/v1/review`).
- **Reports** bind `{{asset.review_state}}`, `{{asset.review_due}}`, `{{asset.confirmed_at}}` and
  `{{asset.confirmed_by}}` as columns, and **labels** bind them too (see [Labels](labels.md)).
- **CSV and Excel exports** have a `review_state` column when any asset's categories use review
  states. An import ignores it: the assets it adds are drafts unless the importer holds
  `assets.confirm` in their scopes and chooses to confirm them (`confirm=true`; see [CSV](csv.md)).

## Assistants

Changes made through an assistant, over MCP, as [history records them](mcp.md), or by [the assistant beside the page](assistant.md), can land as drafts,
whatever the person could otherwise do. This is how an assistant proposes and a person decides. It is
on by default, and turned off per instance under **Admin → Notifications**, in **Review** (`assistant_drafts`). It applies
only to assets whose categories use review states; elsewhere an assistant's changes are taken as
they are today.

Where it is on:

- **An asset an assistant adds** is a draft, even if asked to confirm it, and is marked as new.
- **An asset an assistant changes** returns to draft, and what it was before the first of the
  assistant's changes is kept, so they can be undone. Further changes through an assistant add to
  the same proposal.
- **An assistant cannot confirm, verify, or accept changes** (`403`). It can return an asset to
  draft.

Each asset with changes waiting carries `review.proposal`: who made them, through which assistant,
whether the asset is new, the state it had before, and whether they can be undone. Someone then:

- **Accepts** them, `POST /api/v1/assets/{id}/proposal/accept`, with `assets.write` in its scopes.
  The changes become the accepter's own: the asset takes the state their own edit would have left it
  in, the state it had before where its categories keep the state on editing, a draft otherwise, or
  confirmed with `{"confirm": true}` and `assets.confirm`.
- **Rejects** them, `POST /api/v1/assets/{id}/proposal/reject`, with an optional `note`. An asset the
  assistant added is archived, which needs `assets.delete` too. Any other is put back as it was, in
  the state it had: its name, categories, fields, codes, stock settings, icons, purchase details and
  links. Its attachments and quantity stay as they are, and links to assets archived since are not
  put back. A code another asset has taken since refuses the rejection (`409`).

The asset page offers **Accept**, **Accept and confirm**, **Edit**, which opens the asset to edit, and **Reject**. The assistant beside the page asks before any change that would not land as a draft, so nothing it does elsewhere changes without the person confirming it.

A person editing an asset with changes waiting accepts them, since they are taking the record as
their own. Both decisions are recorded in the asset's history as `asset.review_changed`.

## Everywhere else

- **Flows:** a Create step can file as confirmed (`"confirm": true`, needing `assets.confirm`), and
  a Change step can confirm (`{"kind": "confirm"}`), doing nothing where the asset's categories do
  not use review states (see [Flows](flows.md)).
- **MCP:** `confirm_asset`, `verify_asset`, `return_to_draft` and `review_summary`;
  `set_category_review` and `get_review_settings`; `search_assets` takes `review`; `create_asset`
  takes `confirm`; and assets carry their review (see [MCP](mcp.md)). Accepting and rejecting an
  assistant's changes, and turning off their landing as drafts, are refused over MCP: they are a
  person's to do, in the web app.
- **Backups** carry review states with the assets, the categories' settings with the catalog, and
  the changes waiting as `asset_proposals`. **Packages** bring categories' settings, but an imported
  asset is a draft: who checked it is the instance's own to say.

## Storage

Migration `079` adds `review_state`, `review_by`, `review_at`, `confirmed_by`, `confirmed_at`,
`reviewed_at`, `review_due` and `review_notified` to `assets`; `review_enabled`, `review_months`,
`review_on_edit` and `review_self_verify` to `categories`; and the `asset_proposals` table, on every
database. It adds `assets.confirm` and `assets.verify`, granting `assets.confirm` to every role that
edits assets, where it edits them, so that what they add is taken as true as before until a category
asks for more; `assets.verify` goes to the admin role.

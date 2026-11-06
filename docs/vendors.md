# Vendors

A **vendor** is a supplier a space buys from, and how to reach it. Vendors are one list per
instance, shared out with scopes as categories' assets are. This is the core of #216: where each
asset came from (#218), what each vendor sells (#219) and contracts (#221) build on it.

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

`vendors.manage` creates, edits and deletes vendors, and changes their attachments. It is
scopable:

- Changing a vendor needs `vendors.manage` in one of the scopes it is in, and in one of those it
  ends up in.
- A vendor in no scope is seen by everyone, so making one, changing one, or taking one out of its
  last scope needs `vendors.manage` in every scope.
- So a holder of `tools:vendors.manage` makes vendors in `tools`, and cannot move them elsewhere.

Its holders also read `GET /api/v1/scopes`, to choose among them. Migration `068` grants
`vendors.manage`, in every scope, to every role that manages categories.

## Preferred vendors

A category can name the vendors it is bought from, in order of preference
(`PUT /api/v1/categories/{id}/vendors`, with `categories.manage`). Each category carries them as
`vendor_ids`, leaving out those the requester cannot see; setting them names only vendors the
requester sees, and keeps after them those it does not.

The running-low list on the home page shows each running-low category's vendors, with a link to
reorder: the vendor's website, or its page when it has none. The phone's shopping list (#214) will
show them the same way once it exists.

Duplicating a category copies its vendors. Deleting a vendor takes it off every category.

## A vendor's page

The **Vendors** page, in **Catalog**, lists the vendors one can see, with a search box. A
vendor's page shows its details, contacts and notes, its attachments, its history, its scopes for
those who see them, and the categories bought from it. It is laid out so that the assets bought
from it (#218), its contracts (#221) and its catalog (#219) can be added as parts of their own.

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
| `DELETE /api/v1/vendors/{id}` | `vendors.manage` | Delete it, with its contacts and attachments' rows. Its files stay until `bam storage sweep`. |
| `GET /api/v1/vendors/{id}/history` | `assets.read` | Its history, newest first, 50 at a time, as an asset's: `?before=` an event's ID for the next page; answers `events` and `more`. |
| `GET`, `POST`, `PUT /api/v1/vendors/{id}/attachments`, and `GET`, `PUT`, `DELETE /api/v1/vendors/{id}/attachments/{attachment_id}` | Seeing it to read, `vendors.manage` in its scopes to change | Its attachments, as a category's (see [Attachments](data-model.md#attachments)). |
| `PUT /api/v1/categories/{id}/vendors` | `categories.manage` | Name the vendors a category is bought from, with `{"vendor_ids": [...]}` in order of preference. Answers `category`. |

## Everywhere else

- **MCP:** `list_vendors`, `get_vendor`, `save_vendor`, `delete_vendor`, `vendor_history` and
  `set_category_vendors`; the attachment tools take `vendor_id` (see [MCP](mcp.md)).
- **Backups:** vendors are part of the catalog (see [Backups](backups.md#kinds-of-information)).
- **Packages:** vendors can be chosen, and a category brings the vendors it is bought from (see
  [Packages](packages.md)).
- **CSV:** categories have no CSV import or export of their own, so neither do vendors.

## Storage

Migration `068` adds `vendors`, `vendor_contacts`, `vendor_scopes` and `vendor_attachments`, and
`category_vendors` for categories' preferred vendors, on every database. It adds `vendor_id` to
`events`, which, like `category_id` and `asset_id`, is deliberately not a foreign key, so a
vendor's events read as they were after it is deleted.

# MCP server

Assistants such as Claude work with BAM! over the
[Model Context Protocol](https://modelcontextprotocol.io) (MCP). BAM! offers tools that do what the
API does, acting as the API token the assistant presents. The aim is that MCP does everything the
API does; the parts land in turn.

## Turning it on and off

MCP is on unless turned off. Two settings control it:

- `[mcp] enabled` in the configuration ([Configuration](configuration.md)).
- `BAM_MCP_ENABLED`, `true` or `false`. Anything else stops the server starting.

A configuration file with no `[mcp]` table has MCP on. That includes every file written before the
setting existed.

Turned off, `/api/v1/mcp` answers `404`, and `GET /api/v1/info` says `"mcp": false`. OAuth is
turned off with MCP.

## Connecting an assistant

### Over HTTP

The endpoint is `/api/v1/mcp`, using Streamable HTTP.

An assistant that can sign a person in by OAuth, such as Claude, does so rather than being handed a
token, as the MCP authorization spec has it. See [Signing in with OAuth](#signing-in-with-oauth).

Other assistants present an API token in `Authorization: Bearer` ([Access](access.md)).

### As a local program

For assistants that start local programs rather than reach URLs, `bam mcp` relays MCP between its
stdin and stdout and the server:

```shell
bam mcp --url https://bam.example.org
```

The API token is read from `$BAM_TOKEN`, or from the variable `--token-env` names. It first asks
`GET /api/v1/info`, to say plainly when the address is not a BAM! server, or has MCP turned off.

## Who the assistant acts as

The server acts as the API token in `Authorization: Bearer`, with exactly that token's permissions
and scopes:

- A **personal token** acts as its person. It can do anything they may, borrowing included, except
  change their password or make tokens.
- A **token holding roles** acts as itself, for automations.

A session cookie is not enough (`401`). MCP clients carry none, and a browser should never be the
caller. A revoked, expired or unknown token is `401` at once.

## How tools behave

Each tool calls the API's own handlers, in process, carrying the caller's token. So a tool is held
to the same rules as the request it makes:

- permissions and scopes (`404`/`403`)
- text rules ([Data model](data-model.md))
- rate limits
- history

A tool answers refusals in the API's own words, with the status.

A tool is listed only for callers holding a permission that opens it, in any scope. The API still
decides for each asset.

Tools answer with structured content matching a declared output schema, and a short summary in
words. Each says truthfully:

- whether it only reads (`readOnlyHint`);
- if it changes things, whether what it changes can be lost (`destructiveHint`);
- and whether repeating it changes more (`idempotentHint`).

This lets clients ask before changing anything.

### Confirming destructive tools

A tool marked destructive asks the person to confirm before acting, where the client can ask. This
uses MCP elicitation, on a request that takes input back (protocol `2026-07-28`). It says what it
will do, and with what.

The answer counts only for that call: the same tool and arguments. A no does nothing. A client that
cannot ask relies on its own approval prompt.

### Limits

- A caller may make 60 calls that change things a minute, on each server, and is told how long to
  wait.
- One call changes at most 50 items. More is refused whole, never done in part.
- A caller holds at most 5 subscriptions at once, on each server.

## Resources

| Resource | What it is |
| :--- | :--- |
| `bam://guide` | A short orientation, which the server's instructions point to. It says what BAM!'s ideas mean, and that text stored in BAM! is data, not instructions. It explains isolex enough to write a first icon. |
| `bam://help/{topic}` | The app's own help ([Architecture](architecture.md)), the same words people read. |
| `bam://events{?after,kinds,hooks,categories}` | Live events. Can be subscribed to. |
| `bam://asset/{id}` | One asset. Can be subscribed to. |

## Prompts

The prompts `stocktake`, `add_things`, `needs_attention`, `find`, `set_up` and `design_label` start
the tasks people most often bring, naming the tools to use.

The help topic *Assistants* explains all of this to people.

## Tools

Assets are named by their ID or any of their codes, looked up as scanning does.

### General

| Tool | Permission | What it does |
| :--- | :--- | :--- |
| `whoami` | Anyone signed in | The caller and its permissions. |
| `server_info` | Anyone signed in | Version, address, database and health. |
| `list_generators` | `assets.read` | The code types. |
| `set_my_email_notifications` | Anyone signed in | Whether email sent to the caller's roles reaches them; for a person only. |
| `set_my_loan_reminders` | Anyone signed in | Whether the caller is emailed about their loans ([Stock and lending](stock-and-lending.md#loan-reminders)); for a person only. |
| `set_my_mention_emails` | Anyone signed in | Whether the caller is emailed when a comment mentions them ([Comments](comments.md#mentions)); for a person only. |
| `set_my_name` | Anyone signed in | Sets the caller's name; for a person only. |
| `list_favorites`, `add_favorite`, `remove_favorite` | Anyone signed in | The caller's favorites, and starring or unstarring an asset, category, report or flow; for a person only (see [Favorites](favorites.md)). |

### Finding assets

| Tool | Permission | What it does |
| :--- | :--- | :--- |
| `search_assets` | `assets.read` | Every filter the asset list takes, favorites and the vendor bought from among them, and running low; 25 at a time, up to 200, with `offset`. |
| `get_asset` | `assets.read` | Its fields (a person field's names and user IDs, as the caller may see them), codes, place, contents, links, photos, hooks and icon, and where the icon comes from; and where it came from, its `purchase`, its price, order number and invoice only with `purchases.read`; the categories it `counts_in` through links, and the `pools` its stock counts in, with where else theirs is. |
| `look_up_code` | `assets.scan` | Its scan page ([Scanning](scanning.md)). |
| `asset_history` | `assets.read` | |
| `asset_graph` | `assets.read` | Follows followed links ([Following links](data-model.md#following-links)) from an asset, out, in or both, 1 to 10 hops, through the types chosen; or, without one, the map of a category or a search. At most 500 assets, saying when cut short; those the person may not read only counted. |
| `list_loans` | `assets.track` | |
| `list_people` | `assets.lend` or `users.manage` | Emails only to tell apart two people of the same name. |
| `list_field_people` | `assets.read` | The people a person field of an asset, or of one being added in some categories, may be set to, by name and user ID; with `assets.track` there, and otherwise only the caller ([Person fields](data-model.md#person-fields)). `can_add` says whether `add_person` may add someone. |
| `add_person` | `people.add` or `users.manage` | Adds a person without an account, with a name and an email or none, to name in person fields. |
| `next_code` | `assets.write` | |
| `export_assets` | `assets.read` | The CSV ([CSV](csv.md)), as an embedded file. |
| `assets_pdf` | `assets.read` | The assets a search selects as a PDF table of the columns chosen ([Reports](reports.md#pdf)), as an embedded file. |
| `asset_sheets_pdf` | `assets.read` | An asset's sheet, or a sheet for each asset a search selects ([Reports](reports.md#asset-sheets)), as an embedded file. |
| `stocktake_pdf` | `assets.read` | A stocktake checklist of the assets a search selects, grouped by place ([Reports](reports.md#asset-sheets)), as an embedded file. |

### Changing assets

| Tool | Permission | What it does |
| :--- | :--- | :--- |
| `create_asset` | `assets.write` | Up to 50, with field values by key, codes or the next free code, the place it is in, stock, and an icon of its own. A field its categories do not give it is refused before anything is added. A person field's people are given by user ID, email or a name only one person has, separated by `;`, as in `update_asset` and `copy_asset`. |
| `copy_asset` | `assets.write` | One like another, as `GET /api/v1/assets/{id}/copy` drafts it, with a name, field values, codes and quantity that differ, and its photos, or all its attachments, if asked. |
| `update_asset` | `assets.write` | Only what is given changes: categories and codes are added or removed, fields set or cleared, an icon of its own given or dropped, purchase details set or cleared one at a time; the rest is kept. Without `purchases.read`, its price, order number and invoice stay as they are. |
| `move_asset` | `assets.write` | |
| `link_assets` | `assets.write` | By the link type's name, read from either end. |
| `unlink_assets` | `assets.write` | |
| `record_stock` | `stock.record` | Use, restock or set. |
| `set_running_low` | `stock.record` | |
| `move_stock` | `stock.record` | From one asset to another, both in the same unit; refuses more than there is. See [Moving stock](stock-and-lending.md#moving-stock). |
| `set_condition` | `assets.report` or `assets.write` | |
| `borrow` | `assets.borrow` | A person, for themselves. |
| `lend` | `assets.lend` | |
| `return_asset` | `assets.borrow` or `assets.lend` | |
| `archive_asset`, `restore_asset` | `assets.delete` | |
| `trigger_hook` | `assets.hook` | Waits up to 10 seconds for devices' answers. |
| `hook_replies` | `assets.hook` | |
| `list_photos` | `assets.read` | As images; thumbnails unless asked. |
| `add_photo` | `assets.write` | Base64. |
| `reorder_photos`, `remove_photo` | `assets.write` | |
| `list_attachments`, `read_attachment` | `assets.read` | An asset's, a category's, a vendor's or a contract's attachments (docs/data-model.md#attachments), named by `asset`, `category_id`, `vendor_id` or `contract_id`; a file is read as an image, as text, or as a file to keep. |
| `add_attachment` | `assets.write` for an asset's, `categories.manage` for a category's, `vendors.manage` for a vendor's or a contract's | Base64, with its file's name; its type is checked by its content. |
| `update_attachment`, `reorder_attachments`, `remove_attachment` | `assets.write` for an asset's, `categories.manage` for a category's, `vendors.manage` for a vendor's or a contract's | |
| `import_assets` | `assets.write` | See below. |

`import_assets` runs a preview first. Then it is called with the same file, mapping and choice of
links again, with `confirm` and the preview's `preview_id`, so nothing is imported that was not
previewed. It creates or changes at most 50 assets, so a larger file is split.

### Catalog

Reading needs `assets.read`; changing needs `categories.manage`. Deleting is marked destructive.

| Tool | Permission | What it does |
| :--- | :--- | :--- |
| `list_categories` | `assets.read` | |
| `get_category` | `assets.read` | Its settings, the fields its assets record (own and inherited), the link types it suggests, the hooks it offers, and its scopes for those who may see them. |
| `fields_for` | `assets.read` | What an asset in some categories would record. |
| `save_category` | `categories.manage` | Makes one, or changes only what is given, though the API replaces every property. |
| `clone_category`, `delete_category` | `categories.manage` | |
| `set_category_running_low` | `stock.record` | |
| `assign_field`, `unassign_field` | `categories.manage` | |
| `set_suggested_link_types` | `categories.manage` | |
| `set_scan_template`, `set_label_templates` | `categories.manage` | |
| `scan_template_conflicts`, `label_template_conflicts` | `assets.read` | |
| `set_category_hooks` | `categories.manage` | |
| `list_fields`, `get_field` | `assets.read` | |
| `save_field`, `delete_field` | `categories.manage` | |
| `list_link_types` | `assets.read` | |
| `save_link_type`, `delete_link_type` | `categories.manage` | |
| `list_hooks` | `assets.read` | |
| `save_hook`, `delete_hook` | `categories.manage` | `save_hook` takes `triggers`: a button, events or a schedule (see [Hooks](hooks.md#triggers)); left out, they stay as they were. |
| `set_category_vendors` | `categories.manage` | The vendors a category is bought from, in order of preference. |
| `set_category_links` | `categories.manage` | The categories a category's members also count in (`implies`), or whose members count in it (`included_by`); each list given replaces that side. See [Pools](stock-and-lending.md#pools). |

### Vendors

See [Vendors](vendors.md). Reading needs `assets.read`, and shows only the vendors the caller sees;
changing needs `vendors.manage` in the vendor's scopes. Deleting is marked destructive.

| Tool | Permission | What it does |
| :--- | :--- | :--- |
| `list_vendors` | `assets.read` | The vendors, with their contacts; `query` keeps those matching every word. |
| `get_vendor` | `assets.read` | One, with whether the caller may change it. |
| `save_vendor` | `vendors.manage` | Makes one, or changes only what is given; contacts and scopes, when given, replace the vendor's. |
| `delete_vendor` | `vendors.manage` | |
| `vendor_history` | `assets.read` | Its history, newest first, a page at a time. |
| `list_catalog` | `assets.read` | What vendors sell (see [Catalogs](vendors.md#catalogs)), narrowed by `vendor_id`, `category_id` or `asset_id`, or to the `preferred` entries. |
| `get_catalog_entry` | `assets.read` | One entry. |
| `save_catalog_entry` | `vendors.manage` | Makes one, or changes only what is given; naming a category forgets an asset, and the other way about. |
| `delete_catalog_entry` | `vendors.manage` | |
| `export_vendor_catalog` | `assets.read` | A vendor's catalog as CSV. |
| `import_vendor_catalog` | `vendors.manage` | Checks a vendor's catalog as CSV, then, with `confirm`, imports it; up to 50 entries made or changed at a time. |

### Contracts

See [Contracts](contracts.md). Reading needs `assets.read`, and shows only the contracts whose
vendors the caller sees, their costs only with `purchases.read` there; changing needs
`vendors.manage` in the vendor's scopes. Deleting is marked destructive. The attachment tools take
`contract_id`.

| Tool | Permission | What it does |
| :--- | :--- | :--- |
| `list_contracts` | `assets.read` | The contracts, by when they end; `vendor_id`, `category_id`, `asset` and `query` narrow them. |
| `get_contract` | `assets.read` | One, with what it covers and whether the caller may change it. |
| `save_contract` | `vendors.manage` | Makes one, or changes only what is given; the assets and categories it covers, when given, replace its. |
| `delete_contract` | `vendors.manage` | |
| `contract_history` | `assets.read` | Its history, newest first, a page at a time, its reminders among it. |

### Purchase orders

See [Purchase orders](purchase-orders.md). Reading needs `assets.read`, and shows only the orders
whose vendors the caller sees, their prices only with `purchases.read` there; changing needs
`vendors.manage` in the vendor's scopes, and receiving what each line's change needs. Deleting is
marked destructive. The attachment tools take `purchase_order_id`.

| Tool | Permission | What it does |
| :--- | :--- | :--- |
| `list_purchase_orders` | `assets.read` | The orders, newest first; `vendor_id`, `state` (or `open`), `category_id`, `asset` and `query` narrow them. |
| `get_purchase_order` | `assets.read` | One, with its lines, what has come, and totals where the caller sees prices. |
| `save_purchase_order` | `vendors.manage` | Makes a draft, or changes only what is given; its lines, when given, replace its. |
| `delete_purchase_order` | `vendors.manage` | |
| `place_purchase_order`, `cancel_purchase_order` | `vendors.manage` | Places a draft; cancels one placed. |
| `receive_purchase_order` | `stock.record` or `assets.write` per line | Receives a delivery, every line or none; `queue_labels` adds the assets made to the print queue. |
| `suggest_purchase_orders` | `vendors.manage` | Draft orders of what is running low, by vendor, with what is already on order apart. |
| `purchase_order_history` | `assets.read` | Its history, newest first, a page at a time. |
| `purchase_order_pdf` | `assets.read` | The order as a PDF to send, as an embedded file. |
| `get_order_numbering`, `set_order_numbering` | `assets.read`, and `settings.manage` to set | How orders' numbers are shown: a prefix, and at least how many digits. |

### Comments

See [Comments](comments.md). Reading needs `assets.read`, and shows only the comments on what the
caller may read; writing needs `comments.write` in its scopes. A comment is named by `asset` (an ID
or a code), `category_id`, `loan_id`, or `asset` with `flow_id` for a filing.

| Tool | Permission | What it does |
| :--- | :--- | :--- |
| `list_comments` | `assets.read` | The comments on something, with its categories' and their ancestors' unless `own` is set, each with its Markdown and HTML. |
| `get_comment`, `comment_history` | `assets.read` | One comment, and its history: each version's text. |
| `search_comments` | `assets.read` | Comments whose text holds some words. |
| `list_filings` | `assets.read` | The flows that filed an asset, whose filings take comments. |
| `list_comment_people` | `comments.write` and `assets.track` | The people a comment on something may mention, by name and user ID ([Mentions](comments.md#mentions)). |
| `add_comment` | `comments.write` | Writes one, in Markdown; `<@id>` mentions a person, who is emailed once. |
| `attach_to_comment` | `comments.write` | Attaches a file, base64, to the asset or category a comment is being written on, answering the Markdown that links to it. |
| `edit_comment` | `comments.write` | The caller's own. |
| `delete_comment` | `comments.write` or `comments.moderate` | The caller's own, or anyone's with `comments.moderate`. |

The editor's preview, `POST /api/v1/markdown`, is left out: comments come back with their HTML.

### Icons

These cover [icons](icons.md). Reading needs `assets.read`, changing a category's icon needs
`categories.manage`, and changing the rest needs `icons.manage`.

| Tool | Permission | What it does |
| :--- | :--- | :--- |
| `icons_overview` | `assets.read` | Whether icons can change here; the build's icons and warnings; the palette and the settings; and the icons assets have as their own that nothing draws. |
| `get_icon_ir` | `assets.read` | One icon's shapes, or every icon's. |
| `draw_icon` | `assets.read` | An icon as a PNG, by name or a category's or an asset's, to show the person. |
| `list_icon_sources` | `assets.read` | |
| `get_category_icon`, `get_icon_library_file` | `assets.read` | With their published versions. |
| `check_category_icon`, `save_category_icon`, `rename_category_icon`, `delete_category_icon`, `discard_category_icon_draft` | `categories.manage` | A category's icon. |
| `save_icon_library_file`, `rename_icon_library_file`, `delete_icon_library_file`, `discard_icon_library_draft` | `icons.manage` | Icon library files. |
| `set_icon_palette`, `set_icon_settings` | `icons.manage` | |

A save that comes back as a draft says so, listing the compiler's errors as
`file:line:col: message`.

### Labels and templates

| Tool | Permission | What it does |
| :--- | :--- | :--- |
| `preview_template` | `templates.manage` | Shows a template as it prints. See below. |
| `import_svg` | `templates.manage` | Turns SVG artwork into shapes fitted into a box, to add with `save_template`. |
| `save_template` | `templates.manage` | Makes a template or changes one, whole or by elements. See below. |
| `list_templates`, `get_template` | `assets.read` | |
| `delete_template`, `adapt_template`, `template_size_range` | `templates.manage` | |
| `test_print` | `labels.print` | |
| `render_label` | `assets.read` | An image, or a PDF or SVG file. |
| `export_labels` | `assets.read` | PDF or ZIP. |
| `label_options` | `assets.read` | |
| `print_labels` | `labels.print` | Up to 50. |
| `list_images`, `get_image` | `assets.read` | Images for templates; `get_image` returns the image. |
| `upload_image`, `delete_image` | `templates.manage` | Images for templates. |

`preview_template` shows a template, saved or a draft, as it prints. It gives a PNG of each label,
filled from up to 10 assets or placeholders, with small ones shown a whole number of times larger.
For each label, it gives every element's box and exactly what it prints (the preview's JSON), so a
model reads rather than guesses. Optionally, it lays the template out for another size or a
printer's roll, and checks it against a printer, naming the elements outside what it prints.

`save_template` changes a template whole, or by elements: change, move in the drawing order,
remove, or add in front, by index. So an edit and a look are two calls. Templates an assistant
gives are drawn in list order unless they say otherwise.

### Printers and the print queue

Changing printers, label stock and pools needs `settings.manage`. See [Printing](printing.md).

| Tool | Permission | What it does |
| :--- | :--- | :--- |
| `list_printer_drivers`, `list_printers`, `get_printer` | `labels.print` or `settings.manage` or `templates.manage` | |
| `check_fit` | `labels.print` or `settings.manage` or `templates.manage` | |
| `save_printer` | `settings.manage` | Changes only what is given. A change affecting labels waiting is not saved until called again with `confirm`. |
| `delete_printer` | `settings.manage` | |
| `list_label_stock`, `list_printer_pools`, `get_printer_pool` | `labels.print` or `settings.manage` or `templates.manage` | |
| `save_label_stock`, `delete_label_stock`, `save_printer_pool`, `delete_printer_pool` | `settings.manage` | |

The print queue needs `labels.print`:

| Tool | Permission | What it does |
| :--- | :--- | :--- |
| `list_print_queue`, `queue_labels`, `update_print_queue` | `labels.print` | |
| `remove_from_print_queue`, `put_back_in_print_queue` | `labels.print` | |
| `print_queue`, `download_print_queue` | `labels.print` | |
| `list_print_jobs`, `cancel_print_job`, `resend_print_job` | `labels.print` | |

### Reports and scan templates

Reading needs `assets.read`; changing needs `reports.manage`. See [Reports](reports.md) and
[Scanning](scanning.md).

| Tool | Permission | What it does |
| :--- | :--- | :--- |
| `list_reports` | `assets.read` | Reports, or scan templates, with `kind`. |
| `get_report` | `assets.read` | |
| `run_report` | `assets.read` | Each section's rows, groups or chart data. |
| `report_csv` | `assets.read` | A section as a CSV file. |
| `report_pdf` | `assets.read` | The report as a PDF ([Reports](reports.md#pdf)), as an embedded file. |
| `get_loan_reminder_settings`, `set_loan_reminder_settings` | `settings.manage` | Whether borrowers are emailed about their loans, and when ([Stock and lending](stock-and-lending.md#loan-reminders)). |
| `get_document_settings`, `set_document_settings` | `assets.read`, and `settings.manage` to set | The instance's name on PDFs, and their page size. |
| `preview_report` | `reports.manage` | A spec unsaved; or a scan template for an asset. |
| `save_report` | `reports.manage` | Its name or spec; what is not given is kept. |
| `delete_report` | `reports.manage` | |
| `set_default_scan_template` | `reports.manage` | |

### Administration

People, roles and scopes need `users.manage`. Channels need `settings.manage`.

| Tool | Permission | What it does |
| :--- | :--- | :--- |
| `list_users`, `create_user`, `set_user_active`, `archive_user`, `set_user_name` | `users.manage` | `create_user` can email an invitation in place of a password. |
| `invite_user` | `users.manage` | Email someone an invitation again ([Access](access.md#invitations)). |
| `set_user_password` | `users.manage` | Someone else's. The anti-lockout rule holds, as anywhere. |
| `clear_sign_in_delay` | `users.manage` | Lets someone sign in again at once, when wrong passwords hold them back. |
| `merge_person` | `users.manage` | Merges a person without an account into another person: every person field naming them names the other, each asset's history saying so ([Person fields](data-model.md#person-fields)). |
| `reset_user_factors` | `users.manage` | Removes someone's second factors, for one who has lost theirs, and signs them out everywhere. Setting up one's own is never over MCP. |
| `set_user_roles` | `users.manage` | Each role in every scope, or narrowed to some. |
| `list_roles`, `save_role` (changing only what is given), `delete_role` | `users.manage` | `save_role` sets whether a role requires two-factor sign-in, but not the admin role's: turning that off stays with a person. |
| `list_scopes` | `users.manage` or `categories.manage` | |
| `save_scope`, `delete_scope`, `set_category_scopes` | `users.manage` | |
| `list_live_connections` | `users.manage` | |
| `list_identity_providers`, `save_identity_provider` (given whole; secrets left out are kept), `delete_identity_provider`, `set_password_sign_in` | `settings.manage` | Identity providers, and whether a password signs in ([Access](access.md#signing-in-with-an-identity-provider)). |
| `list_user_identities`, `link_user_identity`, `unlink_user_identity` | `users.manage` | Someone's identities at providers. Signing in through one, and linking one's own, are a person's, in a browser. |
| `list_channel_drivers`, `list_channels`, `get_channel`, `save_channel`, `delete_channel`, `test_channel`, `list_deliveries`, `retry_delivery` | `settings.manage` | Channels ([Notifications](notifications.md)). |
| `list_relays`, `add_relay`, `new_relay_code`, `revoke_relay` | `settings.manage` | Relays: adding one gives the code that pairs it ([Relays](relays.md#pairing)). |

A channel's secrets, such as a webhook URL, are written and never read back. They show as set or
not, and a change that leaves one out keeps it.

| Tool | Permission | What it does |
| :--- | :--- | :--- |
| `list_incoming_hooks`, `save_incoming_hook` (changing only what is given), `delete_incoming_hook` | `settings.manage` | Incoming hooks ([Hooks](hooks.md#incoming-hooks)). An incoming hook's secret is in the answer that makes or replaces it, and never again. |

An incoming hook's own address, which senders outside BAM! post to, is not a tool: an assistant does
what it does with the tools themselves.

### Flows

Managing needs `flows.manage`; running needs `flows.run`, as a person. See [Flows](flows.md).

| Tool | Permission | What it does |
| :--- | :--- | :--- |
| `list_flows` | `flows.manage` or `flows.run` | Flows; a flow; those an asset starts; and what is overdue. |
| `flow_starters` | `flows.manage` | The starter flows over some categories. |
| `save_flow` | `flows.manage` | Changes only what is given. A version 1 definition is refused, as the API refuses it. |
| `delete_flow` | `flows.manage` | |
| `run_flow` | `flows.run` | For a person: starts a run (optionally `from_asset_id`), reads it, answers it, or gives it up. A form is answered by its questions' keys, which a form step's summary names. A photo question cannot be answered with a photo over MCP. |

### Live events and credentials

| Tool | Permission | What it does |
| :--- | :--- | :--- |
| `list_events` | `events.read` | Uses `GET /api/v1/events`. |
| `reply_to_hook` | `events.read` | Uses `POST /api/v1/events/{id}/reply`. |
| `list_tokens` | `users.manage` | A person's own personal tokens; with `users.manage`, every token. |
| `revoke_token` | `users.manage` | As `list_tokens`. Revoking the token it acts through signs an assistant out. |

## Live events

`GET /api/v1/events` (`events.read`) lists the events a live connection would be sent, oldest
first ([Hooks](hooks.md)). They are filtered and stripped of people as they are there.

| Parameter | Meaning |
| :--- | :--- |
| `?after=` | Since an event, by its ID. Without it, the last hour. |
| `?kinds=` | Narrows by kind. |
| `?hooks=` | Narrows by hook. |
| `?categories=` | Narrows by category. |
| `?limit=` | Up to 500. The answer has `more` when there are more. |

`POST /api/v1/events/{id}/reply` (`events.read` in the asset's scopes) answers a hook event as a
device on a live connection does. Each can be answered once; a second answer is `409`.

### Subscriptions

The resources `bam://events{?after,kinds,hooks,categories}` and `bam://asset/{id}` can be read and
subscribed to. A subscription is made on a `subscriptions/listen` request (protocol `2026-07-28`).
A stateless server keeps no session for the older `resources/subscribe`.

A subscription asks the API every second, as its caller, whether anything new happened. It tells
the client which resource changed, for the client to read. It ends with its request, or when the
caller is revoked or loses the permission. A caller holds at most 5 at once on each server.

The WebSocket itself is for devices, and is not reached over MCP.

## Credentials

`list_tokens` and `revoke_token` cover, for a person, their own personal tokens, and with
`users.manage`, every token. Revoking the token it acts through signs an assistant out.

Nothing over MCP makes tokens or changes anyone's own password. `internal/mcpserver/coverage.go`
lists those routes as refused, with why. It also lists signing in with a password, whose
counterpart over MCP is OAuth, and turning two-factor sign-in off for the admin role.

## Signing in with OAuth

### Discovery and registration

Asked without a token, `/api/v1/mcp` answers `401` with
`WWW-Authenticate: Bearer resource_metadata="…"`. That names
`/.well-known/oauth-protected-resource/api/v1/mcp` (RFC 9728), which names the server itself as
the authorization server. `/.well-known/oauth-authorization-server` (RFC 8414) gives its endpoints.

Clients register themselves at `POST /oauth/register` (RFC 7591):

- as public clients, with no secret;
- with redirect URIs that are `https`, or `http` on the machine itself;
- at most 30 a server an hour.

A client no one approved within a day is dropped.

### Authorization

`GET /oauth/authorize` takes the authorization code flow with PKCE, `S256` only. It takes the
optional `resource` (RFC 8707), which must be the MCP endpoint.

- A client or redirect URI that is not registered is answered in the browser, and never followed.
- Anything else wrong is sent back to the client as an error.
- A good request goes to the consent page, `/oauth/consent`.

On the consent page, the person signs in if they have not, reads what the assistant will be able
to do, and allows or denies it. The page uses `GET /api/v1/oauth/clients/{id}` and
`POST /api/v1/oauth/authorize`, from a signed-in session.

### Tokens

`POST /oauth/token` trades the code for a grant. The code can be traded once, within 10 minutes,
with its verifier.

The grant is a personal token ([Access](access.md)) named after the client:

- Its access secret lasts an hour.
- Its refresh secret, `bamr_…`, renews both. The old ones then no longer work.
- It lasts 30 days at a time, while used.

A grant is listed and revoked under *Your account › Assistants*. Revoking it, or turning the
account off, ends it.

## Internals

The server is built on the official Go SDK (`github.com/modelcontextprotocol/go-sdk`), and speaks
every protocol version it speaks.

It is stateless. Each request is answered on its own, by a server holding the tools its caller may
use. So any of several servers sharing a database answers any request, and permissions are read
afresh each time. Nothing is announced to clients unasked, since there is no session to announce it
on; the lists are always current instead.

OAuth's storage was added by migration `032`. Codes and refresh secrets are kept only as hashes.

### Route coverage

A test lists every API route, and fails for one that no tool reaches and that is not named in
`internal/mcpserver/coverage.go`, with the issue that will cover it. So a route added later cannot
be left out.

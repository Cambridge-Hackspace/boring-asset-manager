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
| `set_my_name` | Anyone signed in | Sets the caller's name; for a person only. |

### Finding assets

| Tool | Permission | What it does |
| :--- | :--- | :--- |
| `search_assets` | `assets.read` | Every filter the asset list takes, and running low; 25 at a time, up to 200, with `offset`. |
| `get_asset` | `assets.read` | Its fields, codes, place, contents, links, photos, hooks and icon, and where the icon comes from. |
| `look_up_code` | `assets.scan` | Its scan page ([Scanning](scanning.md)). |
| `asset_history` | `assets.read` | |
| `list_loans` | `assets.track` | |
| `list_people` | `assets.lend` or `users.manage` | Emails only to tell apart two people of the same name. |
| `next_code` | `assets.write` | |
| `export_assets` | `assets.read` | The CSV ([CSV](csv.md)), as an embedded file. |

### Changing assets

| Tool | Permission | What it does |
| :--- | :--- | :--- |
| `create_asset` | `assets.write` | Up to 50, with field values by key, codes or the next free code, the place it is in, stock, and an icon of its own. A field its categories do not give it is refused before anything is added. |
| `copy_asset` | `assets.write` | One like another, as `GET /api/v1/assets/{id}/copy` drafts it, with a name, field values, codes and quantity that differ, and its photos if asked. |
| `update_asset` | `assets.write` | Only what is given changes: categories and codes are added or removed, fields set or cleared, an icon of its own given or dropped; the rest is kept. |
| `move_asset` | `assets.write` | |
| `link_assets` | `assets.write` | By the link type's name, read from either end. |
| `unlink_assets` | `assets.write` | |
| `record_stock` | `stock.record` | Use, restock or set. |
| `set_running_low` | `stock.record` | |
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
| `save_hook`, `delete_hook` | `categories.manage` | |

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
| `preview_report` | `reports.manage` | A spec unsaved; or a scan template for an asset. |
| `save_report` | `reports.manage` | Its name or spec; what is not given is kept. |
| `delete_report` | `reports.manage` | |
| `set_default_scan_template` | `reports.manage` | |

### Administration

People, roles and scopes need `users.manage`. Channels need `settings.manage`.

| Tool | Permission | What it does |
| :--- | :--- | :--- |
| `list_users`, `create_user`, `set_user_active`, `archive_user`, `set_user_name` | `users.manage` | |
| `set_user_password` | `users.manage` | Someone else's. The anti-lockout rule holds, as anywhere. |
| `set_user_roles` | `users.manage` | Each role in every scope, or narrowed to some. |
| `list_roles`, `save_role` (changing only what is given), `delete_role` | `users.manage` | |
| `list_scopes` | `users.manage` or `categories.manage` | |
| `save_scope`, `delete_scope`, `set_category_scopes` | `users.manage` | |
| `list_live_connections` | `users.manage` | |
| `list_channel_drivers`, `list_channels`, `get_channel`, `save_channel`, `delete_channel`, `test_channel`, `list_deliveries` | `settings.manage` | Channels ([Notifications](notifications.md)). |

A channel's secrets, such as a webhook URL, are written and never read back. They show as set or
not, and a change that leaves one out keeps it.

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
counterpart over MCP is OAuth.

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

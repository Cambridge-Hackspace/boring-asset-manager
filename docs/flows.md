# Flows

A **flow** is a guided page for doing one everyday thing with assets, without knowing bam. For
example: leaving wood in the wood shop overnight with a name on it, or taking it home again.
Whoever manages flows builds each one from an ordered list of configurable steps, over the
categories it files into or takes from. Anyone allowed to run it then follows the steps on a phone
or the shop's tablet.

## Permissions

| Permission | Allows |
| :--- | :--- |
| `flows.manage` | Making and changing flows. |
| `flows.run` | Running flows. It is scopable. |

A flow can be run by those holding `flows.run` in the scopes of at least one of its categories. Its
Choose category step offers only the categories they hold it in.

Saving a flow needs every permission its steps use, so a flow never offers more than its creator
could do:

| Step does | Needs |
| :--- | :--- |
| Create, set fields, or move | `assets.write` |
| Print or queue a label | `labels.print` |
| Archive | `assets.delete` |
| Set a condition | `assets.report` |
| Borrow to oneself, or return | `assets.borrow` |
| Trigger a hook | `assets.hook` |
| Find | `assets.read`, and `assets.scan` to find by scanning |

## Running a flow

**Flows** (`/flows`) lists the flows to run. A flow runs at `/f/<slug>`, with large controls, for a
phone or the shop's tablet. It works with an on-screen keyboard, or a barcode scanner that types,
and scans with the camera where the browser can read codes.

Each type of question has its own control:

- text: a box
- long text: a larger box
- number or date: a number or date box
- yes or no: **Yes** and **No** to choose
- choice: the options to choose, as a list when there are more than eight
- photo: a photo to take or choose

The page shows progress, such as "step 2 of 3". See [Progress](#progress).

### What a run does

A run acts as the person running it, signed in. Every step that changes something calls the API's
own endpoints, in process, as them. So it is held to their permissions and scopes (`404`/`403`),
and to every rule those endpoints keep. It is recorded in history as theirs, made through the flow:
"Sam, via flow “Leave something”".

A run moves one step at a time, and each answer names the step it answers. A step can be neither
skipped nor answered twice. Two answers at once do the step's work once.

Steps that ask nothing are done as soon as they are reached. These are Set fields, Label, Change, a
fixed category, and Create without a scanned place.

A step that fails, such as on an asset the runner may not change, leaves the run on it, to try
again. What it had done before failing is not done twice.

A run left for 12 hours goes.

### Starting from an asset

A flow whose first step is a Find can be started from what it would find. The asset page offers
the flows that can start from it, and the scan page offers them through the quick action `flows`
([Scanning](scanning.md)). The run then starts with the asset found, past the Find.

### Left past its date

A flow's `leave_until` names a date field its categories record, as a date to collect by. What the
flow filed, while still active, is overdue the day after that date.

- It is listed under `overdue` for the flow, by `asset_id`, `name` and `until`, among the assets the
  requester may read.
- It is marked in Find's list.
- The notification worker records `asset.flow_overdue` once for each, with the flow and the date.
  Channels subscribe to it as they do to running low, and live connections receive it
  ([Notifications](notifications.md)).

Moving the date on clears it, so it is announced again if the new date passes too.

## Starter flows

Two ready-made starter flows are there to adjust:

- **Leave something**: Choose category → Form → Set fields → Create → Label → Done. Its form asks
  what it is, as `{name}`, and for each field the categories record. Its Set fields step sets each
  field from its answer, and is left out when the categories record none. With only one category,
  there is nothing to choose, so it starts with its Form.
- **Collect it**: Find → Confirm → Change (archive) → Done.

## Building a flow

For those with `flows.manage`, **Flows** lists every flow, to turn on or off, delete, and see what
each filed past its date.

`/flows/new` and `/flows/<slug>/edit` edit a flow, laid out as form builders lay out questions. Its
steps are cards, in order, each with its settings and what stops it being saved. Beside them is a
preview of the chosen step, as a phone shows it.

### Step IDs

A step's `id` is the editor's to give. It is made from the step's kind and is unique in the flow
(`form`, `form_2`, …), and it is not shown. An ID a step already has is kept, so runs under way
still match their answers to it.

### Templates and values

Each box that takes a template suggests the values that step can use. This applies to a heading,
text, Create's name, a question's default, a Set fields value, Find's match, and a Change's note.

- Suggestions appear as `{` is typed, or from the box's **{ }** button.
- Each says where it comes from.
- A value from a step shown only sometimes is marked as possibly empty.
- A name no earlier step sets is marked.

The list follows the rules saving checks with. `internal/flows/testdata/values.json` pins the two
together.

A step's **Only when** chooses its value from that list, and compares it with what suits it: a
category, a choice question's choices, yes or no, a date, or text.

### Questions

A question's key is made from its text as it is typed, and shown under it as "Used as `{what}`".
**Change** sets another. Changing a key that later steps name says which steps first. A choice's
values are made from its options' labels the same way.

### Set fields rows

A Set fields step's rows choose a field from those each of the flow's categories records, grouped
by category under their own labels. A choice field offers its choices as values. The step says
which asset it sets.

### Posters

`/flows/<slug>/poster` is the flow's poster, to print. It has the flow's name, a QR code of its
address, and its one line of instructions. It can also be made into a label template, to print on
a label printer from the template editor.

The QR code comes from `GET /api/v1/flows/{slug}/qr.svg`. It is drawn by bam's own encoder, to
`[server] base_url` or the address it was asked at.

## The definition

A flow's `definition` is versioned JSON, `version` 2. It holds:

- `description`: one line of instructions, for its poster;
- `category_ids`: the categories it files into or takes from;
- `steps`: its steps, in order;
- optionally `leave_until`: a date field to collect by.

### Values

Steps share a run. Each step reads and writes the run's values, which later steps name in
templates, such as `{owner}: {what}`, and in defaults.

| Value | Set by |
| :--- | :--- |
| `{today}`, `{now}`, `{user}` (who runs it), `{flow}` | Every run. |
| `{category}`, `{category_id}` | Choosing a category. Set from the start when the flow has only one category. |
| Each question's key | A form, with its answer. |
| Each field's key | A Set fields step, for each field it sets. |
| `{asset}`, `{asset_id}`, `{code}`, and the asset's field values | Finding or making an asset. |

A value not set, such as by a step that was skipped, is empty.

### Common step settings

Each step has:

- an `id`;
- a `kind`;
- an optional `title`, shown as the step's heading, and called **Heading** in the editor;
- optional `text`;
- optionally `when`, a condition on an earlier value: `is`, `is_not`, `set` or `unset`. The step is
  shown only when it holds.

### Kinds of step

#### Choose category (`choose_category`)

Offers the flow's categories, or only those in `categories`. Empty or absent `categories` offers
every category the flow has, including any added later. Alternatively, it fixes one category
(`fixed`) and asks nothing.

#### Form (`form`)

Asks its own `questions`, in order. Each question has:

- `key`: the run's value its answer becomes;
- `label`: the question;
- `help`;
- `default`: a template, such as `{user}` or `{today}`;
- `required`;
- `type`, one of:
  - `text` (the default)
  - `long_text`
  - `number`
  - `date`, as `2026-10-30`
  - `yes_no`, answered `yes` or `no`
  - `choice`: one of the `options` it lists, each `{"value", "label"}`, shown as its value when it
    has no label
  - `photo`

A photo question's answer is the name of the file chosen. The photo itself stays on the page that
took it until a Set fields step puts it on an asset that exists. That page then adds it with the
photo endpoint (see the run's `photos`, under [API](#api)).

A form knows nothing of the categories' fields. What it asks is the flow's, in its own words, and a
Set fields step decides which field each answer goes into.

#### Set fields (`set_fields`)

Puts values into the asset's fields, and asks nothing.

Its `fields` are rows, `{"field": key, "value": template}`. Each names a field the flow's
categories record and the value to give it: an answer such as `{leave_until}`, a value every run
has such as `{today}` or `{user}`, fixed text, or a mix.

Its `photo`, a photo question's answer such as `{photo}`, puts that photo on the asset.

Where the step is decides what it sets:

- When a Create comes after it, before any Find, it fills the new asset that Create files, as it is
  filed, so a category's required fields are met. The editor says "On the new asset, as it is
  filed".
- Otherwise it updates the asset the nearest Find or Create before it found or filed. The editor
  says "On the asset found" or "On the asset filed".

A row naming a field the asset's category does not record, as one of the flow's other categories
does, is passed over. A value that does not suit its field, such as an answer that is not a date,
fails the step, saying why, as any other failure does.

#### Find (`find`)

Finds an asset by scanning its code, by search, or from a list (`find_by`). It looks among the
active assets in the flow's categories, or below them, that the runner may read and run flows on.
Optionally it narrows to:

- those a flow filed (`filed_by`, a flow's ID);
- those whose field matches a value (`match`, such as the owner being `{user}`).

#### Create (`create`)

Files a new asset in the chosen category, with the fields Set fields steps gave it, named from a
template (`name`). Optionally it puts the asset in a place, fixed or scanned (`location`).

#### Label (`label`)

Prints the asset's label to a printer or pool (`print`), adds it to the print queue (`queue`), or
shows its code on screen (`show`). It uses the category's label template, or `template_id`.

#### Change (`change`)

Does, in order, any of:

- `archive`
- set its `condition`, with a note
- `borrow` it to the runner, or `return` it
- move it to a `place`, or another `category`
- trigger a `hook`: `{"kind": "hook", "hook": key}`

Archiving is how bam removes an asset: it is kept, out of the lists, and restorable. Setting its
fields is a Set fields step's job.

#### Confirm and Done (`confirm`, `done`)

`confirm` shows what will happen, including Set fields' values. `done`, last, shows what did.

### Example

*Leave something*, over short and long boards. Both record an owner, and long boards also record a
date to collect by:

```json
{
  "version": 2,
  "description": "Scan to leave something here, with your name on it.",
  "category_ids": ["<short boards>", "<long boards>"],
  "leave_until": "until",
  "steps": [
    {"id": "kind", "kind": "choose_category", "title": "What are you leaving?"},
    {"id": "about", "kind": "form", "title": "About it", "questions": [
      {"key": "name", "label": "What is it?", "type": "text", "required": true},
      {"key": "owner", "label": "Whose is it?", "type": "text", "default": "{user}", "required": true},
      {"key": "photo", "label": "A photo", "type": "photo"}
    ]},
    {"id": "long_only", "kind": "form", "when": {"value": "category_id", "op": "is", "to": "<long boards>"},
     "questions": [{"key": "leave_until", "label": "Leave until", "type": "date", "help": "When you'll take it home"}]},
    {"id": "set", "kind": "set_fields", "fields": [
      {"field": "owner", "value": "{owner}"},
      {"field": "until", "value": "{leave_until}"}
    ], "photo": "{photo}"},
    {"id": "file", "kind": "create", "name": "{owner}: {name}"},
    {"id": "label", "kind": "label", "label": {"mode": "show"}},
    {"id": "done", "kind": "done", "title": "Filed", "text": "{asset} is filed. Put its label on it."}
  ]
}
```

### Rules checked when saving

When a flow is saved:

- It must be version 2. Version 1 is refused as below, and any other version is refused outright.
- It must name at least one category to file into or take from, and have between 1 and 50 steps.
- Its last step must be Done, and Done must be the last step.
- Each step ID must be 1 to 32 lowercase letters, digits and `_`, starting with a letter, and
  unique in the flow. Each question key follows the same rule, up to 64 characters.
- A date to collect by must be a date field the flow's categories record.
- Every category, field, template, printer, pool, place, hook and flow it names must exist.
- Every template and condition must name only values set before it.
- A fixed value in Set fields must suit its field: a date a date, a number a number, a choice one of
  the field's choices.
- A question's default must suit its question.
- The creator must hold every permission the steps use.

Required fields must also be set before each Create. Consider each category a Create may file
into: those its Choose category step offers, the one it fixes, or the flow's only category. For a
Create shown only when `category_id is` some category, only that one counts. Every field that
category requires, own or inherited, must be set by a Set fields step before that Create. The Set
fields step must have no condition, or be shown only when `category_id is` that category. Otherwise
saving names each field missing.

The editor makes the same checks as it goes.

### Version 1 definitions

A definition of `version` 1, from before forms asked their own questions, is refused (`400`). The
refusal says what changed:

- A Form's fields, `ask_name` and `ask_photo` became questions of its own.
- A Set fields step (`set_fields`) took the place of Change's `fields`.

Migration `050` converted every saved flow to version 2. An archive written before it is converted
the same way on import ([Backups](backups.md)). The conversion:

- Turned a Form's fields, as it asked them for the categories it could be asked for, into questions
  with the same key (so `{owner}` still works), label, help, default and required.
- Gave each question the type of its field: a select field's choices copied as options, numbers and
  dates as such, and the rest text.
- Asked a field that only some of those categories record, or require, of all of them, required
  only where all require it.
- Turned asking for the name and a photo into the questions `{name}` and `{photo}`.
- Turned a field hidden with a default into a row of Set fields, set to that default.
- Put a Set fields step just before each Create, setting each of those fields from its answer, and
  the photo.
- Turned each Change's fields into a Set fields step where they were, splitting the Change around
  it so that everything happens in the same order.

New steps took IDs as the editor gives them (`set`, `set_2`, …). Runs under way when the migration
ran were dropped, since their place and answers were by the steps they began with; the person
starts again.

## API

| Method | Route | Permission | Purpose |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/flows` | See below | List flows, with `overdue`. |
| `GET` | `/api/v1/flows/{slug}` | See below | Read one flow, with its `overdue`. |
| `POST` | `/api/v1/flows` | `flows.manage` | Make a flow. |
| `PUT` | `/api/v1/flows/{slug}` | `flows.manage` | Change a flow. |
| `DELETE` | `/api/v1/flows/{slug}` | `flows.manage` | Delete a flow. |
| `GET` | `/api/v1/flow-starters?category_ids=…` | `flows.manage` | The starters over those categories, to adjust and save. |
| `GET` | `/api/v1/flows/{slug}/qr.svg` | | The QR code for the flow's poster. |
| `GET` | `/api/v1/assets/{id}/flows` | `flows.run` | The flows the requester may start from an asset. |
| `POST` | `/api/v1/flows/{slug}/runs` | `flows.run` | Start a run. |
| `GET` | `/api/v1/flows/{slug}/runs/{id}` | The run's starter | Read a run. |
| `POST` | `/api/v1/flows/{slug}/runs/{id}` | The run's starter | Answer the step the run is on. |
| `DELETE` | `/api/v1/flows/{slug}/runs/{id}` | The run's starter | Give the run up. |

### Managing flows

`GET /api/v1/flows` lists every flow to those with `flows.manage`. To others it lists the flows
turned on that the requester may run. `GET /api/v1/flows/{slug}` reads one flow, to either.

`POST /api/v1/flows` and `PUT /api/v1/flows/{slug}` take `{"slug", "name", "enabled", "definition"}`.
`enabled` is on for a new flow, and kept when left out on a change.

- A flow that cannot be saved is `400`, with `problems`, each with the `step` at fault.
- An address already in use is `409`.

Deleting a flow deletes its runs and its record of what it filed. The assets stay.

### Starting a run

`POST /api/v1/flows/{slug}/runs` needs a signed-in person or a personal token. An API token holding
roles is no one, and is refused with `403`. It answers `201` with the run, its `prompt` and its
`progress`.

To start from an asset, send `{"asset_id"}`. The run then starts with the asset found, past the
Find. An asset the Find would not find leaves the run on the Find, with `400`. A flow that does not
start with a Find is refused, `400`.

`GET /api/v1/assets/{id}/flows` lists, as `{slug, name}`, the flows turned on that the requester
may start from that asset.

### The prompt

The `prompt` is what the current step asks or shows:

- asks: the categories offered; the form's questions, with their defaults rendered; the ways to
  find, and, by list or search, up to 200 assets to pick from; a place to scan;
- shows: what Confirm says will happen; what the run did.

### Progress

`progress` is `{"step", "of"}`, for "step 2 of 3". It counts only steps that ask something.

- `step` counts those the person has answered, and the current one.
- `of` adds those still ahead whose condition holds on the answers so far. So a choice that brings
  a conditional step in raises it.

Done, steps that ask nothing, and a Find answered for the person when a run starts from an asset
are not counted. `progress` is left out on Done, and on a step that asks nothing and failed.

### Reading and answering a run

`GET /api/v1/flows/{slug}/runs/{id}` reads the run again. `?search=` narrows Find's list.

`POST` to the same address answers the step the run is on, with `{"step", …}` naming that step and
its answer:

| Answer | For |
| :--- | :--- |
| `category_id` | Choose category. |
| `answers` | A form, by its questions' keys. |
| `asset_id`, or a scanned `code` | Find. |
| `place_code` | A scanned place. |
| nothing | Trying a step that failed again. |

An answer refused, by the flow or by the endpoint behind a step, is answered with its status, the
`error`, and the run as it is. An answer to a step the run is not on is `409`.

Only the person who started a run can read or answer it; others get `404`. Once finished, the run
is gone. `DELETE` gives it up.

### Photos

The run's `photos` list each photo a Set fields step put on an asset, as `{"question", "asset_id"}`.
There is no `asset_id` until the Create the step was set before has filed the asset. The page that
took the photo for that question adds it, once.

## Over MCP

The flow tools are `list_flows`, `flow_starters`, `save_flow`, `delete_flow` and `run_flow`
([MCP](mcp.md)). Managing needs `flows.manage`; running needs `flows.run`, as a person.

- `list_flows` lists flows, reads a flow, lists those an asset starts, and lists what is overdue.
- `save_flow` changes only what is given. A version 1 definition is refused, as the API refuses it.
- `run_flow`, for a person, starts a run (optionally `from_asset_id`), reads it, answers it, and
  gives it up. A form is answered by its questions' keys, which a form step's summary names.
- A photo question cannot be answered with a photo over MCP.

## Storage

| Table | Holds |
| :--- | :--- |
| `flows` | Each flow: its address (`slug`, named as scopes are, and unique), its name, whether it is enabled, and its `definition`. |
| `flow_runs` | Each run under way. |
| `flow_filings` | Which assets each flow filed, and who ran it. `overdue_notified_at` records when it was announced overdue. |

| Migration | Change |
| :--- | :--- |
| `042` | Added `flows`, `flow_runs` and `flow_filings`. |
| `044` | Added `flow_filings.overdue_notified_at`, for `asset.flow_overdue`. |
| `045` | Added the `flows` quick action to the seeded scan template. |
| `050` | Converted every flow to definition version 2. |

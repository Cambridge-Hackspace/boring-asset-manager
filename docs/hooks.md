# Hooks and live connections

A hook is something that happens to an asset, such as **Identify**, told to the devices and
notification channels listening for it: a cabinet that lights the drawer an asset is in, say. A hook
fires when someone presses its button on an asset, when something happens to an asset, or on a
schedule (see [Triggers](#triggers)). Each firing records an event for them to act on.

Devices hear about hooks, and about other events, through live connections: a device dials in over
a WebSocket and is sent the events meant for it as they happen.

The other way round, [incoming hooks](#incoming-hooks) are URLs that something outside BAM! calls to
trigger a hook, record a use or restock, set fields, or run a flow.

## Hooks

### Defining hooks

`categories.manage` defines hooks through `/api/v1/hooks`. A hook has:

- a key that devices listen for: 1–64 lowercase letters, digits, `-` and `_`, starting with a
  letter, unique;
- a label that people see;
- optionally, a question to confirm first.

Anyone who reads assets can list hooks.

The API takes the key as given. The Hooks page makes a new hook's key from its label, as field keys
are made, and warns before an existing hook's key is changed, since devices and flows know the hook
by it.

### Triggers

A hook has one or more triggers, saying what fires it:

| Trigger | Fires | Takes |
|---|---|---|
| `button` | When someone presses it on an asset's page or scan page | Nothing more. A hook has one button at most. |
| `event` | When an event of one of its `kinds` is recorded about an asset | `kinds`; optionally a `condition` and `interval_seconds` |
| `schedule` | At the times its `cron` names, for the assets its `filter` finds | `cron`; optionally `time_zone` and `filter` |

A hook saved without triggers is a button, as every hook was before triggers.

Whatever fires it, a hook fires only on assets in the categories offering it
([below](#offering-hooks-on-assets)). A hook with no button is not shown for pressing.

#### Events

An event trigger fires when an event about an asset is recorded, for any of these `kinds`:

| Kind | When an asset is… |
|---|---|
| `asset.created` | Added |
| `asset.edited` | Edited |
| `asset.restored` | Restored from the archive |
| `asset.quantity_changed` | Used or restocked: its quantity changes |
| `asset.running_low`, `asset.restocked` | Running low, or restocked |
| `asset.condition_changed` | Reported as needing maintenance or out of service, or working again |
| `asset.checked_out`, `asset.checked_in` | Lent, or returned |
| `asset.overdue` | Overdue: its loan is past due |
| `asset.flow_overdue` | Past the date a flow left it until |
| `asset.label_printed` | Its label printed or downloaded |

A hook's own events (`asset.hook`) never fire a hook, so hooks cannot fire each other in a loop.
Archiving does not either: no hook fires on an archived asset.

- `condition`, `{"field", "equals"}`, fires only when the asset's field with that key, or with
  `"field": "condition"` its condition (`ok`, `needs_maintenance` or `out_of_service`), equals the
  value.
- `interval_seconds`, up to a week, is the least time between two firings on one asset, so a burst
  of events fires it once.

An event recorded before the hook was last saved does not fire it. Events are looked at for 10
minutes after they are recorded: those recorded while no server was running do not fire hooks
later.

#### Schedules

A schedule trigger fires at the times its `cron` names, a cron expression of five fields: minute,
hour, day of the month, month, and day of the week (0-6 from Sunday, or 7 for Sunday). Each field
is `*`, a number, a range `a-b`, a list `a,b`, or any of these with a step `/n`. When both day
fields are restricted, a day matching either matches, as in cron. `0 9 * * 1` is 9:00 on Mondays;
`*/30 8-18 * * 1-5` is every half hour from 8:00 to 18:30 on weekdays.

- `time_zone` is an IANA time zone, such as `Europe/London`, that `cron` is read in: UTC when left
  out. Changes of daylight saving time are followed.
- `filter` chooses the assets, as a report's filter does (see [Reports](reports.md)): `running_low`,
  `loan` (`open` or `overdue`), `condition` (a condition or `problem`), `category_ids` and `search`.
  Archived assets are never chosen. Left out, every asset the hook is offered on.
- One run fires the hook on each asset found, at most 500.

A schedule seen for the first time starts then, firing at its next time. When servers were down
across several of its times, it fires once, for the latest.

#### Who fires it

A button is fired by whoever pressed it. Events and schedules fire a hook by itself: its event
records no person, and says which trigger fired it.

#### Several servers

Every server looks for events and schedules every 10 seconds. Each event, each firing within an
interval, and each run of a schedule is claimed in the database first, so servers sharing a
database fire each once.

### Offering hooks on assets

A category offers hooks on its assets, and with `cascades` on, on its subcategories' assets too. Set
them with `PUT /api/v1/categories/{id}/hooks` and `{"hooks": [{"hook_id", "cascades"}]}`
(`categories.manage`); `GET` lists them.

An asset's `hooks` are those its categories offer and those cascading from their ancestors, by
label. Deleting a hook takes it off every category.

### Pressing a hook

`POST /api/v1/assets/{id}/hooks/{key}` (`assets.hook`, scopable) triggers a hook. It records
`asset.hook` on an active asset offering the hook, and answers `201` with the event's ID.

One person may trigger an asset's hooks once every 2 seconds, and 30 times a minute, on each server.
Beyond that the answer is `429` with `Retry-After`.

The web app shows hooks on the asset's page. It also shows them on the asset's scan page when its
scan template includes the `hooks` quick action. A hook with a question asks it first.

### The hook event

The event's `hook` carries:

- the hook's key and label;
- `trigger`: `button`, `event` or `schedule`, and for an event trigger `cause`, the event that fired
  it (`{"id", "kind"}`);
- the asset;
- the containers the asset is in through location links (see [Data model](data-model.md)),
  outermost first, each with its codes by generator type and its field values by key.

This lets a device find the thing. Give each drawer an `led` field, and identifying a resistor
lights its drawer.

The event is kept in the asset's history, and notification channels can subscribe to it (see
[Notifications](notifications.md)). Discord shows it as "Identify: Drawer A3", pressed by whom, or
fired by an event, such as "Fired as it was lent", or on its schedule.

### Device replies

A device may answer an `asset.hook` event it was sent over a [live connection](#live-connections),
once:

```
{"type": "reply", "event_id", "ok": true, "message": "Lit drawer A3"}
```

The message is up to 255 characters. The reply is acknowledged with `{"type": "replied"}`.

The answer is kept with the device's token name. `GET /api/v1/assets/{id}/hooks/replies?event_id=`
(`assets.hook`) lists the answers to a hook triggered. The button that was pressed shows them for a
few seconds, or shows that nothing answered.

## Live connections

Devices dial in for events rather than being called. A cabinet on the hackspace network opens a
WebSocket and is sent the events meant for it as they happen.

### Connecting

`GET /api/v1/events/live` upgrades to a WebSocket (RFC 6455, version 13; anything else is `426`). It
authenticates with an API token as `Authorization: Bearer …` or with a signed-in session.

It needs `events.read`, which is scopable and held by no role but `admin` until given. A device's
token gets a role holding it in the scopes its cabinet covers. One token or user holds at most 5
connections at once (`429` beyond that).

Nothing is set up for a connection in the admin pages: the token is what lets it in. The tokens page
lists each token's open connections, on every server (`GET /api/v1/tokens/connections`,
`users.manage`).

### Messages

A connection is sent first:

```
{"type": "hello", "connection_id", "filter"}
```

Then it is sent `{"type": "event", "event": …}` for each event recorded after it opened that is
about an asset or category in a scope where it holds `events.read`. People appear by name only. An
event is sent only when the connection holds `assets.track` in that scope too.

### Filtering

A connection may narrow what it is sent when connecting, with comma-separated query parameters:

- `?kinds=`: event kinds;
- `?hooks=`: hook keys;
- `?categories=`: categories, each with its subcategories.

It may change the filter later by sending:

```
{"type": "filter", "filter": {"kinds": […], "hooks": […], "categories": […]}}
```

Empty lists do not narrow. The filter's old name, `actions`, is refused: with `400` when connecting,
and with an error message after.

Anything else a device sends, other than a filter or a [reply](#device-replies), is answered with
`{"type": "error", "message"}`.

### Delivery across servers

Every server reads new events from the database while it holds connections. It reads at once after
a request that changed something, and every second otherwise, which brings what other servers
sharing the database recorded. Clocks differ between servers, so each read looks back a few seconds
and skips what was sent.

Connections are live only. One that drops is forgotten, and what happened meanwhile is not
replayed. A connection falling far behind is closed.

### Keeping connections authorized

The server pings each connection every 30 seconds. Every 10 pings it checks the token or user may
still read events, and refreshes its permissions.

A connection whose token or user is revoked, deactivated or without `events.read` is closed with a
policy-violation status saying why. This happens at once on the server that revoked it, and within
seconds on others.

Each server lists its open connections in the database, so every server can count a token's. Those
a stopped server left drop off the list after a minute.

## Incoming hooks

An incoming hook is a URL that lets something outside BAM! act on it without an API client: a
button on a cabinet, a Home Assistant automation, a form on another site, or a scale reporting a
weight. Where a [hook](#hooks) tells devices that something is happening, an incoming hook lets a
device, or anything else that can send an HTTP request, make something happen.

`settings.manage` makes them, on the **Incoming hooks** page or through `/api/v1/incoming`.

### What an incoming hook has

- **A URL**, `{base_url}/api/v1/incoming/{id}`. Its ID is 48 random hexadecimal characters, made by
  BAM!.
- **A way to authenticate** (`auth`), one of:
  - `bearer`: a secret, sent as `Authorization: Bearer <secret>`;
  - `hmac`: requests signed with a secret, in the same scheme BAM!'s webhooks sign theirs with (see
    [below](#signed-requests)), for senders that sign.
- **What it does** (`action`), one of:
  - `hook`: trigger a hook on the asset, by its key (`hook`), as a person pressing its button does;
  - `quantity`: record a use or restock of the asset's stock, from the request's `adjust` or `set`,
    as `POST /api/v1/assets/{id}/quantity` takes them (see [Stock and lending](stock-and-lending.md));
  - `fields`: set fields on the asset, from the request's JSON (see [Setting fields](#setting-fields));
  - `flow`: run a flow (`flow_id`) to its end, as a form elsewhere would file something (see
    [Running a flow](#running-a-flow)).
- **How the asset is found** (`asset`), with `by` one of:
  - `fixed`: always the same asset (`asset_id`);
  - `id`: the asset ID at `path` in the request;
  - `code`: a code at `path` in the request, of any generator type;
  - `field`: the asset whose field keyed `field` holds exactly the value at `path` in the request;
  - nothing, for a flow only: the flow finds or files the asset itself.
- **Who it acts as** (`roles`): roles, each in every scope or narrowed to some, as an API token's are
  (see [Access](access.md#narrowing-a-role-to-scopes)). It holds the public role's permissions too.
- **A rate** (`rate_limit`): how many requests it takes a minute, 1 to 600; 60 unless given.
- Whether it is on (`enabled`).

A path is a key, or keys and list indexes separated by dots, optionally after `$.`: `code`,
`data.code`, `$.items.0.id`. A key holding a dot cannot be reached.

The asset is looked for among the active assets the hook may read, or act on as its action needs,
in their scopes: `assets.hook`, `stock.record`, `assets.write` or `flows.run`. A value no such asset
has is `404`; one several have is `409`.

### Who it acts as

The hook does what it does by asking the API's own endpoints, in process, as itself: with its roles,
narrowed to their scopes. So it is held to every rule those endpoints keep, and to its permissions
there. An asset outside the scopes its roles are narrowed to cannot be found, and a hook whose roles
do not hold the permission its action needs is refused (`403`).

Every change is recorded in the asset's history as made by the incoming hook: incoming hook
“Scale”, or, for a flow, incoming hook “Form”, via flow “Leave something”. Its events record no
person.

An incoming hook can do no more than whoever saves it. Saving one is refused (`403`), naming what is
missing, when its roles grant a permission the person saving it does not hold wherever the hook
would hold it.

### Secrets

A hook's secret is made by BAM!, starts `bamin_`, and is shown once: in the answer that makes the
hook, and in the one that gives it a new secret (`new_secret`). Changing how a hook authenticates
gives it a new secret too. The old secret stops working at once.

It is kept so that it cannot be read back:

- a bearer secret as its SHA-256 hash;
- a signing secret, which checking a signature needs, sealed with the configuration's
  `[auth] secret_key`, as identity providers' secrets are (see [Configuration](configuration.md)).
  Without a key, saving a hook with `hmac` is refused (`409`). Opened with another key, as after
  restoring into another instance, its requests are refused (`503`) until it is given a new secret.

A backup without secrets (see [Backups](backups.md#authentication)) holds incoming hooks without
theirs. Restored, they refuse every request until each is given a new secret.

### Signed requests

A hook with `hmac` takes requests signed as BAM!'s webhooks sign theirs (see
[Notifications](notifications.md#checking-the-signature)):

| Header | Holds |
|---|---|
| `BAM-Timestamp` | When it was sent, in seconds since 1970 (UTC) |
| `BAM-Delivery` | The sender's ID for this request, up to 255 characters, different for each |
| `BAM-Signature` | `v1=` and the hex HMAC-SHA256, keyed by the secret, of the timestamp, a `.`, and the body |

A request is refused (`401`) when a header is missing, its timestamp is more than five minutes from
the server's time, or its signature does not match the body exactly as it arrived. A delivery ID
the hook has taken already is refused (`409`), on any server sharing the database, so a request
recorded on its way cannot be sent again. Delivery IDs are kept until their timestamps are too old
to be taken anyway.

### Limits

- **Bodies** are at most 64 KB (`413`).
- **Rate:** each hook takes its `rate_limit` of requests in any minute. Beyond that the answer is
  `429` with `Retry-After`. Only requests that authenticate count, so someone who knows the URL but
  not the secret cannot use up its sender's share. The count is kept by each server, in memory: so
  several servers sharing a database each allow as much, and a restart forgets it. A hook that
  triggers a hook is also held to the limit on [pressing hooks](#pressing-a-hook), on each asset.
- **Replays:** signed requests more than five minutes old, and reused delivery IDs, are refused, as
  above.

### The request

A request is a `POST`, with a JSON object for its body, or form fields
(`application/x-www-form-urlencoded`), which read as an object of text. An empty body is an empty
object, which is enough for a hook that triggers a hook on a fixed asset.

#### Recording a use or restock

`{"adjust": -1}` uses one, `{"adjust": 10}` restocks ten, and `{"set": 42}` counts 42. Each is a
whole number, or text holding one. Give one of the two. The asset must be counted by quantity.

#### Setting fields

The hook's `fields` map each field's key to a path into the request: `{"weight": "data.grams"}`
sets the field `weight` from `data.grams`. Without `fields`, the request's own `field_values`, an
object of the fields to set by key, are set. Each value is text, a number, or true or false. The
asset's other fields stay as they are, and each value is checked as editing the asset checks it.

#### Running a flow

The flow runs from its start to its end at once, as a person running it would, answering each step
that asks something from the request:

| Step | Answered with |
|---|---|
| Choose category | `category_id` |
| Form | `answers`, by its questions' keys; without `answers`, the request's own keys |
| Find | the asset the hook found |
| Create, with a place to scan | `place_code` |
| Link, finding what to link to | nothing: one not required is passed over, and one required stops the flow |

A step that cannot be answered, or that fails, stops the flow there. The answer names the step, as
in `Running the flow: Step "about": What is it? is required`. What the steps before it did stays
done. The flow must be on, and the hook's roles must hold `flows.run` in the scopes of one of its
categories. Photo questions are left unanswered.

### Answers

- **`202`**, with the events recorded: `{"event_id", "event_ids": [...], "asset_id"}`. `event_id` is
  the first, such as the `asset.hook` event, and `event_ids` all of them, such as a use and the
  `asset.running_low` it caused. `event_id` is `null` when nothing changed.
- **An error**, `{"error"}`, naming what failed, never the secret.

| Status | When |
|---|---|
| `400` | The body is not a JSON object or form fields, or lacks what the action needs, such as the asset's code |
| `401` | The secret is missing or wrong; a signature is missing, too old or does not match; or the hook has no secret |
| `403` | The hook is off, or its roles do not allow what it does on the asset |
| `404` | No hook has the URL, or no asset it can reach was found |
| `409` | A delivery ID taken already; several assets found; or the endpoint behind the action refused, such as for too little stock |
| `413` | The body is over 64 KB |
| `429` | Too many requests, with `Retry-After` |
| `503` | A signing secret this server's key cannot open |

A refusal by the endpoint behind the action comes with its status, after what was being done:
`Recording the use or restock: …`.

### Examples

With `curl`, a counter using one of the asset whose code it read, with a bearer secret:

```bash
curl -X POST https://bam.example.org/api/v1/incoming/7c1f…e9 \
  -H "Authorization: Bearer bamin_…" \
  -H "Content-Type: application/json" \
  -d '{"code": "R-10K", "adjust": -1}'
```

Signed, with `openssl`:

```bash
body='{"code": "R-10K", "adjust": -1}'
ts=$(date +%s)
sig=$(printf '%s.%s' "$ts" "$body" | openssl dgst -sha256 -hmac "$SECRET" | sed 's/^.* //')
curl -X POST https://bam.example.org/api/v1/incoming/7c1f…e9 \
  -H "Content-Type: application/json" \
  -H "BAM-Timestamp: $ts" \
  -H "BAM-Delivery: $(uuidgen)" \
  -H "BAM-Signature: v1=$sig" \
  -d "$body"
```

With Home Assistant, a `rest_command` triggering **Identify** on a fixed asset, and another setting a
field from a sensor:

```yaml
# configuration.yaml
rest_command:
  bam_identify_drawer:
    url: "https://bam.example.org/api/v1/incoming/7c1f…e9"
    method: post
    headers:
      Authorization: !secret bam_incoming_identify
  bam_shelf_weight:
    url: "https://bam.example.org/api/v1/incoming/a04d…31"
    method: post
    headers:
      Authorization: !secret bam_incoming_weight
    content_type: "application/json"
    payload: '{"grams": {{ states("sensor.shelf_3_weight") | int }}}'
```

```yaml
# secrets.yaml
bam_incoming_identify: "Bearer bamin_…"
bam_incoming_weight: "Bearer bamin_…"
```

The second hook sets fields, with `fields` `{"weight": "grams"}`, on a fixed asset. An automation
calls `rest_command.bam_shelf_weight` when the sensor changes.

### Several servers

Delivery IDs are claimed in the database, so a signed request is taken once, on whichever server it
reaches. The rate is kept by each server, as above.

## Formerly "actions"

Earlier releases called hooks actions, in the interface, the API, events, the permission and flows
alike. Migration `049` carries over what was stored under those names:

| Before | After |
|---|---|
| Roles holding `assets.act` | Hold `assets.hook` |
| Channels following `asset.action` | Follow `asset.hook` |
| `asset.action` events | Become `asset.hook` events, with their `action` as `hook` |
| Flows whose Change steps pressed an action | Trigger the same hook |
| Scan templates offering the `actions` quick action | Offer `hooks` |

A package written before migration `049` is imported the same way. The tables keep their names
(`asset_actions`, `category_actions`, `action_replies`), and so does the backup archive's layout.

## API

| Method and route | Permission | Purpose |
|---|---|---|
| `GET /api/v1/hooks` | anyone who reads assets | List hooks |
| `POST /api/v1/hooks` | `categories.manage` | Define a hook, with `key`, `label`, `confirm` and `triggers` |
| `PUT /api/v1/hooks/{id}` | `categories.manage` | Change a hook; left out, `triggers` stay as they were |
| `DELETE /api/v1/hooks/{id}` | `categories.manage` | Delete a hook, taking it off every category |
| `GET /api/v1/categories/{id}/hooks` | `assets.read` | List the hooks a category offers |
| `PUT /api/v1/categories/{id}/hooks` | `categories.manage` | Set the hooks a category offers |
| `POST /api/v1/assets/{id}/hooks/{key}` | `assets.hook` (scopable) | Trigger a hook |
| `GET /api/v1/assets/{id}/hooks/replies?event_id=` | `assets.hook` | Device answers to a triggered hook |
| `GET /api/v1/events/live` | `events.read` (scopable) | Open a live connection |
| `GET /api/v1/tokens/connections` | `users.manage` | Each token's open connections |
| `GET /api/v1/incoming` | `settings.manage` | List incoming hooks, each with its `url` |
| `GET /api/v1/incoming/{id}` | `settings.manage` | Read an incoming hook |
| `POST /api/v1/incoming` | `settings.manage` | Make an incoming hook, with `name`, `enabled`, `auth`, `action`, `hook`, `flow_id`, `fields`, `asset`, `rate_limit` and `roles`; answers its `secret`, once |
| `PUT /api/v1/incoming/{id}` | `settings.manage` | Change an incoming hook, given whole; `new_secret` replaces its secret, answered once |
| `DELETE /api/v1/incoming/{id}` | `settings.manage` | Delete an incoming hook |
| `POST /api/v1/incoming/{id}` | The hook's secret | Send a request to an incoming hook (see [above](#incoming-hooks)) |

## Permissions

| Permission | Scopable | Granted by default |
|---|---|---|
| `assets.hook` | Yes | Migration `026` granted it to every role that reads assets, in the same scopes, except the public role |
| `events.read` | Yes | No role but `admin` until given |

Live connections also need `assets.track` in a scope to be sent its events. See
[Access](access.md) for roles and scopes.

## Storage

| Migration | Adds |
|---|---|
| `026` | Hooks |
| `027` | Live connections |
| `049` | Renames actions to hooks in stored data (see [above](#formerly-actions)) |
| `066` | Incoming hooks (`incoming_hooks`, their roles in `incoming_hook_roles` and `incoming_hook_role_scopes`), and the delivery IDs of signed requests taken (`incoming_deliveries`) |

The hook tables keep their old names: `asset_actions`, `category_actions` and `action_replies`.

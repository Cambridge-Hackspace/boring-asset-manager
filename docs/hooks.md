# Hooks and live connections

A hook is a button people press on assets, such as **Identify**. Pressing it records an event for
devices and notification channels to act on, such as a cabinet that lights the drawer an asset is
in.

Devices hear about hooks, and about other events, through live connections: a device dials in over
a WebSocket and is sent the events meant for it as they happen.

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
- the asset;
- the containers the asset is in through location links (see [Data model](data-model.md)),
  outermost first, each with its codes by generator type and its field values by key.

This lets a device find the thing. Give each drawer an `led` field, and identifying a resistor
lights its drawer.

The event is kept in the asset's history, and notification channels can subscribe to it (see
[Notifications](notifications.md)). Discord shows it as "Identify: Drawer A3", triggered by whom.

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
| `POST /api/v1/hooks` | `categories.manage` | Define a hook |
| `PUT /api/v1/hooks/{id}` | `categories.manage` | Change a hook |
| `DELETE /api/v1/hooks/{id}` | `categories.manage` | Delete a hook, taking it off every category |
| `GET /api/v1/categories/{id}/hooks` | `assets.read` | List the hooks a category offers |
| `PUT /api/v1/categories/{id}/hooks` | `categories.manage` | Set the hooks a category offers |
| `POST /api/v1/assets/{id}/hooks/{key}` | `assets.hook` (scopable) | Trigger a hook |
| `GET /api/v1/assets/{id}/hooks/replies?event_id=` | `assets.hook` | Device answers to a triggered hook |
| `GET /api/v1/events/live` | `events.read` (scopable) | Open a live connection |
| `GET /api/v1/tokens/connections` | `users.manage` | Each token's open connections |

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

The hook tables keep their old names: `asset_actions`, `category_actions` and `action_replies`.

# Notifications

BAM! sends notifications when something happens that people should hear about: a category or
asset running low, a loan overdue, an asset out of service, a hook pressed. You set up any number
of channels, each sending through a notifier driver to a service such as Discord or an MQTT broker.

Notifications go out through pluggable notifier drivers, in the same way printers use print drivers
(see [Printing](printing.md)). Two drivers ship: Discord and MQTT.

## Channels

A channel has:

- a name;
- a driver;
- the driver's options;
- an enabled switch;
- the event kinds the channel subscribes to.

There can be any number. Channels are configured through `/api/v1/notifiers` (`settings.manage`).

Secret options can be written but are never returned; reads show only whether they are set.
`POST /api/v1/notifiers/{id}/test` sends a test message.

Which events a channel can subscribe to is listed with each feature; see
[Stock and lending](stock-and-lending.md) and [Hooks](hooks.md).

## Delivery

Each event fans out to one delivery per enabled, subscribed channel, created in the same transaction
as the event. A worker in `bam serve` sends them. A delivery is claimed with a lease, so several
servers sharing a database send it once.

| Outcome | What happens |
|---|---|
| Network error or `5xx` response | Retried with exponential backoff |
| `429` | Waits as long as the service asks, then retries |
| Any other `4xx` | Fails at once |
| Still unsent after 24 hours | Given up |

The outcome of recent deliveries is visible in `GET /api/v1/notifiers/deliveries`.

## Discord

The Discord driver (`discord`) takes a webhook URL, which is secret. The URL is accepted only on
Discord's own hosts (`discord.com`, `discordapp.com` and their `ptb.` and `canary.` variants), so the
server cannot be pointed at arbitrary addresses.

Messages look like this:

| Event | Message |
|---|---|
| A category running low | "Running low": an embed in the category's color with the category name, a link to `{base_url}/categories/{id}`, the triggering user, the reason and a timestamp |
| A category restocked | "Restocked": a shorter, neutral embed |
| An asset's stock | Links to `{base_url}/assets/{id}`, gives its quantity and threshold with their unit; amber, since an asset has no color of its own |
| A change of condition | "Out of service" (red), "Needs maintenance" (amber) or "Working again" (neutral), with the note and who reported it |
| An overdue loan | "Overdue" (amber), naming the borrower and the day it was due (UTC) |
| A hook | For example "Identify: Drawer A3", triggered by whom (see [Hooks](hooks.md)) |

## MQTT

The MQTT driver (`mqtt`) is for those running a broker, such as Home Assistant's.

Each event is published as JSON, with people by name only and a link to what it is about. It goes
to `<topic prefix>/<kind>/<asset or category ID>` (a test message goes to `<prefix>/test`), at QoS 1,
not retained.

### Options

| Option | Notes |
|---|---|
| Broker | `mqtts://host[:port]` for TLS (8883 unless given), or `mqtt://host[:port]` to send without encryption (1883). Sending in the clear is chosen explicitly. |
| Topic prefix | No `+`, `#` or NUL |
| Client ID | `bam` unless given; at most 23 characters |
| User name and password | The password is secret |
| CA certificate | In PEM, to trust a broker whose certificate no public authority signed; the system's authorities are used otherwise |
| Client certificate and key | In PEM (the key secret), for brokers that know clients by certificate |

Like the other channels, MQTT channels are set up by `settings.manage`, and their broker may be any
address.

### Connections and errors

Each delivery connects, publishes, waits for the broker's acknowledgment and disconnects, so no
connection is kept alive between them.

- A broker refusing the credentials or the client fails the delivery at once.
- A broker whose certificate is not trusted fails the delivery at once.
- A broker that is not reachable, or not answering, is retried.

The client is the part of MQTT 3.1.1 publishing needs, written here rather than taken from a
library.

## API

| Method and route | Permission | Purpose |
|---|---|---|
| `GET /api/v1/notifiers/drivers` | `settings.manage` | List notifier drivers |
| `/api/v1/notifiers` | `settings.manage` | Configure channels |
| `POST /api/v1/notifiers/{id}/test` | `settings.manage` | Send a test message |
| `GET /api/v1/notifiers/deliveries` | `settings.manage` | Outcome of recent deliveries |

## Storage

Channels are stored in the `notification_channels` table.

## Internals

Drivers (`internal/notify`) declare:

- their options;
- which options are secret;
- how to validate them;
- how to deliver an event.

Events themselves are neutral; each driver decides how to present them.

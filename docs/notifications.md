# Notifications

BAM! sends notifications when something happens that people should hear about: a category or
asset running low, a loan overdue, an asset out of service, a hook pressed. You set up any number
of channels, each sending through a notifier driver: to Discord, an MQTT broker, or email.

Notifications go out through pluggable notifier drivers, in the same way printers use print drivers
(see [Printing](printing.md)). Three drivers ship: Discord, MQTT and email.

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

## Email

The email driver (`email`) sends each event as an email, saying what Discord would say, with a
link to what it is about. Every message has an HTML part and a plain-text part, built from the same
template.

### Setting it up

Email goes out through the SMTP server in the configuration file's `[email]` table (see
[Configuration](configuration.md#the-configuration-file)). No provider's own API is used, so any
server that speaks SMTP will do: your own, or a provider's SMTP relay.

- `tls` is `starttls` (the default, on port 587), `implicit` (port 465), or `none`, which sends in
  the clear and must be chosen explicitly. With `starttls`, a server that does not offer STARTTLS is
  refused.
- The server's certificate is checked against the system's authorities, or against `ca`.
- With a `username`, BAM! signs in with PLAIN authentication, which is sent only over TLS (or to the
  machine itself).
- `bam email test --to you@example.org` sends a test email, so a mistake shows straight away.

While `[email]` has no `host`, an email channel cannot be created (`400`), and one made earlier
fails its deliveries, saying email is not set up.

### Options

| Option | Notes |
|---|---|
| Addresses | Email addresses, one per line or separated by commas; at most 100 |
| Roles | Role names, separated by commas: everyone holding one of them, as below |

A channel needs addresses, roles or both.

### Who receives it

- **Addresses** listed in the channel always receive it.
- **Roles** reach every active person holding one of them, at the email they sign in with:
  - A role narrowed to scopes (see [Access](access.md#scopes)) reaches its holder only for events
    about something in one of those scopes.
  - An event about no asset or category, such as sign-ins held back, reaches only those holding the
    role in every scope.
  - Anyone can turn off email reaching them through a role, under **Your account**
    (`PUT /api/v1/auth/email` with `{"notifications": false}`). Addresses listed in the channel are
    not affected.
  - A test message reaches everyone holding the roles, wherever narrowed.
- Each event is one message to everyone it reaches. With more than one recipient, it is addressed to
  `undisclosed-recipients`, so nobody sees the others' addresses.
- When nobody is left to receive it, nothing is sent and the delivery succeeds.

### Errors

| Server's answer | What happens |
|---|---|
| Unreachable, or a `4xx` reply | Retried with exponential backoff, as other deliveries are |
| A `5xx` reply, such as an address refused | Fails at once |

## API

| Method and route | Permission | Purpose |
|---|---|---|
| `GET /api/v1/notifiers/drivers` | `settings.manage` | List notifier drivers |
| `/api/v1/notifiers` | `settings.manage` | Configure channels |
| `POST /api/v1/notifiers/{id}/test` | `settings.manage` | Send a test message |
| `GET /api/v1/notifiers/deliveries` | `settings.manage` | Outcome of recent deliveries |

## Storage

Channels are stored in the `notification_channels` table. Whether email reaching someone through a
role reaches them is `users.email_notifications` (migration `052`).

## Internals

Drivers (`internal/notify`) declare:

- their options;
- which options are secret;
- how to validate them;
- how to deliver an event.

Events themselves are neutral; each driver decides how to present them.

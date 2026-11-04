# Notifications

BAM! sends notifications when something happens that people should hear about: a category or
asset running low, a loan overdue, an asset out of service, a hook pressed. You set up any number
of channels, each sending through a notifier driver: to Discord, an MQTT broker, email, or any
URL as a webhook.

Notifications go out through pluggable notifier drivers, in the same way printers use print drivers
(see [Printing](printing.md)). Four drivers ship: Discord, MQTT, email and webhooks.

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
[Stock and lending](stock-and-lending.md), [Hooks](hooks.md) and [Access](access.md#too-many-wrong-passwords).

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

### The delivery log

**Recent deliveries**, on the Notifications page, lists every channel's deliveries, or one channel's
(its **Deliveries** button):

- the event, and when;
- the outcome, after how many attempts, and when the next is due;
- the last error;
- for a webhook, what the receiver answered: its HTTP status and the first kilobyte of its body. The
  channel's secrets are taken out of it, as a receiver may echo what it was sent.

**Send again** queues a failed delivery to be sent at once, with its attempts started again, once
whatever made it fail is fixed. Its channel must be on.

Finished deliveries are kept for 30 days, then removed: `[notifications] keep_days` in the
configuration (see [Configuration](configuration.md#the-configuration-file)), and `0` keeps them.

### Channels that keep failing

A channel that fails every attempt for 24 hours is turned off, so that a receiver gone for good is
not tried forever:

- **Counting:** each failed attempt counts, retries included; a delivered one starts the count again.
- **Other limits:** `[notifications] disable_after_hours` sets the 24, and `disable_after_failures`,
  when set, turns a channel off after that many failed attempts in a row, however soon. `0` turns
  either off.
- **Turned off,** the channel says why on the Notifications page, and `notifier.disabled` is
  recorded once, so the other channels subscribed to it can say so.
- **Turned on again,** by editing it, it starts its count again.

A test message does not count.

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
| A channel turned off | "Notification channel turned off: Workshop hook" (red), saying why, and a link to the Notifications page |
| Sign-ins held back | "Sign-ins held back: Ada" (red), for an account getting too many wrong passwords or codes, or "Sign-ins held back from 203.0.113.7", for an address; when it may sign in again, and a link to the Users page (see [Access](access.md#too-many-wrong-passwords)) |
| Second factors removed | "Second factors removed: Ada" (amber), when an admin, or someone on the command line, removes their second factors, saying who; a link to the Users page (see [Access](access.md#two-factor-sign-in)) |
| A recovery code used | "Recovery code used: Ada" (amber), when someone signs in with one of their recovery codes; a link to the Users page (see [Access](access.md#recovery-codes)) |
| A break-glass sign-in | "Break-glass sign-in: Ada" (amber), when someone signs in with a password while passwords are off; a link to the Users page (see [Access](access.md#turning-passwords-off)) |
| Two-factor sign-in no longer required | "Two-factor sign-in no longer required: admin" (amber), when a role stops requiring it, saying who turned it off, or that it was the command line; a link to the Roles page (see [Access](access.md#requiring-two-factor-sign-in)) |

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

## Webhooks

The webhook driver (`webhook`) posts each event to a URL, for anything that takes HTTP: your own
service, a home automation system, a chat service's incoming webhook, a status board.

### Options

| Option | Notes |
|---|---|
| URL | `https://…`, or `http://…` to send without encryption, chosen by writing it |
| Signing secret | Signs each request (below). Secret: written, never returned. Left empty, requests are not signed |
| Extra headers | `Name: value`, one per line, such as an `Authorization` the receiver wants. Secret, as a whole. The `BAM-*`, `Host`, `Content-Type` and `Content-Length` headers are BAM!'s own |
| Body template | Empty for BAM!'s own JSON (below), or a template of your own |
| Content type | `application/json` unless given |
| Allow private networks | `true` to post to a private address (below) |

### The request

Each event is a `POST` with a JSON body:

```json
{"version": 1, "id": "<delivery ID>", "event": {...}, "instance": "https://bam.example.org"}
```

- `event` is the event as MQTT publishes it: its `id`, `kind`, `asset_id` or `category_id`, a `url`
  to what it is about, its `payload` and `created_at`. People appear by name only.
- `id` names the delivery, the same at every retry, so a receiver can drop one it already had.

Its headers are:

| Header | Holds |
|---|---|
| `BAM-Event` | The event's kind, or `test` for a test message |
| `BAM-Delivery` | The delivery's ID, as in the body |
| `BAM-Timestamp` | When it was sent, in seconds since 1970 (UTC) |
| `BAM-Signature` | With a signing secret: `v1=` and the hex HMAC-SHA256, keyed by the secret, of the timestamp, a `.`, and the body |

A redirect is not followed, since it could lead anywhere: the delivery fails, saying so, and the URL
it leads to should be given instead. Other answers are treated as for every channel (see
[Delivery](#delivery)).

### Checking the signature

A receiver recomputes the signature over the body exactly as it arrived, compares it in constant
time, and refuses a timestamp more than a few minutes old, so a request recorded on its way cannot
be sent again later.

Go:

```go
func signed(secret string, r *http.Request, body []byte) bool {
	ts, err := strconv.ParseInt(r.Header.Get("BAM-Timestamp"), 10, 64)
	if err != nil || math.Abs(float64(time.Now().Unix()-ts)) > 300 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(r.Header.Get("BAM-Timestamp") + "."))
	mac.Write(body)
	want := "v1=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(r.Header.Get("BAM-Signature")))
}
```

Python:

```python
import hashlib, hmac, time

def signed(secret: bytes, headers, body: bytes) -> bool:
    ts = headers.get("BAM-Timestamp", "")
    if not ts.isdigit() or abs(time.time() - int(ts)) > 300:
        return False
    want = "v1=" + hmac.new(secret, ts.encode() + b"." + body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(want, headers.get("BAM-Signature", ""))
```

JavaScript (Node.js):

```js
const crypto = require("node:crypto");

function signed(secret, headers, body /* a Buffer */) {
  const ts = headers["bam-timestamp"] ?? "";
  if (!/^\d+$/.test(ts) || Math.abs(Date.now() / 1000 - Number(ts)) > 300) return false;
  const want = "v1=" + crypto.createHmac("sha256", secret).update(ts + ".").update(body).digest("hex");
  const got = headers["bam-signature"] ?? "";
  return got.length === want.length && crypto.timingSafeEqual(Buffer.from(want), Buffer.from(got));
}
```

### A body of your own

For a receiver that wants its own shape, such as a chat service's, a body template replaces the JSON
above. It is a Go text template over the same data (`.version`, `.id`, `.event`, `.instance`), with a
`json` function that writes a value as JSON:

```
{"text": {{json (printf "%s: %s" .event.kind .event.payload.asset.name)}}}
```

Unless a content type is given, the result must be valid JSON. A template is checked when saved, but
one that fails on an event, such as reading an asset on an event about a category, fails that
delivery and says why; `{{with .event.payload.asset}}…{{end}}` reads it only when there is one. The
test button shows what a channel sends.

### Private networks

A webhook could otherwise be pointed at what sits behind BAM!: the machine itself, other servers on
its network, or a cloud provider's metadata address. So by default a URL is refused when its host is,
or resolves to, any of:

- loopback (`127.0.0.0/8`, `::1`) or unspecified (`0.0.0.0`, `::`);
- private (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `fc00::/7`);
- link-local (`169.254.0.0/16`, the metadata address among them, and `fe80::/10`);
- shared address space (`100.64.0.0/10`), multicast, or the IETF's and benchmarking ranges;
- IPv4-mapped IPv6 forms of any of these.

A name is looked up for each request, and the request is sent to the very addresses that were checked,
so DNS changing in between cannot redirect it. An address written in the URL is refused when the
channel is saved.

**Allow private networks** lifts this for a channel, for receivers on the local network, such as a
cabinet controller or Home Assistant. Like every channel option, it is set by those holding
`settings.manage`, which is never narrowed to scopes, so in practice by admins. Environment proxy
settings are not used for webhooks.

## API

| Method and route | Permission | Purpose |
|---|---|---|
| `GET /api/v1/notifiers/drivers` | `settings.manage` | List notifier drivers |
| `/api/v1/notifiers` | `settings.manage` | Configure channels |
| `POST /api/v1/notifiers/{id}/test` | `settings.manage` | Send a test message |
| `GET /api/v1/notifiers/deliveries` | `settings.manage` | Recent deliveries, newest first, of one channel (`channel_id`) or all; `limit` up to 200, 50 unless given |
| `POST /api/v1/notifiers/deliveries/{id}/retry` | `settings.manage` | Send a failed delivery again at once. `409` if it has not failed, or its channel is off |

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

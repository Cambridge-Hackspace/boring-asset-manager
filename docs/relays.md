# Relays

A relay is BAM! itself, run on a machine on a local network, such as a Raspberry Pi beside a label
printer. It lets a BAM! hosted elsewhere reach printers and devices on that network. The relay dials
out to the main BAM!, so nothing on the local network is opened to the internet.

This page describes the relay bus, how the main node and its relays talk, and how a relay is paired,
watched and revoked. `bam relay` itself, which runs a relay beside printers, is built on these, and
is not part of BAM! yet: `bam relay join` pairs a machine and keeps its pairing, ready for it.

## Transports

The bus runs over two transports, **BoneMesh** and **WebSocket**. Each is turned on or off on its
own, on the main node and on each relay:

- either alone;
- both together;
- neither, which turns relays off on that node.

```toml
[relay]
bonemesh = false
bonemesh_port = 7443
websocket = false
```

Both are off unless turned on. See [Configuration](configuration.md) for the environment variables.

The same protocol runs over both. A transport carries messages to the peers it links its node with.
Everything above that is written once: messages and their version, addressing, acknowledgments and
retries, splitting large messages, and delivering each message once.

A node linked over both is reachable over either. A message goes over BoneMesh while it is up, and
over WebSocket otherwise. Each retry starts with the next transport that is up, so a link that has
gone quiet without closing is passed over at the next retry.

### WebSocket

The main node takes relays' WebSocket connections at `GET /api/v1/relay/live`, on its own HTTPS
server. So it works through any reverse proxy, and wherever only port 443 is open.

- **Authentication:** the relay presents the credential pairing gave it, as
  `Authorization: Relay <credential>`, over TLS. A credential no paired relay holds is refused with
  `401`. The scheme is not `Bearer`, so the credential is never mistaken for an API token.
- **Turned off:** while `websocket` is off, the route answers `404`.
- **Framing:** one message per text frame, as JSON.
- **One at a time:** a relay's newer connection replaces its older one on the same server.
- **Keeping it alive:** each end pings the other every 30 seconds. Every 10 seconds the main node
  checks each relay's credential still holds, and closes the connection of one that was revoked.

### BoneMesh

[BoneMesh](https://forge.axonibyte.com/axonibyte/bonemesh) links are mutually authenticated and
encrypted: ML-DSA certificates, a hybrid X25519 and ML-KEM-768 handshake, and ChaCha20-Poly1305.
BAM! uses its Go implementation, `forge.axonibyte.com/axonibyte/bonemesh/go/v3`.

- **Trust:** the main node holds the mesh root, and pairing issues each relay a membership
  certificate for the relay's own identity key. The certificate's label is the relay's name. A node
  whose certificate has expired, or was issued by another root, cannot link at all.
- **Ports:** the main node listens on `bonemesh_port`, 7443 unless set, on every interface. The relay
  dials it, which suits NAT. BoneMesh cannot share the HTTPS port: its sessions are its own protocol
  over TCP from the first byte, and the Go node takes connections only from a listener of its own.
  Where only 443 is open, relays use WebSocket.
- **A relay listens too:** the Go node always listens, so a relay's BoneMesh node takes a port of its
  own choosing. Nothing links to it without a certificate from the mesh root.
- **Signatures:** BoneMesh hands a node what a message carries, but not which peer sent it, and each
  node that routes a message reads it. So over BoneMesh every message is signed by the node sending
  it on, with the identity key its certificate binds, and checked against the key pairing recorded
  for that node. A relay therefore cannot speak as another, and relays accept messages from the main
  node alone.
- **Turned off:** while `bonemesh` is off, nothing listens on the port. While it is on, the main node
  listens once there is a mesh root, which pairing the first relay makes (see [Pairing](#pairing)).

BoneMesh carries a message in segments of up to 24,000 bytes, and keeps a retry queue of its own.
The bus relies on neither: its own parts, signed, usually fit in a segment each, and its own
retries decide delivery.

## The protocol

### Messages

Every message is a JSON object:

```json
{"v": 1, "id": "5f0c…", "type": "job", "from": "main.4b1e…", "to": "front-desk", "body": {…}}
```

| Field | Meaning |
|---|---|
| `v` | The protocol's version, 1. |
| `id` | 128 random bits as hex. A message is delivered once by its ID. |
| `type` | What the message is (below). |
| `from`, `to` | Node names (below). |
| `re` | The message this one answers: what an `ack` or a `nak` is for, or the request a reply answers. |
| `reason` | Why a `nak` refused. |
| `part`, `seg` | For a part of a large message (below). |
| `body` | What the message carries, by its type. |

| Type | Body | Sent by |
|---|---|---|
| `ack` | none | The node a message was for, once it arrived. |
| `nak` | none; `reason` says why | The node a message was for, or the main node when it cannot route it. |
| `job` | `{"destination": {"type", "address"}, "data", "timeout_ms"}`. `data` is the bytes to deliver, as base64. A destination is named as a printer's connection names it (`tcp`, `device`, `ipp` or `windows`). | The main node. |
| `job.result` | `{"ok", "reason"}`: accepted, or failed with a reason. Its `re` is the job. | The relay. |
| `status` | `{"version", "protocol", "allowlist", "destinations"}`: the relay's BAM! version, the protocol it speaks, its allowlist's patterns, and each local destination's reachability, as `{"destination", "reachable", "reason"}`. | A relay, unasked, or answering a `status` sent to it. |
| `event` | An event. | Either, once events are carried. |

### Names

- **Relays** are named by pairing: lowercase letters, digits and hyphens, up to 63.
- **The main node** is `main`. A relay sends to `main` whichever server it reaches.
- **Each server** of the main node is also `main.<server ID>`, a new ID each time it starts. A message
  a server sends comes from that name, so the reply finds the server that asked.

### Delivery

The protocol, not the transport, owns delivery, so it behaves the same over either.

- **Acknowledgment:** the node a message is for answers it with an `ack` once it has arrived, before
  it is handled. Answers to it, such as a `job.result`, are messages of their own.
- **Retries:** a sender sends again until it has an `ack` or a `nak`, or gives up, after a minute
  unless it was given a deadline of its own. It waits 2 seconds, then twice as long each time, up
  to 16 seconds, so a link too slow to answer in time is not flooded with copies that slow it
  further. When a link comes up or goes, sends waiting try at once, and wait 2 seconds again.
- **Refusals:** a `nak` ends a send at once, with its reason: a relay that is not connected, a type
  the node does not handle, or a version it does not speak.
- **Once:** a node remembers the IDs it delivered for 10 minutes. A message that arrives again, over
  the same transport or the other, is acknowledged again and not delivered again.
- **End to end:** an `ack` comes from the node the message was for, not from a node routing it. The
  main node routes a message between relays without acknowledging it.

BAM!'s own queues (the spool, for printing) stay the source of truth. The bus delivers a message
once, or says it could not.

### Large messages

A message whose body is over 16 KiB is split into parts. Each part is a message of its own, with its
own ID, acknowledged and sent again alone, carrying:

```json
{"part": {"of": "<the whole message's ID>", "i": 0, "n": 7}, "seg": "…"}
```

`seg` is that part of the body's JSON text, cut between characters. The node it is for puts the
parts back together, in any order, and delivers the whole message once, by its ID.

- A message's body is at most 16 MiB, in at most 1,024 parts.
- A node keeps at most 256 messages arriving in parts at once, and 64 MiB of them, and drops one not
  whole within 2 minutes. A part beyond those bounds is refused with a `nak`.
- A sender has 16 parts of a message in flight at once.

Print jobs go base64-encoded, so a large batch is several parts, or several jobs.

### Versions

A message of a version the node does not speak is answered with a `nak` saying which version it
speaks.

## Routing

On the main node, a message to a relay goes over a link to that relay. The main node routes between
transports, so a relay on WebSocket and a relay on BoneMesh reach each other through it. A relay
sends everything to the main node, and takes messages from it alone.

A relay speaks only for itself. The main node refuses a message from a relay whose `from` is any name
but the relay's own. The main node speaks for anyone, as it passes messages on.

A message for a relay that no server holds is answered at once with a `nak`: "relay … is not
connected".

## Several servers

When several servers share a database, one holds each relay over each transport at a time, and
claims it in the database (`relay_holds`), every 10 seconds while it does. A claim not kept for 30
seconds was left by a stopped server.

- **Reaching a relay another server holds:** a server leaves the message in the database
  (`relay_mail`) for the server holding it, preferring the one holding it over BoneMesh. That server
  reads its mail every second and sends the message on. The relay's answer comes back the same way,
  to the server it names. Mail no server takes is dropped after 2 minutes.
- **Dialing again:** a relay that dials again reaches whichever server it reaches, which takes the
  claim. The server that held it before finds it has lost the claim, and closes its link.
- **No loops:** a message passed on through the database is not passed on through it again.
- **Messages to `main`:** a relay's message to `main` is delivered by the server it arrives at. A
  relay on both transports, reaching a different server over each, may have the same message to
  `main` delivered by both, as each server remembers only what it delivered. Whatever handles
  messages to `main` takes each as possibly repeated.

## Pairing

Relays are added under **Admin › Relays**, by someone with `settings.manage`. Pairing needs
`[auth] secret_key` in the configuration, which seals the mesh root.

1. **An admin adds a relay,** naming it. Its name is its node name, and printers will name it by it:
   lowercase letters, digits and hyphens. BAM! gives a one-time code, which works once, for an hour,
   and the command to run on the relay's machine:

   ```
   bam relay join https://bam.example.org ABCD-EFGH-JKMN-PQRS
   ```

2. **The relay makes its own identity key,** an ML-DSA-65 key pair, and sends the public half with
   the code (`POST /api/v1/relays/join`). The private key never leaves the relay.
3. **The main node issues** a BoneMesh membership certificate for that key, valid for 7 days, and a
   WebSocket credential. It keeps the relay's public key and the credential's SHA-256, never the
   credential itself. It also tells the relay its BoneMesh address, if BoneMesh is on, and whether it
   takes WebSocket.
4. **The relay keeps its pairing** in a file only its owner reads: `relay.json`, or `--state`, or
   `$BAM_RELAY_STATE`. `bam relay join` refuses to replace a pairing already kept.

A code is used up by pairing, and refused after its hour: "This code has expired". A code used
already, or mistyped, is refused alike: "This code is not valid, or was used already". **New code**
gives a relay a new one, as to pair it again on a new machine; what it holds works until the new
code is used.

### Without the web interface

For instances a portal provisions, `bam admin relay add --name <name>` adds a relay on the main node
and prints its code and command. On the relay, `bam relay join` with no arguments reads the address
and code from `$BAM_RELAY_JOIN`, as `"https://bam.example.org ABCD-EFGH-JKMN-PQRS"`.

### Renewing

A relay's certificate lasts 7 days. While it is paired, the relay renews it with its credential
(`POST /api/v1/relays/renew`), and is given a new one for the same key. `bam relay` will renew on a
schedule; `relay.Pairing.Renew` is what it calls.

### Versions

A relay says which version of the relay protocol it speaks as it pairs and renews. One older than
the main node takes is refused with `409`, saying which version is needed and which BAM! to update
the relay to. Once linked, a message of a version a node does not speak is refused with a `nak`
([above](#versions)).

### The mesh root

The mesh root is the main node's, made the first time a relay pairs, with the main node's own
BoneMesh identity. Both private keys are kept in the database (`relay_mesh`), sealed with
`[auth] secret_key`, and kept in backups with authentication (see [Backups](backups.md)). Every
server sharing the database uses them, and starts its BoneMesh transport once there is a root. The
main node's own certificate is issued as the server starts BoneMesh, valid for 30 days, and the
server starts BoneMesh again with a new one a week before it expires.

**Rotate the mesh root** makes a new one. Every relay is unpaired at once, keeping its name, and
pairs again with a new code. Servers start BoneMesh again under the new root, at once on the server
that rotated it and within 10 seconds on others.

## Health

**Admin › Relays** lists each relay:

- online or offline, and since when, and over which transports it is linked to the server showing
  the page;
- its version, and the address it last paired or renewed from;
- what its allowlist covers, as the relay reports in its `status`;
- when its certificate expires;
- waiting to pair, with when its code lapses, or revoked.

A relay is online once any server holds a link to it. It goes offline when no server has held it for
the 30 seconds a claim lasts, so a relay that dials again at once is not said to have gone. Each
server checks every 10 seconds.

- **Events:** `relay.online` and `relay.offline` are recorded as a relay comes and goes, once
  however many servers see it, and notification channels can subscribe to them
  ([Notifications](notifications.md)).
- **Printers:** no printer uses a relay yet. Once printers can print through one, the relays page
  will list them.

## Trust

Trust does not depend on the transport. A relay is the same paired identity over either. Its
WebSocket credential and its BoneMesh certificate are both issued at pairing. The bus asks a `Trust`
(`internal/relay`) for each relay's credential and identity key, and for the node's own BoneMesh
identity; on the main node, they come from the relays paired and the mesh root.

### Revoking

**Revoke** ends a relay:

- its WebSocket credential stops working, and its connections close, at once on the server that
  revoked it and within 10 seconds on others;
- it can no longer renew its certificate;
- its BoneMesh messages are refused at once, since its identity key is no longer trusted.

BoneMesh checks a certificate by its validity window alone, so a revoked relay's certificate still
passes BoneMesh's handshake until it expires. The revoked relay stays listed until then, and that is
the denylist: its key is refused, the main node will not link to it, and its name cannot be given to
another relay, so a new relay of that name could not be mistaken for it. Once its certificate
expires, it is forgotten. The Go node cannot close a session from outside, so a session a revoked
relay already had lasts until then too; it carries nothing.

A relay that never paired is removed at once.

## API

| Method and route | Permission | Purpose |
|---|---|---|
| `GET /api/v1/relay/live` | A relay's credential, as `Authorization: Relay …` | A relay's WebSocket link to the main node |
| `GET /api/v1/relays` | `settings.manage` | The relays, with their health and the transports each is linked over here; the mesh root's name; whether `bonemesh`, `websocket` and `secret_key` are set |
| `POST /api/v1/relays` | `settings.manage` | Add a relay, `{name}`: answers its `code`, when it expires, and the `command` that pairs it |
| `POST /api/v1/relays/{id}/code` | `settings.manage` | Give a relay a new code |
| `DELETE /api/v1/relays/{id}` | `settings.manage` | Revoke a relay, or remove one that never paired |
| `POST /api/v1/relays/mesh/rotate` | `settings.manage` | Rotate the mesh root, unpairing every relay |
| `POST /api/v1/relays/join` | The code | Pair: `{code, identity_key, version, protocol}`, answered with the certificate, credential and what the relay needs to link |
| `POST /api/v1/relays/renew` | A relay's credential | Renew a relay's certificate: `{version, protocol}` |

Over MCP, `list_relays`, `add_relay`, `new_relay_code` and `revoke_relay` manage relays. Linking,
pairing and renewing are a relay's own, and rotating the mesh root stays with a person signed in.

## Storage

| Migration | Adds |
|---|---|
| `061` | `relay_holds` and `relay_mail`, for [several servers](#several-servers) |
| `063` | `relays`, with their codes, keys and health, and `relay_mesh`, the mesh root |

`relay_holds` and `relay_mail` are not kept in backups: both describe the servers running now.
`relays` is kept with the settings, its codes' and credentials' hashes with authentication, and
`relay_mesh` with authentication (see [Backups](backups.md)). Restored without authentication, a
relay pairs again with a new code.

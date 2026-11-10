# The assistant beside the page

Assistants can already work with BAM! over [MCP](mcp.md), from an app the person runs and connects.
Most people asked to record things will not set that up, and many find it easier to describe what
they have ("there's a band saw, and it only runs when the extractor's on") than to fill in forms. The
assistant beside the page is BAM!'s own: a panel in the web app, next to whatever page is open,
using BAM!'s own tools as the person signed in.

It is off until an admin turns it on, and then shown only to those holding `assistant.use`.

## In practice

The wood shop's lead writes a guide, **Record the workshop**: walk someone through the equipment in
their area, and what each piece needs to run. Its categories are **Machines** and **Extraction**, its
link type **needs**, and its questions "What is in your area?" and "What does each machine need to
run?". The guide's poster goes up on the shop door.

A volunteer scans it, and the guide opens beside the page, with its questions. They write "there's a
band saw, and it only runs when the extractor's on". The assistant finds the categories, adds the
band saw and the extractor, and links them. **Machines** uses [review states](review.md), so each
lands at once as a draft: the asset pages show it, with **Accept**, **Edit** and **Reject**, and the
page beside the panel shows each change as it is made. The volunteer adds the drill press too,
which is not in any category using review states, so the panel asks first, and adds it once they
confirm. The questions tick off as they are answered.

## The panel

**Assistant**, in the header, opens it: beside the page on a wide screen, and over the whole screen on
a phone, with **Back to the page** to return.

- **It knows where the person is.** The asset, category, list or flow on screen is offered with each
  message, shown above the box to type in, and the person can take it away. The app gives it, as
  data, with the page's address and the thing's name and ID.
- **The page keeps up.** Each turn is streamed to the panel as it happens: what the model says, each
  tool it uses, and each change. A change made, the page beside it is read again, so what the
  assistant changed appears on it at once. A form being filled in is left as it is, with a note.
- **Conversations are kept** under [Your account](#conversations).

## What it can do

Exactly what the person can, through the [MCP server's tools](mcp.md#tools), run in process as
them: no token for them to make or manage. Their permissions and scopes are read again for every
call to the model and every tool it uses, so a role taken away mid-conversation is gone at the next
call; tools the person cannot use are not offered. What it changes is recorded in history as theirs,
through the assistant, as an MCP client's changes are: "Ada, via BAM! assistant", by the name an
admin gives it.

It proposes, and the person decides:

- **Where review states are on**, and the instance lands assistants' changes as drafts (the
  default; see [Review states](review.md#assistants)), adding, editing and linking assets whose
  categories all use review states is done at once, as a draft. The asset page shows it, with
  **Accept**, **Accept and confirm**, **Edit** and **Reject**.
- **Elsewhere, it asks before each change**: the panel shows what the tool will do, with its
  arguments, and **Confirm** or **Decline**. Nothing is changed until the person confirms. Sending
  another message instead declines it.
- **Reading** needs no confirmation.

Text stored in BAM!, and what the tools answer, is data, not instructions, as `bam://guide` tells
assistants. The panel tells the model so, and says the page it gives is data too.

Like any MCP client, it is held to 60 changes a minute, and to at most 50 items a call.

## Guides

An admin (`settings.manage`) writes guides under **Admin → Assistant**:

- a **name**, and an **address**, as flows have: `/g/<address>`;
- the **purpose**, in plain words, which the model is given;
- the **categories** and **link types** it works with;
- the **questions** it makes sure are answered.

A guide starts a conversation with its purpose. The model records each answer as it is given, and
the panel shows the questions beside the conversation, those answered and those still open. Each
guide has a poster with its QR code, as flows do, and a link to share. A guide turned off is not
offered, and its address finds nothing, except for those who manage guides.

## The model

Any provider whose chat API takes tools, set by an admin under **Admin → Assistant**:

| Setting | Default | Means |
|---|---|---|
| Provider | `anthropic` | `anthropic`, Anthropic's Messages API, or `openai`, any chat completions API that takes tools: OpenAI's, or one that copies it, such as a model served on your own network. |
| Endpoint | the provider's | `https://api.anthropic.com` or `https://api.openai.com/v1`. |
| Model | `claude-opus-5-5` or `gpt-5` | The model to ask. |
| Key | none | Written and never returned, as notification channels' secrets are; the browser never sees it. Anthropic's needs one; a chat completions server of your own may not. |
| Name | `BAM! assistant` | What it is called, in the panel and in history. |
| Most tokens a reply | 16000 | The longest one reply from the model may be. |
| Most steps a message | 20 | How many times one message may call the model before the panel stops and asks the person to go on. |
| Most tools offered | 128 | The first this many of the tools the person may use, in the order MCP lists them (finding and changing assets first); 0 offers all. OpenAI's API takes at most 128, and every tool offered is sent with every call. |
| Messages a day, each person | 100 | 0 for no limit. |
| Tokens a day, each person | 2,000,000 | Every token each call counts, cached ones included. |
| Messages a day, the instance | 1000 | Everyone's together. |
| Tokens a day, the instance | 20,000,000 | |
| Keep conversations | 30 days | After their last message. |

**Off until an admin turns it on.** Before it is turned on, Admin says plainly that what people type,
the page they are on, and what the tools answer, which can be anything they may see, go to that
provider, to be read by that model; turning it on takes saying it was read
(`PUT /api/v1/settings/assistant` with `"acknowledge": true`), and who did, and when, is kept.

Requests to Anthropic mark the tools and the instructions for caching, since they are the same from
one call to the next and the most of what is sent; a reply is sent back to the same provider just as
it came, its thinking included.

**Limits** count in days, in UTC. A person who reaches one is told which, and when it resets: a
message past the messages a day is refused (`429`, with `resets_at`), and a turn past the tokens a
day stops before its next call to the model.

### In the configuration

Each setting can be given in `config.toml` instead, under `[assistant]`, or in the environment, for
instances provisioned by a script. One given there takes the place of Admin's, which shows it as set
in the configuration and cannot change it:

```toml
[assistant]
enabled = true
provider = "anthropic"          # or "openai"
endpoint = "https://api.anthropic.com"
model = "claude-opus-5-5"
api_key = "sk-ant-…"
name = "BAM! assistant"
max_tokens = 16000
max_steps = 20
max_tools = 128
person_messages = 100
person_tokens = 2000000
instance_messages = 1000
instance_tokens = 20000000
keep_days = 30
```

The environment's are `BAM_ASSISTANT_ENABLED`, `BAM_ASSISTANT_PROVIDER`, `BAM_ASSISTANT_ENDPOINT`,
`BAM_ASSISTANT_MODEL`, `BAM_ASSISTANT_API_KEY`, `BAM_ASSISTANT_NAME`, `BAM_ASSISTANT_MAX_TOKENS`,
`BAM_ASSISTANT_MAX_STEPS`, `BAM_ASSISTANT_MAX_TOOLS`, `BAM_ASSISTANT_PERSON_MESSAGES`,
`BAM_ASSISTANT_PERSON_TOKENS`, `BAM_ASSISTANT_INSTANCE_MESSAGES`, `BAM_ASSISTANT_INSTANCE_TOKENS` and
`BAM_ASSISTANT_KEEP_DAYS`.

## Conversations

- **Kept for each person**, listed under **Your account**, where they open and delete them.
- **Kept for a set time**, per instance (30 days by default) after their last message, then deleted,
  checked every hour. Archiving an account deletes its person's.
- **Never readable by anyone else**, admins included: another's conversation reads as not there. What
  the assistant changed is in history as usual.
- **Backups leave them out** unless asked for (`bam backup --with-conversations`, or the
  `conversations` kind named; see [Backups](backups.md#kinds-of-information)), as authentication is.
  A backup from the web never holds them.

## Who may

| Permission | Allows |
|---|---|
| `assistant.use` | Seeing the panel, and talking with it, as oneself. Not scopable: the tools hold the person to their scopes. Migration 081 gives it to the admin role. |
| `settings.manage` | Setting the assistant up, and writing guides. |

A personal token, or an API token, cannot use the panel: an assistant of one's own reaches BAM! over
MCP instead. MCP reaches none of these routes (see `mcpserver.Refused`): an assistant does not set up
another, nor read a person's conversations.

## API

| Route | Does |
|---|---|
| `GET /api/v1/assistant` | Whether the panel is available to the requester, what it is called, the limits and what is used of them today, and when they reset. |
| `GET`, `PUT /api/v1/settings/assistant` | The settings (`settings.manage`). The key is given as `api_key`, kept when left out, and removed with `"clear_key": true`; it is never returned, only `key_set`. `fixed` names those the configuration sets. |
| `GET`, `POST /api/v1/assistant/guides`; `GET`, `PUT`, `DELETE /api/v1/assistant/guides/{slug}` | Guides (reading: `assistant.use` or `settings.manage`; changing: `settings.manage`). |
| `GET /api/v1/assistant/guides/{slug}/qr.svg` | The QR code of a guide's address, for its poster. |
| `GET`, `POST /api/v1/assistant/conversations` | The requester's conversations, most recent first; or a new one, with `{"guide"}` to start from one (`assistant.use`). |
| `GET`, `DELETE /api/v1/assistant/conversations/{id}` | One, with its messages, its guide and the change waiting, if any; or deleting it. |
| `POST /api/v1/assistant/conversations/{id}/messages` | A message, `{"text", "context"}`, answered as the turn goes (below) (`assistant.use`). |
| `POST /api/v1/assistant/conversations/{id}/confirm` | The person's answer to the change waiting, `{"approve"}`, after which the turn goes on. |

A turn is answered as `application/x-ndjson`, a JSON object a line, as it happens: `message` (each
message stored: the person's, the model's, and each tool's answer, shown only begun), `changed`
(a change was made: the page reads itself again), `confirm` (a change waits: `pending`, with the
tool's `name`, `title` and `input`), `progress` (a guide's answers), `limit` (with `resets_at`),
`error`, and last `done`, with the conversation and anything `pending`. One turn at a time runs in
each conversation (`409` for another).

## Storage

Migration 081 adds `assistant_settings` (one row), `assistant_guides`, `assistant_conversations`,
`assistant_messages` and `assistant_usage`, on every database. Messages keep their blocks as JSON
(text, tool calls and tools' answers), the page they were sent from, and the model's reply as its
provider gave it, to be sent back unchanged. In archives, the settings are settings (the key in
`auth/`), guides go with flows, conversations are a kind of their own, and the day's use is left
out.

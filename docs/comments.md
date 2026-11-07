# Comments

A **comment** is a note in Markdown on an asset, a category, a loan or something a flow filed: "the
left motor makes a noise above 50%", "don't lend this to first-timers", or "ordered a replacement
belt" (#223). Typing `@` in one mentions someone, who is emailed (#224).

## What comments are on

| On | Where it shows | Its history |
|---|---|---|
| An asset | Its page, under **Comments** | The asset's |
| A category | Its **Comments** tab | The category's events (`category_id`), which have no page yet; each comment's own history is `GET /api/v1/comments/{id}/history` |
| A loan (`asset_loans`) | The **Loan** box of its asset's page, while it is lent | Its asset's |
| A filing (`flow_filings`: an asset a flow filed) | **Filed by flows** on its asset's page | Its asset's |

### Ancestors

An asset's comments are followed by those on its categories and their ancestors, and a
category's by those on its ancestors:

- Its own come first, oldest first.
- Then each category's together, nearest first: an asset's categories, then their parents, and so
  on; a category's parent, then its parent. Categories as near as each other come by name, and each
  comes once.
- Each is marked with the category it is on (`category`), and the web app links to it on the
  category's page, at the comment: `/categories/{id}?tab=comments#comment-{comment id}`.
- `?own=1`, or **Only this asset's**, keeps its own only.

Ancestors' comments are listed only to those who read the catalog (`assets.read` anywhere), so
someone signed out who sees a public asset sees its own comments only.

## Writing

Comments are Markdown, up to 20000 characters: GitHub's flavor, with headings, lists, tables,
code, links, task lists and images.

### Rendering

Comments are rendered to HTML on the server (`internal/markdown`), so that the page's Content
Security Policy is not the only defense:

1. [goldmark](https://github.com/yuin/goldmark), pure Go, with GitHub's tables, strikethrough,
   autolinks and task lists, renders it. HTML written in the Markdown is left out, never passed
   through.
2. [bluemonday](https://github.com/microcosm-cc/bluemonday), an allowlist sanitizer, then keeps
   only the elements Markdown makes (`p`, headings, lists, `table`, `pre`, `code`, `blockquote`,
   `a`, `img`, `em`, `strong`, `del`, `br`, `hr`) and disabled checkboxes for task lists. Links and
   images may be `http`, `https` or `mailto` addresses, or paths within BAM!. Everything else is
   removed: scripts, event handlers, `style`, frames, forms, SVG, and `javascript:`, `vbscript:` and
   `data:` addresses. Links to other sites get `rel="nofollow noreferrer"`.

A comment is stored as written and rendered each time it is read, so a fix to the renderer applies
to every comment at once. `POST /api/v1/markdown` renders the same way, for the editor's
**Preview**.

### Images

An image within BAM!, such as an attachment, a path starting with one `/`, is shown. An image from
another site is shown as a link to it instead: loading it would tell that site the address of
everyone who read the comment, and the web app's Content Security Policy (`img-src 'self' data:
blob:`) would not load it anyway. BAM! does not fetch other sites' images for them either, which
would let anyone who comments make the server fetch any address.

### Files

A file attached while writing goes on what the comment is on, as an
[attachment](data-model.md#attachments): `POST /api/v1/{assets,categories}/{id}/comments/attachments`
takes it as the attachment routes do, with `comments.write` in its scopes rather than the
permission to change the asset or category. It answers the attachment, its `url`, and `markdown`
linking to it (`![name](url)` for an image, `[name](url)` otherwise), which the editor puts in the
text where the cursor is. Loans and filings have no attachments of their own, so theirs go on
their asset. The usual [limits](configuration.md) apply, and removing a file still takes the
permission to change its owner.

## Mentions

Typing `@` in the editor offers the people who may read what the comment is on, by name, as
other sites' comment boxes do; choosing one (click, or arrow keys and Enter or Tab) puts `@` and
their name in the text.

- **Stored by ID.** The comment stores a mention as `<@id>`, the person's user ID, so it follows
  them when they are renamed. The editor shows `@name` while writing, and turns each name it
  offered back into `<@id>` for the preview and when saving. Through the API or MCP, write
  `<@id>` yourself. `<@id>` in code is text, not a mention, and an ID naming no one mentions no
  one.
- **Rendered with the name now.** A mention reads as `@` and the person's name as it is when the
  comment is read, in a `<span class="mention">`: the one element the sanitizer lets through for
  it, with the name escaped. As who wrote a comment does, the name shows only to those with
  `assets.track` in its scopes; to anyone else, and for a person with no name or who is gone, it
  reads `@someone`. A comment lists those it mentions, by name, as `mentions`, to those who see
  who. The preview names people to those who hold `assets.track` anywhere.
- **Who is offered.** `GET …/comments/people` lists the active people who may read what the
  comment is on (as for reading comments: the asset in its scopes, or the catalog for a
  category), by name, leaving out the requester. It answers only those with both
  `comments.write` and `assets.track` in its scopes, who may comment there and already see the
  people around it, so that it tells no one else who has an account. Someone who cannot read it
  is never offered.
- **Who is told.** A person mentioned is emailed once, through the email set up for BAM! (see
  [Notifications](notifications.md#email)): when the comment first mentions them, by the
  notification worker, within seconds. An edit emails only those it mentions for the first time;
  taking a mention out and putting it back tells no one again. No one is emailed who cannot read
  what it is on when it goes out, nor anyone who has left, nor about a comment deleted by then,
  nor about one's own comment. The email says who wrote it and names those it mentions only to
  someone who sees who. A mention not emailed within a day, such as while email is not set up,
  is not emailed at all.
- **Each person's choice.** **Email me when a comment mentions me**, under **Your account**
  (`PUT /api/v1/auth/mention-emails` with `{"mentions": false}`), stops these emails. It is
  separate from the email that notification channels send to one's roles.

BAM! has no inbox of its own for each person: a mention is told by email, and the comment itself
is linked from it (`…#comment-{id}`). The mentions recorded for each person
([Storage](#storage)) are what an inbox would list, if one is added.

## Editing and deleting

- Only a comment's author edits it, and only with `comments.write` in its scopes. An edited one
  has `edited_at`, and the web app says *(edited)*. The same text again changes nothing.
- Its author, with `comments.write`, or anyone with `comments.moderate` in its scopes, deletes it.
  A deleted comment keeps its place, with no text, `deleted_at` and `deleted_by`, and the web app
  says *Deleted by …*. It cannot be edited or deleted again (`409`).
- Every version's text is kept in history: `comment.added` with its text, `comment.edited` with
  what it said and says (`changes`, `what` `comment`), and `comment.deleted` with what it said.

## Who sees and writes them

Reading follows what the comment is on:

- an asset's, a loan's and a filing's: whoever reads the asset, in its scopes, signed out too for
  an asset's when the public role allows;
- a category's: whoever reads the catalog, as the category itself.

`comments.write` writes comments, in the scopes of what they are on (for a category, the scopes
it gives its assets), and lets authors edit and delete their own. `comments.moderate` deletes
anyone's, in the same scopes. Both are scopable. Migration `069` grants `comments.write` to every
role that writes assets, and `comments.moderate` to every role that archives assets, each in the
same scopes. See [Access](access.md).

Who wrote and deleted a comment (`author_id`, `author_name`, `deleted_by`, `deleted_by_name`) is
shown only to those with `assets.track` in its scopes, as history shows people. `mine` tells the
author a comment is theirs, and `can_edit` and `can_delete` what the requester may do.

## Search

`GET /api/v1/comments?q=` finds the comments, not deleted, whose text holds the words given,
ignoring case, newest first, up to 100, among those the requester may read, each naming what it
is on (`place`). The asset list's `?search=` also finds assets by their comments' text, and their
loans' and filings'.

## Events

`comment.added` is sent to the notification channels that subscribe to it, with the comment's
text and what it is on (`payload.comment`), and a link to the comment itself
(`…#comment-{id}`). Email reaches only those whose roles reach what it is on, as for other
events. The text's mentions are told by the names the people had when it was written
(`payload.comment.mentions`), or as `@someone`. `comment.edited` and `comment.deleted` are for
history only. See [Notifications](notifications.md). A mention's own email is not an event: it
goes to the person mentioned, as [Mentions](#mentions) says.

## API

| Route | Permission | What |
|---|---|---|
| `GET /api/v1/assets/{id}/comments` | Reading the asset | Its comments, then its categories' and their ancestors' (`?own=1`: its own only) |
| `POST /api/v1/assets/{id}/comments` | `comments.write` in its scopes | `{"body": "…"}` |
| `POST /api/v1/assets/{id}/comments/attachments` | `comments.write` in its scopes | A file attached while writing |
| `GET /api/v1/{assets,categories,loans}/{id}/comments/people`, `GET /api/v1/assets/{id}/filings/{flow_id}/comments/people` | `comments.write` and `assets.track` in its scopes | The people a comment there may mention: `{"people": [{"id", "name"}]}` |
| `GET`, `POST /api/v1/categories/{id}/comments` | `assets.read`; `comments.write` in its scopes | As an asset's, with its ancestors' |
| `POST /api/v1/categories/{id}/comments/attachments` | `comments.write` in its scopes | |
| `GET`, `POST /api/v1/loans/{id}/comments` | Reading its asset; `comments.write` in its scopes | |
| `GET /api/v1/assets/{id}/filings` | Reading the asset | The flows that filed it, with how many comments each filing has |
| `GET`, `POST /api/v1/assets/{id}/filings/{flow_id}/comments` | Reading the asset; `comments.write` in its scopes | |
| `GET /api/v1/comments?q=` | `assets.read` | Search |
| `GET /api/v1/comments/{id}` | Reading what it is on | One comment |
| `PUT /api/v1/comments/{id}` | Its author, with `comments.write` | `{"body": "…"}` |
| `DELETE /api/v1/comments/{id}` | Its author, with `comments.write`, or `comments.moderate` | Leaves *deleted by* in its place |
| `GET /api/v1/comments/{id}/history` | Reading what it is on | Its events, oldest first |
| `POST /api/v1/markdown` | Signed in | `{"body": "…"}` rendered as a comment is, as `{"html": "…"}` |
| `PUT /api/v1/auth/mention-emails` | Signed in, as a person | Turn one's own mention emails on or off: `{"mentions": true}` |

The [MCP tools](mcp.md#comments) reach each of these but the preview.

## Storage

Migration `069` adds a table for each kind of thing comments are on: `asset_comments`,
`category_comments`, `loan_comments` and `flow_filing_comments`. Each has a foreign key to what it
is on, so it is deleted with it, and goes with its kind of information in backups, as attachments
do (see [Backups](backups.md#kinds-of-information)): an asset's and a loan's with the assets, a
category's with the catalog, and a filing's with the flows. One table for every kind, keyed by kind
and ID, could have none of those foreign keys, and would put a category's comments in the same kind
of information as an asset's.

`author_id` and `deleted_by` are the user or API token who acted, and are not foreign keys, so a
comment outlives its author. Search matches in Go rather than with `LIKE`, as the asset search
does, so it folds case the same way on every database.

Migration `071` adds `comment_mentions`: for each comment, each person it has mentioned, by user
ID, when, and when they were dealt with (`notified_at`: emailed, or passed over), which servers
sharing a database claim each by setting, so each goes out once. A row stays when an edit takes
its mention out, so that putting it back tells no one again. Its `comment_id` names a comment in
any of the four tables, so it has no foreign key, as a favorite's `object_id` has none; it goes
with its person, deleted with them, and with the people in backups. The same migration adds
`users.mention_emails`, each person's choice.

[Packages](packages.md) carry their categories' comments when asked (`--comments`, **With the
categories' comments**, or MCP's `comments`), without who wrote them or deleted ones. Their
mentions name people by this instance's IDs, so elsewhere they read as `@someone`.

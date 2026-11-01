# Access

BAM! decides who may see and change what with roles, permissions and scopes. People sign in with
an email and password, and hold permissions through the roles they are given. Scripts, dashboards
and assistants use API tokens instead of a session. Scopes, labels put on categories, let a role
reach some assets and not others.

This page covers signing in, user accounts, roles and permissions, scopes, the public role, and
API tokens.

## Signing in

People sign in with an email and a password.

- Emails are case-insensitive and unique.
- A new password must be 8 to 72 bytes. 72 is the most bcrypt uses, so a longer password is
  refused rather than silently cut short.
- Two-factor authentication with TOTP (RFC 6238) is optional, turned on per user.

Signing in starts a session. The session lives in the database, and the browser holds a cookie
that refers to it (see [Sessions](#sessions)).

### Forgotten passwords

Whoever manages users sets a new password for someone who has forgotten theirs. The sign-in page
says whom to ask. Setting someone's password signs them out everywhere, except in the session
making the change when it is their own.

On the command line, `bam admin password --email … --password …` does the same for anyone, such
as an admin who is locked out. It changes nothing else about the account. (`bam admin create`
would also make them an admin.)

## Your account

### Display name

Each user has a display name of up to 255 characters. Asset pages show people by it: borrowers,
history, the loans list, and report and label bindings. Asset pages, lists, history and the loans
list never show emails.

- A name is required. Admins give it when creating a user and can change it later, and anyone
  signed in can change their own. A blank name is refused (`400`).
- A user from before names were required, or the first admin made by `bam admin create` without
  `--name`, shows as "Unnamed user" until named. The home page's setup checklist asks an admin
  with no name to give one.
- To the signed-in user, what they did themselves reads "you".
- The header and the user list show the email when there is no name.
- The lending form names people by name, adding the email only to tell apart two people with the
  same name.
- History keeps the name as it was when the change was made. Discord messages name the person by
  it.

The name is for display only. Signing in is always by email.

### Theme

Each user chooses whether the web interface is `light` or `dark` for them, or `system`, following
their system's setting. Everyone starts at `system` until they choose.

Wherever the API returns a user (`GET /api/v1/auth/me` among them), it carries the theme as
`theme`, always one of those three. Anyone signed in changes their own. Any other value is
refused (`400`).

The theme is a preference rather than a change to the account, so changing it leaves the user's
`updated_at` as it was. A backup keeps it with the person, and restoring a backup made before
migration `048` puts everyone at `system`. No MCP tool reaches it, since an assistant sees no
interface.

## Roles and permissions

A user holds permissions only through roles. A user given several roles holds the union of what
they grant, each as narrowed to its scopes (see [Narrowing a role](#narrowing-a-role-to-scopes)).

### Permissions

Permissions are `resource.action` keys. Keys were written `resource:action` until migration `019`
renamed them, so that a key can carry a scope (`tools:assets.write`).

| Permission | Allows | Scopable |
|---|---|---|
| `assets.read` | Reading assets. | Yes |
| `assets.write` | Creating and editing assets, their codes, photos and links, and marking an asset `ok` again. | Yes |
| `assets.delete` | Archiving and restoring assets. | Yes |
| `assets.report` | Reporting an asset as needing maintenance or out of service. | Yes |
| `assets.borrow` | Borrowing assets (see [Stock and lending](stock-and-lending.md)). | Yes |
| `assets.lend` | Lending assets (see [Stock and lending](stock-and-lending.md)). | Yes |
| `assets.scan` | Scanning (see [Scanning](scanning.md)). | Yes |
| `assets.track` | Seeing the people around assets: who borrowed them, who changed them, and the loans list. | Yes |
| `assets.hook` | Triggering hooks (see [Hooks](hooks.md)). | Yes |
| `events.read` | Receiving events live (see [Hooks](hooks.md)). | Yes |
| `stock.record` | Using, restocking and flagging stock, without being able to edit assets. | Yes |
| `flows.run` | Running flows (see [Flows](flows.md)). | Yes |
| `flows.manage` | Making flows (see [Flows](flows.md)). | No |
| `templates.manage` | Managing label templates (see [Labels](labels.md)). | No |
| `labels.print` | Printing labels (see [Printing](printing.md)). | No |
| `reports.manage` | Managing reports (see [Reports](reports.md)). | No |
| `categories.manage` | Managing categories, including moving them under others, and relation types. | No |
| `icons.manage` | Changing what every icon is drawn with: the icon library, the palette and the icon settings (see [Icons](icons.md)). | No |
| `settings.manage` | Managing settings. | No |
| `users.manage` | Managing users, roles, scopes and API tokens. | No |

### Scopes

A scope is a label put on categories and passed down to their subcategories. Its name is 1 to 64
lowercase letters, digits, `-` and `_`.

An asset is in the scopes of its categories and of every ancestor of theirs. When there are none,
it is in the reserved `default` scope. So an asset leaves `default` when it joins a category with
a scope, and returns to it when it leaves the last one.

Two scopes are built in:

- `default`, holding every asset in no other scope. It cannot be put on a category (`409`).
- `public`, which labels no categories until someone puts some in it (see
  [The public role](#the-public-role)).

Neither can be renamed or deleted (`409`). Renaming any other scope carries along the roles'
permissions in it and the roles narrowed to it. A scope can be deleted only when no category,
role or role assignment uses it (`409` otherwise). Deleting it also deletes the narrowing of
archived users' and revoked tokens' roles to it.

Only `users.manage` creates, renames and deletes scopes, and puts them on categories, since scopes
decide who reaches what. Moving categories under others changes their assets' scopes, so
`categories.manage` is trusted with that.

### Scoped permissions

A role holds each permission either in every scope (`assets.write`) or in some
(`tools:assets.write`, `default:assets.write`). Only the asset permissions are scopable:
`assets.read`, `assets.scan`, `assets.hook`, `events.read`, `assets.track`, `assets.write`,
`assets.delete`, `assets.report`, `assets.borrow`, `assets.lend`, `stock.record` and `flows.run`.

A permission in any one of an asset's scopes is enough. A permission held in every scope covers
each scope, and a role given it both ways keeps the wider.

### How checks work

Holding a permission in any scope opens its endpoints and pages. Each then checks the asset:

- An asset the requester can neither read nor act on as asked answers `404`, as if it did not
  exist.
- An asset they can read but not act on answers `403`.

Lists, exports, reports, loans and labels hold only the assets the requester can read. An asset's
links and location leave out the assets the requester cannot read.

Some actions check more than one set of scopes:

- Creating an asset, or moving one to other categories, needs `assets.write` in the scopes it ends
  up in as well as the scopes it was in.
- A link needs `assets.write` on both assets.
- An import checks each row and link the same way, and reports what it may not change as
  problems. Assets it cannot read are, to it, unknown.
- Flagging a category running low needs `stock.record` in the scopes its assets get from it.

`GET /api/v1/auth/me` lists scoped keys as they are, so the web app shows each asset only what its
scopes allow.

### Narrowing a role to scopes

Assigning a role to someone may narrow it to some scopes, as "Editor, in `tools`". That way a
role need not be copied for each combination of scopes.

Naming no scopes is the usual case, and works as it always has: the role applies in every scope
its permissions name. Narrowed, a role grants:

- each scopable permission it holds in every scope (`assets.write`) in each scope the assignment
  names (`tools:assets.write`);
- each scopable permission it holds in some scopes (`kitchen:assets.write`) in the scopes both
  name, and not at all if they share none;
- none of its permissions that are not scopable (`users.manage`, `labels.print` and the rest). So
  "Admin, in `tools`" manages no one.

Narrowing limits where a role applies. It never adds to what the role grants.

The union principle holds across assignments. A user holding the same role unrestricted and
narrowed, or several roles, holds what each assignment gives. The same role given twice is one
assignment, over the union of its scopes, or unrestricted if either is.

`default` and `public` can be named like any other scope. The public role, held by everyone,
cannot be narrowed (`400`).

API tokens' roles are narrowed the same way (see [API tokens](#api-tokens)).

### Seeing people

Without `assets.track` for an asset:

- its loan says only that it is lent out, and to the requester if it is theirs;
- its history leaves out who did each thing and who borrowed it;
- `{{loan.borrower}}` is blank.

With it, people show by name. Emails never show on asset pages, lists, history or the loans list.

### The public role

Everyone holds the `public` role, signed in or not, and API tokens hold it too. It is not given to
users, who all have it already, and `GET /api/v1/roles` marks it `public`.

- It may only look: `assets.read` and `assets.scan`, in any scopes.
- It keeps its name and cannot be deleted (`409`).
- It cannot be narrowed to scopes (`400`).

It starts with `public:assets.read`: reading the assets in the `public` scope. That scope labels
no categories until someone puts some in it, so nothing is public until then.

Signed in, the public role's permissions add to the user's own wherever they apply. Signed out,
they apply only to an asset's own page:

- `GET /api/v1/assets/{id}`, the asset's photos, and its history, without people (see
  [Stock and lending](stock-and-lending.md));
- `GET /api/v1/generators`.

The catalog (the asset list, categories, fields, templates, reports and the rest) answers `401`.
The web app opens only the asset page, with a link to sign in. `GET /api/v1/auth/me` answers
signed-out visitors with `{"user": null}` and the public role's permissions.

A role or scope already named `public` before migration `021` is renamed `public (renamed)` or
`public-renamed`, so that what it allowed never passes to everyone.

### The admin role

Seeding grants every permission to the `admin` role, including permissions added by later
releases. The `admin` role is protected: it cannot be renamed, edited or deleted (`409`), since
seeding recreates it by name and grants it every permission at each start.

### Managing roles

Roles are listed with those made here first, by name regardless of case, then the built-in
`admin` and `public`. Each is listed with its permissions, how many users who are not archived
hold it (`user_count`), and whether it is `protected`.

- Role names are unique and up to 64 characters.
- Permissions must be known keys, each alone or, if scopable, as `scope:key` naming an existing
  scope.
- Deleting a role takes it from everyone who holds it, tokens included.
- Editing and deleting roles follow the anti-lockout rule.

### Anti-lockout

Any deletion, deactivation, role removal, role narrowing or role edit that would leave no active
user holding `users.manage` is rejected.

A role narrowed to scopes grants no `users.manage`, so only users holding it through a role
assigned in every scope count. Tokens do not count.

Each such change runs in one transaction that first takes the `users_manage` row of the
`app_locks` table. Concurrent changes are therefore checked one after another, and two admins
cannot remove each other at the same time.

## Sessions

Sessions are stateful. They are stored in the active database, in the `sessions` table, and
referenced by an `HttpOnly`, `SameSite=Lax`, `Secure` cookie.

The cookie holds a random 256-bit token. The table stores only its SHA-256 hash, so a copy of the
database cannot be used to sign in.

## API tokens

Scripts and dashboards authenticate with `Authorization: Bearer <token>` instead of a session.

A token has:

- a name;
- roles whose permissions it holds, as a user holds theirs, each narrowed to some scopes or not;
- an optional expiry;
- the time it was last used, written at most once a minute.

Its secret is `bam_` followed by 64 hex digits (256 random bits). It is returned once, when the
token is made. As with a session token, only its SHA-256 hash is stored.

### Rules for tokens

- Only a signed-in session makes tokens, so that a token, or an assistant led astray by what it
  read, cannot leave itself a way back in.
- One holding `users.manage` lists and revokes tokens, as it lists live connections.
- No token can use the password or logout endpoints, which answer it `403`. Only a personal token
  can use the rest of the account's endpoints (`/api/v1/auth/me`, `name`, `theme`).
- A revoked, expired or unknown token is answered `401` at once, even with a session cookie
  alongside.
- Revoked tokens stay listed.
- Tokens do not count toward the anti-lockout rule.
- Deleting a role takes it from tokens too.

### History

Changes a token makes are recorded in history under its name (`"actor": {"token": true, ...}`).

A change made through an MCP tool also records the assistant it came through, as `"via"` on the
actor. The assistant is named as the MCP client names itself (`clientInfo`, its title if it has
one), cleaned as uploaded image names are. It is `MCP` when the client gives no name, as clients
of protocol versions before `2026-07-28` do not on each request. History shows "Ada, via Claude
Desktop", and so do Discord messages and live events.

Only a tool's request can carry `"via"`: nothing a request from outside sends sets it. Labels
printed later by the spooler, from the print queue, are recorded without it.

### Personal tokens

A signed-in user makes tokens for themselves, for an assistant (see [MCP](mcp.md)), under
*Your account › Assistants*. Each user lists, makes and revokes only their own, from a signed-in
session. Anyone else's is `404`.

A personal token holds no roles. It acts as its user, with their permissions as they are at each
request, so a role taken from the user is gone at the token's next request. What it changes is
recorded as theirs.

A personal token can:

- borrow as its user;
- answer `GET /api/v1/auth/me` as them;
- change their name and theme;
- list and revoke their own personal tokens, itself among them.

Like any token, it cannot change the password, sign out, or make tokens (`403`).

Deactivating or archiving the user revokes their personal tokens, as it ends their sessions, so
turning the user back on brings no assistant back.

`GET /api/v1/tokens` lists personal tokens with every other token, with `user_id` and
`user_name`, and `users.manage` revokes any of them there. A personal token's live connections
(see [Hooks](hooks.md)) count against its user.

## API

### Users and accounts

| Method and route | Permission | Purpose |
|---|---|---|
| `GET /api/v1/auth/me` | Signed in, signed out, or a personal token | The current user, their permissions after narrowing, and `role_scopes`. |
| `PUT /api/v1/auth/name` | Signed in, or a personal token | Change your own name, with `{"name": "..."}`. |
| `PUT /api/v1/auth/theme` | Signed in, or a personal token | Change your own theme, with `{"theme": "dark"}`. Answers `{"user"}`, as a rename does. |
| `GET /api/v1/users` | `users.manage` | List users, each with `role_ids` and `roles`. |
| `POST /api/v1/users` | `users.manage` | Create a user; `name` is required. |
| `PUT /api/v1/users/{id}/name` | `users.manage` | Change a user's name, with `{"name": "..."}`. |
| `PUT /api/v1/users/{id}/password` | `users.manage` | Set a user's password, with `{"password"}`. Signs them out everywhere. |
| `PUT /api/v1/users/{id}/roles` | `users.manage` | Set a user's roles. |

`PUT /api/v1/users/{id}/roles` takes `{"roles": [{"role_id", "scope_ids": [...]}]}`. An empty or
missing `scope_ids` means every scope. It still takes `{"role_ids": [...]}`, each role in every
scope. It answers with both `role_ids` and `roles`.

`GET /api/v1/auth/me` lists the keys a user holds after narrowing, and in `role_scopes` the names
of the scopes each narrowed role holds, by role name.

### Roles and scopes

| Method and route | Permission | Purpose |
|---|---|---|
| `GET /api/v1/roles` | `users.manage` | List roles, with `user_count`, `protected` and `public`. |
| `POST /api/v1/roles` | `users.manage` | Create a role, with `{"name", "description", "permissions"}`. |
| `PUT /api/v1/roles/{id}` | `users.manage` | Change a role, with the same body. |
| `DELETE /api/v1/roles/{id}` | `users.manage` | Delete a role. |
| `GET /api/v1/scopes` | `users.manage` or `categories.manage` | List scopes. |
| `POST /api/v1/scopes` | `users.manage` | Create a scope, with `{"name"}`. |
| `PUT /api/v1/scopes/{id}` | `users.manage` | Rename a scope, with `{"name"}`. |
| `DELETE /api/v1/scopes/{id}` | `users.manage` | Delete an unused scope (`409` otherwise). |
| `GET /api/v1/categories/{id}/scopes` | `users.manage` or `categories.manage` | A category's own scopes. |
| `PUT /api/v1/categories/{id}/scopes` | `users.manage` | Set them, with `{"scope_ids": [...]}`. |

`GET /api/v1/scopes` gives, for each scope:

- how many categories it labels;
- how many roles hold permissions in it (`role_count`);
- how many roles of users who are not archived, and of tokens not revoked, are narrowed to it
  (`assignment_count`).

### Tokens

| Method and route | Permission | Purpose |
|---|---|---|
| `GET /api/v1/tokens` | `users.manage` | List all tokens, personal ones with `user_id` and `user_name`. |
| `POST /api/v1/tokens` | `users.manage`, from a signed-in session | Make a token. |
| `DELETE /api/v1/tokens/{id}` | `users.manage` | Revoke any token. |
| `GET /api/v1/auth/tokens` | Signed in, or a personal token | List your own personal tokens. |
| `POST /api/v1/auth/tokens` | Signed in, from a session | Make a personal token, with `{"name", "expires_at"}`. |
| `DELETE /api/v1/auth/tokens/{id}` | Signed in, or a personal token | Revoke one of your own personal tokens. |

`POST /api/v1/tokens` takes `{"name", "roles", "expires_at"}`. `roles` is given as a user's are,
each narrowed to some scopes or not; `role_ids` gives roles in every scope instead. `expires_at`
is an RFC 3339 time, or `null` for never.

## Storage

| Tables | Hold |
|---|---|
| `users`, `roles`, `permissions`, `user_roles`, `role_permissions` | Users, roles and their permissions. |
| `scopes`, `category_scopes`, `role_permission_scopes` | Scopes, the categories they label, and the scopes of roles' permissions. |
| `user_role_scopes`, `api_token_role_scopes` | Roles assigned in some scopes only, to users and to tokens. |
| `sessions` | Sessions, by the SHA-256 hash of their token. |
| `app_locks` | The `users_manage` row, taken by each change the anti-lockout rule checks. |

### Migrations

| Migration | Change |
|---|---|
| `004` | The `app_locks` table, for the anti-lockout rule. |
| `005` | Sessions stored by the SHA-256 hash of their token. |
| `010` | Users' display names. |
| `011`, `013`, `014` | Grant `stock.record`, `assets.report`, `assets.borrow` and `assets.lend` to every role that had `assets.write`, so nobody loses what they could do. |
| `012` | API tokens. |
| `017` | Grants `reports.manage` to every role that had `templates.manage`. |
| `019` | Scopes (`scopes`, `category_scopes`, `role_permission_scopes`); renames permission keys from `resource:action` to `resource.action`. |
| `020` | Grants `assets.track` to every role that reads assets, in the scopes it reads them in, since reading showed people until then. |
| `021` | The `public` role and scope; renames any role or scope already named `public`. |
| `031` | Personal tokens. |
| `035` | Grants `icons.manage` to every role that manages categories, as icons are part of how categories look. |
| `041` | Roles narrowed to scopes (`user_role_scopes`, `api_token_role_scopes`). |
| `043` | Grants `flows.manage` to every role that manages categories, and `flows.run` to every role that writes assets, in the same scopes. |
| `048` | Users' themes. |

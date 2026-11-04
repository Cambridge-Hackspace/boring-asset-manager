# Favorites

Each person stars what they use often, to find it again: assets, categories, reports and flows.

## Starring

A star sits beside the name on an asset's, a category's and a report's page, and on each row of the
asset list, the category tree and the list of reports. A flow is starred on its card under **Flows**.
The star is pressed while the thing is a favorite; pressing it again unstars it.

## Where they show

- **The home page** has a *Favorites* card, grouped by kind (assets, categories, reports, flows), each
  by name. A starred flow links to where it runs, `/f/<slug>`. Archived assets are left out.
- **The asset list** has a *Favorites* filter, *Only favorites*, kept in the address as `?favorite=true`
  like the other filters.

## Whose they are

Favorites are each person's own: nobody else sees them, admins included. An API token holding roles
is no one, so has none; a personal token has its person's (see [Access](access.md#personal-tokens)).

A favorite the person can no longer see, because a role or a scope was taken away, a flow was turned
off, or the thing was deleted, is hidden, not removed. It comes back if they can see it again. Only
what they can see can be starred.

## API

| Method and route | Who | Purpose |
|---|---|---|
| `GET /api/v1/auth/me/favorites` | Signed in, or a personal token | The favorites they can see, oldest first: `{"favorites": [{"kind", "id", "name", "slug", "archived", "created_at"}]}`. `slug` is a flow's; `archived` marks an archived asset. |
| `PUT /api/v1/auth/me/favorites/{kind}/{id}` | Signed in, or a personal token | Star an `asset`, `category`, `report` or `flow`. Answers `{"favorite"}`; starring again changes nothing. `404` for what is not there or they cannot see, `400` for another kind. |
| `DELETE /api/v1/auth/me/favorites/{kind}/{id}` | Signed in, or a personal token | Unstar it, even when they can no longer see it. Answers `204`. |
| `GET /api/v1/assets?favorite=true` | `assets.read`, and a person | Only the assets they have starred, with the list's other filters. |

Over MCP, `list_favorites`, `add_favorite` and `remove_favorite` do the same, and `search_assets`
takes `favorite` (see [Assistants](mcp.md)).

## Storage and backups

Favorites are kept in `user_favorites` (migration 056): the person, the kind, the thing's ID and when
it was starred. Backups keep them with the people and roles (see [Backups](backups.md)); packages do
not.

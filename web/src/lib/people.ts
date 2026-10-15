// How people, and API tokens acting for them, are shown.

/** A person's display name, or their email when they have none. */
export function displayName(person: { name?: string; email: string }): string {
  return person.name?.trim() || person.email;
}

/** Who made a change in an asset's history: a person or an API token. */
export function actorName(actor: { name?: string; email: string; token?: boolean }): string {
  return actor.token ? `API token “${actor.name ?? ''}”` : displayName(actor);
}

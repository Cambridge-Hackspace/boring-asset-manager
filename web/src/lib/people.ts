// How people, and API tokens acting for them, are shown.

/** A person's display name, or their email when they have none: for the signed-in user's own account. */
export function displayName(person: { name?: string; email: string }): string {
  return person.name?.trim() || person.email;
}

/** A person as asset pages show them: by name, never by email (README §3.3). */
export function personName(person: { name?: string }): string {
  return person.name?.trim() || 'Unnamed user';
}

/**
 * Who made a change in an asset's history: a person, "you" when it was me, or
 * an API token; and the assistant it was made through, if any.
 */
export function actorName(actor: { id?: string; name?: string; token?: boolean; via?: string }, me?: string): string {
  const who = actor.token ? `API token “${actor.name ?? ''}”` : me && actor.id === me ? 'you' : personName(actor);
  return actor.via ? `${who}, via ${actor.via}` : who;
}

/**
 * People to choose from, by name; a name two people share gets its email
 * too, so the choice is never a guess.
 */
export function choiceNames<P extends { id: string; name?: string; email: string }>(people: P[]): Map<string, string> {
  const counts = new Map<string, number>();
  for (const p of people) counts.set(personName(p), (counts.get(personName(p)) ?? 0) + 1);
  return new Map(people.map((p) => [p.id, (counts.get(personName(p)) ?? 0) > 1 ? `${personName(p)} (${p.email})` : personName(p)]));
}

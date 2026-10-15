// How people are shown.

/** A person's display name, or their email when they have none. */
export function displayName(person: { name?: string; email: string }): string {
  return person.name?.trim() || person.email;
}

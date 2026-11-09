// Person fields (docs/data-model.md#person-fields) name people by user ID,
// separated by commas, so that a renamed person's fields follow them. The
// asset says who they are, by name, as the viewer may see them, and how
// many it names that the viewer may not.

import type { AssetWithDetails, FieldPerson } from './models';

/** The user IDs a person field's value names, in order, each once. */
export function personIds(value: string | undefined): string[] {
  const out: string[] = [];
  for (const id of (value ?? '').split(',')) {
    const t = id.trim();
    if (t && !out.includes(t)) out.push(t);
  }
  return out;
}

/** The value of a person field naming these user IDs. */
export function personValue(ids: string[]): string {
  return personIds(ids.join(',')).join(',');
}

/** A person as a person field shows them: their name, and whether they have left or have no account. */
export function personLabel(p: FieldPerson): string {
  if (p.inactive) return `${p.name} (no longer active)`;
  if (p.no_account) return `${p.name} (no account)`;
  return p.name;
}

/** Whom an asset's person field names, as the viewer sees them, and how many it names that they may not see. */
export function fieldPeople(asset: Pick<AssetWithDetails, 'field_values' | 'people' | 'hidden_people'>, key: string): { people: FieldPerson[]; hidden: number } {
  const people = personIds(asset.field_values[key])
    .map((id) => asset.people?.[id])
    .filter((p): p is FieldPerson => !!p);
  return { people, hidden: asset.hidden_people?.[key] ?? 0 };
}

/** A person field's value as text: the names shown, and "someone" for those that are not. */
export function fieldPeopleText(asset: Pick<AssetWithDetails, 'field_values' | 'people' | 'hidden_people'>, key: string): string {
  const { people, hidden } = fieldPeople(asset, key);
  const names = people.map(personLabel);
  if (hidden > 0) names.push(names.length ? (hidden === 1 ? 'someone else' : `${hidden} others`) : hidden === 1 ? 'Someone' : `${hidden} people`);
  return names.join(', ');
}

/** The people whose names hold q, ignoring case, those starting with it first, as the server orders them. */
export function matchPeople<P extends { name: string }>(people: P[], q: string): P[] {
  const t = q.trim().toLowerCase();
  if (!t) return people;
  const first = people.filter((p) => p.name.toLowerCase().startsWith(t));
  const rest = people.filter((p) => !p.name.toLowerCase().startsWith(t) && p.name.toLowerCase().includes(t));
  return [...first, ...rest];
}

/** Whether a name is one of these people's, ignoring case: typing it offers no one new. */
export function namesSomeone(people: { name: string }[], q: string): boolean {
  const t = q.trim().toLowerCase();
  return people.some((p) => p.name.toLowerCase() === t);
}

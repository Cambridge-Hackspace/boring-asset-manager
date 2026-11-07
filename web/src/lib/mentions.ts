// Mentions in comments (docs/comments.md#mentions). A comment stores a
// mention as <@id>, the person's user ID, so that it follows them when they
// are renamed. The editor shows it as @ and their name instead, and turns
// the names it knows back into <@id> when the comment is previewed or saved.

/** A person a comment mentions, or may mention. */
export interface MentionPerson {
  id: string;
  name: string;
}

const uuid = '[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}';

/** A mention as stored. */
export const mention = (id: string) => `<@${id}>`;

const escapeRegExp = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/**
 * Names the people a stored text mentions, as the editor shows it: each
 * <@id> of a person named in people becomes @ and their name, and the
 * names used are added to known. Two people of the same name, and anyone
 * not named, stay as <@id>, so that saving cannot mix them up.
 */
export function toDisplay(body: string, people: MentionPerson[], known: Map<string, string>): string {
  const byID = new Map(people.map((p) => [p.id.toLowerCase(), p.name]));
  return body.replace(new RegExp(`<@(${uuid})>`, 'g'), (whole, id: string) => {
    const name = byID.get(id.toLowerCase());
    if (!name || !remember(known, name, id.toLowerCase())) return whole;
    return `@${name}`;
  });
}

/**
 * Adds a name for an ID to known, answering false when the name is
 * already another person's, as a mention by that name would be ambiguous.
 */
export function remember(known: Map<string, string>, name: string, id: string): boolean {
  const had = known.get(name);
  if (had && had !== id) return false;
  known.set(name, id);
  return true;
}

/**
 * Turns each @name the editor knows back into <@id>, longest names first,
 * where the name stands alone: no letter or digit before the @ or after
 * the name.
 */
export function toStored(body: string, known: Map<string, string>): string {
  const names = [...known.keys()].filter((n) => n.trim()).sort((a, b) => b.length - a.length);
  if (!names.length) return body;
  const pattern = new RegExp(`(?<![\\p{L}\\p{N}])@(${names.map(escapeRegExp).join('|')})(?![\\p{L}\\p{N}])`, 'gu');
  return body.replace(pattern, (whole, name: string) => {
    const id = known.get(name);
    return id ? mention(id) : whole;
  });
}

/**
 * The @ being typed before the caret, if any: where it starts, and what
 * follows it so far. An @ counts at the start, or after a space or an
 * opening bracket, so that an email address does not.
 */
export function mentionQuery(text: string, caret: number): { start: number; query: string } | null {
  const before = text.slice(0, caret);
  const m = /(^|[\s([{])@([^\s@<>]{0,40})$/u.exec(before);
  if (!m) return null;
  const query = m[2] ?? '';
  return { start: caret - query.length - 1, query };
}

/** The people whose names match what is typed: any word starting with it, best first. */
export function matching(people: MentionPerson[], query: string, limit = 8): MentionPerson[] {
  const q = query.toLowerCase();
  const scored: [number, MentionPerson][] = [];
  for (const p of people) {
    const name = p.name.toLowerCase();
    if (name.startsWith(q)) scored.push([0, p]);
    else if (name.split(/\s+/).some((w) => w.startsWith(q))) scored.push([1, p]);
  }
  scored.sort((a, b) => a[0] - b[0] || a[1].name.localeCompare(b[1].name));
  return scored.slice(0, limit).map(([, p]) => p);
}

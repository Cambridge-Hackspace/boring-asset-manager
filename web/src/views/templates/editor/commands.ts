// The editor's actions, found by name, as Krita's action search (Ctrl+Enter)
// finds them: each word typed starts a word of the action's name or of the
// other words it goes by, or failing that is found in it, letters in order.

/** An action the editor can take, by name. */
export interface Command {
  name: string;
  /** Other words it is found by, such as "remove" for Delete. */
  keywords?: string;
  /** The keys that take it, shown beside it. */
  keys?: string;
  run: () => void;
}

const words = (s: string) => s.toLowerCase().split(/[^\p{L}\p{N}°%]+/u).filter(Boolean);

/** Whether the letters of part come in word, in order. */
function inOrder(part: string, word: string): boolean {
  let k = 0;
  for (const ch of word) if (ch === part[k]) k++;
  return k === part.length;
}

/**
 * How well a command's name answers what was typed: higher is better, and
 * null is no match. Each word typed counts most starting a word of its
 * name, less starting one of its other words, then found within, then its
 * letters found in order in the whole name.
 */
export function score(command: Command, query: string): number | null {
  const typed = words(query);
  if (!typed.length) return 0;
  const name = words(command.name);
  const other = words(command.keywords ?? '');
  const flat = command.name.toLowerCase();
  let total = 0;
  for (const t of typed) {
    if (name.some((w) => w.startsWith(t))) total += 4;
    else if (other.some((w) => w.startsWith(t))) total += 3;
    else if (flat.includes(t)) total += 2;
    else if (inOrder(t, flat.replace(/\s+/g, ''))) total += 1;
    else return null;
  }
  // The name's own first word, typed first, counts a little more.
  if (name[0]?.startsWith(typed[0]!)) total += 1;
  return total;
}

/** The commands that answer what was typed, the best first, in their own order where as good. */
export function findCommands(commands: Command[], query: string): Command[] {
  return commands
    .map((c, i) => ({ c, i, s: score(c, query) }))
    .filter((m): m is { c: Command; i: number; s: number } => m.s !== null)
    .sort((a, b) => b.s - a.s || a.i - b.i)
    .map((m) => m.c);
}

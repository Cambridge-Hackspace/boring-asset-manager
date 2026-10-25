// What may be typed at the caret in an isolex source: node names, with
// their arguments as snippets, property names, and the names a value can
// take there, such as the palette's tokens after fill= or the icons one
// can extend.

import { tokenize, outline, type Definition } from './syntax';

/** What completion knows of the lexicon around the source. */
export interface Vocabulary {
  /** The palette's tokens, and category. */
  tokens: string[];
  /** What every other source defines (this one's is read from it). */
  definitions: Definition[];
  /** The icons the current build emits, variants among them. */
  icons: string[];
}

export interface Suggestion {
  label: string;
  /** What replaces the word being typed. */
  insert: string;
  /** A line saying what it is. */
  detail: string;
  /** The selection after inserting, within insert: a placeholder to type over. */
  select?: [number, number];
}

export interface Completion {
  /** The range the chosen suggestion replaces. */
  from: number;
  to: number;
  items: Suggestion[];
}

/** A node: its name, what it is, and its arguments as typed when chosen,
    with $ marking the placeholder selected afterwards: $name$. */
interface NodeSpec {
  name: string;
  detail: string;
  snippet: string;
}

const node = (name: string, detail: string, args = ''): NodeSpec => ({ name, detail, snippet: args ? `${name} ${args}` : name });

/** Nodes at the top of a source. */
const topNodes: NodeSpec[] = [
  node('icon', 'An icon others can see', '"$name$" {\n  category "c"\n  footprint 1 1 1\n  part "body" {\n    box 0 0 0 1 1 1\n  }\n}'),
  node('base', 'Geometry icons extend, never drawn itself', '"$name$" {\n  \n}'),
  node('fragment', 'Geometry inserted into icons, with its own params', '"$name$" {\n  \n}'),
  node('axis', 'Variants every icon that opts in gets', '"$name$" {\n  variant "v" {\n    \n  }\n}'),
];

/** Nodes inside a definition. */
const defNodes: NodeSpec[] = [
  node('category', 'The group it is listed under', '"$c$"'),
  node('footprint', 'Its volume in tiles: width, depth, height', '$1$ 1 1'),
  node('canvas', 'A flat frame instead: width and height', '$24$ 24'),
  node('describe', 'What it is, in words', '"$…$"'),
  node('extends', 'The icon or base it builds on', '"$name$"'),
  node('param', 'A value, which those extending it can set', '$name$ 1'),
  node('part', 'Named geometry, which others can replace', '"$name$" {\n  \n}'),
  node('replace', 'Draw a part of what it extends differently', '"$part$" {\n  \n}'),
  node('remove', 'Leave out a part of what it extends', '"$part$"'),
  node('axes', 'The axes it takes variants from', '"$state$"'),
  node('grant', 'Paint with a reserved token, saying why', '"$accent$" reason="…"'),
  node('badge', 'Another icon, small, in a corner', '"$icon$" corner="ne" scale=0.5'),
  node('variant', 'One variant of an axis', '"$name$" {\n  \n}'),
  node('emit-base', 'Whether the icon without a variant is drawn too', '#$false$'),
];

/** Geometry. */
const shapeNodes: NodeSpec[] = [
  node('box', 'A box: x y z, width depth height', '$0$ 0 0 1 1 1'),
  node('tile', 'A flat tile: x y z, width depth', '$0$ 0 0 1 1'),
  node('cylinder', 'A cylinder: center x y, base z, radius, height', '$0.5$ 0.5 0 0.4 1'),
  node('facerect', 'A rectangle on a face: face, along, up, z, extent, height', '"$front$" 0.1 1 0.1 0.2 0.2'),
  node('poly3d', 'A polygon in tile space, from its points', '{\n  pt $0$ 0 0\n  pt 1 0 0\n  pt 1 1 0\n}'),
  node('shadow', 'A soft ground shadow: center, radii', '$0.5$ 0.5 0.48 0.48'),
  node('seg', 'A line between two tile-space points', '$0$ 0 0 1 0 0'),
  node('at', 'A point in tile space, to draw flat shapes around (x, y)', '$0.5$ 0.5 1 {\n  \n}'),
  node('line', 'A line on the screen', '"$x-5$" "y" "x+5" "y"'),
  node('circle', 'A circle on the screen', '"$x$" "y" 4'),
  node('ellipse', 'An ellipse on the screen', '"$x$" "y" 6 4'),
  node('rect', 'A rectangle on the screen', '"$x-5$" "y-5" 10 10'),
  node('arc', 'An arc: center, radius, from and to degrees', '"$x$" "y" 6 0 180'),
  node('path', 'An SVG path; {expr} is worked out, x and y included', 'd="$M{x},{y} h10$"'),
  node('poly', 'A flat polygon, from its points', '{\n  pt $"x"$ "y-5"\n  pt "x+5" "y+5"\n  pt "x-5" "y+5"\n}'),
  node('pt', 'A point of a polygon', '$0$ 0 0'),
  node('repeat', 'The same, several times, counting in a variable', '$3$ "i" {\n  \n}'),
  node('insert', 'A fragment, with its params', '"$fragment$"'),
];

/** Properties, by what takes them. */
const properties: Record<string, string> = {
  fill: 'Its fill: a palette token, or none',
  stroke: 'Its outline: a palette token, or none',
  width: 'Its stroke width',
  dash: 'A dash pattern, such as "3 2.5"',
  opacity: 'How opaque it is, 0 to 1',
  fills: 'A box drawn as body, virt (translucent) or none (outline)',
  top: "A cylinder's top: a palette token",
  side: "A cylinder's side: a palette token",
  when: 'Draw it only when this holds',
  extends: 'The icon or base it builds on',
  corner: "A badge's corner: ne, nw, se or sw",
  scale: "A badge's size",
  reason: 'Why a grant is needed',
};

const tokenProps = new Set(['fill', 'stroke', 'top', 'side']);

const isWord = (c: string | undefined) => c !== undefined && /[A-Za-z0-9_.\-]/u.test(c);

/** What fits at caret, or null for nothing to offer. */
export function complete(text: string, caret: number, vocab: Vocabulary): Completion | null {
  const ls = text.lastIndexOf('\n', caret - 1) + 1;
  const before = text.slice(ls, caret);
  const defs = [...vocab.definitions, ...outline(text)];

  // inside a string: a name to give
  const quote = openQuote(before);
  if (quote >= 0) {
    const from = ls + quote + 1;
    const typed = text.slice(from, caret);
    const head = before.slice(0, quote).trimEnd();
    const prop = /([A-Za-z-]+)=$/u.exec(head)?.[1];
    // the node this string belongs to: the last on the line to begin
    const nodeName = /^\s*([A-Za-z-]+)(?:\s|$)/u.exec(head.split(/[{;]/u).pop()!)?.[1];
    const names = valuesFor(prop ?? nodeName ?? '', prop !== undefined, text, caret, defs, vocab);
    return finish(from, caret, typed, names.map((n) => ({ label: n.label, insert: n.label, detail: n.detail })));
  }

  let start = caret;
  while (start > ls && isWord(text[start - 1])) start--;
  const typed = text.slice(start, caret);
  const lead = text.slice(ls, start);

  // after prop=, unquoted: a quoted value
  const prop = /([A-Za-z-]+)=$/u.exec(lead)?.[1];
  if (prop) {
    const names = valuesFor(prop, true, text, caret, defs, vocab);
    return finish(start, caret, typed, names.map((n) => ({ label: n.label, insert: `"${n.label}"`, detail: n.detail })));
  }

  if (/^\s*$/u.test(lead) || /[{;]\s*$/u.test(lead)) {
    const depth = depthAt(text, start);
    const specs = depth === 0 ? topNodes : [...(depth === 1 ? defNodes : []), ...shapeNodes];
    return finish(start, caret, typed, specs.map(snippet));
  }

  // anywhere else on a node's line: a property
  if (typed !== '') {
    return finish(start, caret, typed, Object.entries(properties).map(([p, detail]) => ({ label: p + '=', insert: p + '=', detail })));
  }
  return null;
}

function snippet(spec: NodeSpec): Suggestion {
  const m = /\$([^$]*)\$/u.exec(spec.snippet);
  if (!m) return { label: spec.name, insert: spec.snippet, detail: spec.detail };
  const insert = spec.snippet.replace(m[0], m[1]!);
  return { label: spec.name, insert, detail: spec.detail, select: [m.index, m.index + m[1]!.length] };
}

function finish(from: number, to: number, typed: string, items: Suggestion[]): Completion | null {
  const t = typed.toLowerCase();
  const matching = items
    .filter((s) => s.label.toLowerCase().startsWith(t) && s.insert !== typed)
    .concat(items.filter((s) => !s.label.toLowerCase().startsWith(t) && t !== '' && s.label.toLowerCase().includes(t)));
  return matching.length > 0 ? { from, to, items: matching } : null;
}

/** Where on the line a string still open at its end began, or -1. */
function openQuote(line: string): number {
  let open = -1;
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (open < 0 && c === '/' && line[i + 1] === '/') return -1;
    if (c === '\\' && open >= 0) {
      i++;
      continue;
    }
    if (c === '"') open = open < 0 ? i : -1;
  }
  return open;
}

/** How many blocks are open at an offset. */
function depthAt(text: string, at: number): number {
  let depth = 0;
  for (const t of tokenize(text.slice(0, at))) {
    if (t.kind === 'punct' && text[t.start] === '{') depth++;
    if (t.kind === 'punct' && text[t.start] === '}') depth = Math.max(0, depth - 1);
  }
  return depth;
}

/** The definition a caret is inside. */
function enclosing(text: string, at: number): string | undefined {
  let depth = 0;
  let current: string | undefined;
  const tokens = tokenize(text.slice(0, at)).filter((t) => t.kind !== 'space' && t.kind !== 'comment');
  tokens.forEach((t, k) => {
    if (t.kind === 'punct' && text[t.start] === '{') depth++;
    if (t.kind === 'punct' && text[t.start] === '}') depth = Math.max(0, depth - 1);
    if (depth === 0 && t.kind === 'node' && /^(icon|base|fragment)$/u.test(text.slice(t.start, t.end)) && tokens[k + 1]?.kind === 'string') {
      current = tokens[k + 1]!.value;
    }
  });
  return depth > 0 ? current : undefined;
}

/** The parts what a definition extends has, all the way up. */
function inheritedParts(name: string | undefined, defs: Definition[]): string[] {
  const parts: string[] = [];
  const seen = new Set<string>();
  let def = defs.find((d) => d.name === name);
  let up = def?.extends;
  while (up && !seen.has(up)) {
    seen.add(up);
    def = defs.find((d) => d.name === up && d.kind !== 'axis');
    for (const p of def?.parts ?? []) if (!parts.includes(p)) parts.push(p);
    up = def?.extends;
  }
  return parts;
}

interface Named {
  label: string;
  detail: string;
}

function valuesFor(what: string, isProp: boolean, text: string, caret: number, defs: Definition[], vocab: Vocabulary): Named[] {
  const named = (list: string[], detail: string) => [...new Set(list)].map((label) => ({ label, detail }));
  if (isProp && tokenProps.has(what)) return named([...vocab.tokens, 'none'], 'palette token');
  switch (what) {
    case 'grant':
      return named(vocab.tokens, 'palette token');
    case 'fills':
      return [
        { label: 'body', detail: 'the body tokens' },
        { label: 'virt', detail: 'translucent' },
        { label: 'none', detail: 'outline only' },
      ];
    case 'corner':
      return named(['ne', 'nw', 'se', 'sw'], 'corner');
    case 'extends': {
      // never itself: the definition the caret is in, or whose line it is on
      const ls = text.lastIndexOf('\n', caret - 1) + 1;
      const self = enclosing(text, caret) ?? /^\s*(?:icon|base)\s+"([^"]*)"/u.exec(text.slice(ls, caret))?.[1];
      return named(
        defs.filter((d) => (d.kind === 'icon' || d.kind === 'base') && d.name !== self).map((d) => d.name),
        'icon or base',
      );
    }
    case 'insert':
      return named(defs.filter((d) => d.kind === 'fragment').map((d) => d.name), 'fragment');
    case 'axes':
      return named(defs.filter((d) => d.kind === 'axis').map((d) => d.name), 'axis');
    case 'badge':
      return named([...vocab.icons, ...defs.filter((d) => d.kind === 'icon').map((d) => d.name)], 'icon');
    case 'replace':
    case 'remove':
      return named(inheritedParts(enclosing(text, caret), defs), 'part of what it extends');
    case 'facerect':
      return named(['front', 'right'], 'face');
  }
  return [];
}

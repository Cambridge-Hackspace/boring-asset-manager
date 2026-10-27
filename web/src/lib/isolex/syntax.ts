// Reading isolex sources (README §3.7) for the editor: tokens to color
// them, and an outline of what they define, for completion. This is a
// reader for an editor, forgiving of half-typed text; the compiler is the
// judge of what is valid.

export type TokenKind =
  | 'comment'
  | 'string'
  | 'number'
  | 'keyword' // #true, #false, #null, #inf, #-inf, #nan
  | 'node' // a node's name: box, icon, part
  | 'property' // a property's name, before =
  | 'punct' // { } ; =
  | 'ident' // any other bare word: an argument
  | 'space';

export interface Token {
  kind: TokenKind;
  start: number;
  end: number;
  /** For strings, the value between the quotes, escapes read. */
  value?: string;
}

const identChar = /[^\s\\/(){}\[\];="#,]/u;

/** Splits text into tokens covering it end to end. */
export function tokenize(text: string): Token[] {
  const out: Token[] = [];
  let i = 0;
  // Whether the next word is a node's name: at the start, and after
  // { } ; or a newline that is not continued.
  let nodeNext = true;
  const push = (kind: TokenKind, start: number, end: number, value?: string) => {
    out.push(value === undefined ? { kind, start, end } : { kind, start, end, value });
  };
  while (i < text.length) {
    const c = text[i]!;
    const start = i;
    if (c === '\n') {
      i++;
      push('space', start, i);
      nodeNext = true;
      continue;
    }
    if (/\s/u.test(c)) {
      while (i < text.length && text[i] !== '\n' && /\s/u.test(text[i]!)) i++;
      push('space', start, i);
      continue;
    }
    if (c === '/' && text[i + 1] === '/') {
      while (i < text.length && text[i] !== '\n') i++;
      push('comment', start, i);
      continue;
    }
    if (c === '/' && text[i + 1] === '*') {
      let depth = 0;
      while (i < text.length) {
        if (text[i] === '/' && text[i + 1] === '*') {
          depth++;
          i += 2;
        } else if (text[i] === '*' && text[i + 1] === '/') {
          depth--;
          i += 2;
          if (depth === 0) break;
        } else i++;
      }
      push('comment', start, i);
      continue;
    }
    if (c === '/' && text[i + 1] === '-') {
      // slashdash: what follows is commented out; colored as the rest
      i += 2;
      push('comment', start, i);
      continue;
    }
    if (c === '\\') {
      // a line continuation: the newline after it does not end the node
      i++;
      while (i < text.length && text[i] !== '\n' && /\s/u.test(text[i]!)) i++;
      if (text[i] === '\n') i++;
      push('space', start, i);
      continue;
    }
    if (c === '{' || c === '}' || c === ';') {
      i++;
      push('punct', start, i);
      nodeNext = true;
      continue;
    }
    if (c === '=') {
      i++;
      push('punct', start, i);
      continue;
    }
    if (c === '"' || (c === '#' && /^#+"/u.test(text.slice(i, i + 64)))) {
      i = readString(text, i);
      push('string', start, i, stringValue(text.slice(start, i)));
      nodeNext = false;
      continue;
    }
    if (c === '#') {
      i++;
      while (i < text.length && identChar.test(text[i]!)) i++;
      push('keyword', start, i);
      nodeNext = false;
      continue;
    }
    if (c === '(') {
      // a type annotation: (u8)
      while (i < text.length && text[i] !== ')' && text[i] !== '\n') i++;
      if (text[i] === ')') i++;
      push('ident', start, i);
      continue;
    }
    i++;
    while (i < text.length && identChar.test(text[i]!)) i++;
    const word = text.slice(start, i);
    if (nodeNext) {
      push('node', start, i);
      nodeNext = false;
    } else if (text[i] === '=') {
      push('property', start, i);
    } else if (/^[+-]?(\d|\.\d)/u.test(word)) {
      push('number', start, i);
    } else {
      push('ident', start, i);
    }
  }
  return out;
}

function readString(text: string, i: number): number {
  let hashes = 0;
  while (text[i] === '#') {
    hashes++;
    i++;
  }
  const raw = hashes > 0;
  const multi = text.startsWith('"""', i);
  const close = (multi ? '"""' : '"') + '#'.repeat(hashes);
  i += multi ? 3 : 1;
  while (i < text.length) {
    if (!raw && text[i] === '\\') {
      i += 2;
      continue;
    }
    if (text.startsWith(close, i)) return i + close.length;
    if (!multi && text[i] === '\n') return i; // unterminated: stop at the line
    i++;
  }
  return i;
}

function stringValue(s: string): string {
  const m = /^(#*)("""|")([\s\S]*?)(?:\2\1)?$/u.exec(s);
  if (!m) return s;
  return m[1] ? m[3]! : m[3]!.replace(/\\(.)/gu, '$1');
}

/** The definitions sources hold, for completion. */
export interface Definition {
  kind: 'icon' | 'base' | 'fragment' | 'axis' | 'font';
  name: string;
  /** What it extends, if it says. */
  extends?: string;
  /** Its parts, by name. */
  parts: string[];
  /** Its params, by name. */
  params: string[];
  /** Those of its params that are text: param name (text)"…". */
  textParams?: string[];
  /** For an axis, its variants. */
  variants: string[];
}

const defKinds = new Set(['icon', 'base', 'fragment', 'axis', 'font']);

/** Reads the top-level definitions of a source. */
export function outline(text: string): Definition[] {
  const defs: Definition[] = [];
  const tokens = tokenize(text).filter((t) => t.kind !== 'space' && t.kind !== 'comment');
  let depth = 0;
  let current: Definition | undefined;
  for (let k = 0; k < tokens.length; k++) {
    const t = tokens[k]!;
    if (t.kind === 'punct') {
      const p = text[t.start];
      if (p === '{') depth++;
      if (p === '}') {
        depth = Math.max(0, depth - 1);
        if (depth === 0) current = undefined;
      }
      continue;
    }
    if (t.kind !== 'node') continue;
    const name = text.slice(t.start, t.end);
    const arg = tokens[k + 1]?.kind === 'string' ? tokens[k + 1]!.value : undefined;
    if (depth === 0 && defKinds.has(name) && arg !== undefined) {
      current = { kind: name as Definition['kind'], name: arg, parts: [], params: [], variants: [] };
      defs.push(current);
      // extends= as a property on the same line
      for (let j = k + 2; j < tokens.length && tokens[j]!.kind !== 'punct'; j++) {
        if (tokens[j]!.kind === 'property' && text.slice(tokens[j]!.start, tokens[j]!.end) === 'extends' && tokens[j + 2]?.kind === 'string') {
          current.extends = tokens[j + 2]!.value;
        }
      }
      continue;
    }
    if (!current || depth !== 1) continue;
    if ((name === 'part' || name === 'replace') && arg !== undefined && !current.parts.includes(arg)) current.parts.push(arg);
    if (name === 'extends' && arg !== undefined) current.extends = arg;
    if (name === 'variant' && arg !== undefined) current.variants.push(arg);
    if (name === 'param' && tokens[k + 1]) {
      const p = tokens[k + 1]!;
      const param = p.value ?? text.slice(p.start, p.end);
      current.params.push(param);
      const type = tokens[k + 2];
      if (type?.kind === 'ident' && text.slice(type.start, type.end) === '(text)') (current.textParams ??= []).push(param);
    }
  }
  return defs;
}

/** The offset of a 1-based line and column, as the compiler counts them
    (columns in characters). */
export function offsetOf(text: string, line: number, col: number): number {
  let offset = 0;
  for (let l = 1; l < line; l++) {
    const nl = text.indexOf('\n', offset);
    if (nl < 0) return text.length;
    offset = nl + 1;
  }
  const end = text.indexOf('\n', offset);
  const lineText = text.slice(offset, end < 0 ? text.length : end);
  const chars = Array.from(lineText);
  return offset + chars.slice(0, Math.max(0, col - 1)).join('').length;
}

/** The range a diagnostic at an offset underlines: the token there, or
    one character where there is none. */
export function rangeAt(text: string, offset: number): [number, number] {
  if (offset >= text.length) return [Math.max(0, text.length - 1), text.length];
  const tokens = tokenize(text);
  const t = tokens.find((t) => t.start <= offset && offset < t.end && t.kind !== 'space');
  if (!t) return [offset, offset + 1];
  // never past the end of its line, for a token that spans lines
  const lineEnd = text.indexOf('\n', t.start);
  return [t.start, lineEnd < 0 ? t.end : Math.min(t.end, lineEnd)];
}

/** A diagnostic for a source, at a 1-based line and column. */
export interface Problem {
  line: number;
  col: number;
  message: string;
  severity: 'error' | 'warning';
}

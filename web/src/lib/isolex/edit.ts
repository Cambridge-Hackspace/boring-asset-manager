// Editing isolex sources as a code editor does: each operation is one
// replacement of a range of the text, and the selection after it, so the
// editor applies it as one step its undo can take back.

export const INDENT = '  ';

export interface Change {
  from: number;
  to: number;
  insert: string;
  /** The selection afterwards, in the new text. */
  selStart: number;
  selEnd: number;
}

function lineStart(text: string, at: number): number {
  return text.lastIndexOf('\n', at - 1) + 1;
}

function lineEnd(text: string, at: number): number {
  const nl = text.indexOf('\n', at);
  return nl < 0 ? text.length : nl;
}

/** The whole lines a selection touches: a selection ending at the start
    of a line does not take that line. */
function block(text: string, start: number, end: number): [number, number] {
  const from = lineStart(text, start);
  const last = end > start && text[end - 1] === '\n' ? end - 1 : end;
  return [from, lineEnd(text, Math.max(from, last))];
}

function eachLine(text: string, start: number, end: number, f: (line: string) => string): Change {
  const [from, to] = block(text, start, end);
  const lines = text.slice(from, to).split('\n');
  const changed = lines.map(f);
  const insert = changed.join('\n');
  // the selection keeps its place on its first line, and takes in the
  // rest of the block
  const firstShift = changed[0]!.length - lines[0]!.length;
  const selStart = Math.max(from, start + firstShift);
  const selEnd = start === end ? selStart : Math.max(selStart, end + insert.length - (to - from));
  return { from, to, insert, selStart, selEnd };
}

/** Tab: with no selection, an indent at the caret; with a selection
    inside one line, an indent in its place; with one across lines, every
    line it touches indented. */
export function indent(text: string, start: number, end: number): Change {
  if (start === end || !text.slice(start, end).includes('\n')) {
    const caret = start + INDENT.length;
    return { from: start, to: end, insert: INDENT, selStart: caret, selEnd: caret };
  }
  return eachLine(text, start, end, (line) => (line.trim() === '' ? line : INDENT + line));
}

/** Shift+Tab: every line touched loses one indent, as far as it has one. */
export function outdent(text: string, start: number, end: number): Change {
  return eachLine(text, start, end, (line) => {
    if (line.startsWith(INDENT)) return line.slice(INDENT.length);
    if (line.startsWith('\t') || line.startsWith(' ')) return line.slice(1);
    return line;
  });
}

/** Enter: a new line, indented as this one, and once more after an
    opening brace; between braces, the closing one goes to its own line. */
export function newline(text: string, start: number, end: number): Change {
  const ls = lineStart(text, start);
  const indent = /^[ \t]*/u.exec(text.slice(ls, start))![0];
  const before = text.slice(ls, start).trimEnd();
  const opens = before.endsWith('{');
  const after = text.slice(end, lineEnd(text, end)).trimStart();
  if (opens && after.startsWith('}')) {
    const insert = '\n' + indent + INDENT + '\n' + indent;
    const caret = start + 1 + indent.length + INDENT.length;
    return { from: start, to: end, insert, selStart: caret, selEnd: caret };
  }
  const insert = '\n' + indent + (opens ? INDENT : '');
  return { from: start, to: end, insert, selStart: start + insert.length, selEnd: start + insert.length };
}

/** Typing } on a line of nothing but indent takes one indent off, so the
    brace lines up with what it closes; otherwise null, to type as usual. */
export function closeBrace(text: string, start: number, end: number): Change | null {
  if (start !== end) return null;
  const ls = lineStart(text, start);
  const lead = text.slice(ls, start);
  if (lead === '' || lead.trim() !== '' || !lead.endsWith(INDENT)) return null;
  const insert = lead.slice(0, -INDENT.length) + '}';
  return { from: ls, to: start, insert, selStart: ls + insert.length, selEnd: ls + insert.length };
}

/** Ctrl+/: comments out every line touched with //, or, if each line
    that has anything on it already is, uncomments them. */
export function toggleComment(text: string, start: number, end: number): Change {
  const [from, to] = block(text, start, end);
  const lines = text.slice(from, to).split('\n');
  const filled = lines.filter((l) => l.trim() !== '');
  const commented = filled.length > 0 && filled.every((l) => l.trimStart().startsWith('//'));
  if (commented) {
    return eachLine(text, start, end, (l) => l.replace(/^([ \t]*)\/\/ ?/u, '$1'));
  }
  const at = Math.min(...filled.map((l) => /^[ \t]*/u.exec(l)![0].length), Infinity);
  const col = Number.isFinite(at) ? at : 0;
  return eachLine(text, start, end, (l) => (l.trim() === '' ? l : l.slice(0, col) + '// ' + l.slice(col)));
}

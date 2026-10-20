import { describe, expect, it } from 'vitest';
import { findCommands, score, type Command } from './commands';

const command = (name: string, keywords?: string): Command => ({ name, keywords, run: () => {} });

describe('finding commands', () => {
  const all = [
    command('Undo'),
    command('Redo'),
    command('Delete', 'remove erase'),
    command('Turn 90° left', 'rotate'),
    command('Turn the label right', 'rotate orientation'),
    command('Zoom to 100%'),
    command('Line tool'),
    command('Align left edges', 'line up'),
    command('Select all'),
  ];
  const names = (q: string) => findCommands(all, q).map((c) => c.name);

  it('finds by the start of each word, in any order', () => {
    expect(names('turn left')).toEqual(['Turn 90° left']);
    expect(names('left turn')).toEqual(['Turn 90° left']);
    expect(names('zo 100')).toEqual(['Zoom to 100%']);
  });

  it('finds by other words, below names', () => {
    expect(names('remove')).toEqual(['Delete']);
    expect(names('rotate')).toEqual(['Turn 90° left', 'Turn the label right']);
    // "line" starts the Line tool's name, and only one of Align's other words.
    expect(names('line')).toEqual(['Line tool', 'Align left edges']);
  });

  it('finds letters in order as a last resort, and nothing for what is not there', () => {
    expect(names('sall')).toEqual(['Select all']);
    expect(names('xyz')).toEqual([]);
    expect(score(command('Undo'), 'und')).toBeGreaterThan(score(command('Redo'), 'do') ?? Infinity);
  });

  it('lists everything, in order, for nothing typed', () => {
    expect(names('  ')).toEqual(all.map((c) => c.name));
  });
});

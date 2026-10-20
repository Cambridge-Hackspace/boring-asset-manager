import { describe, expect, it } from 'vitest';
import { History } from './history';

describe('History', () => {
  it('undoes and redoes', () => {
    const h = new History<{ n: number }>();
    h.record({ n: 1 });
    expect(h.undo({ n: 2 })).toEqual({ n: 1 });
    expect(h.redo({ n: 1 })).toEqual({ n: 2 });
  });

  it('discards a change cancelled before it was finished, which cannot then be redone', () => {
    const h = new History<{ n: number }>();
    h.record({ n: 1 });
    h.record({ n: 2 });
    expect(h.discard()).toEqual({ n: 2 });
    expect(h.canRedo).toBe(false);
    expect(h.undo({ n: 2 })).toEqual({ n: 1 });
    expect(h.discard()).toBeNull();
  });

  it('names each change, done and undone, and jumps to any point', () => {
    const h = new History<{ n: number }>();
    h.record({ n: 0 }, 'Add text');
    h.record({ n: 1 }, 'Move');
    h.record({ n: 2 }, 'Turn');
    // Now at 3.
    expect(h.changes).toEqual({ done: ['Add text', 'Move', 'Turn'], undone: [] });
    expect(h.jump({ n: 3 }, 1)).toEqual({ n: 1 });
    expect(h.changes).toEqual({ done: ['Add text'], undone: ['Move', 'Turn'] });
    expect(h.jump({ n: 1 }, 3)).toEqual({ n: 3 });
    expect(h.jump({ n: 3 }, 0)).toEqual({ n: 0 });
    expect(h.jump({ n: 0 }, 0)).toBeNull();
    // A new change drops what was undone.
    h.record({ n: 0 }, 'Add a code');
    expect(h.changes).toEqual({ done: ['Add a code'], undone: [] });
  });
});

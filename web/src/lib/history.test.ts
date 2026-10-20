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
});

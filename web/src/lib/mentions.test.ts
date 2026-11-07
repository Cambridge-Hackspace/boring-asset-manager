import { describe, expect, it } from 'vitest';
import { matching, mention, mentionQuery, remember, toDisplay, toStored } from './mentions';

const bob = '0b0b0b0b-0000-4000-8000-00000000000b';
const bo = '0c0c0c0c-0000-4000-8000-00000000000c';
const other = '0d0d0d0d-0000-4000-8000-00000000000d';

describe('mentions', () => {
  it('shows stored mentions by name, and stores them by ID again', () => {
    const known = new Map<string, string>();
    const stored = `Ask ${mention(bob)} or ${mention(bo)}, not ${mention(other)}.`;
    const shown = toDisplay(stored, [{ id: bob, name: 'Bob Smith' }, { id: bo, name: 'Bob' }], known);
    expect(shown).toBe(`Ask @Bob Smith or @Bob, not ${mention(other)}.`);
    expect(toStored(shown, known)).toBe(stored);
  });

  it('leaves a name that goes on, and two people of one name, as they are', () => {
    const known = new Map<string, string>();
    expect(remember(known, 'Bob', bob)).toBe(true);
    expect(remember(known, 'Bob', bo)).toBe(false);
    expect(toStored('@Bobby and @Bob, and mail@Bob', known)).toBe(`@Bobby and ${mention(bob)}, and mail@Bob`);
    expect(toDisplay(`${mention(bo)}`, [{ id: bo, name: 'Bob' }], known)).toBe(mention(bo));
  });

  it('finds the @ being typed, but not in an email address', () => {
    expect(mentionQuery('Hi @bo', 6)).toEqual({ start: 3, query: 'bo' });
    expect(mentionQuery('@', 1)).toEqual({ start: 0, query: '' });
    expect(mentionQuery('(@ada', 5)).toEqual({ start: 1, query: 'ada' });
    expect(mentionQuery('ada@example', 11)).toBeNull();
    expect(mentionQuery('@bob smith', 10)).toBeNull();
  });

  it('matches names by any word, those starting with it first', () => {
    const people = [
      { id: '1', name: 'Ada Bobbins' },
      { id: '2', name: 'Bob' },
      { id: '3', name: 'Cy' },
    ];
    expect(matching(people, 'bo').map((p) => p.name)).toEqual(['Bob', 'Ada Bobbins']);
    expect(matching(people, '').length).toBe(3);
  });
});

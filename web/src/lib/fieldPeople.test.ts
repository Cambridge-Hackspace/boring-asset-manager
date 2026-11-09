import { describe, expect, it } from 'vitest';
import { fieldPeople, fieldPeopleText, matchPeople, namesSomeone, personIds, personLabel, personValue } from './fieldPeople';

const asset = {
  field_values: { keeper: 'b', trained: 'c, a', ask: '' },
  people: { a: { id: 'a', name: 'Ada' }, b: { id: 'b', name: 'Bob', inactive: true }, c: { id: 'c', name: 'Cy', no_account: true } },
  hidden_people: { trained: 2, ask: 1 },
};

describe('person fields', () => {
  it('reads and writes user IDs, each once', () => {
    expect(personIds(' a,,b,a ')).toEqual(['a', 'b']);
    expect(personIds(undefined)).toEqual([]);
    expect(personValue(['b', 'a', 'b'])).toBe('b,a');
  });

  it('names people as the viewer sees them, marking those who left or have no account', () => {
    expect(personLabel({ id: 'a', name: 'Ada' })).toBe('Ada');
    expect(personLabel({ id: 'b', name: 'Bob', inactive: true })).toBe('Bob (no longer active)');
    expect(personLabel({ id: 'c', name: 'Cy', no_account: true })).toBe('Cy (no account)');
    expect(fieldPeople(asset, 'trained')).toEqual({ people: [asset.people.c, asset.people.a], hidden: 2 });
  });

  it('says someone is set without saying who', () => {
    expect(fieldPeopleText(asset, 'keeper')).toBe('Bob (no longer active)');
    expect(fieldPeopleText(asset, 'trained')).toBe('Cy (no account), Ada, 2 others');
    expect(fieldPeopleText(asset, 'ask')).toBe('Someone');
    expect(fieldPeopleText({ ...asset, hidden_people: { ask: 3 } }, 'ask')).toBe('3 people');
  });

  it('offers those whose names hold what is typed, those starting with it first', () => {
    const people = [{ name: 'Cyril' }, { name: 'Lucy' }, { name: 'Ada' }];
    expect(matchPeople(people, 'cy').map((p) => p.name)).toEqual(['Cyril', 'Lucy']);
    expect(matchPeople(people, ' ')).toEqual(people);
    expect(namesSomeone(people, 'ada ')).toBe(true);
    expect(namesSomeone(people, 'Ad')).toBe(false);
  });
});

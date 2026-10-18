import { describe, expect, it } from 'vitest';
import { actorName, choiceNames, displayName, personName } from './people';

describe('displayName', () => {
  it('prefers the name, falling back to the email', () => {
    expect(displayName({ name: 'Ada Lovelace', email: 'ada@hackspace.org' })).toBe('Ada Lovelace');
    expect(displayName({ name: '', email: 'ada@hackspace.org' })).toBe('ada@hackspace.org');
    expect(displayName({ email: 'ada@hackspace.org' })).toBe('ada@hackspace.org');
  });
});

describe('personName', () => {
  it('never falls back to an email', () => {
    expect(personName({ name: ' Ada Lovelace ' })).toBe('Ada Lovelace');
    expect(personName({ name: '' })).toBe('Unnamed user');
    expect(personName({})).toBe('Unnamed user');
  });
});

describe('actorName', () => {
  it('names API tokens as such, and people by name', () => {
    expect(actorName({ name: 'Dashboard', token: true })).toBe('API token “Dashboard”');
    expect(actorName({ name: 'Ada' })).toBe('Ada');
    expect(actorName({ name: 'Ada', via: 'Claude Desktop' })).toBe('Ada, via Claude Desktop');
    expect(actorName({ id: 'u1', name: 'Ada', via: 'MCP' }, 'u1')).toBe('you, via MCP');
    expect(actorName({ name: 'Front desk', token: true, via: 'Claude Code' })).toBe('API token “Front desk”, via Claude Code');
    expect(actorName({})).toBe('Unnamed user');
  });
});

describe('choiceNames', () => {
  it('adds the email only where two people share a name', () => {
    const names = choiceNames([
      { id: '1', name: 'Ada', email: 'ada@a.org' },
      { id: '2', name: 'Sam', email: 'sam@a.org' },
      { id: '3', name: 'Sam', email: 'sam@b.org' },
    ]);
    expect([...names.values()]).toEqual(['Ada', 'Sam (sam@a.org)', 'Sam (sam@b.org)']);
  });
});

describe('actorName, for the signed-in user', () => {
  it('says "you" for what they did, and names others as before', () => {
    expect(actorName({ id: 'u1', name: '' }, 'u1')).toBe('you');
    expect(actorName({ id: 'u2', name: '' }, 'u1')).toBe('Unnamed user');
    expect(actorName({ id: 'u1', name: 'Kiosk', token: true }, 'u1')).toBe('API token “Kiosk”');
  });
});

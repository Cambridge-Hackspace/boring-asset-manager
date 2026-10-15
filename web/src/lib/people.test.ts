import { describe, expect, it } from 'vitest';
import { actorName, displayName } from './people';

describe('displayName', () => {
  it('prefers the name, falling back to the email', () => {
    expect(displayName({ name: 'Ada Lovelace', email: 'ada@hackspace.org' })).toBe('Ada Lovelace');
    expect(displayName({ name: '', email: 'ada@hackspace.org' })).toBe('ada@hackspace.org');
    expect(displayName({ email: 'ada@hackspace.org' })).toBe('ada@hackspace.org');
  });
});

describe('actorName', () => {
  it('names API tokens as such', () => {
    expect(actorName({ name: 'Dashboard', email: '', token: true })).toBe('API token “Dashboard”');
    expect(actorName({ name: 'Ada', email: 'ada@hackspace.org' })).toBe('Ada');
  });
});

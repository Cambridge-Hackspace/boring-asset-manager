import { describe, expect, it } from 'vitest';
import { displayName } from './people';

describe('displayName', () => {
  it('prefers the name, falling back to the email', () => {
    expect(displayName({ name: 'Ada Lovelace', email: 'ada@hackspace.org' })).toBe('Ada Lovelace');
    expect(displayName({ name: '', email: 'ada@hackspace.org' })).toBe('ada@hackspace.org');
    expect(displayName({ email: 'ada@hackspace.org' })).toBe('ada@hackspace.org');
  });
});

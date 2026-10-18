import { describe, expect, it } from 'vitest';
import { describeHeld, describeKey, heldIn, setHeld } from './permissions';

describe('scoped permissions', () => {
  it('reads where a role holds a permission', () => {
    expect(heldIn(['assets.read', 'tools:assets.write'], 'assets.read')).toBe('all');
    expect(heldIn(['assets.read', 'tools:assets.write', 'default:assets.write'], 'assets.write')).toEqual(['tools', 'default']);
    expect(heldIn(['assets.read'], 'assets.delete')).toEqual([]);
  });

  it('replaces where a role holds a permission, leaving the rest', () => {
    const keys = ['assets.read', 'tools:assets.write', 'labels.print'] as const;
    expect(setHeld(keys, 'assets.write', 'all')).toEqual(['assets.read', 'labels.print', 'assets.write']);
    expect(setHeld(keys, 'assets.write', ['kitchen'])).toEqual(['assets.read', 'labels.print', 'kitchen:assets.write']);
    expect(setHeld(keys, 'assets.read', [])).toEqual(['tools:assets.write', 'labels.print']);
  });

  it('describes keys in words', () => {
    expect(describeKey('assets.read')).toBe('Read assets');
    expect(describeKey('tools:assets.write')).toBe('Edit assets in tools');
    expect(describeKey('mystery.key')).toBe('mystery.key');
  });
});

describe('describeHeld', () => {
  it('says what someone may do in words, and where when only in some scopes', () => {
    expect(describeHeld(['assets.borrow', 'public:assets.read', 'tools:assets.read', 'users.manage'])).toEqual([
      { label: 'Read assets', description: expect.stringContaining('See assets'), where: ['public', 'tools'] },
      { label: 'Borrow', description: expect.stringContaining('Check lendable'), where: [] },
      { label: 'Manage users', description: expect.stringContaining('Create and archive'), where: [] },
    ]);
  });
});

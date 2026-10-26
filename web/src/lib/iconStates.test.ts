import { describe, expect, it } from 'vitest';
import { drawnIn, plainIcon, stateNote } from './iconStates';

describe('icon states', () => {
  it('finds the icons drawn in a variant', () => {
    const names = ['drill', 'drill-down', 'saw-down', 'switch', 'switch-poe', 'switch-poe-down', 'state-badge-down'];
    // saw-down has no saw beside it; state-badge-down no state-badge
    expect(drawnIn(names, 'down')).toEqual(['drill-down', 'switch-poe-down']);
    expect(drawnIn(names, 'poe')).toEqual(['switch-poe']);
    expect(drawnIn(names, '')).toEqual([]);
  });

  it('says what an asset is drawn as', () => {
    const down = { name: 'drill-down', from: 'category', base: 'drill', state: 'out_of_service', variant: 'down' } as const;
    expect(plainIcon(down)).toBe('drill');
    expect(stateNote(down)).toBe('Drawn as drill-down: out of service.');
    expect(plainIcon({ name: 'drill', from: 'asset' })).toBe('drill');
    expect(stateNote({ name: 'drill', from: 'asset' })).toBe('');
  });
});

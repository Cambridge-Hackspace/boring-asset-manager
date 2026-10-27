import { describe, expect, it } from 'vitest';
import { drawnIn, paramsNote, plainIcon, stateNote, textsNote } from './iconStates';

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
    expect(stateNote(down)).toBe('Drawn as its down variant: out of service.');
    expect(plainIcon({ name: 'drill', from: 'asset' })).toBe('drill');
    expect(stateNote({ name: 'drill', from: 'asset' })).toBe('');
  });

  it('says what params an asset is drawn with', () => {
    const tall = { name: 'bam--server--ports_8--u_2', from: 'category', base: 'server', bound: 'bam--server--ports_8--u_2', params: { u: 2, ports: 8 } } as const;
    expect(plainIcon(tall)).toBe('server');
    expect(paramsNote(tall)).toBe('Drawn with ports 8, u 2, from its fields.');
    expect(paramsNote({ name: 'server', from: 'category' })).toBe('');
  });

  it('says what texts of its own an asset is written with', () => {
    expect(textsNote({ name: 'tag', from: 'category', texts: { note: 'Spare', label: 'Drill 3' } })).toBe('Written with label “Drill 3”, note “Spare”.');
    expect(textsNote({ name: 'tag', from: 'category' })).toBe('');
  });
});

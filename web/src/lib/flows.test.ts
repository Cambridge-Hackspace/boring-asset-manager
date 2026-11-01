import { afterEach, describe, expect, it, vi } from 'vitest';
import { fakeApi } from '../test/fetch';
import { formDefaults, inputType, missing, optionLabel, photosToAdd, runRequest, runsPath, scannedCode, type Prompt, type Run } from './flows';

afterEach(() => vi.unstubAllGlobals());

describe('scannedCode', () => {
  it('reads a code, or the code or asset in a scan link', () => {
    expect(scannedCode(' HS-100 ')).toBe('HS-100');
    expect(scannedCode('https://bam.example.org/scan/WOOD%201')).toBe('WOOD 1');
    expect(scannedCode('https://bam.example.org/assets/0b5e2c1a-1111-4222-8333-444455556666')).toBe('0b5e2c1a-1111-4222-8333-444455556666');
    expect(scannedCode('  ')).toBeNull();
  });
});

describe('forms', () => {
  const prompt: Prompt = {
    step: 'about',
    kind: 'form',
    questions: [
      { key: 'name', label: 'What is it?', type: 'text', required: true },
      { key: 'owner', label: 'Owner', default: 'Sam', required: true },
      { key: 'size', label: 'Size', type: 'choice', options: [{ value: 's', label: 'Small' }, { value: 'l' }], default: 's' },
      { key: 'until', label: 'Until', type: 'date', required: true },
      { key: 'photo', label: 'A photo', type: 'photo', default: 'ignored' },
    ],
  };
  it('starts from the defaults the server rendered, by key', () => {
    expect(formDefaults(prompt)).toEqual({ name: '', owner: 'Sam', size: 's', until: '', photo: '' });
  });
  it('names what is required and left empty', () => {
    expect(missing(prompt, { name: ' ', owner: '', until: '' })).toEqual(['What is it?', 'Owner', 'Until']);
    expect(missing(prompt, { name: 'Oak', owner: 'Sam', until: '2026-11-06' })).toEqual([]);
  });
  it('asks each type with its input', () => {
    expect(inputType('date')).toBe('date');
    expect(inputType('number')).toBe('number');
    expect(inputType('yes_no')).toBe('text');
    expect(inputType(undefined)).toBe('text');
  });
  it('shows a choice by its label, or its value', () => {
    expect(optionLabel({ value: 's', label: 'Small' })).toBe('Small');
    expect(optionLabel({ value: 'l' })).toBe('l');
  });
});

describe('photosToAdd', () => {
  const run = (photos: Run['photos']): Run => ({ id: 'r', flow_id: 'f', step: 2, values: {}, done: [], finished: false, photos });
  const file = new File(['x'], 'oak.jpg');
  it('adds a photo once the asset it is of exists, once', () => {
    expect(photosToAdd(run([{ question: 'photo' }]), { photo: file }, new Set())).toEqual([]);
    expect(photosToAdd(run([{ question: 'photo', asset_id: 'a1' }]), { photo: file }, new Set())).toEqual([{ key: 'photo:a1', assetID: 'a1', file }]);
    expect(photosToAdd(run([{ question: 'photo', asset_id: 'a1' }]), { photo: file }, new Set(['photo:a1']))).toEqual([]);
    // One not taken here is not added.
    expect(photosToAdd(run([{ question: 'other', asset_id: 'a1' }]), { photo: file }, new Set())).toEqual([]);
    expect(photosToAdd(run(undefined), { photo: file }, new Set())).toEqual([]);
  });
});

describe('runRequest', () => {
  it('keeps the run when an answer is refused, and says why', async () => {
    const view = { run: { id: 'r', step: 1 }, flow: 'leave', prompt: { step: 'about', kind: 'form' }, error: 'Owner is required' };
    fakeApi({ 'POST /flows/leave/runs/r': () => ({ status: 400, body: view }) });
    const got = await runRequest('POST', runsPath('leave', 'r'), { step: 'about' });
    expect(got.error).toBe('Owner is required');
    expect(got.prompt.step).toBe('about');
  });
  it('throws what is not a run', async () => {
    fakeApi({ 'POST /flows/gone/runs': () => ({ status: 404, body: { error: 'Flow not found' } }) });
    await expect(runRequest('POST', runsPath('gone'))).rejects.toThrow('Flow not found');
  });
});

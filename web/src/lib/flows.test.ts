import { afterEach, describe, expect, it, vi } from 'vitest';
import { fakeApi } from '../test/fetch';
import { formDefaults, inputType, missing, runRequest, runsPath, scannedCode, type Prompt } from './flows';

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
    ask_name: true,
    name_label: 'What is it?',
    fields: [
      { key: 'owner', label: 'Owner', type: 'text', default: 'Sam', required: true },
      { key: 'size', label: 'Size', type: 'select', options: { choices: [{ label: 'Small', value: 's' }], default: 's' }, required: false },
    ],
    questions: [{ key: 'until', label: 'Until', type: 'date', required: true }],
  };
  it('starts from the defaults the server rendered', () => {
    expect(formDefaults(prompt)).toEqual({ fields: { owner: 'Sam', size: 's' }, answers: { until: '' } });
  });
  it('names what is required and left empty', () => {
    expect(missing(prompt, ' ', { owner: '' }, { until: '' })).toEqual(['What is it?', 'Owner', 'Until']);
    expect(missing(prompt, 'Oak', { owner: 'Sam' }, { until: '2026-11-06' })).toEqual([]);
  });
  it('asks each type with its input', () => {
    expect(inputType('date')).toBe('date');
    expect(inputType('select')).toBe('text');
    expect(inputType(undefined)).toBe('text');
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

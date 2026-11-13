import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import WriteTag from './WriteTag.svelte';
import { fakeApi } from '../test/fetch';

type WithNFC = Window & { NDEFReader?: unknown };

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  delete (window as WithNFC).NDEFReader;
});

describe('WriteTag', () => {
  it('writes the scan link to a tag, then locks it once asked again', async () => {
    const write = vi.fn(async () => {});
    const makeReadOnly = vi.fn(async () => {});
    (window as WithNFC).NDEFReader = class {
      write = write;
      makeReadOnly = makeReadOnly;
    };
    fakeApi({ 'GET /info': () => ({ base_url: 'https://bam.example.org/' }) });
    render(WriteTag, { assetId: 'a1' });

    await fireEvent.click(screen.getByRole('button', { name: 'Write tag' }));
    expect((await screen.findByText(/^Written\./)).textContent).toContain('Written. The tag holds https://bam.example.org/scan/a1');
    expect(write).toHaveBeenCalledWith({ records: [{ recordType: 'url', data: 'https://bam.example.org/scan/a1' }] }, expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(screen.getByText(/Locking can’t be undone/)).toBeTruthy();

    await fireEvent.click(screen.getByRole('button', { name: 'Lock the tag' }));
    expect(makeReadOnly).not.toHaveBeenCalled();
    await fireEvent.click(screen.getByRole('button', { name: 'Lock the tag' }));
    expect((await screen.findByText(/^Locked\./)).textContent).toContain('https://bam.example.org/scan/a1');
    expect(makeReadOnly).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('button', { name: 'Lock the tag' })).toBeNull();
  });

  it('says why a tag wasn’t written, and tries again', async () => {
    const write = vi.fn().mockRejectedValueOnce(new DOMException('lost', 'NetworkError')).mockResolvedValueOnce(undefined);
    (window as WithNFC).NDEFReader = class {
      write = write;
      makeReadOnly = vi.fn();
    };
    fakeApi({ 'GET /info': () => ({ base_url: '' }) });
    render(WriteTag, { assetId: 'a1' });

    await fireEvent.click(screen.getByRole('button', { name: 'Write tag' }));
    expect((await screen.findByRole('alert')).textContent).toMatch(/moved away/);
    await fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect((await screen.findByText(/^Written\./)).textContent).toContain(`${window.location.origin}/scan/a1`);
  });

  it('shows the link to copy where the browser can’t write tags', async () => {
    const writeText = vi.fn(async () => {});
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } });
    fakeApi({ 'GET /info': () => ({ base_url: 'https://bam.example.org' }) });
    render(WriteTag, { assetId: 'a1' });

    await fireEvent.click(screen.getByRole('button', { name: 'Write tag' }));
    expect(await screen.findByText(/such as NFC Tools/)).toBeTruthy();
    const field = screen.getByRole<HTMLInputElement>('textbox', { name: 'The link to write' });
    await vi.waitFor(() => expect(field.value).toBe('https://bam.example.org/scan/a1'));
    await fireEvent.click(screen.getByRole('button', { name: 'Copy' }));
    expect(writeText).toHaveBeenCalledWith('https://bam.example.org/scan/a1');
    expect(screen.getByRole('button', { name: 'Copied' })).toBeTruthy();
  });
});

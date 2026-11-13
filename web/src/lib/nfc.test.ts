import { afterEach, describe, expect, it, vi } from 'vitest';
import { lockTag, nfcMessage, scanLink, webNFC, writeLink, type NDEFWriter } from './nfc';

describe('nfc', () => {
  afterEach(() => {
    delete (window as Window & { NDEFReader?: unknown }).NDEFReader;
  });

  it('finds Web NFC only where the browser has it', () => {
    expect(webNFC()).toBeNull();
    class Reader {}
    (window as Window & { NDEFReader?: unknown }).NDEFReader = Reader;
    expect(webNFC()).toBe(Reader);
  });

  it('links to the scan page at the server’s address, else this page’s', () => {
    expect(scanLink('https://bam.example.org/', 'a1', 'http://localhost:5173')).toBe('https://bam.example.org/scan/a1');
    expect(scanLink('', 'a1', 'http://localhost:5173')).toBe('http://localhost:5173/scan/a1');
    expect(scanLink(undefined, 'a1', 'https://here.test')).toBe('https://here.test/scan/a1');
  });

  it('writes the link as one URI record, and locks the tag', async () => {
    const reader: NDEFWriter = { write: vi.fn(async () => {}), makeReadOnly: vi.fn(async () => {}) };
    const signal = new AbortController().signal;
    await writeLink(reader, 'https://bam.test/scan/a1', signal);
    expect(reader.write).toHaveBeenCalledWith({ records: [{ recordType: 'url', data: 'https://bam.test/scan/a1' }] }, { signal });
    await lockTag(reader, signal);
    expect(reader.makeReadOnly).toHaveBeenCalledWith({ signal });
  });

  it('says what went wrong in words', () => {
    const named = (name: string) => Object.assign(new Error('raw'), { name });
    expect(nfcMessage(named('NotAllowedError'))).toMatch(/allowed to use NFC/);
    expect(nfcMessage(named('NotSupportedError'))).toMatch(/NTAG213/);
    expect(nfcMessage(named('AbortError'))).toBe('Stopped.');
    expect(nfcMessage(named('NetworkError'))).toMatch(/moved away/);
    expect(nfcMessage(new DOMException('x', 'NetworkError'))).toMatch(/moved away/);
    expect(nfcMessage(new Error('Something else'))).toBe('Something else');
    expect(nfcMessage('?')).toBe('The tag couldn’t be written.');
  });
});

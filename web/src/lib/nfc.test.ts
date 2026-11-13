import { afterEach, describe, expect, it, vi } from 'vitest';
import { lockTag, nfcAvailable, nfcMessage, normalizeUID, readTagSerial, scanLink, webNFC, writeLink, type NDEFWriter } from './nfc';

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

describe('NFC tag serial numbers', () => {
  it('writes them one way, however they come', () => {
    expect(normalizeUID('04:a2:3b:1c:5d:80:00')).toBe('04:A2:3B:1C:5D:80:00');
    expect(normalizeUID(' 04A23B1C5D8000 ')).toBe('04:A2:3B:1C:5D:80:00');
    expect(normalizeUID('04 a2 3b 1c 5d 80 00')).toBe('04:A2:3B:1C:5D:80:00');
    expect(normalizeUID('de-ad-be-ef')).toBe('DE:AD:BE:EF');
    expect(normalizeUID('4:a2:3b:1c')).toBe('04:A2:3B:1C');
  });

  it('refuses what no tag has', () => {
    expect(normalizeUID('')).toBe('');
    expect(normalizeUID('04A23B1C5')).toBe('');
    expect(normalizeUID('04:A2:3B')).toBe('');
    expect(normalizeUID('ZZ:A2:3B:1C')).toBe('');
    expect(normalizeUID('HS-106')).toBe('');
  });
});

type Listener = (e: { serialNumber: string }) => void;

class FakeReader {
  static last: FakeReader | undefined;
  listeners: Record<string, Listener[]> = {};
  signal: AbortSignal | undefined;
  constructor() {
    FakeReader.last = this;
  }
  scan(options?: { signal?: AbortSignal }) {
    this.signal = options?.signal;
    return Promise.resolve();
  }
  addEventListener(type: string, l: Listener) {
    (this.listeners[type] ??= []).push(l);
  }
  emit(type: string, serialNumber = '') {
    for (const l of this.listeners[type] ?? []) l({ serialNumber });
  }
}

describe('reading a tag', () => {
  const g = globalThis as { NDEFReader?: unknown };
  afterEach(() => {
    delete g.NDEFReader;
  });

  it('is offered only where Web NFC is', async () => {
    expect(nfcAvailable()).toBe(false);
    await expect(readTagSerial()).rejects.toThrow('type the serial number');
    g.NDEFReader = FakeReader;
    expect(nfcAvailable()).toBe(true);
  });

  it('gives the serial number of the tag held to it, and stops listening', async () => {
    g.NDEFReader = FakeReader;
    const read = readTagSerial();
    await Promise.resolve();
    FakeReader.last!.emit('reading', '04:a2:3b:1c:5d:80:00');
    await expect(read).resolves.toBe('04:A2:3B:1C:5D:80:00');
    expect(FakeReader.last!.signal?.aborted).toBe(true);
  });

  it('says when a tag cannot be read, or listening stops', async () => {
    g.NDEFReader = FakeReader;
    const bad = readTagSerial();
    FakeReader.last!.emit('readingerror');
    await expect(bad).rejects.toThrow('could not be read');

    const stop = new AbortController();
    const stopped = readTagSerial(stop.signal);
    stop.abort();
    await expect(stopped).rejects.toThrow('Stopped listening');
  });
});

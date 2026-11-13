import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { listenForTags, lockTag, nfcAvailable, nfcMessage, normalizeUID, ownTagPath, readTagSerial, scanLink, tagLinks, webNFC, writeLink, type NDEFWriter } from './nfc';

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

const text = (s: string) => new DataView(new TextEncoder().encode(s).buffer);

describe('what a tag holds', () => {
  it('reads links from URL and text records, and nothing else', () => {
    expect(
      tagLinks([
        { recordType: 'url', data: text('https://bam.test/scan/a1') },
        { recordType: 'text', data: text('HS-105'), encoding: 'utf-8' },
        { recordType: 'text', data: text(' https://bam.test/assets/a2 '), encoding: 'utf-8' },
        { recordType: 'mime', data: text('https://bam.test/scan/a3') },
        { recordType: 'empty' },
      ]),
    ).toEqual(['https://bam.test/scan/a1', 'https://bam.test/assets/a2']);
    expect(tagLinks(undefined)).toEqual([]);
  });

  it('opens only links to this instance, at its address or this page’s', () => {
    const bases = ['https://bam.example.org/bam/', 'http://localhost:5173'];
    expect(ownTagPath('https://bam.example.org/bam/scan/a1', bases)).toBe('/scan/a1');
    expect(ownTagPath('https://bam.example.org/bam/assets/a1/', bases)).toBe('/assets/a1');
    expect(ownTagPath('http://localhost:5173/scan/HS-105', bases)).toBe('/scan/HS-105');
    expect(ownTagPath('https://other.example.org/scan/a1', bases)).toBeNull();
    expect(ownTagPath('https://bam.example.org/scan/a1', bases)).toBeNull();
    expect(ownTagPath('https://bam.example.org/bam/settings', bases)).toBeNull();
    expect(ownTagPath('not a link', bases)).toBeNull();
  });
});

type Reading = { serialNumber?: string; message?: { records: { recordType: string; data?: DataView }[] } };

class ListeningReader {
  static all: ListeningReader[] = [];
  static refuse: Error | null = null;
  listeners: Record<string, ((e: Reading) => void)[]> = {};
  signal: AbortSignal | undefined;
  constructor() {
    ListeningReader.all.push(this);
  }
  scan(options?: { signal?: AbortSignal }) {
    this.signal = options?.signal;
    return ListeningReader.refuse ? Promise.reject(ListeningReader.refuse) : Promise.resolve();
  }
  addEventListener(type: string, l: (e: Reading) => void) {
    (this.listeners[type] ??= []).push(l);
  }
  emit(type: string, e: Reading = {}) {
    for (const l of this.listeners[type] ?? []) l(e);
  }
}

describe('listening for tags', () => {
  const g = globalThis as { NDEFReader?: unknown };
  let visibility: DocumentVisibilityState = 'visible';
  beforeEach(() => {
    ListeningReader.all = [];
    ListeningReader.refuse = null;
    visibility = 'visible';
    vi.spyOn(document, 'visibilityState', 'get').mockImplementation(() => visibility);
    g.NDEFReader = ListeningReader;
  });
  afterEach(() => {
    vi.restoreAllMocks();
    delete g.NDEFReader;
  });
  const show = (state: DocumentVisibilityState) => {
    visibility = state;
    document.dispatchEvent(new Event('visibilitychange'));
  };

  it('passes on each tag read, with its serial number and links', () => {
    const read = vi.fn();
    const l = listenForTags(read, vi.fn());
    ListeningReader.all[0]!.emit('reading', { serialNumber: '04:a2:3b:1c', message: { records: [{ recordType: 'url', data: text('https://bam.test/scan/a1') }] } });
    expect(read).toHaveBeenCalledWith({ serial: '04:A2:3B:1C', links: ['https://bam.test/scan/a1'] });
    l.stop();
  });

  it('stops while the page is hidden, starts again when shown, and stops for good', () => {
    const read = vi.fn();
    const l = listenForTags(read, vi.fn());
    const first = ListeningReader.all[0]!;
    show('hidden');
    expect(first.signal?.aborted).toBe(true);
    first.emit('reading', { serialNumber: '04:a2:3b:1c' });
    expect(read).not.toHaveBeenCalled();
    show('visible');
    expect(ListeningReader.all).toHaveLength(2);
    expect(ListeningReader.all[1]!.signal?.aborted).toBe(false);
    l.stop();
    expect(ListeningReader.all[1]!.signal?.aborted).toBe(true);
    show('hidden');
    show('visible');
    expect(ListeningReader.all).toHaveLength(2);
  });

  it('waits to start until the page is shown', () => {
    visibility = 'hidden';
    const l = listenForTags(vi.fn(), vi.fn());
    expect(ListeningReader.all).toHaveLength(0);
    show('visible');
    expect(ListeningReader.all).toHaveLength(1);
    l.stop();
  });

  it('says when a tag can’t be read, or listening is refused', async () => {
    const failed = vi.fn();
    const l = listenForTags(vi.fn(), failed);
    ListeningReader.all[0]!.emit('readingerror');
    expect(failed.mock.calls[0]![0].message).toMatch(/could not be read/);
    l.stop();

    ListeningReader.refuse = Object.assign(new Error('no'), { name: 'NotAllowedError' });
    const refused = listenForTags(vi.fn(), failed);
    await Promise.resolve();
    await Promise.resolve();
    expect(failed.mock.calls[1]![0].name).toBe('NotAllowedError');
    refused.stop();
  });

  it('is not offered where Web NFC is missing', () => {
    delete g.NDEFReader;
    expect(() => listenForTags(vi.fn(), vi.fn())).toThrow('cannot read NFC tags');
  });
});

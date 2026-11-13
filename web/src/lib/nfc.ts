// NFC tags (docs/scanning.md#nfc-tags), through Web NFC, which only Chrome on
// Android has: writing an asset's scan link to a tag, which phones read with
// no app as they read the link in a QR code, and reading a tag's serial number,
// which elsewhere is typed.

/** The part of Web NFC's NDEFReader used here. */
export interface NDEFWriter {
  write(message: { records: { recordType: string; data: string }[] }, options?: { signal?: AbortSignal; overwrite?: boolean }): Promise<void>;
  makeReadOnly(options?: { signal?: AbortSignal }): Promise<void>;
}

type NDEFReaderClass = new () => NDEFWriter;

/** Web NFC's NDEFReader, or null in a browser without it. */
export function webNFC(): NDEFReaderClass | null {
  if (typeof window === 'undefined') return null;
  return (window as Window & { NDEFReader?: NDEFReaderClass }).NDEFReader ?? null;
}

/**
 * The link a tag holds: the asset's scan page, `{{scan_url}}` on labels,
 * at the server's address, or this page's where none is set.
 */
export function scanLink(baseURL: string | undefined, assetId: string, origin: string): string {
  return (baseURL || origin).replace(/\/+$/, '') + '/scan/' + assetId;
}

/** Writes a link to the tag next held to the phone, as one URI record. */
export async function writeLink(reader: NDEFWriter, link: string, signal?: AbortSignal): Promise<void> {
  await reader.write({ records: [{ recordType: 'url', data: link }] }, { signal });
}

/** Locks the tag next held to the phone, for good. */
export async function lockTag(reader: NDEFWriter, signal?: AbortSignal): Promise<void> {
  await reader.makeReadOnly({ signal });
}

/** What went wrong with a tag, in words. */
export function nfcMessage(err: unknown): string {
  const name = err instanceof Error || (typeof err === 'object' && err !== null && 'name' in err) ? (err as { name: string }).name : '';
  switch (name) {
    case 'NotAllowedError':
      return 'This site isn’t allowed to use NFC. Allow it in the browser’s site settings, and turn NFC on in the phone’s settings.';
    case 'NotSupportedError':
      return 'This phone can’t write that tag. Use a blank NTAG213, NTAG215 or NTAG216 tag.';
    case 'AbortError':
      return 'Stopped.';
    case 'NetworkError':
      return 'The tag moved away before it was done. Hold it still against the phone and try again.';
    case 'InvalidStateError':
      return 'Another NFC request is still waiting. Wait for it, or reload the page.';
  }
  return err instanceof Error && err.message ? err.message : 'The tag couldn’t be written.';
}

/** The part of Web NFC's NDEFReader that reads serial numbers. */
interface Reader {
  scan(options?: { signal?: AbortSignal }): Promise<void>;
  addEventListener(type: 'reading', listener: (e: { serialNumber: string }) => void): void;
  addEventListener(type: 'readingerror', listener: () => void): void;
}

type ReaderConstructor = new () => Reader;

function readerClass(): ReaderConstructor | undefined {
  return (globalThis as { NDEFReader?: ReaderConstructor }).NDEFReader;
}

/** Whether this browser can read NFC tags. */
export function nfcAvailable(): boolean {
  return readerClass() !== undefined;
}

/**
 * An NFC tag's serial number as BAM! keeps it: upper-case hex bytes joined
 * by colons, from bytes run together or separated by colons, spaces or
 * dashes. It is '' for anything else, or a length no tag has (4, 7 or 10
 * bytes).
 */
export function normalizeUID(s: string): string {
  const t = s.trim();
  let bytes: string[];
  if (/[: -]/.test(t)) {
    bytes = t
      .split(/[: -]+/)
      .filter((b) => b !== '')
      .map((b) => (b.length === 1 ? '0' + b : b));
  } else {
    if (t.length % 2 !== 0) return '';
    bytes = t.match(/../g) ?? [];
  }
  if (![4, 7, 10].includes(bytes.length) || !bytes.every((b) => /^[0-9a-f]{2}$/i.test(b))) return '';
  return bytes.map((b) => b.toUpperCase()).join(':');
}

/**
 * Waits for a tag to be held to the phone and returns its serial number,
 * normalized. It rejects when the browser has no Web NFC, the person
 * refuses NFC, the tag cannot be read, or signal aborts.
 */
export async function readTagSerial(signal?: AbortSignal): Promise<string> {
  const Ctor = readerClass();
  if (!Ctor) throw new Error('This browser cannot read NFC tags: type the serial number.');
  const reader = new Ctor();
  const inner = new AbortController();
  const stop = () => inner.abort();
  signal?.addEventListener('abort', stop);
  try {
    return await new Promise<string>((resolve, reject) => {
      reader.addEventListener('reading', (e) => {
        const uid = normalizeUID(e.serialNumber ?? '');
        if (uid) resolve(uid);
        else reject(new Error('That tag reports no serial number BAM! can keep: type it instead.'));
      });
      reader.addEventListener('readingerror', () => reject(new Error('That tag could not be read: hold it still against the back of the phone.')));
      inner.signal.addEventListener('abort', () => reject(new Error('Stopped listening for a tag.')));
      reader.scan({ signal: inner.signal }).catch((err: unknown) => reject(err instanceof Error ? err : new Error(String(err))));
    });
  } finally {
    inner.abort();
    signal?.removeEventListener('abort', stop);
  }
}

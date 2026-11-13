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

/** An NDEF record as Web NFC reads it. */
export interface TagRecord {
  recordType: string;
  data?: DataView | ArrayBuffer | null;
  encoding?: string | null;
}

/** A tag held to the phone: its serial number, normalized ('' if none), and the links it holds. */
export interface TagRead {
  serial: string;
  links: string[];
}

/** The links among a tag's records: URL records, and text records that are links. */
export function tagLinks(records: readonly TagRecord[] | undefined): string[] {
  const links: string[] = [];
  for (const r of records ?? []) {
    if (!r.data || !['url', 'absolute-url', 'text'].includes(r.recordType)) continue;
    const bytes = r.data instanceof DataView ? new Uint8Array(r.data.buffer, r.data.byteOffset, r.data.byteLength) : new Uint8Array(r.data);
    let text: string;
    try {
      text = new TextDecoder(r.recordType === 'text' && r.encoding ? r.encoding : 'utf-8').decode(bytes).trim();
    } catch {
      continue;
    }
    if (/^https?:\/\//i.test(text)) links.push(text);
  }
  return links;
}

/**
 * Where a link read from a tag leads in this instance: the path of the scan
 * or asset page it names, when it is at one of bases (the server's address,
 * this page's). Null for a link elsewhere, such as another instance's.
 */
export function ownTagPath(link: string, bases: readonly string[]): string | null {
  let url: URL;
  try {
    url = new URL(link);
  } catch {
    return null;
  }
  for (const b of bases) {
    let base: URL;
    try {
      base = new URL(b);
    } catch {
      continue;
    }
    if (url.origin !== base.origin) continue;
    const prefix = base.pathname.replace(/\/+$/, '');
    if (!url.pathname.startsWith(prefix + '/')) continue;
    const m = /^\/(scan|assets)\/([^/]+)\/?$/.exec(url.pathname.slice(prefix.length));
    if (m) return `/${m[1]}/${m[2]}`;
  }
  return null;
}

/** Listening for tags, and how to stop. */
export interface TagListener {
  stop(): void;
}

type ListeningReader = Omit<Reader, 'addEventListener'> & {
  addEventListener(type: 'reading', listener: (e: { serialNumber?: string; message?: { records: TagRecord[] } }) => void): void;
  addEventListener(type: 'readingerror', listener: () => void): void;
};

/**
 * Listens for tags while doc is visible: it stops when the page is hidden
 * and starts again when it is shown, until stop. Each tag read goes to
 * onread; a tag that can't be read, or listening refused, to onerror.
 */
export function listenForTags(onread: (tag: TagRead) => void, onerror: (err: unknown) => void, doc: Document = document): TagListener {
  const Ctor = readerClass() as (new () => ListeningReader) | undefined;
  if (!Ctor) throw new Error('This browser cannot read NFC tags.');
  let running: AbortController | null = null;
  let stopped = false;

  const start = () => {
    if (stopped || running || doc.visibilityState === 'hidden') return;
    const ctl = new AbortController();
    running = ctl;
    const reader = new Ctor();
    reader.addEventListener('reading', (e) => {
      if (!ctl.signal.aborted) onread({ serial: normalizeUID(e.serialNumber ?? ''), links: tagLinks(e.message?.records) });
    });
    reader.addEventListener('readingerror', () => {
      if (!ctl.signal.aborted) onerror(new Error('That tag could not be read: hold it still against the back of the phone.'));
    });
    reader.scan({ signal: ctl.signal }).catch((err: unknown) => {
      if (ctl.signal.aborted) return;
      if (running === ctl) running = null;
      onerror(err);
    });
  };
  const pause = () => {
    running?.abort();
    running = null;
  };
  const onVisibility = () => (doc.visibilityState === 'hidden' ? pause() : start());

  doc.addEventListener('visibilitychange', onVisibility);
  start();
  return {
    stop() {
      stopped = true;
      pause();
      doc.removeEventListener('visibilitychange', onVisibility);
    },
  };
}

/** Whether this site may already use NFC, so listening needs no prompt. */
export async function nfcAllowed(): Promise<boolean> {
  try {
    const status = await navigator.permissions.query({ name: 'nfc' as PermissionName });
    return status.state === 'granted';
  } catch {
    return false;
  }
}

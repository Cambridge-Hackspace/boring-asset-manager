// NFC tags (docs/scanning.md#nfc-tags): writing an asset's scan link to a tag
// through Web NFC, which only Chrome on Android has. Phones read such a tag
// with no app, as they read the link in a QR code.

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

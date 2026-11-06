// A Bluetooth LE printer's connection address (docs/printing.md#bluetooth):
// its Bluetooth address, then, where they differ from the driver's, the
// service and characteristics as a query, such as
// 7C:2F:80:A1:B2:C3?service=fff0&write=fff2.

/** The parts of a ble connection's address. Empty means the driver's. */
export interface BLEAddress {
  mac: string;
  service: string;
  write: string;
  notify: string;
  /** Whether each write waits for the printer's acknowledgment: '' is the driver's say. */
  ack: string;
}

const keys = ['service', 'write', 'notify', 'ack'] as const;

/** Reads a ble connection's address into its parts. */
export function parseBLEAddress(address: string): BLEAddress {
  const at = address.indexOf('?');
  const mac = (at < 0 ? address : address.slice(0, at)).trim();
  const query = new URLSearchParams(at < 0 ? '' : address.slice(at + 1));
  const out: BLEAddress = { mac, service: '', write: '', notify: '', ack: '' };
  for (const k of keys) out[k] = query.get(k) ?? '';
  return out;
}

/** Writes a ble connection's address from its parts, leaving out those left to the driver. */
export function bleAddress(a: BLEAddress): string {
  const query = keys
    .map((k) => [k, a[k].trim()] as const)
    .filter(([, v]) => v !== '')
    .map(([k, v]) => `${k}=${encodeURIComponent(v)}`)
    .join('&');
  return query ? `${a.mac.trim()}?${query}` : a.mac.trim();
}

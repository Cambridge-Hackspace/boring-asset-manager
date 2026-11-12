// Printing from the browser, over its own Bluetooth
// (docs/printing.md#from-a-browser): the person picks the printer, the
// server renders the job and sends it over a WebSocket, and this writes it
// to the printer, passing back what the printer answers. The drivers stay
// on the server: a printer that holds a conversation, such as a Niimbot,
// holds it with the server, through here. Web Bluetooth is in Chrome and
// Edge, on Android and computers, and only on a secure page.

/** How the browser reaches a printer, as the server gives it. */
export interface BrowserProfile {
  service: string;
  write: string;
  notify?: string;
  /** Each write waits for the printer to acknowledge it. */
  ack?: boolean;
  /** The conversation the job is sent in, held with the server. */
  protocol?: string;
  /** The most bytes one write carries. */
  chunk?: number;
}

export interface BrowserPrintRequest {
  template_id: string;
  asset_ids: string[];
  copies?: number;
  mirror?: string;
}

export interface BrowserPrintResult {
  labels: number;
  warnings: string[];
  /** The printer as the browser names it. */
  device: string;
}

// The parts of Web Bluetooth used here, which TypeScript's DOM types lack.
interface Characteristic extends EventTarget {
  readonly value?: DataView | null;
  readonly properties: { readonly writeWithoutResponse: boolean };
  writeValueWithResponse(value: BufferSource): Promise<void>;
  writeValueWithoutResponse(value: BufferSource): Promise<void>;
  startNotifications(): Promise<Characteristic>;
}
interface GATTServer {
  getPrimaryService(uuid: string): Promise<{ getCharacteristic(uuid: string): Promise<Characteristic> }>;
  disconnect(): void;
}
interface Device {
  readonly name?: string;
  readonly gatt?: { connect(): Promise<GATTServer> };
}
export interface Bluetooth {
  requestDevice(options: { acceptAllDevices: boolean; optionalServices: string[] }): Promise<Device>;
}

/** The browser's Bluetooth, where it has Web Bluetooth. */
export function webBluetooth(): Bluetooth | null {
  return typeof navigator !== 'undefined' ? ((navigator as Navigator & { bluetooth?: Bluetooth }).bluetooth ?? null) : null;
}

/** How long the printer may take to acknowledge a write, as the server allows it. */
const ackTimeout = 60_000;

/** The least any link carries in one write. */
const minChunk = 20;

export interface BrowserPrintEnv {
  bluetooth?: Bluetooth | null;
  /** Opens the WebSocket to the server. */
  socket?: (url: string) => WebSocket;
  /** Says what is happening, for the person waiting. */
  onstatus?: (status: string) => void;
}

/**
 * Prints to a printer over the browser's Bluetooth. It must be called from
 * a click, which the browser needs to ask which printer to use.
 */
export async function printFromBrowser(printerId: string, profile: BrowserProfile, req: BrowserPrintRequest, env: BrowserPrintEnv = {}): Promise<BrowserPrintResult> {
  const bluetooth = env.bluetooth === undefined ? webBluetooth() : env.bluetooth;
  if (!bluetooth) throw new Error('This browser has no Web Bluetooth: print from Chrome or Edge, on a secure (https) page.');
  if (!profile.service || !profile.write) {
    throw new Error('The printer’s connection names no Bluetooth service or write characteristic, and its driver has none: set them under Printers.');
  }
  env.onstatus?.('Choose the printer…');
  let device: Device;
  try {
    device = await bluetooth.requestDevice({ acceptAllDevices: true, optionalServices: [profile.service] });
  } catch (err) {
    if (err instanceof DOMException && err.name === 'NotFoundError') throw new Error('No printer was chosen.');
    throw err;
  }
  const name = device.name || 'the printer';
  if (!device.gatt) throw new Error(`${name} cannot be connected to over Bluetooth LE.`);
  env.onstatus?.(`Connecting to ${name}…`);
  const server = await device.gatt.connect();
  try {
    const service = await server.getPrimaryService(profile.service);
    const write = await service.getCharacteristic(profile.write);
    const notify = profile.notify ? await service.getCharacteristic(profile.notify) : null;
    if (notify) await notify.startNotifications();
    env.onstatus?.(`Printing on ${name}…`);
    const result = await relay(printerId, profile, req, write, notify, env);
    return { ...result, device: name };
  } finally {
    server.disconnect();
  }
}

function socketURL(printerId: string): string {
  const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${scheme}//${location.host}/api/v1/printers/${encodeURIComponent(printerId)}/print/browser`;
}

/** Relays the job between the server and the printer, until the server says it has printed. */
function relay(
  printerId: string,
  profile: BrowserProfile,
  req: BrowserPrintRequest,
  write: Characteristic,
  notify: Characteristic | null,
  env: BrowserPrintEnv,
): Promise<{ labels: number; warnings: string[] }> {
  const ws = (env.socket ?? ((url) => new WebSocket(url)))(socketURL(printerId));
  ws.binaryType = 'arraybuffer';
  const chunk = Math.max(minChunk, profile.chunk ?? minChunk);
  const conversation = Boolean(profile.protocol);
  const send = (msg: object) => {
    if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(msg));
  };

  // The printer's notifications: in a conversation they go to the server;
  // otherwise each acknowledges a write.
  const acks: (() => void)[] = [];
  let unacked = 0;
  const onNotify = (e: Event) => {
    const v = (e.target as Characteristic).value;
    if (!v) return;
    if (conversation) {
      if (ws.readyState === WebSocket.OPEN) ws.send(new Uint8Array(v.buffer, v.byteOffset, v.byteLength).slice());
    } else if (acks.length) {
      acks.shift()!();
    } else {
      unacked++;
    }
  };
  notify?.addEventListener('characteristicvaluechanged', onNotify);
  const acknowledged = () =>
    new Promise<void>((resolve, reject) => {
      if (unacked > 0) {
        unacked--;
        resolve();
        return;
      }
      const timer = setTimeout(() => reject(new Error(`the printer did not acknowledge a write within ${ackTimeout / 1000} seconds`)), ackTimeout);
      acks.push(() => {
        clearTimeout(timer);
        resolve();
      });
    });

  const writeAll = async (data: Uint8Array) => {
    for (let at = 0; at < data.length; at += chunk) {
      const piece = data.slice(at, at + chunk);
      if (write.properties.writeWithoutResponse) await write.writeValueWithoutResponse(piece);
      else await write.writeValueWithResponse(piece);
      if (profile.ack && !conversation) await acknowledged();
    }
  };

  return new Promise((resolve, reject) => {
    let warnings: string[] = [];
    let failed = false;
    // Writes, one after another in the order the server sent them.
    let writing = Promise.resolve();
    const finish = () => notify?.removeEventListener('characteristicvaluechanged', onNotify);

    ws.onopen = () => send({ type: 'print', ...req });
    ws.onmessage = (e: MessageEvent) => {
      if (typeof e.data !== 'string') {
        const data = new Uint8Array(e.data as ArrayBuffer);
        writing = writing.then(async () => {
          if (failed) return;
          try {
            await writeAll(data);
          } catch (err) {
            failed = true;
            send({ type: 'failed', message: (err as { message?: string } | null)?.message ?? String(err) });
          }
        });
        return;
      }
      const m = JSON.parse(e.data) as { type: string; labels?: number; warnings?: string[]; message?: string };
      switch (m.type) {
        case 'job':
          warnings = m.warnings ?? [];
          break;
        case 'sent':
          void writing.then(() => {
            if (!failed) send({ type: 'done' });
          });
          break;
        case 'printed':
          finish();
          resolve({ labels: m.labels ?? 0, warnings });
          ws.close();
          break;
        case 'error':
          finish();
          reject(new Error(m.message ?? 'The job failed.'));
          ws.close();
          break;
      }
    };
    ws.onclose = () => {
      finish();
      reject(new Error('The connection to the server closed before the job was printed.'));
    };
  });
}

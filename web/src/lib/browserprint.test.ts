import { describe, expect, it, vi } from 'vitest';
import { printFromBrowser, type Bluetooth, type BrowserProfile } from './browserprint';

// A printer over Web Bluetooth: it keeps what is written to it, and may
// answer each write with a notification.
function fakePrinter(opts: { withoutResponse?: boolean; answer?: (b: Uint8Array) => Uint8Array | null; fail?: boolean } = {}) {
  const written: Uint8Array[] = [];
  const notify = Object.assign(new EventTarget(), { value: null as DataView | null, startNotifications: async () => notify });
  const record = async (v: BufferSource) => {
    if (opts.fail) throw new DOMException('GATT operation failed for unknown reason.', 'NetworkError');
    const b = new Uint8Array(v as ArrayBuffer);
    written.push(b);
    const a = opts.answer?.(b);
    if (a) {
      notify.value = new DataView(a.buffer);
      notify.dispatchEvent(new Event('characteristicvaluechanged'));
    }
  };
  const write = {
    properties: { writeWithoutResponse: opts.withoutResponse ?? true },
    writeValueWithoutResponse: vi.fn(record),
    writeValueWithResponse: vi.fn(record),
  };
  const disconnect = vi.fn();
  const requestDevice = vi.fn(async () => ({
    name: 'M110',
    gatt: {
      connect: async () => ({
        getPrimaryService: async () => ({ getCharacteristic: async (uuid: string) => (uuid === 'write' ? write : notify) }),
        disconnect,
      }),
    },
  }));
  const bluetooth = { requestDevice } as unknown as Bluetooth;
  return { written, write, disconnect, requestDevice, bluetooth, all: () => new Uint8Array(written.flatMap((b) => [...b])) };
}

// The server's side of the WebSocket: told each message the browser sends.
class FakeSocket {
  readyState = 1;
  binaryType = '';
  url = '';
  sent: (string | Uint8Array)[] = [];
  onopen?: () => void;
  onmessage?: (e: { data: unknown }) => void;
  onclose?: () => void;
  constructor(private server: (s: FakeSocket, msg: string | Uint8Array) => void) {
    queueMicrotask(() => this.onopen?.());
  }
  send(m: string | Uint8Array) {
    this.sent.push(m);
    this.server(this, m);
  }
  text(m: object) {
    this.onmessage?.({ data: JSON.stringify(m) });
  }
  binary(b: number[]) {
    this.onmessage?.({ data: new Uint8Array(b).buffer });
  }
  close() {
    this.readyState = 3;
  }
}

const request = { template_id: 't1', asset_ids: ['a1'], copies: 2 };
const stream: BrowserProfile = { service: 'svc', write: 'write', chunk: 20 };

describe('printing over the browser’s Bluetooth', () => {
  it('writes the job the server sends, in chunks the link takes, then says it is done', async () => {
    const printer = fakePrinter();
    const job = Array.from({ length: 50 }, (_, i) => i);
    let socket: FakeSocket | undefined;
    const opened: string[] = [];
    const statuses: string[] = [];
    const res = await printFromBrowser('p 1', stream, request, {
      bluetooth: printer.bluetooth,
      onstatus: (s) => statuses.push(s),
      socket: (url) => {
        opened.push(url);
        socket = new FakeSocket((s, msg) => {
          const m = JSON.parse(msg as string);
          if (m.type === 'print') {
            expect(m).toEqual({ type: 'print', ...request });
            s.text({ type: 'job', labels: 2, bytes: job.length, warnings: ['Tight fit'] });
            s.binary(job.slice(0, 30));
            s.binary(job.slice(30));
            s.text({ type: 'sent' });
          } else if (m.type === 'done') {
            s.text({ type: 'printed', labels: 2 });
          }
        });
        return socket as unknown as WebSocket;
      },
    });
    expect(res).toEqual({ labels: 2, warnings: ['Tight fit'], device: 'M110' });
    expect(opened).toEqual([`ws://${location.host}/api/v1/printers/p%201/print/browser`]);
    expect(printer.requestDevice).toHaveBeenCalledWith({ acceptAllDevices: true, optionalServices: ['svc'] });
    expect([...printer.all()]).toEqual(job);
    expect(printer.written.every((w) => w.length <= 20)).toBe(true);
    expect(printer.write.writeValueWithResponse).not.toHaveBeenCalled();
    expect(printer.disconnect).toHaveBeenCalled();
    expect(statuses).toEqual(['Choose the printer…', 'Connecting to M110…', 'Printing on M110…']);
    expect(socket!.sent.at(-1)).toBe(JSON.stringify({ type: 'done' }));
  });

  it('writes with responses where the printer takes no others, and waits for each acknowledgment it gives', async () => {
    const printer = fakePrinter({ withoutResponse: false, answer: () => new Uint8Array([0xaa]) });
    const res = await printFromBrowser('p1', { ...stream, notify: 'notify', ack: true }, request, {
      bluetooth: printer.bluetooth,
      socket: () =>
        new FakeSocket((s, msg) => {
          const m = JSON.parse(msg as string);
          if (m.type === 'print') {
            s.text({ type: 'job', labels: 1, bytes: 45 });
            s.binary(Array(45).fill(7));
            s.text({ type: 'sent' });
          } else if (m.type === 'done') s.text({ type: 'printed', labels: 1 });
        }) as unknown as WebSocket,
    });
    expect(res.labels).toBe(1);
    expect(printer.written.map((w) => w.length)).toEqual([20, 20, 5]);
    expect(printer.write.writeValueWithoutResponse).not.toHaveBeenCalled();
  });

  it('relays a conversation: what the server sends goes to the printer, and what it answers to the server', async () => {
    const printer = fakePrinter({ answer: (b) => new Uint8Array([b[0]! + 1]) });
    const answers: number[] = [];
    const res = await printFromBrowser('p1', { ...stream, notify: 'notify', protocol: 'niimbot' }, request, {
      bluetooth: printer.bluetooth,
      socket: () =>
        new FakeSocket((s, msg) => {
          if (typeof msg !== 'string') {
            answers.push(msg[0]!);
            if (answers.length < 3) s.binary([answers.length * 10]);
            else s.text({ type: 'printed', labels: 2 });
            return;
          }
          s.text({ type: 'job', labels: 2, bytes: 3 });
          s.binary([0]);
        }) as unknown as WebSocket,
    });
    expect(res.labels).toBe(2);
    expect(answers).toEqual([1, 11, 21]);
  });

  it('tells the server when it cannot write, and gives the server’s reason', async () => {
    const printer = fakePrinter({ fail: true });
    let failure: { type: string; message: string } | undefined;
    const job = printFromBrowser('p1', stream, request, {
      bluetooth: printer.bluetooth,
      socket: () =>
        new FakeSocket((s, msg) => {
          const m = JSON.parse(msg as string);
          if (m.type === 'print') {
            s.text({ type: 'job', labels: 1, bytes: 3 });
            s.binary([1, 2, 3]);
            s.text({ type: 'sent' });
          } else if (m.type === 'failed') {
            failure = m;
            s.text({ type: 'error', message: 'Could not deliver the job to Phone M110: the browser could not write to the printer' });
          }
        }) as unknown as WebSocket,
    });
    await expect(job).rejects.toThrow('the browser could not write to the printer');
    expect(failure?.message).toBe('GATT operation failed for unknown reason.');
    expect(printer.disconnect).toHaveBeenCalled();
  });

  it('says what stops it before anything is sent', async () => {
    const printer = fakePrinter();
    printer.requestDevice.mockRejectedValueOnce(new DOMException('User cancelled the requestDevice() chooser.', 'NotFoundError'));
    const socket = vi.fn();
    await expect(printFromBrowser('p1', stream, request, { bluetooth: printer.bluetooth, socket })).rejects.toThrow('No printer was chosen.');
    await expect(printFromBrowser('p1', { service: 'svc', write: '' }, request, { bluetooth: printer.bluetooth, socket })).rejects.toThrow(
      'names no Bluetooth service or write characteristic',
    );
    await expect(printFromBrowser('p1', stream, request, { bluetooth: null, socket })).rejects.toThrow('This browser has no Web Bluetooth');
    expect(socket).not.toHaveBeenCalled();
  });
});

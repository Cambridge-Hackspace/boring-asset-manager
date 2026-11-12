import { expect, test, unique } from './fixtures';

// Printing over the browser's own Bluetooth (docs/printing.md#from-a-browser),
// with a stand-in for Web Bluetooth that keeps what is written to the
// printer: the server renders the job and sends it over a WebSocket, and the
// page writes it.
test('a printer beside the browser is printed to over its Bluetooth, and only where it has it', async ({ signedIn: page, api }) => {
  const { template } = await api.call('POST', '/templates', {
    name: unique('Rollo 2x1'),
    units: 'in',
    width: 2,
    height: 1,
    dpi: 203,
    elements: [{ type: 'text', x: 0.1, y: 0.2, width: 1.8, height: 0.4, font_size: 10, content_binding: '{{asset.name}}' }],
  });
  const printerName = unique('Phone Rollo');
  await api.call('POST', '/printers', {
    name: printerName,
    driver: 'tspl',
    model: 'Rollo X1038',
    media: '2x1',
    connection: { type: 'browser', address: '?service=fff0&write=fff2' },
  });
  const { asset } = await api.call('POST', '/assets', { name: unique('Drill') });

  // A browser without Web Bluetooth is not offered it.
  await page.addInitScript(() => {
    if (sessionStorage.getItem('bluetooth') !== 'on') {
      Object.defineProperty(navigator, 'bluetooth', { configurable: true, value: undefined });
      return;
    }
    const written: number[] = [];
    const w = window as unknown as { written: number[]; requested: unknown };
    w.written = written;
    const write = {
      properties: { writeWithoutResponse: true },
      writeValueWithoutResponse: async (v: Uint8Array) => {
        written.push(...new Uint8Array(v.buffer, v.byteOffset, v.byteLength));
      },
      writeValueWithResponse: async () => {},
    };
    Object.defineProperty(navigator, 'bluetooth', {
      configurable: true,
      value: {
        requestDevice: async (options: unknown) => {
          w.requested = options;
          return {
            name: 'RL-1038',
            gatt: { connect: async () => ({ getPrimaryService: async () => ({ getCharacteristic: async () => write }), disconnect: () => {} }) },
          };
        },
      },
    });
  });
  await page.goto(`/assets/${asset.asset.id}`);
  await page.getByLabel('Template').selectOption(template.id);
  await expect(page.getByLabel('Printer').getByRole('option', { name: new RegExp(printerName) })).toHaveCount(0);

  // One that has it is, and prints through it.
  await page.evaluate(() => sessionStorage.setItem('bluetooth', 'on'));
  await page.reload();
  await page.getByLabel('Template').selectOption(template.id);
  await page.getByLabel('Printer').selectOption({ label: `${printerName}, over this browser’s Bluetooth` });
  await page.getByRole('button', { name: 'Print' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Printed' })).toHaveText(`Printed 1 label on ${printerName}.`);
  const written = await page.evaluate(() => (window as unknown as { written: number[] }).written);
  expect(Buffer.from(written).toString('latin1')).toMatch(/^SIZE 50\.8 mm,25\.4 mm\r?\n/);
  expect(await page.evaluate(() => (window as unknown as { requested: unknown }).requested)).toEqual({
    acceptAllDevices: true,
    optionalServices: ['0000fff0-0000-1000-8000-00805f9b34fb'],
  });
});

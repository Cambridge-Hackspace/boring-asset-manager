import { expect, test, unique } from './fixtures';

// Safari, and so BAM! installed on iOS, has no BarcodeDetector. There the
// scanner reads codes with the one BAM! packages, from the instance's own
// copy of its WebAssembly, and nothing is fetched from anywhere else
// (docs/scanning.md#looking-codes-up). The camera is a canvas showing the
// asset's label, as BAM! draws it.

for (const [type, name] of [
  ['qr', 'QR Code'],
  ['micro_qr', 'Micro QR'],
  ['code128', 'Code 128'],
] as const) {
  test(`with no BarcodeDetector, the camera reads a ${name} label`, async ({ signedIn: page, api }) => {
    const elsewhere: string[] = [];
    await page.context().route(
      (url) => url.hostname !== 'localhost' && url.hostname !== '127.0.0.1',
      (route) => {
        elsewhere.push(route.request().url());
        return route.abort();
      },
    );
    const refused: string[] = [];
    page.on('console', (msg) => {
      if (msg.text().includes('Content Security Policy')) refused.push(msg.text());
    });

    const code = unique('C').toUpperCase();
    const asset = unique('Drill');
    const { asset: created } = await api.call('POST', '/assets', { name: asset, generator_keys: [{ generator_type: type, assigned_code: code }] });
    const { template } = await api.call('POST', '/templates', {
      name: unique('Code only'),
      units: 'mm',
      width: 70,
      height: 40,
      dpi: 300,
      elements: [{ type: 'generator', generator_type: type, x: 5, y: 5, width: 60, height: 30 }],
    });
    const label = `/api/v1/assets/${created.asset.id}/label?template_id=${template.id}&format=png`;

    await page.addInitScript((src) => {
      delete (window as { BarcodeDetector?: unknown }).BarcodeDetector;
      navigator.mediaDevices.getUserMedia = async () => {
        const img = new Image();
        img.src = src;
        await img.decode();
        const canvas = document.createElement('canvas');
        canvas.width = img.naturalWidth + 80;
        canvas.height = img.naturalHeight + 80;
        const ctx = canvas.getContext('2d')!;
        const paint = () => {
          ctx.fillStyle = '#fff';
          ctx.fillRect(0, 0, canvas.width, canvas.height);
          ctx.drawImage(img, 40, 40);
        };
        paint();
        setInterval(paint, 100);
        return canvas.captureStream(10);
      };
    }, label);

    await page.goto('/scan');
    expect(await page.evaluate(() => 'BarcodeDetector' in window)).toBe(false);
    const wasm = page.waitForResponse(/\/assets\/zxing_reader-[^/]*\.wasm$/);
    await page.getByRole('button', { name: 'Scan with the camera' }).click();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText(asset);
    await expect(page).toHaveURL(new RegExp(`/scan/${code}$`));

    // The WebAssembly is the instance's own, served as WebAssembly.
    const res = await wasm;
    expect(new URL(res.url()).origin).toBe(new URL(page.url()).origin);
    expect(res.status()).toBe(200);
    expect(res.headers()['content-type']).toBe('application/wasm');
    expect(elsewhere).toEqual([]);
    expect(refused).toEqual([]);
  });
}

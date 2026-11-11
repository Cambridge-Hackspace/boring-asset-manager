import { expect, goTo, test, unique } from './fixtures';

test('lasers: add a laser, tune a material, and engrave an asset’s tag from its page', async ({ signedIn: page, api }) => {
  // A tag naming its asset, its outline cut.
  const { template } = await api.call('POST', '/templates', {
    name: unique('Tool tag'),
    units: 'mm',
    width: 50,
    height: 20,
    dpi: 300,
    elements: [
      { type: 'text', x: 4, y: 4, width: 42, height: 12, font_size: 14, content_binding: '{{asset.name}}' },
      { type: 'shape', shape: 'rectangle', x: 0, y: 0, width: 50, height: 20, corner_radius: 3, stroke: 'black', stroke_width: 0.2, fill: 'none', cut: true },
    ],
  });
  const { asset } = await api.call('POST', '/assets', { name: unique('Drill') });

  // A laser is a printer with a laser's model, and no roll.
  const name = unique('Workshop laser');
  await goTo(page, 'Printers');
  await page.getByRole('button', { name: 'New printer' }).click();
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page.getByLabel('Printer model').selectOption('test-laser|panel');
  await expect(page.getByText('A laser has no roll: each job names its material and workpiece. Its work area is 200 × 150 mm.')).toBeVisible();
  await expect(page.getByLabel('Roll loaded')).toHaveCount(0);
  await page.getByLabel('Address').fill(unique('laser').toLowerCase().replace(/[^a-z0-9]/g, '') + '.test');
  await page.getByRole('button', { name: 'Create printer' }).click();
  await expect(page.getByRole('article', { name })).toBeVisible();
  const { printers } = await api.call('GET', '/printers');
  const laser = (printers as { id: string; name: string }[]).find((p) => p.name === name)!;

  // The print queue never offers it.
  const spool = await api.call('GET', '/spool');
  expect((spool.printers as { id: string }[]).map((p) => p.id)).not.toContain(laser.id);

  // An admin tunes a preset for this laser.
  await goTo(page, 'Lasers');
  await page.getByLabel('Laser', { exact: true }).selectOption(laser.id);
  await page.getByRole('button', { name: 'Edit Birch plywood, 3 mm' }).click();
  await page.getByLabel('Power (%)').first().fill('35');
  await page.getByRole('button', { name: 'Save the material' }).click();
  await expect(page.getByText('Birch plywood, 3 mm (preset, tuned)')).toBeVisible();

  // From the asset's page, Engrave opens the Lasers page with its label.
  await page.goto(`/assets/${asset.asset.id}`);
  await page.getByRole('link', { name: 'Engrave' }).first().click();
  await expect(page).toHaveURL(/\/lasers\?asset=/);
  await page.getByLabel('Laser', { exact: true }).selectOption(laser.id);
  await page.getByLabel('Template', { exact: true }).selectOption(template.id);
  await expect(page.getByLabel('Material', { exact: true })).toHaveValue('birch-plywood-3mm');
  await page.getByLabel('Width', { exact: true }).fill('60');
  await page.getByLabel('Height', { exact: true }).fill('30');
  await page.getByLabel('From the left').fill('10');
  await page.getByLabel('From the top').fill('10');
  await page.getByRole('button', { name: 'Prepare', exact: true }).click();
  await expect(page.getByTestId('job-state')).toHaveText('Prepared, not started.');
  await expect(page.getByRole('img', { name: /Where the label goes/ })).toBeVisible();
  await expect(page.getByText('cutting 1 outline')).toBeVisible();
  await expect(page.getByLabel('Interlocks')).toContainText('Lid: fine');

  // Framed, then started by the person at the machine; the job runs to its end.
  await page.getByRole('button', { name: 'Frame' }).click();
  await expect(page.getByText('The machine is tracing the outline with its pointer.')).toBeVisible();
  await page.getByRole('button', { name: 'Start', exact: true }).click();
  await expect(page.getByTestId('job-state')).toHaveText('Done.', { timeout: 15_000 });
  await expect(page.getByText(/on Birch plywood, 3 mm at 35% and 150 mm\/s\. Done\./)).toBeVisible();

  // The asset's history says it was engraved, and on what.
  await page.goto(`/assets/${asset.asset.id}`);
  await expect(page.getByText(`Engraved its label on Birch plywood, 3 mm with ${name}`)).toBeVisible();
});

test('lasers: calibrate the camera, measure a workpiece, and confirm the plan over the photo', async ({ signedIn: page, api }) => {
  const { template } = await api.call('POST', '/templates', {
    name: unique('Bin tag'),
    units: 'mm',
    width: 50,
    height: 20,
    dpi: 300,
    elements: [{ type: 'text', x: 4, y: 4, width: 42, height: 12, font_size: 14, content_binding: '{{asset.name}}' }],
  });
  const name = unique('Camera laser');
  const { printer } = await api.call('POST', '/printers', {
    name,
    driver: 'test-laser',
    model: 'panel',
    connection: { type: 'tcp', address: unique('camera').toLowerCase().replace(/[^a-z0-9]/g, '') + '.test' },
  });

  await goTo(page, 'Lasers');
  await page.getByLabel('Laser', { exact: true }).selectOption(printer.id);
  await expect(page.getByTestId('calibration')).toHaveText(/^Not calibrated/);

  // The marks are a job like any other, started at the machine.
  await page.getByRole('button', { name: 'Prepare the marks' }).click();
  await expect(page.getByRole('heading', { name: 'Calibration marks' })).toBeVisible();
  await page.getByRole('button', { name: 'Start', exact: true }).click();
  await expect(page.getByTestId('job-state')).toHaveText('Done.', { timeout: 15_000 });

  // Found at the scrap's height.
  await page.locator('#calibration-z').fill('3');
  await page.getByRole('button', { name: 'Find the marks' }).click();
  await expect(page.getByTestId('calibration')).toContainText('for a surface 3 mm above the bed, from 9 marks');

  // The workpiece, wherever it lies, measured by the camera.
  await page.getByLabel('Template', { exact: true }).selectOption(template.id);
  await page.locator('#wp-z').fill('3');
  await page.getByRole('button', { name: 'Measure with the camera' }).click();
  await expect(page.getByText(/^Measured: /)).toBeVisible();
  await expect(page.getByLabel('Turned (°)')).not.toHaveValue('0');
  await page.getByLabel('Label on the workpiece', { exact: true }).selectOption('top_left');
  await page.getByLabel('In from its sides (mm)').fill('3');
  await page.getByRole('button', { name: 'Prepare', exact: true }).click();
  await expect(page.getByRole('img', { name: /over the camera’s photo/ })).toBeVisible();

  // Framing waits for the plan to be confirmed over the photo.
  const frame = page.getByRole('button', { name: 'Frame' });
  await expect(frame).toBeDisabled();
  await page.getByLabel('The plan sits on the workpiece in the photo').check();
  await frame.click();
  await expect(page.getByText('The machine is tracing the outline with its pointer.')).toBeVisible();

  // At another height, the camera asks to be calibrated again.
  await page.locator('#wp-z').fill('9');
  await page.getByRole('button', { name: 'Measure with the camera' }).click();
  await expect(page.getByText(/calibrate again at this height/)).toBeVisible();
});

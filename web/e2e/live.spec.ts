import { expect, test, unique } from './fixtures';

// Live events (docs/hooks.md): a device connects, is sent a hook triggered on
// an asset's page, and answers; the person who pressed sees the answer.
// The device here is a second page of the same signed-in user, which opens
// the WebSocket with its session.
test('a device connected live answers a hook', async ({ signedIn: page, api }) => {
  const key = unique('identify').toLowerCase();
  const { hook } = await api.call('POST', '/hooks', { key, label: 'Identify' });
  const cat = await api.call('POST', '/categories', { name: unique('Drawers') });
  await api.call('PUT', `/categories/${cat.category.id}/hooks`, { hooks: [{ hook_id: hook.id, cascades: true }] });
  const asset = await api.call('POST', '/assets', { name: unique('Resistors'), category_ids: [cat.category.id] });

  const device = await page.context().newPage();
  await device.goto('/help');
  await device.evaluate((k) => {
    const w = window as unknown as { seen: string[]; ready: boolean };
    w.seen = [];
    w.ready = false;
    const url = `${location.origin.replace(/^http/, 'ws')}/api/v1/events/live?kinds=asset.hook&hooks=${k}`;
    const ws = new WebSocket(url);
    ws.onmessage = (m) => {
      const msg = JSON.parse(m.data);
      if (msg.type === 'hello') w.ready = true;
      if (msg.type === 'event') {
        w.seen.push(msg.event.payload.hook.asset.name);
        ws.send(JSON.stringify({ type: 'reply', event_id: msg.event.id, ok: true, message: 'Lit drawer A3' }));
      }
    };
  }, key);
  await expect.poll(() => device.evaluate(() => (window as unknown as { ready: boolean }).ready)).toBe(true);

  await page.goto(`/assets/${asset.asset.asset.id}`);
  const hooks = page.getByRole('region', { name: 'Hooks' });
  await hooks.getByRole('button', { name: 'Identify' }).click();
  await expect(hooks.getByText(/: Lit drawer A3$/)).toBeVisible();
  expect(await device.evaluate(() => (window as unknown as { seen: string[] }).seen)).toEqual([asset.asset.asset.name]);
});

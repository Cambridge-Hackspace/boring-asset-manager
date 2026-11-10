import http from 'node:http';
import type { AddressInfo } from 'node:net';
import { expect, goTo, test, unique } from './fixtures';

// The assistant beside the page (docs/assistant.md), against a fake model:
// a chat completions server in this process, which the bam server beside it
// reaches on localhost. Nothing calls out.

interface ChatMessage {
  role: string;
  content?: string | null;
  tool_calls?: unknown[];
}

let server: http.Server;
let endpoint = '';

/** Answers as a model would: a lathe is added, through a tool; anything else is greeted. */
function reply(messages: ChatMessage[]) {
  const last = messages[messages.length - 1]!;
  if (last.role === 'tool') return { role: 'assistant', content: 'Added it.' };
  const said = String(last.content ?? '');
  const named = /named (\S+)/.exec(said);
  if (named) {
    return {
      role: 'assistant',
      content: null,
      tool_calls: [{ id: 'call_1', type: 'function', function: { name: 'create_asset', arguments: JSON.stringify({ assets: [{ name: named[1] }] }) } }],
    };
  }
  return { role: 'assistant', content: 'Hello from the fake model.' };
}

test.beforeAll(async () => {
  server = http.createServer((req, res) => {
    let body = '';
    req.on('data', (d) => (body += d));
    req.on('end', () => {
      const { messages } = JSON.parse(body || '{}') as { messages: ChatMessage[] };
      const message = reply(messages ?? []);
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify({ choices: [{ message, finish_reason: message.tool_calls ? 'tool_calls' : 'stop' }], usage: { total_tokens: 10 } }));
    });
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  endpoint = `http://127.0.0.1:${(server.address() as AddressInfo).port}/v1`;
});

test.afterAll(async () => {
  await new Promise<void>((resolve) => server.close(() => resolve()));
});

test('the assistant: turned on in Admin, then asked beside a page, confirming its change, and a guide', async ({ signedIn: page, api }) => {
  test.setTimeout(90_000);
  try {
    // Turned on in Admin, having said what goes where.
    await page.goto('/');
    await goTo(page, 'Assistant');
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Assistant');
    await page.getByLabel('Provider', { exact: true }).selectOption('openai');
    await page.getByLabel('Endpoint', { exact: true }).fill(endpoint);
    await page.getByLabel('Model', { exact: true }).fill('fake-model');
    await page.getByLabel('Name', { exact: true }).fill('Shop helper');
    await page.getByLabel('The assistant is on').check();
    await expect(page.getByRole('note')).toContainText(endpoint);
    await page.getByLabel('I have read this, and people here may know it').check();
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page.getByRole('status')).toHaveText('Saved.');

    // Beside an asset's page, it is told of the asset, and answers.
    const { asset } = await api.call('POST', '/assets', { name: unique('Band saw') });
    await page.goto(`/assets/${asset.asset.id}`);
    await page.getByRole('button', { name: 'Assistant', exact: true }).click();
    const panel = page.getByRole('complementary', { name: 'Shop helper' });
    await expect(panel).toBeVisible();
    await expect(panel.getByText(`On asset: ${asset.asset.name}`)).toBeVisible();
    await panel.getByLabel('Message to Shop helper').fill('Hello');
    await panel.getByRole('button', { name: 'Send' }).click();
    await expect(panel.getByText('Hello from the fake model.')).toBeVisible();

    // A change waits for the person, and is made once confirmed.
    const lathe = unique('Lathe');
    await panel.getByLabel('Message to Shop helper').fill(`Add a lathe named ${lathe}`);
    await panel.getByRole('button', { name: 'Send' }).click();
    await expect(panel.getByRole('heading', { name: /^Confirm: / })).toBeVisible();
    expect((await api.call('GET', `/assets?search=${encodeURIComponent(lathe)}`)).assets ?? []).toHaveLength(0);
    await panel.getByRole('button', { name: 'Confirm', exact: true }).click();
    await expect(panel.getByText('Added it.')).toBeVisible();
    expect((await api.call('GET', `/assets?search=${encodeURIComponent(lathe)}`)).assets).toHaveLength(1);

    // Listed on Your account.
    await page.goto('/account');
    await expect(page.getByRole('heading', { name: 'Conversations with the assistant' })).toBeVisible();

    // A guide opens at its address, and starts with its questions beside it.
    const slug = unique('workshop').toLowerCase();
    await api.call('POST', '/assistant/guides', {
      slug, name: 'Record the workshop', purpose: 'Walk through the equipment in your area.', enabled: true,
      questions: ['What is in your area?', 'What does each need to run?'],
    });
    await page.goto(`/g/${slug}`);
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Record the workshop');
    await page.getByRole('button', { name: 'Start' }).click();
    const guided = page.getByRole('complementary', { name: 'Shop helper' });
    await expect(guided.getByText('0 of 2 answered')).toBeVisible();
    await expect(guided.getByText('What does each need to run?')).toBeVisible();
    await page.goto(`/assistant/guides/${slug}/poster`);
    await expect(page.getByRole('img', { name: 'QR code opening Record the workshop' })).toBeVisible();
  } finally {
    // Off again, so the specs after this one see the app as they expect.
    const set = await api.call('GET', '/settings/assistant');
    await api.call('PUT', '/settings/assistant', { ...set, enabled: false });
  }
});

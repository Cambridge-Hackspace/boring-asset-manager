import AxeBuilder from '@axe-core/playwright';
import type { APIRequestContext } from '@playwright/test';
import { expect, test, unique } from './fixtures';

// An assistant made on Your account acts as its person over MCP, until
// revoked.

async function mcp(request: APIRequestContext, secret: string, method: string, params: object) {
  const res = await request.post('/api/v1/mcp', {
    headers: { Authorization: `Bearer ${secret}`, Accept: 'application/json, text/event-stream', 'Content-Type': 'application/json' },
    data: { jsonrpc: '2.0', id: 1, method, params },
  });
  return { status: res.status(), text: await res.text() };
}

test('make an assistant, connect it, and revoke it', async ({ signedIn: page, playwright }) => {
  const name = unique('Claude');
  await page.goto('/account');
  const section = page.getByRole('region', { name: 'Assistants' });
  await section.getByRole('button', { name: 'New assistant' }).click();
  await section.getByLabel('Name').fill(name);
  await section.getByRole('button', { name: 'Create assistant' }).click();

  const code = section.getByLabel('Claude Code', { exact: true });
  await expect(code).toContainText('claude mcp add --transport http bam');
  const secret = /Bearer (bam_[0-9a-f]+)/.exec((await code.textContent()) ?? '')![1]!;
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(results.violations.map((v) => v.id)).toEqual([]);
  await section.getByRole('button', { name: 'Done' }).click();
  await expect(section.getByText(name)).toBeVisible();

  // Over MCP it is the person signed in.
  const request = await playwright.request.newContext({ baseURL: new URL(page.url()).origin });
  const init = await mcp(request, secret, 'initialize', { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'e2e', version: '1' } });
  expect(init.status).toBe(200);
  const who = await mcp(request, secret, 'tools/call', { name: 'whoami', arguments: {} });
  expect(who.text).toContain('through their personal token');

  await section.getByRole('button', { name: 'Revoke' }).click();
  await section.getByRole('button', { name: 'Revoke' }).click();
  await expect(section.getByRole('status')).toContainText(`Revoked ${name}`);
  expect((await mcp(request, secret, 'tools/call', { name: 'whoami', arguments: {} })).status).toBe(401);
  await request.dispose();
});

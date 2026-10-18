// Shared fixtures for the end-to-end tests: a signed-in page, the API as the
// same user, and a fake raw TCP printer that records every job it receives.

import { test as base, expect, type APIRequestContext, type Page } from '@playwright/test';
import net from 'node:net';

export const ADMIN_EMAIL = process.env.E2E_EMAIL ?? 'ada@hackspace.org';
export const ADMIN_PASSWORD = process.env.E2E_PASSWORD ?? 'Pass1234!';

/** A short unique suffix, so tests sharing the database do not collide. */
export function unique(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.floor(Math.random() * 1e4).toString(36)}`;
}

export class FakePrinter {
  readonly jobs: Buffer[] = [];
  private server!: net.Server;
  port = 0;

  async start(): Promise<void> {
    this.server = net.createServer((sock) => {
      const parts: Buffer[] = [];
      sock.on('data', (d) => parts.push(d));
      sock.on('end', () => this.jobs.push(Buffer.concat(parts)));
    });
    await new Promise<void>((resolve) => this.server.listen(0, '127.0.0.1', resolve));
    this.port = (this.server.address() as net.AddressInfo).port;
  }

  /** Waits for the nth job (1-based) to arrive. */
  async job(n = 1): Promise<Buffer> {
    await expect.poll(() => this.jobs.length, { timeout: 5000 }).toBeGreaterThanOrEqual(n);
    return this.jobs[n - 1]!;
  }

  stop(): Promise<void> {
    return new Promise((resolve) => this.server.close(() => resolve()));
  }
}

export async function signIn(page: Page, email = ADMIN_EMAIL, password = ADMIN_PASSWORD): Promise<void> {
  await page.goto('/sign-in');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).not.toHaveURL(/\/sign-in/);
}

/** Calls the API as the signed-in user, failing the test on any error status. */
export class Api {
  constructor(private readonly request: APIRequestContext) {}

  async call<T = any>(method: string, path: string, body?: unknown): Promise<T> {
    const res = await this.request.fetch('/api/v1' + path, { method, data: body });
    if (!res.ok()) throw new Error(`${method} ${path}: ${res.status()} ${await res.text()}`);
    return (await res.json()) as T;
  }
}

export const test = base.extend<{ signedIn: Page; api: Api; printer: FakePrinter }>({
  signedIn: async ({ page }, use) => {
    await signIn(page);
    await use(page);
  },
  // The page's own cookies, so API calls act as the signed-in user.
  api: async ({ signedIn }, use) => {
    await use(new Api(signedIn.context().request));
  },
  printer: async ({}, use) => {
    const p = new FakePrinter();
    await p.start();
    await use(p);
    await p.stop();
  },
});

export { expect };

/** The navigation group each grouped page is listed under (src/routes.ts). */
const navGroupOf: Record<string, string> = {
  Categories: 'Catalog', Fields: 'Catalog', 'Link types': 'Catalog', Actions: 'Catalog',
  Templates: 'Labels', Images: 'Labels', Printers: 'Labels',
  Users: 'Admin', Roles: 'Admin', 'API tokens': 'Admin', Notifications: 'Admin',
};

/**
 * Clicks a link in the main navigation, opening its group's menu first
 * when the page is in one (a group of one page is a plain link).
 */
export async function goTo(page: Page, name: string): Promise<void> {
  const nav = page.getByRole('navigation', { name: 'Main' });
  const link = nav.getByRole('link', { name, exact: true });
  const group = navGroupOf[name];
  if (group) {
    const menu = nav.getByRole('button', { name: group, exact: true });
    await expect(link.or(menu)).toBeVisible();
    if (await menu.isVisible()) await menu.click();
  }
  await link.click();
}

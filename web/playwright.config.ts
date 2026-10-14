import { defineConfig } from '@playwright/test';

// End-to-end tests against a real `bam serve` (see tools/web/e2e.sh, which
// starts one on a fresh SQLite database and sets BAM_URL).
export default defineConfig({
  testDir: 'e2e',
  // One server and one database: run in order, one at a time.
  workers: 1,
  fullyParallel: false,
  retries: 0,
  timeout: 30_000,
  reporter: [['list']],
  use: {
    baseURL: process.env.BAM_URL ?? 'http://127.0.0.1:8080',
    viewport: { width: 1400, height: 1000 },
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  outputDir: 'test-results',
});

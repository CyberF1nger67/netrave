import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  timeout: 30_000,
  // All checks use the same server and shared database: parallel scenarios
  // create notifications for each other and break unread counter checks.
  workers: 1,
  fullyParallel: false,
  use: {
    baseURL: process.env['NETRAVE_URL'] ?? 'http://127.0.0.1:18080',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  reporter: [['list']],
});

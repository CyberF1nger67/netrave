import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import { fileURLToPath, URL } from 'node:url';
import { yamlLocales } from './vite-plugins/yaml-locales.ts';

export default defineConfig({
  // Tests import the same virtual:locales module as the application.
  plugins: [yamlLocales(), react()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./tests/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
    // Lazy route and page chunks are transformed on first use; a cold run on a slow machine needs
    // more than vitest's default 5 s per test. The per-test budget must exceed the waitFor budget.
    testTimeout: 30_000,
  },
});

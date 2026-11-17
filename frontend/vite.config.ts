import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { fileURLToPath, URL } from 'node:url';
import { yamlLocales } from './vite-plugins/yaml-locales.ts';

const backend = process.env['NETRAVE_BACKEND'] ?? 'http://127.0.0.1:8080';
const root = fileURLToPath(new URL('.', import.meta.url));

export default defineConfig({
  base: '/',
  // UI translations are compiled from locales/*.yaml into the virtual:locales module.
  plugins: [yamlLocales(), react(), tailwindcss()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  build: {
    // Baseline browser set: modern Chrome/Edge, Firefox and Safari 16+ (including iOS).
    // Older ones are not supported: CSS features and APIs used by the UI do not work there.
    target: ['chrome107', 'edge107', 'firefox104', 'safari16'],
    outDir: '../internal/web/static',
    emptyOutDir: true,
    sourcemap: false,
    rollupOptions: {
      input: { main: `${root}index.html`, console: `${root}console.html` },
      output: {
        // Vendor libraries in a separate chunk: they change rarely and cache better
        // than application code, which is updated with every release.
        manualChunks(id) {
          if (!id.includes('node_modules')) return undefined;
          if (/[\\/](react|react-dom|scheduler)[\\/]/.test(id)) return 'react';
          if (id.includes('react-router')) return 'router';
          if (id.includes('@tanstack') || id.includes('zustand')) return 'state';
          return undefined;
        },
      },
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': { target: backend, changeOrigin: false },
      '/ws': { target: backend, ws: true, changeOrigin: false },
      '/img': { target: backend, changeOrigin: false },
    },
  },
});

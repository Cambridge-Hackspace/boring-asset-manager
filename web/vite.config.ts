import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  plugins: [tailwindcss(), svelte()],
  build: {
    // Embedded into the Go binary by internal/webui.
    outDir: '../internal/webui/static/app',
    emptyOutDir: true,
    assetsDir: 'assets',
  },
  server: {
    // During development, `bam serve` answers the API.
    proxy: { '/api': 'http://localhost:8080' },
  },
  resolve: process.env.VITEST ? { conditions: ['browser'] } : undefined,
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts'],
  },
});

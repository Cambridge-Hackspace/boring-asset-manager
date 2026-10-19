import { searchForWorkspaceRoot } from 'vite';
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
    // The help topics live beside the Go code that embeds them too.
    fs: { allow: [searchForWorkspaceRoot(process.cwd()), '../internal/help/topics'] },
  },
  resolve: process.env.VITEST ? { conditions: ['browser'] } : undefined,
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts'],
    setupFiles: ['src/test/setup.ts'],
  },
});

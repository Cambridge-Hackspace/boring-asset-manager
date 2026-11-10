import { readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join, relative } from 'node:path';
import { brotliCompressSync, constants, gzipSync } from 'node:zlib';
import { build, searchForWorkspaceRoot, type Plugin } from 'vite';
import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import { buildName, shellFiles } from './src/lib/install/shell';

const outDir = '../internal/webui/static/app';

// precompress writes a Brotli and a gzip copy beside each built asset worth
// compressing, which internal/webui sends to browsers that take them: most
// of all the editor's live renderer, a 2.5 MB WebAssembly module that
// Brotli makes 0.7 MB.
function precompress(): Plugin {
  return {
    name: 'bam-precompress',
    apply: 'build',
    closeBundle() {
      const dir = join(outDir, 'assets');
      for (const name of readdirSync(dir)) {
        if (!/\.(js|css|wasm|svg|json)$/.test(name)) continue;
        const raw = readFileSync(join(dir, name));
        if (raw.length < 1024) continue;
        const br = brotliCompressSync(raw, { params: { [constants.BROTLI_PARAM_QUALITY]: 11, [constants.BROTLI_PARAM_SIZE_HINT]: raw.length } });
        const gz = gzipSync(raw, { level: 9 });
        if (br.length < raw.length) writeFileSync(join(dir, `${name}.br`), br);
        if (gz.length < raw.length) writeFileSync(join(dir, `${name}.gz`), gz);
      }
    },
  };
}

// files lists every file under dir, as paths relative to it.
function files(dir: string): string[] {
  return readdirSync(dir, { recursive: true, withFileTypes: true })
    .filter((e) => e.isFile())
    .map((e) => relative(dir, join(e.parentPath, e.name)));
}

// serviceWorker builds src/sw/worker.ts to sw.js at the root of the build,
// which internal/webui serves with the whole app as its scope, told the
// build's shell to keep and named after it (docs/installing.md#on-a-phone).
// It is built as a classic script, as every browser that installs web apps
// runs one, after the app, whose files it lists.
function serviceWorker(): Plugin {
  return {
    name: 'bam-service-worker',
    apply: 'build',
    async closeBundle() {
      const shell = shellFiles(files(outDir));
      const name = buildName(readFileSync(join(outDir, 'index.html'), 'utf8'), shell);
      await build({
        configFile: false,
        logLevel: 'warn',
        define: { __BAM_BUILD__: JSON.stringify(name), __BAM_SHELL__: JSON.stringify(shell) },
        build: {
          outDir,
          emptyOutDir: false,
          copyPublicDir: false,
          lib: { entry: 'src/sw/worker.ts', formats: ['iife'], name: 'bamServiceWorker', fileName: () => 'sw.js' },
        },
      });
    },
  };
}

export default defineConfig({
  plugins: [tailwindcss(), svelte(), precompress(), serviceWorker()],
  build: {
    // Embedded into the Go binary by internal/webui.
    outDir,
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
    // Time enough for a file's first test, which pays for the first
    // render, on a machine busy with the other suites.
    testTimeout: 15_000,
  },
});

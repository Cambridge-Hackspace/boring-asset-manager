// What the service worker (src/sw/worker.ts) keeps and serves, as plain
// functions it and the build share, and the tests read (docs/installing.md#on-a-phone).

/**
 * Paths the server answers itself, which the service worker leaves to the
 * network: the API, sign-in by identity providers and OAuth, the health
 * checks, the manifest and its icons, and the worker itself. Every other
 * page is the app's, and opens from its shell. internal/api's tests check
 * that every route the server has outside the API is here.
 */
export const serverPaths = [
  '/api/',
  '/auth/',
  '/oauth/',
  '/.well-known/',
  '/healthz',
  '/readyz',
  '/test-oidc/',
  '/manifest.webmanifest',
  '/app-icons/',
  '/sw.js',
];

/** How the service worker answers a request. */
export type Handling =
  /** Not at all: the browser asks the network, as with no worker. */
  | 'network'
  /** With the app's shell, index.html, kept since the release was installed. */
  | 'shell'
  /** From what is kept, else the network, keeping what it gets: hashed files never change. */
  | 'kept';

export function handling(method: string, url: URL, origin: string, mode: RequestMode): Handling {
  if (method !== 'GET' || url.origin !== origin) return 'network';
  const path = url.pathname;
  if (serverPaths.some((p) => path === p || (p.endsWith('/') ? path.startsWith(p) : path.startsWith(p + '/')))) return 'network';
  if (mode === 'navigate') return 'shell';
  if (path.startsWith('/assets/') || path === '/favicon.svg') return 'kept';
  return 'network';
}

/** The cache a release's shell is kept in; older releases' are removed once it takes over. */
export const cachePrefix = 'bam-shell-';

/**
 * The files of a build kept on installing it, as addresses: index.html as
 * the root, and every file but the editor's WebAssembly (fetched, and kept,
 * when the editor first opens) and the compressed copies the server sends
 * in their place.
 */
export function shellFiles(files: string[]): string[] {
  const kept = files
    .map((f) => f.replace(/\\/g, '/').replace(/^\/+/, ''))
    .filter((f) => f !== 'sw.js' && f !== 'index.html' && !/\.(br|gz|wasm|map)$/.test(f))
    .map((f) => '/' + f)
    .sort();
  return ['/', ...kept];
}

/**
 * Names a build by its shell: the files' names carry their contents'
 * hashes, and index.html names them, so any change to the app changes it,
 * and so the worker, which a browser then installs as an update.
 */
export function buildName(indexHTML: string, shell: string[]): string {
  // FNV-1a, twice over, for 16 hex digits: enough to tell releases apart.
  let a = 0x811c9dc5;
  let b = 0x01000193;
  for (const ch of indexHTML + '\n' + shell.join('\n')) {
    const c = ch.codePointAt(0)!;
    a = Math.imul(a ^ c, 0x01000193) >>> 0;
    b = Math.imul(b ^ c, 0x811c9dc5) >>> 0;
  }
  return a.toString(16).padStart(8, '0') + b.toString(16).padStart(8, '0');
}

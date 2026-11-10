// BAM!'s service worker (docs/installing.md#on-a-phone), built by
// vite.config.ts to sw.js at the root of the app, so that its scope is the
// whole app. It keeps the app's shell, so that an installed BAM! opens at
// once, with or without a signal, and fetches a new release's in the
// background, which takes over when the person chooses Reload to update.
//
// It leaves the API to the network. What the app keeps to use offline
// (#214) is the page's, not the worker's: the copy and the changes waiting
// to be sent in IndexedDB, and thumbnails in a cache of their own,
// bam-offline-thumbs, which activate leaves alone (src/lib/offline).

import { cachePrefix, handling } from '../lib/install/shell';

// Set by the build: the release's name, and the addresses of its shell.
declare const __BAM_BUILD__: string;
declare const __BAM_SHELL__: string[];

// The few parts of a service worker's scope this uses; the app is typed
// for a page, and a page's types and a worker's cannot be had together.
interface ExtendableEvent extends Event {
  waitUntil(p: Promise<unknown>): void;
}
interface FetchEvent extends ExtendableEvent {
  readonly request: Request;
  respondWith(r: Response | Promise<Response>): void;
}
interface MessageEvent extends ExtendableEvent {
  readonly data: unknown;
}
interface Scope {
  readonly location: Location;
  skipWaiting(): Promise<void>;
  readonly clients: { claim(): Promise<void> };
  addEventListener(type: 'install' | 'activate', listener: (e: ExtendableEvent) => void): void;
  addEventListener(type: 'fetch', listener: (e: FetchEvent) => void): void;
  addEventListener(type: 'message', listener: (e: MessageEvent) => void): void;
}

const worker = self as unknown as Scope;
const cacheName = cachePrefix + __BAM_BUILD__;

worker.addEventListener('install', (event) => {
  // Fetched afresh, past the browser's own cache. The worker then waits,
  // while an older one runs the app, until the app asks it to take over.
  event.waitUntil(caches.open(cacheName).then((cache) => cache.addAll(__BAM_SHELL__.map((url) => new Request(url, { cache: 'reload' })))));
});

worker.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      for (const key of await caches.keys()) {
        if (key.startsWith(cachePrefix) && key !== cacheName) await caches.delete(key);
      }
      // The pages open now are this release's: the first install's, or
      // those that asked for it.
      await worker.clients.claim();
    })(),
  );
});

worker.addEventListener('message', (event) => {
  if ((event.data as { type?: string } | null)?.type === 'skip-waiting') event.waitUntil(worker.skipWaiting());
});

worker.addEventListener('fetch', (event) => {
  const req = event.request;
  switch (handling(req.method, new URL(req.url), worker.location.origin, req.mode)) {
    case 'shell':
      event.respondWith(shell(req));
      break;
    case 'kept':
      event.respondWith(kept(req));
      break;
    case 'network':
      // Not answered here: the browser fetches it as if there were no worker.
      break;
  }
});

// The app's shell, for every page of the app: kept, so it opens at once.
// Should it not be, as when the cache was cleared, the network's; and with
// no network either, a page that says so rather than the browser's error.
async function shell(req: Request): Promise<Response> {
  const cache = await caches.open(cacheName);
  const page = await cache.match('/');
  if (page) return page;
  try {
    return await fetch(req);
  } catch {
    return offline();
  }
}

async function kept(req: Request): Promise<Response> {
  const cache = await caches.open(cacheName);
  const hit = await cache.match(req);
  if (hit) return hit;
  const res = await fetch(req);
  if (res.ok && res.type === 'basic') await cache.put(req, res.clone());
  return res;
}

function offline(): Response {
  const page = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark"><title>Offline · BAM!</title>
<style>body{font:1rem/1.5 system-ui,sans-serif;max-width:28rem;margin:3rem auto;padding:0 1rem}
a{display:inline-flex;align-items:center;min-height:2.75rem}</style></head>
<body><main><h1>You're offline</h1><p>BAM! cannot reach its server from here. It opens again when there is a connection.</p>
<p><a href="">Try again</a></p></main></body></html>`;
  return new Response(page, {
    status: 503,
    headers: {
      'Content-Type': 'text/html; charset=utf-8',
      'Content-Security-Policy': "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'",
      'Cache-Control': 'no-store',
    },
  });
}

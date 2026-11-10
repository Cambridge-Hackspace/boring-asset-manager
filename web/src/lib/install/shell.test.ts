import { describe, expect, it } from 'vitest';
import { buildName, handling, serverPaths, shellFiles } from './shell';

const origin = 'https://bam.example.org';
const at = (path: string) => new URL(path, origin);

describe('the service worker', () => {
  it('opens every page of the app from its shell', () => {
    for (const path of ['/', '/scan/HS-100', '/assets/123?tab=history', '/help/install', '/shopping']) {
      expect(handling('GET', at(path), origin, 'navigate'), path).toBe('shell');
    }
  });

  it('leaves what the server answers itself to the network', () => {
    for (const path of [
      '/api/v1/auth/me',
      '/api/v1/assets/export.pdf',
      '/oauth/authorize',
      '/auth/saml/x/metadata',
      '/.well-known/oauth-authorization-server',
      '/healthz',
      '/readyz',
      '/manifest.webmanifest',
      '/app-icons/icon-192.png',
      '/sw.js',
    ]) {
      expect(handling('GET', at(path), origin, 'navigate'), path).toBe('network');
      expect(handling('GET', at(path), origin, 'cors'), path).toBe('network');
    }
    // A page whose name only starts as theirs does is the app's.
    expect(handling('GET', at('/healthzone'), origin, 'navigate')).toBe('shell');
  });

  it("keeps the build's files, and nothing sent or from elsewhere", () => {
    expect(handling('GET', at('/assets/index-85J0ClI_.js'), origin, 'cors')).toBe('kept');
    expect(handling('GET', at('/assets/bamlive-1.wasm'), origin, 'cors')).toBe('kept');
    expect(handling('GET', at('/favicon.svg'), origin, 'no-cors')).toBe('kept');
    expect(handling('POST', at('/assets/x.js'), origin, 'cors')).toBe('network');
    expect(handling('POST', at('/scan/HS-100'), origin, 'navigate')).toBe('network');
    expect(handling('GET', new URL('https://elsewhere.example/assets/x.js'), origin, 'cors')).toBe('network');
    expect(handling('GET', at('/robots.txt'), origin, 'no-cors')).toBe('network');
  });

  it("keeps a build's shell: the root for index.html, without the editor's WebAssembly or compressed copies", () => {
    expect(
      shellFiles([
        'index.html',
        'favicon.svg',
        'sw.js',
        'assets\\index-1.js',
        'assets/index-1.js.br',
        'assets/index-1.js.gz',
        'assets/index-2.css',
        'assets/bamlive-3.wasm',
        'assets/font-4.woff2',
      ]),
    ).toEqual(['/', '/assets/font-4.woff2', '/assets/index-1.js', '/assets/index-2.css', '/favicon.svg']);
  });

  it('names a build after its shell, so that a new release is a new worker', () => {
    const a = buildName('<script src="/assets/index-1.js">', ['/', '/assets/index-1.js']);
    expect(a).toMatch(/^[0-9a-f]{16}$/);
    expect(buildName('<script src="/assets/index-1.js">', ['/', '/assets/index-1.js'])).toBe(a);
    expect(buildName('<script src="/assets/index-2.js">', ['/', '/assets/index-2.js'])).not.toBe(a);
  });

  it("names the server's paths from the root", () => {
    for (const p of serverPaths) expect(p.startsWith('/'), p).toBe(true);
  });
});

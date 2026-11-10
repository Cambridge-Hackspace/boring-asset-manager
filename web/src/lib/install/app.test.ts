import { afterEach, describe, expect, it, vi } from 'vitest';
import { applyThemeColor, applyUpdate, isIOS, isOffline, isStandalone, watchUpdates, type Container, type Registration } from './app.svelte';

afterEach(() => {
  document.head.innerHTML = '';
});

describe('installing', () => {
  it('knows an iPhone, and an iPad, which says it is a Mac, from a Mac', () => {
    const iphone = 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1';
    const mac = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15';
    const android = 'Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Mobile Safari/537.36';
    expect(isIOS({ userAgent: iphone, maxTouchPoints: 5 })).toBe(true);
    expect(isIOS({ userAgent: mac, maxTouchPoints: 5 })).toBe(true);
    expect(isIOS({ userAgent: mac, maxTouchPoints: 0 })).toBe(false);
    expect(isIOS({ userAgent: android, maxTouchPoints: 5 })).toBe(false);
  });

  it('knows when it runs installed, by display mode or, on iOS, navigator.standalone', () => {
    const win = (standalone: boolean, ios?: boolean) => ({ matchMedia: () => ({ matches: standalone }), navigator: { standalone: ios } }) as unknown as Window;
    expect(isStandalone(win(true))).toBe(true);
    expect(isStandalone(win(false, true))).toBe(true);
    expect(isStandalone(win(false))).toBe(false);
  });

  it('tells no connection from an answer', () => {
    expect(isOffline(new TypeError('Failed to fetch'), { onLine: true })).toBe(true);
    expect(isOffline(new Error('500 Internal Server Error'), { onLine: false })).toBe(true);
    expect(isOffline(new Error('500 Internal Server Error'), { onLine: true })).toBe(false);
  });

  it("colors the bar above the page in the instance's colors, or the one the person chose", () => {
    document.head.innerHTML =
      '<meta name="theme-color" content="#faf7f0" media="(prefers-color-scheme: light)"><meta name="theme-color" content="#0b0b0a" media="(prefers-color-scheme: dark)">';
    const colors = () => [...document.head.querySelectorAll('meta')].map((m) => m.content);
    const settings = { theme_color: '#d41920', theme_color_dark: '#141413' };
    applyThemeColor(settings, 'system');
    expect(colors()).toEqual(['#d41920', '#141413']);
    applyThemeColor(settings, 'dark');
    expect(colors()).toEqual(['#141413', '#141413']);
    applyThemeColor(settings, 'light');
    expect(colors()).toEqual(['#d41920', '#d41920']);
  });
});

// A registration, its container and a worker, standing in for the browser's.
function fakes(controlled: boolean) {
  const reg = Object.assign(new EventTarget(), { waiting: null, installing: null, update: vi.fn() }) as unknown as Registration & {
    waiting: ServiceWorker | null;
    installing: ServiceWorker | null;
  };
  const container = Object.assign(new EventTarget(), { controller: controlled ? ({} as ServiceWorker) : null }) as Container;
  const worker = Object.assign(new EventTarget(), { state: 'installing', postMessage: vi.fn() }) as unknown as ServiceWorker & { state: string };
  return { reg, container, worker };
}

describe('updates', () => {
  it('offers a new release once its worker is installed and waiting', () => {
    const { reg, container, worker } = fakes(true);
    const ready = vi.fn();
    watchUpdates(reg, container, ready);
    reg.installing = worker;
    reg.dispatchEvent(new Event('updatefound'));
    expect(ready).not.toHaveBeenCalled();
    worker.state = 'installed';
    worker.dispatchEvent(new Event('statechange'));
    expect(ready).toHaveBeenCalledWith(worker);
  });

  it('offers one already waiting when the page opens', () => {
    const { reg, container, worker } = fakes(true);
    reg.waiting = worker;
    const ready = vi.fn();
    watchUpdates(reg, container, ready);
    expect(ready).toHaveBeenCalledWith(worker);
  });

  it('offers nothing on the first install, when no worker ran the page before', () => {
    const { reg, container, worker } = fakes(false);
    const ready = vi.fn();
    watchUpdates(reg, container, ready);
    reg.installing = worker;
    reg.dispatchEvent(new Event('updatefound'));
    worker.state = 'installed';
    worker.dispatchEvent(new Event('statechange'));
    expect(ready).not.toHaveBeenCalled();
  });

  it('reloads once the waiting release takes over, as asked', () => {
    const { container, worker } = fakes(true);
    const reload = vi.fn();
    applyUpdate(worker, container, reload);
    expect(worker.postMessage).toHaveBeenCalledWith({ type: 'skip-waiting' });
    expect(reload).not.toHaveBeenCalled();
    container.dispatchEvent(new Event('controllerchange'));
    container.dispatchEvent(new Event('controllerchange'));
    expect(reload).toHaveBeenCalledTimes(1);
  });
});

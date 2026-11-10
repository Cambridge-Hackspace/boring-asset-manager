// BAM! as an installed app (docs/installing.md#on-a-phone): whether it can
// be installed here, and how; the service worker that keeps its shell, and
// a new release waiting for Reload to update; whether the browser is
// online; and the instance's colors for the bar above the page.

import { request } from '../api';
import type { Theme } from '../theme.svelte';

/** The installed app's settings, as GET /settings/app gives them. */
export interface AppSettings {
  /** Empty follows the instance's name. */
  name: string;
  /** Empty follows the name. */
  short_name: string;
  theme_color: string;
  theme_color_dark: string;
  background_color: string;
  /** A library image, or empty for BAM!'s own icon. */
  icon_image_id: string;
  instance_name: string;
}

/** Chrome's beforeinstallprompt event, which no standard types yet. */
export interface InstallPromptEvent extends Event {
  prompt(): Promise<void>;
  readonly userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>;
}

/** Whether the page runs as an installed app, in a window of its own. */
export function isStandalone(win: Window = window): boolean {
  return win.matchMedia?.('(display-mode: standalone)').matches || (win.navigator as { standalone?: boolean }).standalone === true;
}

/**
 * Whether this is an iPhone or iPad, where no browser offers to install a
 * web app and Share › Add to Home Screen does it. An iPad says it is a Mac,
 * but a Mac has no touch screen.
 */
export function isIOS(nav: Pick<Navigator, 'userAgent' | 'maxTouchPoints'> = navigator): boolean {
  return /iPhone|iPad|iPod/.test(nav.userAgent) || (/Macintosh/.test(nav.userAgent) && nav.maxTouchPoints > 1);
}

/** Whether a failure to reach the server means there is no connection, rather than an answer. */
export function isOffline(err: unknown, nav: Pick<Navigator, 'onLine'> = navigator): boolean {
  return !nav.onLine || err instanceof TypeError;
}

/**
 * Colors the bar a phone draws above the page: the instance's light and
 * dark colors, each in its scheme, or the one the person chose in the
 * footer for both.
 */
export function applyThemeColor(settings: Pick<AppSettings, 'theme_color' | 'theme_color_dark'>, theme: Theme, doc: Document = document): void {
  const metas = [...doc.head.querySelectorAll<HTMLMetaElement>('meta[name="theme-color"]')];
  for (const meta of metas) {
    const dark = (meta.getAttribute('media') ?? '').includes('dark');
    const scheme = theme === 'system' ? (dark ? 'dark' : 'light') : theme;
    meta.content = scheme === 'dark' ? settings.theme_color_dark : settings.theme_color;
  }
}

/** What watchUpdates needs of a registration and its container, for tests to stand in for. */
export interface Registration extends EventTarget {
  readonly waiting: ServiceWorker | null;
  readonly installing: ServiceWorker | null;
  update(): Promise<unknown>;
}
export interface Container extends EventTarget {
  readonly controller: ServiceWorker | null;
}

/**
 * Calls ready with a new release's worker once it is installed and waits,
 * while an older one runs the page. The first install is no update: no
 * worker ran the page before it.
 */
export function watchUpdates(reg: Registration, container: Container, ready: (worker: ServiceWorker) => void): void {
  if (reg.waiting && container.controller) ready(reg.waiting);
  reg.addEventListener('updatefound', () => {
    const worker = reg.installing;
    worker?.addEventListener('statechange', () => {
      if (worker.state === 'installed' && container.controller) ready(worker);
    });
  });
}

/** Has a waiting worker take over, then loads the page again from its release. */
export function applyUpdate(worker: ServiceWorker, container: Container, reload: () => void = () => location.reload()): void {
  container.addEventListener('controllerchange', reload, { once: true });
  worker.postMessage({ type: 'skip-waiting' });
}

class InstalledApp {
  /** Running installed, in a window of its own. */
  standalone = $state(false);
  /** The browser offers to install it; install() asks. */
  installable = $state(false);
  /** On iOS, outside the installed app: Share › Add to Home Screen installs it. */
  ios = $state(false);
  online = $state(true);
  /** A new release's worker, installed and waiting for Reload to update. */
  waiting = $state<ServiceWorker | null>(null);
  settings = $state<AppSettings | null>(null);

  #prompt: InstallPromptEvent | null = null;
  #container: Container | null = null;

  /** Listens for all of it, once, before the app is drawn: Chrome offers to install early. */
  start(win: Window = window): void {
    this.standalone = isStandalone(win);
    this.ios = isIOS(win.navigator) && !this.standalone;
    this.online = win.navigator.onLine;
    win.addEventListener('beforeinstallprompt', (e) => {
      // Held, for the Install button, rather than shown when Chrome chooses.
      e.preventDefault();
      this.#prompt = e as InstallPromptEvent;
      this.installable = true;
    });
    win.addEventListener('appinstalled', () => {
      this.#prompt = null;
      this.installable = false;
    });
    win.addEventListener('online', () => (this.online = true));
    win.addEventListener('offline', () => (this.online = false));
    this.loadSettings();
    if (import.meta.env.PROD) void this.register(win);
  }

  async loadSettings(): Promise<void> {
    try {
      this.settings = await request<AppSettings>('GET', '/settings/app');
    } catch {
      // BAM!'s colors, from index.html, stand.
    }
  }

  async install(): Promise<boolean> {
    const prompt = this.#prompt;
    if (!prompt) return false;
    this.#prompt = null;
    this.installable = false;
    await prompt.prompt();
    return (await prompt.userChoice).outcome === 'accepted';
  }

  async register(win: Window): Promise<void> {
    const container = win.navigator.serviceWorker;
    if (!container) return;
    try {
      const reg = await container.register('/sw.js', { scope: '/' });
      this.#container = container;
      watchUpdates(reg, container, (worker) => (this.waiting = worker));
      // A release can come out while the app stays open, as an installed
      // app does: look when it is shown again, and every hour.
      win.document.addEventListener('visibilitychange', () => {
        if (win.document.visibilityState === 'visible') reg.update().catch(() => {});
      });
      win.setInterval(() => reg.update().catch(() => {}), 60 * 60 * 1000);
    } catch {
      // No worker, as in a private window: the app works as a page does.
    }
  }

  /** Reload to update: the waiting release takes over, and the page loads from it. */
  update(): void {
    if (this.waiting && this.#container) applyUpdate(this.waiting, this.#container);
  }
}

export const installedApp = new InstalledApp();

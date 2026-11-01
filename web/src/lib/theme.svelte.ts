// Light or dark (#168): the system's choice unless the person makes their
// own. Theirs is kept on their profile, to follow them to every device, and
// in this browser, so it is applied before the app is drawn and on pages
// seen signed out. app.css reads it from <html data-theme>.

import { request } from './api';

export type Theme = 'system' | 'light' | 'dark';

const storageKey = 'bam.theme';

/** A theme as given, or the system's for anything else. */
export function asTheme(value: unknown): Theme {
  return value === 'light' || value === 'dark' ? value : 'system';
}

/** The theme last chosen in this browser. */
export function storedTheme(): Theme {
  try {
    return asTheme(localStorage.getItem(storageKey));
  } catch {
    return 'system';
  }
}

/** Draws the page in the theme: the system's has no data-theme at all. */
export function applyTheme(theme: Theme, root: HTMLElement = document.documentElement): void {
  if (theme === 'system') delete root.dataset.theme;
  else root.dataset.theme = theme;
}

class Appearance {
  theme = $state<Theme>(storedTheme());

  /** Takes up a theme, keeping it in this browser. */
  adopt(theme: Theme): void {
    this.theme = theme;
    applyTheme(theme);
    try {
      if (theme === 'system') localStorage.removeItem(storageKey);
      else localStorage.setItem(storageKey, theme);
    } catch {
      // Storage unavailable: it lasts as long as the page.
    }
  }

  /** The person's own choice, saved to their profile too when signed in. */
  async choose(theme: Theme, signedIn: boolean): Promise<void> {
    this.adopt(theme);
    if (signedIn) await request('PUT', '/auth/theme', { theme });
  }
}

export const appearance = new Appearance();

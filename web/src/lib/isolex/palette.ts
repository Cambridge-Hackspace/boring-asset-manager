// The palette as CSS (README §3.7): each token a custom property,
// --iso-<token>, so an icon drawn with var(--iso-ink) follows light and
// dark mode, and a palette change, without being drawn again. iso-light
// and iso-dark force one mode, for showing both side by side.

export interface IconColor {
  token: string;
  light: string;
  dark: string;
}

const tokenRule = /^[a-z][a-z0-9-]{0,63}$/u;
const colorRule = /^#(?:[0-9a-f]{3}|[0-9a-f]{6}|[0-9a-f]{8})$/iu;

/** The CSS property a token is painted with. */
export function tokenVar(token: string): string {
  return tokenRule.test(token) ? `var(--iso-${token})` : 'none';
}

/** The palette's custom properties, light and dark; anything that is not
    a token and a color, as the server only stores, is left out. */
export function paletteCss(palette: IconColor[]): string {
  const ok = palette.filter((c) => tokenRule.test(c.token) && colorRule.test(c.light) && colorRule.test(c.dark));
  const props = (mode: 'light' | 'dark') => ok.map((c) => `--iso-${c.token}:${c[mode]};`).join('');
  return (
    `:root,.iso-light{${props('light')}}` +
    `@media (prefers-color-scheme: dark){:root{${props('dark')}}}` +
    `.iso-light{${props('light')}}.iso-dark{${props('dark')}}`
  );
}

import type { Page } from '@playwright/test';
import { expect, test } from './fixtures';
import { seedViews } from './views';

// The automated half of the look's gate (#156), on every view, at a desktop's
// width and a phone's, in light and dark:
//   targets:  every control is at least 44 × 44 CSS px, or 32 inside an
//             editor's dense controls ([data-dense]);
//   primary:  at most one primary button shows;
//   scroll:   at 390 px the page does not scroll sideways.
// Views that fail today are listed in KNOWN, so a change that breaks a passing
// view fails here, and a view that comes to pass must be taken off the list.

type Check = 'targets' | 'primary' | 'scroll';
const widths = [1440, 390] as const;

// `${view} ${width}`, by check. Each part of #156 empties some of these.
const KNOWN: Record<Check, string[]> = {
  // Since #158 the header's controls pass; these fail on the page itself.
  targets: [
    '/ 1440', '/ 390',
    '/assets 1440', '/assets 390',
    '/assets/:id (2) 1440', '/assets/:id (2) 390',
    '/assets/:id (3) 1440', '/assets/:id (3) 390',
    '/assets/:id 1440', '/assets/:id 390',
    '/assets/:id/edit (2) 1440', '/assets/:id/edit (2) 390',
    '/assets/:id/edit 1440', '/assets/:id/edit 390',
    '/assets/import 1440', '/assets/import 390',
    '/assets/new 1440', '/assets/new 390',
    '/assets?archived=true 1440', '/assets?archived=true 390',
    '/assets?condition=problem 1440', '/assets?condition=problem 390',
    '/categories 1440', '/categories 390',
    '/categories/:id 1440', '/categories/:id 390',
    '/categories/new 1440', '/categories/new 390',
    '/help/actions 1440',
    '/help/scanning 1440',
    '/help/spool 1440',
    '/packages 1440', '/packages 390',
    '/reports 1440', '/reports 390',
    '/reports/:id (2) 1440', '/reports/:id (2) 390',
    '/reports/:id 1440', '/reports/:id 390',
    '/reports/new 1440', '/reports/new 390',
    '/scan-templates/new 1440', '/scan-templates/new 390',
    '/sign-in 1440', '/sign-in 390',
    '/spool 1440', '/spool 390',
    '/templates 1440', '/templates 390',
    '/templates/:id/edit (2) 1440', '/templates/:id/edit (2) 390',
    '/templates/:id/edit 1440', '/templates/:id/edit 390',
    '/templates/new 1440', '/templates/new 390',
  ],
  primary: [
    '/account 1440', '/account 390',
    '/categories/:id 1440', '/categories/:id 390',
    '/spool 1440', '/spool 390',
  ],
  scroll: [
    '/fields 390',
    '/tokens 390',
  ],
};

/** A view's name with its IDs taken out, numbered when a name repeats. */
function names(paths: string[]): string[] {
  const seen = new Map<string, number>();
  return paths.map((p) => {
    const base = p.replace(/[0-9a-f]{8}-[0-9a-f-]{27}/g, ':id');
    const n = (seen.get(base) ?? 0) + 1;
    seen.set(base, n);
    return n > 1 ? `${base} (${n})` : base;
  });
}

/** What the page fails, as `check: what` lines. */
async function measure(page: Page, width: number): Promise<{ check: Check; detail: string }[]> {
  const found = await page.evaluate((phone) => {
    const out: { check: string; detail: string }[] = [];
    const visible = (el: Element) => {
      const r = el.getBoundingClientRect();
      const s = getComputedStyle(el);
      return r.width > 1 && r.height > 1 && s.visibility !== 'hidden' && s.display !== 'none';
    };
    const describe = (el: Element) =>
      `${el.tagName.toLowerCase()} "${(el.getAttribute('aria-label') || el.textContent || el.getAttribute('name') || '').trim().replace(/\s+/g, ' ').slice(0, 40)}"`;

    const controls = document.querySelectorAll('a[href], button, input:not([type="hidden"]), select, textarea, summary, [role="button"], [role="tab"]');
    const small: string[] = [];
    for (const el of controls) {
      if (!visible(el)) continue;
      // A link within running text is exempt, as WCAG 2.5.8 exempts it.
      if (el.tagName === 'A' && getComputedStyle(el).display === 'inline') {
        const block = el.parentElement;
        if (block && (block.textContent ?? '').trim().length > (el.textContent ?? '').trim().length) continue;
      }
      // A checkbox or radio is as large as the label it sits in.
      const target = el.matches('input[type="checkbox"], input[type="radio"]') ? (el.closest('label') ?? el) : el;
      const r = target.getBoundingClientRect();
      const least = el.closest('[data-dense]') ? 32 : 44;
      if (Math.min(r.width, r.height) + 0.5 < least) small.push(`${describe(el)} ${Math.round(r.width)}×${Math.round(r.height)}`);
    }
    if (small.length) out.push({ check: 'targets', detail: `${small.length}: ${small.slice(0, 5).join('; ')}` });

    const primary = [...document.querySelectorAll('.btn-primary, .flow-button-primary')].filter(visible);
    if (primary.length > 1) out.push({ check: 'primary', detail: primary.map(describe).join('; ') });

    if (phone && document.documentElement.scrollWidth > document.documentElement.clientWidth + 1) {
      out.push({ check: 'scroll', detail: `${document.documentElement.scrollWidth} px wide` });
    }
    return out;
  }, width < 600);
  return found as { check: Check; detail: string }[];
}

for (const scheme of ['light', 'dark'] as const) {
  for (const width of widths) {
    test(`the gate's checks at ${width} px in ${scheme}`, async ({ signedIn: page }) => {
      test.setTimeout(180_000);
      await page.setViewportSize({ width, height: 900 });
      await page.emulateMedia({ colorScheme: scheme });
      const views = await seedViews(page);
      const named = names(views.map(([path]) => path));
      const failing: Record<Check, string[]> = { targets: [], primary: [], scroll: [] };
      const details: string[] = [];
      for (const [i, [path, heading]] of views.entries()) {
        await page.goto(path);
        if (heading) await expect(page.getByRole('heading', { level: 1 })).toHaveText(heading);
        else await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
        await page.waitForLoadState('networkidle');
        for (const { check, detail } of await measure(page, width)) {
          failing[check].push(`${named[i]} ${width}`);
          details.push(`${named[i]} ${width}, ${check}: ${detail}`);
        }
      }
      await page.context().clearCookies();
      await page.goto('/sign-in');
      await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
      for (const { check, detail } of await measure(page, width)) {
        failing[check].push(`/sign-in ${width}`);
        details.push(`/sign-in ${width}, ${check}: ${detail}`);
      }
      const known = (check: Check) => KNOWN[check].filter((k) => k.endsWith(` ${width}`)).sort();
      for (const check of ['targets', 'primary', 'scroll'] as const) {
        expect.soft(failing[check].sort(), `${check} at ${width} px in ${scheme}:\n${details.join('\n')}`).toEqual(known(check));
      }
    });
  }
}

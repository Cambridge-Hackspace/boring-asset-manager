// A small history router: the current location as state, navigation, and
// pattern matching with :params.

class Router {
  path = $state(window.location.pathname);
  search = $state(window.location.search);

  /** The query string's parameters. */
  get query(): URLSearchParams {
    return new URLSearchParams(this.search);
  }
}

export const router = new Router();

function sync(): void {
  router.path = window.location.pathname;
  router.search = window.location.search;
}

window.addEventListener('popstate', sync);

/** Goes to a local URL, adding a history entry unless replace is set. */
export function navigate(to: string, replace = false): void {
  const url = new URL(to, window.location.origin);
  if (url.origin !== window.location.origin) {
    window.location.assign(to);
    return;
  }
  if (replace) history.replaceState(null, '', url.pathname + url.search + url.hash);
  else history.pushState(null, '', url.pathname + url.search + url.hash);
  sync();
  window.scrollTo(0, 0);
}

/**
 * Matches a path against a pattern such as /assets/:id, returning the
 * decoded parameters, or null when it does not match.
 */
export function matchRoute(pattern: string, path: string): Record<string, string> | null {
  const want = pattern.split('/').filter(Boolean);
  const got = path.split('/').filter(Boolean);
  if (want.length !== got.length) return null;
  const params: Record<string, string> = {};
  for (let i = 0; i < want.length; i++) {
    const w = want[i]!;
    const g = got[i]!;
    if (w.startsWith(':')) {
      try {
        params[w.slice(1)] = decodeURIComponent(g);
      } catch {
        return null;
      }
    } else if (w !== g) {
      return null;
    }
  }
  return params;
}

/**
 * Follows clicks on local links without reloading the page. Links with a
 * target, a download attribute, or a modifier key behave as usual.
 */
export function interceptLinks(root: HTMLElement): () => void {
  const onClick = (event: MouseEvent) => {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
      return;
    }
    const anchor = (event.target as Element | null)?.closest('a');
    if (!anchor || anchor.target || anchor.hasAttribute('download')) return;
    const href = anchor.getAttribute('href');
    if (!href || !href.startsWith('/') || href.startsWith('//') || href.startsWith('/api/')) return;
    event.preventDefault();
    navigate(href);
  };
  root.addEventListener('click', onClick);
  return () => root.removeEventListener('click', onClick);
}

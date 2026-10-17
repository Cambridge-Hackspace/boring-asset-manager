// The user help (README §6.5): Markdown topics bundled with the app, and
// which topic each page's Help link opens.

const sources = import.meta.glob('../help/*.md', { query: '?raw', import: 'default', eager: true }) as Record<string, string>;

export interface HelpTopic {
  slug: string;
  title: string;
  /** The topic's Markdown, without its title. */
  body: string;
}

/** The order the help index lists topics in. */
const order = ['start', 'assets', 'stock', 'loans', 'categories', 'links', 'import', 'labels', 'spool', 'scanning', 'reports', 'people', 'notifications', 'account'];

export const helpTopics: HelpTopic[] = Object.entries(sources)
  .map(([file, text]) => {
    const slug = file.replace(/^.*\/([^/]+)\.md$/, '$1');
    const [first, ...rest] = text.replace(/\r\n/g, '\n').split('\n');
    return { slug, title: (first ?? '').replace(/^#\s*/, '').trim(), body: rest.join('\n') };
  })
  .sort((a, b) => order.indexOf(a.slug) - order.indexOf(b.slug));

export function helpTopic(slug: string): HelpTopic | undefined {
  return helpTopics.find((t) => t.slug === slug);
}

/** Which topic a page's Help link opens, by its path. */
export function helpFor(path: string): string {
  const rules: [RegExp, string][] = [
    [/^\/assets\/import/, 'import'],
    [/^\/assets/, 'assets'],
    [/^\/(scan|scan-templates)(\/|$)/, 'scanning'],
    [/^\/reports/, 'reports'],
    [/^\/loans/, 'loans'],
    [/^\/spool/, 'spool'],
    [/^\/(categories|fields)/, 'categories'],
    [/^\/relations/, 'links'],
    [/^\/(templates|images|printers)/, 'labels'],
    [/^\/notifications/, 'notifications'],
    [/^\/(users|roles|tokens)/, 'people'],
    [/^\/account/, 'account'],
  ];
  return rules.find(([re]) => re.test(path))?.[1] ?? 'start';
}

// A small Markdown renderer for the help pages (docs/architecture.md) and
// for what people write, such as a vendor's notes: headings, paragraphs,
// lists, bold, italics, code and links. Everything is escaped, so no HTML
// gets through, and links go only to the app's own pages, to https, or to
// an email address.

function escape(text: string): string {
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

/**
 * Whether a link may be followed: a page of the app's own, by a single
 * leading slash (two, or a slash and a backslash, lead to another site),
 * an https address, or an email address.
 */
export function safeHref(href: string): boolean {
  return /^\/(?![/\\])/.test(href) || href.startsWith('https://') || href.startsWith('mailto:');
}

// Stands in for a link while the text around it is formatted. Written
// text never holds it: it is taken out first.
const MARK = '\u0000';

/** Bold and italics, in escaped text. */
function emphasis(html: string): string {
  return html.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>').replace(/\*([^*]+)\*/g, '<em>$1</em>');
}

/** Inline formatting: `code`, **bold**, *italics* and [links](/path). */
function inline(text: string): string {
  // Code first, so nothing inside it is formatted.
  return text
    .replaceAll(MARK, '')
    .split(/(`[^`]+`)/)
    .map((part) => {
      if (part.startsWith('`') && part.endsWith('`') && part.length > 1) return `<code>${escape(part.slice(1, -1))}</code>`;
      // Links next, each set aside whole, so that bold and italics never
      // reach into an address; a label is formatted on its own.
      const links: string[] = [];
      const rest = part.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (_m, label: string, href: string) => {
        const shown = emphasis(escape(label));
        links.push(safeHref(href) ? `<a class="underline" href="${escape(href)}">${shown}</a>` : shown);
        return `${MARK}${links.length - 1}${MARK}`;
      });
      return emphasis(escape(rest)).replace(new RegExp(`${MARK}(\\d+)${MARK}`, 'g'), (_m, i: string) => links[Number(i)]!);
    })
    .join('');
}

/** Renders Markdown to HTML. Headings start at level 2: the page has its own h1. */
export function renderMarkdown(source: string): string {
  const out: string[] = [];
  let paragraph: string[] = [];
  let list: { ordered: boolean; items: string[] } | null = null;

  const flush = () => {
    if (paragraph.length) out.push(`<p>${inline(paragraph.join(' '))}</p>`);
    paragraph = [];
    if (list) {
      const tag = list.ordered ? 'ol' : 'ul';
      out.push(`<${tag}>${list.items.map((i) => `<li>${inline(i)}</li>`).join('')}</${tag}>`);
      list = null;
    }
  };

  for (const raw of source.replace(/\r\n/g, '\n').split('\n')) {
    const line = raw.trim();
    const heading = /^(#{1,3})\s+(.*)$/.exec(line);
    const bullet = /^[-*]\s+(.*)$/.exec(line);
    const numbered = /^\d+\.\s+(.*)$/.exec(line);
    if (!line) {
      flush();
    } else if (heading) {
      flush();
      const level = Math.min(heading[1]!.length + 1, 4);
      out.push(`<h${level}>${inline(heading[2]!)}</h${level}>`);
    } else if (bullet || numbered) {
      const ordered = !!numbered;
      if (paragraph.length || (list && list.ordered !== ordered)) flush();
      list ??= { ordered, items: [] };
      list.items.push((bullet ?? numbered)![1]!);
    } else if (list && raw.startsWith('  ')) {
      // A list item carried on to the next line.
      list.items[list.items.length - 1] += ' ' + line;
    } else {
      if (list) flush();
      paragraph.push(line);
    }
  }
  flush();
  return out.join('\n');
}

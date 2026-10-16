// A small Markdown renderer for the help pages (README §6.5): headings,
// paragraphs, lists, bold, italics, code and links. The help is ours and
// bundled with the app, but everything is escaped all the same, and links
// go only to the app's own pages or to https.

function escape(text: string): string {
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

/** Inline formatting: `code`, **bold**, *italics* and [links](/path). */
function inline(text: string): string {
  // Code first, so nothing inside it is formatted.
  return text
    .split(/(`[^`]+`)/)
    .map((part) => {
      if (part.startsWith('`') && part.endsWith('`') && part.length > 1) return `<code>${escape(part.slice(1, -1))}</code>`;
      return escape(part)
        .replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (_m, label: string, href: string) =>
          href.startsWith('/') || href.startsWith('https://') ? `<a class="underline" href="${href}">${label}</a>` : label,
        )
        .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
        .replace(/\*([^*]+)\*/g, '<em>$1</em>');
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

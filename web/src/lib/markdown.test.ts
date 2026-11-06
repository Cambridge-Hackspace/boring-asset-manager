import { describe, expect, it } from 'vitest';
import { renderMarkdown } from './markdown';

describe('renderMarkdown', () => {
  it('renders headings below the page’s own, paragraphs and lists', () => {
    const html = renderMarkdown('# Assets\n\nAn asset is a thing\nyou keep.\n\n- one\n- two\n  carried on\n\n1. first\n2. second\n');
    expect(html).toBe(
      ['<h2>Assets</h2>', '<p>An asset is a thing you keep.</p>', '<ul><li>one</li><li>two carried on</li></ul>', '<ol><li>first</li><li>second</li></ol>'].join('\n'),
    );
  });

  it('formats inline, leaving code alone', () => {
    expect(renderMarkdown('**Bold**, *quiet* and `{{asset.*name*}}`')).toBe('<p><strong>Bold</strong>, <em>quiet</em> and <code>{{asset.*name*}}</code></p>');
  });

  it('links only to the app or to https', () => {
    expect(renderMarkdown('[Assets](/assets) [Docs](https://example.org) [Bad](javascript:void)')).toBe(
      '<p><a class="underline" href="/assets">Assets</a> <a class="underline" href="https://example.org">Docs</a> Bad</p>',
    );
  });

  it('escapes HTML', () => {
    expect(renderMarkdown('<script>x</script> & "y"')).toBe('<p>&lt;script&gt;x&lt;/script&gt; &amp; &quot;y&quot;</p>');
  });
});

// What people write, such as a vendor's notes, goes through it too, so
// nothing written may become markup or a link that runs or leaves quietly.
describe('renderMarkdown, on what people write', () => {
  const html = (s: string) => renderMarkdown(s);
  // Every attribute the output holds, which must be only a link's own.
  const attributes = (s: string) => [...s.matchAll(/\s([a-z-]+)=/gi)].map((m) => m[1]);

  it('escapes raw HTML, in text, labels and code', () => {
    expect(html('<script>alert(1)</script>')).toBe('<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>');
    expect(html('<img src=x onerror=alert(1)>')).toBe('<p>&lt;img src=x onerror=alert(1)&gt;</p>');
    expect(html('[<img src=x onerror=alert(1)>](/assets)')).toBe('<p><a class="underline" href="/assets">&lt;img src=x onerror=alert(1)&gt;</a></p>');
    expect(html('`<b>`')).toBe('<p><code>&lt;b&gt;</code></p>');
  });

  it('drops links that would run script or carry data, in any case', () => {
    for (const href of ['javascript:void0', 'JaVaScRiPt:void0', 'data:text/html;base64,PHNjcmlwdD4=', 'vbscript:x', 'http://example.org']) {
      expect(html(`[Go](${href})`), href).toBe('<p>Go</p>');
    }
  });

  it('drops schemes written as entities', () => {
    expect(html('[Go](&#106;avascript:alert(1))')).toBe('<p>Go)</p>');
    expect(html('[Go](javascript&#58;alert(1))')).toBe('<p>Go)</p>');
    expect(html('[Go](&#x2F;&#x2F;evil.example)')).toBe('<p>Go</p>');
  });

  it('keeps links to the app on the app: none that start with two slashes, or a slash and a backslash', () => {
    expect(html('[Go](//evil.example)')).toBe('<p>Go</p>');
    expect(html('[Go](/\\evil.example)')).toBe('<p>Go</p>');
    expect(html('[Go](/assets/a1)')).toBe('<p><a class="underline" href="/assets/a1">Go</a></p>');
  });

  it('links to email addresses', () => {
    expect(html('[Sales](mailto:sales@example.org)')).toBe('<p><a class="underline" href="mailto:sales@example.org">Sales</a></p>');
  });

  it('cannot close an address with a quote', () => {
    const out = html('[Go](https://example.org/"onmouseover="alert(1))');
    expect(out).toBe('<p><a class="underline" href="https://example.org/&quot;onmouseover=&quot;alert(1">Go</a>)</p>');
    expect(attributes(out)).toEqual(['class', 'href']);
  });

  it('keeps bold and italics out of addresses, but formats labels and the text around them', () => {
    const out = html('[a*b](https://example.org/*x*) and *c* [**d**](/e*f) *g [h](/i) j*');
    expect(out).toBe(
      '<p><a class="underline" href="https://example.org/*x*">a*b</a> and <em>c</em> <a class="underline" href="/e*f"><strong>d</strong></a> <em>g <a class="underline" href="/i">h</a> j</em></p>',
    );
    expect(attributes(out)).toEqual(['class', 'href', 'class', 'href', 'class', 'href']);
  });

  it('takes no stand-in for a link from what is written', () => {
    expect(html('[Go](/a) \u00000\u0000')).toBe('<p><a class="underline" href="/a">Go</a> 0</p>');
  });
});

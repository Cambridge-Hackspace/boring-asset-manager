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

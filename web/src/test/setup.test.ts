import { afterEach, describe, expect, it } from 'vitest';

// What jsdom says it lacks goes to its own console, which the test reads
// directly: it holds the Node console from before any spy.
const { jsdom } = globalThis as unknown as {
  jsdom: { virtualConsole: { on(event: 'jsdomError', listener: (e: Error) => void): void } };
};

describe('the test setup', () => {
  const lacking: string[] = [];
  const note = (e: Error) => lacking.push(e.message);
  jsdom.virtualConsole.on('jsdomError', note);
  afterEach(() => {
    lacking.length = 0;
  });

  it('hears what jsdom says it lacks', () => {
    window.alert('');
    expect(lacking).toEqual([expect.stringContaining('Not implemented')]);
  });

  it('scrolls without saying it cannot', () => {
    window.scrollTo(0, 0);
    window.scrollTo({ top: 0 });
    expect(lacking).toEqual([]);
  });

  it('has no canvas context, without saying so', () => {
    expect(document.createElement('canvas').getContext('2d')).toBeNull();
    expect(lacking).toEqual([]);
  });
});

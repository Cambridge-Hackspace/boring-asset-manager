import { describe, expect, it } from 'vitest';
import { describeBuild } from './version';

describe('describeBuild', () => {
  it('links a release to its page', () => {
    expect(describeBuild({ version: 'v0.2.0', commit: 'abc' })).toEqual({
      text: 'BAM! v0.2.0',
      href: 'https://github.com/Cambridge-Hackspace/boring-asset-manager/releases/tag/v0.2.0',
    });
    expect(describeBuild({ version: 'v1.3.0-rc.1' }).href).toMatch(/v1\.3\.0-rc\.1$/);
  });

  it('calls anything else a development build, with its commit', () => {
    expect(describeBuild({ version: 'dev', commit: '633e7d2a1b2c', modified: true })).toEqual({ text: 'BAM! development build (633e7d2a1b2c, modified)' });
    expect(describeBuild({ version: 'dev' })).toEqual({ text: 'BAM! development build' });
    expect(describeBuild({ version: 'v0.2.0-3-g633e7d2', commit: '633e7d2a1b2c' })).toEqual({ text: 'BAM! development build v0.2.0-3-g633e7d2 (633e7d2a1b2c)' });
  });
});

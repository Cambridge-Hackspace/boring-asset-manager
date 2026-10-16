// What the footer says about the running build.

/** The repository releases are published from. */
export const releasesUrl = 'https://github.com/Cambridge-Hackspace/boring-asset-manager/releases/tag/';

export interface BuildInfo {
  version: string;
  commit?: string;
  modified?: boolean;
}

/**
 * A release's tag, such as v1.2.0 or v1.3.0-rc.1, but not what git describe
 * gives a commit after one (v1.2.0-3-g633e7d2).
 */
const release = /^v\d+\.\d+\.\d+(-(?!\d+-g[0-9a-f]+$)[0-9A-Za-z.-]+)?$/;

/**
 * How the footer names a build: a release links to its page; anything else
 * says it is a development build, with its commit when known.
 */
export function describeBuild(info: BuildInfo): { text: string; href?: string } {
  if (release.test(info.version)) return { text: `BAM! ${info.version}`, href: releasesUrl + encodeURIComponent(info.version) };
  let text = 'BAM! development build';
  if (info.version && info.version !== 'dev') text += ` ${info.version}`;
  if (info.commit) text += ` (${info.commit}${info.modified ? ', modified' : ''})`;
  return { text };
}

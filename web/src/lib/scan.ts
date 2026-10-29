// Scan templates and scanning (README §6.4).

import type { ReportSection, ReportSpec, ScanAction } from './models';

const actionLabels: Record<ScanAction, string> = {
  open: 'Open its page',
  edit: 'Edit it',
  use: 'Use some',
  borrow: 'Borrow',
  return: 'Return',
  report: 'Report a problem',
  actions: 'Its actions, such as Identify',
  add_similar: 'Add another like it',
  add_inside: 'Put something in it',
  flows: 'Its flows, such as Collect it',
};

/** A quick action's name, as editors and previews show it. */
export function scanActionLabel(action: ScanAction): string {
  return actionLabels[action] ?? action;
}

/** A new scan template: the asset's name and where it is, what it holds, and a way to its page. */
export function newScanSpec(): ReportSpec {
  return {
    filter: {},
    sections: [
      newScanSection('details'),
      newScanSection('table'),
      { type: 'actions', columns: [], actions: ['open'] },
    ],
  };
}

export function newScanSection(type: 'details' | 'table' | 'actions'): ReportSection {
  switch (type) {
    case 'details':
      return { type, title: '', columns: [{ heading: 'Name', binding: '{{asset.name}}' }, { heading: 'Where', binding: '{{asset.location}}' }] };
    case 'table':
      return { type, title: 'In it', source: 'contents', columns: [{ heading: 'What', binding: '{{asset.name}}' }], sort_by: 0, descending: false };
    case 'actions':
      return { type, columns: [], actions: ['open'] };
  }
}

/** A scan template's spec as the API takes it: empty titles and defaults left out. */
export function cleanedScanSpec(spec: ReportSpec): ReportSpec {
  return {
    filter: {},
    sections: spec.sections.map((s) => {
      if (s.type === 'actions') return { type: 'actions', actions: [...(s.actions ?? [])] } as ReportSection;
      const out: ReportSection = { type: s.type, columns: s.columns.map((c) => ({ heading: c.heading, binding: c.binding })) };
      if (s.title?.trim()) out.title = s.title.trim();
      if (s.type === 'table') {
        out.source = 'contents';
        if (s.sort_by) out.sort_by = s.sort_by;
        if (s.descending) out.descending = true;
      }
      return out;
    }),
  };
}

/**
 * Where a code read by a camera or scanner, or typed, leads: a scan link or
 * an asset's address (from this or another of the server's addresses) to
 * its page here, and anything else to the scan page for that code. Null
 * for nothing.
 */
export function scanTarget(text: string): string | null {
  const t = text.trim();
  if (!t) return null;
  if (/^https?:\/\//i.test(t)) {
    try {
      const url = new URL(t);
      const m = /^\/(scan|assets)\/([^/]+)\/?$/.exec(url.pathname);
      if (m) return `/${m[1]}/${m[2]}`;
    } catch {
      // Not a link after all: look it up as a code.
    }
  }
  return `/scan/${encodeURIComponent(t)}`;
}

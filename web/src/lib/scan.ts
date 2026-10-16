// Scan templates and scanning (README §6.4).

import type { ReportSection, ReportSpec, ScanAction } from './models';

const actionLabels: Record<ScanAction, string> = {
  open: 'Open its page',
  edit: 'Edit it',
  use: 'Use some',
  borrow: 'Borrow it',
  return: 'Give it back',
  report: 'Report a problem',
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

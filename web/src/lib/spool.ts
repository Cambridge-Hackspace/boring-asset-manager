// The spool, a print queue for labels (README §6.2): adding to it from
// anywhere assets are, and sorting its entries onto templates and printers.

import { request } from './api';
import type { Spool, SpoolAddResult, SpoolDestination, SpoolEntry, SpoolJob, SpoolPrintResult } from './models';

/** What to add: assets by ID, a category's (with its subcategories'), or those an asset filter matches. */
export interface SpoolAddRequest {
  asset_ids?: string[];
  category_id?: string;
  subcategories?: boolean;
  filter?: Record<string, string>;
  template_id?: string;
  destination?: SpoolDestination;
  copies?: number;
}

export function addToSpool(req: SpoolAddRequest): Promise<SpoolAddResult> {
  return request<SpoolAddResult>('POST', '/spool', req);
}

const labels = (n: number) => (n === 1 ? '1 label' : `${n} labels`);

/** Says what adding to the spool did. */
export function addedMessage(r: SpoolAddResult): string {
  const parts = [r.added.length ? `Added ${labels(r.added.length)} to the spool` : 'Nothing new added to the spool'];
  if (r.already_waiting.length) parts.push(`${labels(r.already_waiting.length)} already waiting`);
  const archived = r.skipped.filter((s) => s.reason === 'archived').length;
  if (archived) parts.push(`${archived} archived, never labeled`);
  if (r.skipped.length > archived) parts.push(`${r.skipped.length - archived} not found`);
  return parts.join('; ') + '.';
}

/** Entries going to one destination with one template. */
export interface SpoolGroup {
  key: string;
  destination: SpoolDestination;
  templateId: string | null;
  entries: SpoolEntry[];
}

/** Groups entries by destination and template: printers and pools in name order, then downloads, then entries with nowhere to go. */
export function groupEntries(spool: Spool): SpoolGroup[] {
  const groups = new Map<string, SpoolGroup>();
  for (const e of spool.entries) {
    const key = `${e.destination}|${e.template_id ?? ''}`;
    if (!groups.has(key)) groups.set(key, { key, destination: e.destination, templateId: e.template_id, entries: [] });
    groups.get(key)!.entries.push(e);
  }
  const rank = (g: SpoolGroup) => (printing(g.destination) ? 0 : g.destination === 'download' ? 1 : 2);
  const title = (g: SpoolGroup) => `${destinationName(spool, g.destination)} ${templateName(spool, g.templateId)}`;
  return [...groups.values()].sort((a, b) => rank(a) - rank(b) || title(a).localeCompare(title(b)));
}

/** Whether a destination is a printer or a pool, which print jobs go to. */
export function printing(destination: string): boolean {
  return destination.startsWith('printer:') || destination.startsWith('pool:');
}

/** A destination's name: its printer's or pool's, "Download", or that none is chosen. */
export function destinationName(spool: Pick<Spool, 'printers' | 'pools'>, destination: SpoolDestination): string {
  if (destination === 'download') return 'Download';
  if (!destination) return 'No printer chosen';
  if (destination.startsWith('pool:')) {
    const id = destination.slice('pool:'.length);
    return spool.pools.find((p) => p.id === id)?.name ?? 'A deleted pool';
  }
  const id = destination.slice('printer:'.length);
  return spool.printers.find((p) => p.id === id)?.name ?? 'A deleted printer';
}

export function templateName(spool: Pick<Spool, 'templates'>, templateId: string | null): string {
  if (!templateId) return 'No template chosen';
  return spool.templates.find((t) => t.id === templateId)?.name ?? 'A deleted template';
}

/** The printers entries with this template can go to: those it suits, or every printer when it is not known. */
export function printersFor(spool: Spool, templateId: string | null | undefined): Spool['printers'] {
  if (!templateId) return spool.printers;
  const fit = new Set(spool.fits[templateId] ?? []);
  return spool.printers.filter((p) => fit.has(p.id));
}

/** The pools entries with this template can go to: those with a printer it suits, or every pool when it is not known. */
export function poolsFor(spool: Spool, templateId: string | null | undefined): Spool['pools'] {
  if (!templateId) return spool.pools;
  const fit = new Set(spool.pool_fits[templateId] ?? []);
  return spool.pools.filter((p) => fit.has(p.id));
}

/** The templates entries going to this destination can take: those that suit its printer or pool, or every template. */
export function templatesFor(spool: Spool, destination: SpoolDestination | undefined): Spool['templates'] {
  if (destination?.startsWith('pool:')) {
    const id = destination.slice('pool:'.length);
    return spool.templates.filter((t) => (spool.pool_fits[t.id] ?? []).includes(id));
  }
  if (!destination?.startsWith('printer:')) return spool.templates;
  const id = destination.slice('printer:'.length);
  return spool.templates.filter((t) => (spool.fits[t.id] ?? []).includes(id));
}

/** The one value every item shares, or undefined when they differ or there are none. */
export function shared<T, V>(items: T[], value: (item: T) => V): V | undefined {
  if (!items.length) return undefined;
  const first = value(items[0]!);
  return items.every((i) => value(i) === first) ? first : undefined;
}

const queueKey = (userId: string) => `bam.spool.queue-new.${userId}`;

/** Whether this person last chose to add new assets' labels to the spool: yes until they say otherwise. */
export function queueNewLabels(userId: string): boolean {
  try {
    return localStorage.getItem(queueKey(userId)) !== 'false';
  } catch {
    return true;
  }
}

/** Remembers this person's choice for next time. */
export function rememberQueueNewLabels(userId: string, queue: boolean): void {
  try {
    localStorage.setItem(queueKey(userId), String(queue));
  } catch {
    // Not remembered: private windows and blocked storage start ticked again.
  }
}

/** Says what printing did: the jobs sent off, and what was left out. */
export function printedMessage(r: SpoolPrintResult): string {
  const total = r.jobs.reduce((n, j) => n + j.labels, 0);
  const parts = [
    r.jobs.length
      ? `Sent ${labels(total)} to print in ${r.jobs.length === 1 ? '1 job' : `${r.jobs.length} jobs`}`
      : 'Nothing was ready to print',
  ];
  if (r.skipped.length) parts.push(`${labels(r.skipped.length)} left out: ${r.skipped.map((s) => `${s.asset_name} (${s.reason})`).join(', ')}`);
  return [parts.join('; ') + '.', ...(r.warnings ?? []).map((w) => w + '.')].join(' ');
}

/** A job's state, as the jobs list says it. */
export function jobStateName(j: Pick<SpoolJob, 'state' | 'download'>): string {
  switch (j.state) {
    case 'waiting':
      return 'Waiting';
    case 'sending':
      return 'Sending';
    case 'done':
      return j.download ? 'Downloaded' : 'Printed';
    case 'failed':
      return 'Failed';
    case 'canceled':
      return 'Canceled';
  }
}

/** Whether a job is still going, so the page keeps checking on it. */
export function jobActive(state: string): boolean {
  return state === 'waiting' || state === 'sending';
}

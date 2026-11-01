// Backups from the web, and replacing the instance with one (docs/backups.md).
// Checking and replacing take as long as the archive does, so each is a
// job, asked after until it is done.

import { request } from './api';

export type Kind = 'assets' | 'catalog' | 'labels' | 'reports' | 'icons' | 'flows' | 'people' | 'settings' | 'auth';

/** What each kind holds, and what replacing it does, in plain words. */
export const kinds: Record<Kind, { label: string; holds: string; replacing: string }> = {
  assets: {
    label: 'Assets',
    holds: 'assets, their codes, field values, links, loans, photos, placements and history',
    replacing: 'Assets become the backup’s: those added since are removed, and those removed are back.',
  },
  catalog: {
    label: 'The catalog',
    holds: 'categories, fields, hooks, link types and scopes',
    replacing: 'Categories, fields and hooks become the backup’s; assets in a category it lacks lose that category.',
  },
  labels: {
    label: 'Labels',
    holds: 'label templates and their images',
    replacing: 'Label templates and images become the backup’s.',
  },
  reports: { label: 'Reports', holds: 'reports and scan templates', replacing: 'Reports become the backup’s.' },
  icons: { label: 'Icons', holds: 'the icon library, palette and category icons', replacing: 'Icons become the backup’s, and are built again.' },
  flows: { label: 'Flows', holds: 'flows and what they filed', replacing: 'Flows become the backup’s.' },
  people: {
    label: 'People and roles',
    holds: 'people’s names and emails, roles and who holds them',
    replacing: 'The backup’s people and roles are put back as it had them. No one is removed, passwords are kept, and no one is signed out.',
  },
  settings: {
    label: 'Settings',
    holds: 'printers, pools, label stock, notification channels and the instance’s settings',
    replacing: 'Printers, label stock and notification channels become the backup’s; channels keep no passwords from a backup.',
  },
  auth: {
    label: 'Authentication',
    holds: 'passwords, sessions and tokens',
    replacing: 'Never through the web: bam restore --replace does that, from the command line.',
  },
};

export interface ArchiveHeader {
  format: string;
  bam_version: string;
  schema_version: number;
  created_at: string;
  dialect: string;
  kinds: Kind[];
  secrets: boolean;
  set: { id: string; part: number; parts?: number };
}

export interface CheckResult {
  header: ArchiveHeader;
  tables?: Record<string, number>;
  files: number;
  file_bytes: number;
  missing_files: number;
  parts: number;
  schema_current: boolean;
  instance_host: string;
  warnings: string[];
}

export interface ReplaceSummary {
  kinds: Kind[] | null;
  tables: Record<string, number>;
  files: number;
  file_bytes: number;
  already: number;
  unnamed: number;
  warnings: string[] | null;
  removed: Record<string, number> | null;
  lost: Record<string, number> | null;
}

export interface ReplaceResult {
  summary: ReplaceSummary | null;
  before_upload_id?: string;
}

interface Job<T> {
  id: string;
  done: boolean;
  error?: string;
  result?: T;
}

/** A job failed: its error, as the server said it. */
export class JobError extends Error {}

/** Starts a job and waits for it, asking after it every so often. */
export async function runJob<T>(path: string, body: unknown, opts: { sleep?: (ms: number) => Promise<void> } = {}): Promise<T> {
  const sleep = opts.sleep ?? ((ms: number) => new Promise<void>((r) => setTimeout(r, ms)));
  let job = await request<Job<T>>('POST', path, body);
  for (let wait = 250; !job.done; wait = Math.min(wait * 2, 3000)) {
    await sleep(wait);
    job = await request<Job<T>>('GET', `/backup/jobs/${encodeURIComponent(job.id)}`);
  }
  if (job.error) throw new JobError(job.error);
  return job.result as T;
}

/** The kinds a replace may choose from an archive: all it holds but authentication. */
export function replaceable(header: ArchiveHeader): Kind[] {
  return header.kinds.filter((k) => k !== 'auth');
}

/** Sums a map of counts. */
export function total(counts: Record<string, number> | null | undefined): number {
  return Object.values(counts ?? {}).reduce((a, b) => a + b, 0);
}

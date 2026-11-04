// A set of files imported one after another, each with its own mapping and
// preview (docs/csv.md#importing-several-files).

import { ApiError, request } from './api';
import { messageOf } from './errors';
import { importForm, rememberMapping, rememberedMapping } from './importing';
import type { ImportPlan, ImportResult } from './models';

let nextId = 0;

/** One file of the set: its preview, mapping and outcome. */
export class ImportEntry {
  readonly id = ++nextId;
  readonly file: File;
  /** The sheet, when the file is an Excel workbook (docs/csv.md#excel-workbooks). */
  readonly sheet: string;
  plan = $state<ImportPlan | null>(null);
  mapping = $state<string[]>([]);
  /** The mapping the plan was checked with. */
  checkedWith = $state('');
  /** Removing links no row states, when the file has a links column. */
  removeLinks = $state(false);
  /** Started from the mapping last used for these headers, not the suggested one. */
  remembered = $state(false);
  busy = $state(false);
  error = $state('');
  result = $state<ImportResult | null>(null);
  /** Stopped short by an earlier file's failure. */
  skipped = $state(false);

  constructor(file: File, sheet = '') {
    this.file = file;
    this.sheet = sheet;
  }

  /** How the page names it: the file, or "Workbook › Sheet". */
  get name(): string {
    return this.sheet ? `${this.file.name.replace(/\.xlsx$/i, '')} › ${this.sheet}` : this.file.name;
  }

  get hasLinks(): boolean {
    return this.mapping.includes('links');
  }

  get checkKey(): string {
    return JSON.stringify([this.mapping, this.hasLinks && this.removeLinks]);
  }

  get checked(): boolean {
    return this.plan !== null && this.checkKey === this.checkedWith;
  }

  get linkChanges(): number {
    return this.plan ? this.plan.links_added + this.plan.links_moved + this.plan.links_removed : 0;
  }

  /** Checked, clean, and with something to do. */
  get ready(): boolean {
    const p = this.plan;
    return this.checked && p !== null && p.problem_count === 0 && p.create + p.update + this.linkChanges > 0;
  }

  /** Checked, and wrong only in naming assets that do not exist yet, which an earlier file may create. */
  get waiting(): boolean {
    const p = this.plan;
    return this.checked && p !== null && p.unresolved > 0 && p.problem_count === p.unresolved;
  }

  get headers(): string[] {
    return this.plan?.columns.map((c) => c.header) ?? [];
  }

  /** Previews the file, with the mapping as it stands or as the server suggests. */
  async preview(withMapping: boolean, userId: string): Promise<void> {
    this.busy = true;
    this.error = '';
    try {
      const next = await request<ImportPlan>('POST', '/assets/import/preview', importForm(this.file, withMapping ? this.mapping : undefined, this.hasLinks && this.removeLinks, this.sheet));
      this.plan = next;
      this.mapping = next.columns.map((c) => c.target);
      this.checkedWith = this.checkKey;
      if (withMapping) rememberMapping(userId, this.headers, this.mapping);
    } catch (err) {
      this.plan = null;
      this.error = messageOf(err);
    } finally {
      this.busy = false;
    }
  }

  /** The first preview: the suggested mapping, or the one last used for the same headers. */
  async start(userId: string): Promise<void> {
    await this.preview(false, userId);
    if (!this.plan) return;
    const last = rememberedMapping(userId, this.headers);
    if (!last || JSON.stringify(last) === JSON.stringify(this.mapping)) return;
    this.mapping = last;
    await this.preview(true, userId);
    this.remembered = this.plan !== null;
  }

  /** Back to what the server suggests from the headers. */
  async useSuggested(userId: string): Promise<void> {
    if (!this.plan) return;
    this.mapping = this.plan.columns.map((c) => c.suggested);
    this.remembered = false;
    await this.preview(true, userId);
  }

  /** Imports the file; false when it was refused, with the server's findings shown. */
  async run(userId: string): Promise<boolean> {
    this.busy = true;
    this.error = '';
    try {
      this.result = await request<ImportResult>('POST', '/assets/import', importForm(this.file, this.mapping, this.hasLinks && this.removeLinks, this.sheet));
      rememberMapping(userId, this.headers, this.mapping);
      return true;
    } catch (err) {
      // The server checks again; if something changed, show its findings.
      if (err instanceof ApiError && err.status === 422) {
        const details = (err.body as { details?: ImportPlan } | undefined)?.details;
        if (details) this.plan = details;
      }
      this.error = messageOf(err);
      return false;
    } finally {
      this.busy = false;
    }
  }
}

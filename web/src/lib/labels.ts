// What the Labels panel offers for some assets (POST /labels/options): the
// templates that can label them, in order of preference, and how each suits
// each printer (README §4.4).

import type { Adapt, Mirror } from './template';

export interface LabelProblem {
  asset_id?: string;
  path?: string;
  message: string;
}

export interface LabelTemplateOption {
  id: string;
  name: string;
  units: 'mm' | 'in';
  width: number;
  height: number;
  dpi: number;
  sizing?: 'adaptive' | 'fixed' | '';
  adapt?: Adapt;
  /** How its labels print mirrored and turned, for a preview of them as applied. */
  print_mirror?: Mirror;
  print_rotation?: number;
  /** One of the assets' categories' templates, listed first. */
  preferred: boolean;
  /** The assets it cannot label, and why. */
  problems: LabelProblem[];
}

export interface LabelFit {
  fits: boolean;
  reason?: string;
  warnings: string[];
}

export interface LabelOptions {
  templates: LabelTemplateOption[];
  /** The template to start with. */
  template_id: string;
  /** Empty for those who cannot print. */
  printers: { id: string; name: string }[];
  fits: Record<string, Record<string, LabelFit>>;
}

/** Each printer, with how a template's labels suit it. */
export function printersFor(options: LabelOptions, templateId: string): { id: string; name: string; fit: LabelFit }[] {
  return options.printers.map((p) => ({
    ...p,
    fit: options.fits[templateId]?.[p.id] ?? { fits: false, reason: `It is not known whether ${p.name} takes this label.`, warnings: [] },
  }));
}

const n = (v: number) => String(Math.round(v * 100) / 100);

/** A template's size: "62 × 29 mm", or for an adaptive one the sizes it takes. */
export function sizeText(t: Pick<LabelTemplateOption, 'units' | 'width' | 'height' | 'sizing' | 'adapt'>): string {
  if (t.sizing === 'adaptive' && t.adapt) {
    const a = t.adapt;
    return `any size from ${n(a.min_width)} × ${n(a.min_height)} to ${n(a.max_width)} × ${n(a.max_height)} ${t.units}`;
  }
  return `${n(t.width)} × ${n(t.height)} ${t.units}`;
}

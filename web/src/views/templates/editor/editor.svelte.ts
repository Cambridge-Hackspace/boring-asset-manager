// The template editor's state: the template, what is selected, the tool in
// hand, undo history, and the hint the status bar shows. Every part of the
// editor reads and changes it through one Editor, given to them as context.

import { getContext, setContext } from 'svelte';
import { History } from '../../../lib/history';
import { pxPerMm, screenKey } from '../../../lib/screen';
import {
  arrange,
  blankTemplate,
  clip,
  flipElements,
  gridStep,
  layerName,
  moveLayer,
  newGroupName,
  restack,
  rotateElements,
  unclip,
  type Alignment,
  type Restack,
  type ShapeKind,
  type TemplateDoc,
  type TemplateElement,
} from '../../../lib/template';
import type { Anchor } from './transform';
import { clampScale, fitView, fitWidthView, nextStep, scaleAt100, zoomAround, type View } from './view';

/** What a click or drag on the label does. */
export type Tool = 'select' | 'text' | 'code' | 'image' | ShapeKind;

export class Editor {
  doc = $state<TemplateDoc>(blankTemplate());
  /** Indices of the selected elements, in the order they were chosen. */
  selected = $state<number[]>([]);
  tool = $state<Tool>('select');
  /** The code type the code tool adds. */
  codeType = $state('qr');
  /** What the status bar says can be done now: the modifiers, mostly. */
  hint = $state('');
  /** Where the pointer is on the label, in its units, or null off it. */
  pointer = $state<{ x: number; y: number } | null>(null);
  canUndo = $state(false);
  canRedo = $state(false);
  /** The point of the selection its numbers are given at, and its handles' Ctrl+click sets. */
  anchor = $state<Anchor>('nw');
  /** Width and height changed together, in proportion, from the numbers. */
  aspectLocked = $state(false);
  /** Turning snaps to multiples of 45° when near one. */
  snapAngles = $state(rememberedOn('bam.editor-snap-angles'));
  /** What was last copied, for Paste in the menu, where the system clipboard cannot be read. */
  clipboard = $state('');

  setSnapAngles(on: boolean) {
    this.snapAngles = on;
    try {
      localStorage.setItem('bam.editor-snap-angles', on ? 'on' : 'off');
    } catch {
      // Storage unavailable: it lasts as long as the page.
    }
  }

  /** Everything but the canvas hidden (Tab). */
  canvasOnly = $state(false);

  // The view: zoom and where the label is in the viewport (README §4.1).
  view = $state<View>({ scale: 4, x: 24, y: 24 });
  /** The viewport's size, in CSS pixels. */
  viewport = $state({ w: 0, h: 0 });
  /** Whether the view fits the label, and so fits it again as the label or the viewport changes. */
  fitted = $state(true);
  /** Showing the label at its real size at 100%, rather than a printer dot a screen pixel. */
  printSize = $state(remembered('bam.editor-print-size'));
  /** The screen the window is on, for its calibration. */
  screen = $state(typeof window === 'undefined' ? '' : screenKey());
  /** The rulers' units, as last chosen in this browser; null follows the template's. */
  rulerUnits = $state<'mm' | 'in' | null>(rememberedUnits());
  /** Changed when a screen is calibrated, so what depends on it is worked out again. */
  calibration = $state(0);

  setRulerUnits(units: 'mm' | 'in') {
    this.rulerUnits = units;
    try {
      localStorage.setItem('bam.editor-ruler-units', units);
    } catch {
      // Storage unavailable: it lasts as long as the page.
    }
  }

  /** The view before fitting, which toggling the fit goes back to. */
  private unfitted: View | null = null;

  /** Template units' screen pixels at 100%. */
  get at100(): number {
    const d = this.doc;
    const mmPerUnit = d.units === 'in' ? 25.4 : 1;
    const dpr = typeof window === 'undefined' ? 1 : window.devicePixelRatio || 1;
    void this.calibration;
    return scaleAt100((d.dpi * mmPerUnit) / 25.4, dpr, this.printSize, pxPerMm(this.screen).pxPerMm, mmPerUnit);
  }

  /** The zoom, as a percentage. */
  get zoom(): number {
    return (this.view.scale / this.at100) * 100;
  }

  /** Whether print size is measured on this screen, rather than taken from the browser's nominal inch. */
  get calibrated(): boolean {
    void this.calibration;
    return pxPerMm(this.screen).calibrated;
  }

  /** Shows the whole label (2), remembering the view it leaves, for 0. */
  fit() {
    const { w, h } = this.viewport;
    if (!w || !h) return;
    if (!this.fitted) this.unfitted = { ...this.view };
    this.view = fitView(this.doc.width, this.doc.height, w, h);
    this.fitted = true;
  }

  /** Fills the viewport's width with the label (3). */
  fitWidth() {
    const { w, h } = this.viewport;
    if (!w || !h) return;
    this.view = fitWidthView(this.doc.width, this.doc.height, w, h);
    this.fitted = false;
    this.unfitted = null;
  }

  /** Flips between fitting the label and the view before it (0). */
  toggleFit() {
    if (this.fitted && this.unfitted) {
      this.view = this.unfitted;
      this.unfitted = null;
      this.fitted = false;
      return;
    }
    this.fit();
  }

  /** Zooms to a percentage, keeping the point (ax, ay) of the viewport where it is, or its middle. */
  setZoom(percent: number, ax = this.viewport.w / 2, ay = this.viewport.h / 2) {
    const at100 = this.at100;
    this.view = zoomAround(this.view, clampScale((percent / 100) * at100, at100), ax, ay);
    this.fitted = false;
    this.unfitted = null;
  }

  /** Zooms by a factor, smoothly, about a point of the viewport. */
  zoomBy(factor: number, ax: number, ay: number) {
    this.setZoom(this.zoom * factor, ax, ay);
  }

  /** The next zoom step in or out (+ and -). */
  zoomStep(inward: boolean, ax?: number, ay?: number) {
    this.setZoom(nextStep(this.zoom, inward), ax, ay);
  }

  /** Moves the label on the screen. */
  panBy(dx: number, dy: number) {
    this.view = { ...this.view, x: this.view.x + dx, y: this.view.y + dy };
    this.fitted = false;
    this.unfitted = null;
  }

  /** Switches print size, keeping the zoom's percentage. */
  setPrintSize(on: boolean) {
    const zoom = this.zoom;
    this.printSize = on;
    try {
      localStorage.setItem('bam.editor-print-size', on ? 'on' : 'off');
    } catch {
      // Storage unavailable: it lasts as long as the page.
    }
    if (!this.fitted) this.setZoom(zoom);
  }

  private history = new History<TemplateDoc>();

  /** The selected element, when exactly one is. */
  get single(): TemplateElement | null {
    return this.selected.length === 1 ? (this.doc.elements[this.selected[0]!] ?? null) : null;
  }

  /** The changes made, by name, and those undone, for the History panel. */
  changes = $state<{ done: string[]; undone: string[] }>({ done: [], undone: [] });

  /**
   * Elements hidden while editing, by index: drawn on the canvas without
   * them, though they still print. Kept with their elements as they are
   * reordered or removed, and let go of by undo, redo and loading.
   */
  hidden = $state<number[]>([]);

  /** Marks the start of one undoable change, named for the history. */
  begin(label = 'Change') {
    this.history.record($state.snapshot(this.doc) as TemplateDoc, label);
    this.sync();
  }

  /** Starts afresh with a template: nothing to undo, nothing selected or hidden. */
  load(doc: TemplateDoc) {
    this.doc = doc;
    this.selected = [];
    this.hidden = [];
    this.history.clear();
    this.sync();
  }

  undo() {
    const prev = this.history.undo($state.snapshot(this.doc) as TemplateDoc);
    if (prev) this.replace(prev);
    this.sync();
  }

  redo() {
    const next = this.history.redo($state.snapshot(this.doc) as TemplateDoc);
    if (next) this.replace(next);
    this.sync();
  }

  /** Goes back or on to the state after the first done changes, as the History panel's entries do. */
  jumpTo(done: number) {
    const state = this.history.jump($state.snapshot(this.doc) as TemplateDoc, done);
    if (state) this.replace(state);
    this.sync();
  }

  /** Hides an element while editing, or shows it again. */
  toggleHidden(index: number) {
    this.hidden = this.hidden.includes(index) ? this.hidden.filter((i) => i !== index) : [...this.hidden, index];
  }

  /** Names an element, as its layer shows it; blank takes the name away. */
  rename(index: number, name: string) {
    const e = this.doc.elements[index];
    const trimmed = name.trim().replace(/\s+/g, ' ').slice(0, 100);
    if (!e || trimmed === (e.name ?? '')) return;
    this.begin('Rename');
    if (trimmed) {
      this.doc.elements[index] = { ...e, name: trimmed };
    } else {
      const { name: _n, ...rest } = e;
      this.doc.elements[index] = rest;
    }
  }

  /** Renames a group: every element in it. */
  renameGroup(from: string, to: string) {
    const name = to.trim().slice(0, 64);
    if (!name || name === from) return;
    this.begin('Rename the group');
    this.doc.elements = this.doc.elements.map((e) => (e.group === from ? { ...e, group: name } : e));
  }

  /** Hidden elements follow theirs from before a reordering to after it. */
  private carryHidden(before: TemplateElement[], after: TemplateElement[]) {
    const hidden = new Set(this.hidden.map((i) => before[i]));
    this.hidden = after.flatMap((e, k) => (hidden.has(e) ? [k] : []));
  }

  /** Cancels a change begun but not finished, such as a drag stopped with Escape. */
  cancel() {
    const prev = this.history.discard();
    if (prev) this.replace(prev);
    this.sync();
  }

  /** A change from outside the history, such as a draft restored, undoable as one step. */
  apply(doc: TemplateDoc, label = 'Change the label') {
    this.begin(label);
    this.replace(doc, true);
  }

  private replace(doc: TemplateDoc, keepHidden = false) {
    this.doc = doc;
    this.selected = this.selected.filter((i) => i < doc.elements.length);
    this.hidden = keepHidden ? this.hidden.filter((i) => i < doc.elements.length) : [];
  }

  private sync() {
    this.canUndo = this.history.canUndo;
    this.canRedo = this.history.canRedo;
    this.changes = this.history.changes;
  }

  /** The elements, snapshotted, for the pure geometry in lib/template. */
  elements(): TemplateElement[] {
    return $state.snapshot(this.doc.elements) as TemplateElement[];
  }

  /** Replaces one element, as one undoable step. */
  update(index: number, e: TemplateElement, label = `Change ${layerName(this.doc.elements[index] ?? e)}`) {
    this.begin(label);
    this.doc.elements[index] = e;
  }

  /** Adds elements on top, selected. */
  add(...elements: TemplateElement[]) {
    if (!elements.length) return;
    this.begin(elements.length === 1 ? `Add ${layerName(elements[0]!)}` : `Add ${elements.length} elements`);
    const first = this.doc.elements.length;
    this.doc.elements.push(...elements);
    this.selected = elements.map((_, k) => first + k);
  }

  remove() {
    if (!this.selected.length) return;
    this.begin(this.selected.length === 1 ? `Delete ${layerName(this.doc.elements[this.selected[0]!]!)}` : `Delete ${this.selected.length} elements`);
    const kept = this.doc.elements.flatMap((_, i) => (this.selected.includes(i) ? [] : [i]));
    this.hidden = kept.flatMap((i, k) => (this.hidden.includes(i) ? [k] : []));
    this.doc.elements = this.doc.elements.filter((_, i) => !this.selected.includes(i));
    this.selected = [];
  }

  selectAll() {
    this.selected = this.doc.elements.flatMap((e, i) => (e.locked ? [] : [i]));
  }

  deselect() {
    this.selected = [];
  }

  restack(how: Restack) {
    if (!this.selected.length) return;
    const before = this.elements();
    const next = restack(before, this.selected, how);
    if (JSON.stringify(next.elements) === JSON.stringify(this.doc.elements)) return;
    this.begin({ front: 'Bring to front', forward: 'Bring forward', backward: 'Send backward', back: 'Send to back' }[how]);
    this.carryHidden(before, next.elements);
    this.doc.elements = next.elements;
    this.selected = next.selected;
  }

  /** Moves the element at from to to, in the drawing order. */
  moveLayer(from: number, to: number) {
    this.begin('Move a layer');
    const before = this.elements();
    const next = moveLayer(before, this.selected, from, to);
    this.carryHidden(before, next.elements);
    this.doc.elements = next.elements;
    this.selected = next.selected;
  }

  turn(delta: number) {
    if (!this.selected.length) return;
    this.begin('Turn');
    this.doc.elements = rotateElements(this.doc, this.selected, this.elements(), delta);
  }

  flip(across: boolean) {
    if (!this.selected.length) return;
    this.begin(across ? 'Flip across' : 'Flip down');
    this.doc.elements = flipElements(this.doc, this.selected, across);
  }

  arrange(how: { align?: Alignment; distribute?: 'across' | 'down'; spacing?: 'gaps' | 'centers' }, toLabel: boolean) {
    if (!this.selected.length) return;
    this.begin(how.align ? 'Line up' : 'Spread out');
    this.doc.elements = arrange(this.doc, this.selected, { ...how, toLabel: toLabel || this.selected.length === 1 });
  }

  group() {
    if (this.selected.length < 2) return;
    this.begin('Group');
    const name = newGroupName(this.doc.elements);
    this.doc.elements = this.doc.elements.map((e, i) => (this.selected.includes(i) ? { ...e, group: name } : e));
  }

  ungroup() {
    this.begin('Ungroup');
    this.doc.elements = this.doc.elements.map((e, i) => {
      if (!this.selected.includes(i) || !e.group) return e;
      const { group: _g, ...rest } = e;
      return rest;
    });
  }

  toggleLock(index: number) {
    const e = this.doc.elements[index];
    if (!e) return;
    this.begin(e.locked ? 'Unlock' : 'Lock');
    if (e.locked) {
      const { locked: _l, ...rest } = e;
      this.doc.elements[index] = rest;
    } else {
      this.doc.elements[index] = { ...e, locked: true };
      this.selected = this.selected.filter((i) => i !== index);
    }
  }

  /** The selection as the clipboard holds it, kept for Paste in the menu too. */
  copy(): string {
    this.clipboard = clip(this.doc, this.selected);
    return this.clipboard;
  }

  /** Locks the selection, or unlocks it when all of it is locked. */
  lockSelected() {
    if (!this.selected.length) return;
    const all = this.selected.every((i) => this.doc.elements[i]?.locked);
    this.begin(all ? 'Unlock' : 'Lock');
    this.doc.elements = this.doc.elements.map((e, i) => {
      if (!this.selected.includes(i)) return e;
      if (all) {
        const { locked: _l, ...rest } = e;
        return rest;
      }
      return { ...e, locked: true };
    });
    if (!all) this.selected = [];
  }

  /** Adds elements from the clipboard's text, a little way from where they were. */
  paste(text: string): boolean {
    const elements = unclip(this.doc, text, gridStep(this.doc.units) * 4);
    if (!elements?.length) return false;
    this.add(...elements);
    return true;
  }
}

function rememberedUnits(): 'mm' | 'in' | null {
  try {
    const u = localStorage.getItem('bam.editor-ruler-units');
    return u === 'mm' || u === 'in' ? u : null;
  } catch {
    return null;
  }
}

/** A switch on until switched off in this browser. */
function rememberedOn(key: string): boolean {
  try {
    return localStorage.getItem(key) !== 'off';
  } catch {
    return true;
  }
}

function remembered(key: string): boolean {
  try {
    return localStorage.getItem(key) === 'on';
  } catch {
    return false;
  }
}

const key = Symbol('editor');

export function provideEditor(editor: Editor): Editor {
  return setContext(key, editor);
}

export function useEditor(): Editor {
  return getContext<Editor>(key);
}

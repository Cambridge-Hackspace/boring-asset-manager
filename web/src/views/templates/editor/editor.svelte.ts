// The template editor's state: the template, what is selected, the tool in
// hand, undo history, and the hint the status bar shows. Every part of the
// editor reads and changes it through one Editor, given to them as context.

import { getContext, setContext } from 'svelte';
import { History } from '../../../lib/history';
import {
  arrange,
  blankTemplate,
  clip,
  flipElements,
  gridStep,
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
  /** Everything but the canvas hidden (Tab). */
  canvasOnly = $state(false);

  private history = new History<TemplateDoc>();

  /** The selected element, when exactly one is. */
  get single(): TemplateElement | null {
    return this.selected.length === 1 ? (this.doc.elements[this.selected[0]!] ?? null) : null;
  }

  /** Marks the start of one undoable change. */
  begin() {
    this.history.record($state.snapshot(this.doc) as TemplateDoc);
    this.sync();
  }

  /** Starts afresh with a template: nothing to undo, nothing selected. */
  load(doc: TemplateDoc) {
    this.doc = doc;
    this.selected = [];
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

  /** Cancels a change begun but not finished, such as a drag stopped with Escape. */
  cancel() {
    const prev = this.history.discard();
    if (prev) this.replace(prev);
    this.sync();
  }

  /** A change from outside the history, such as a draft restored, undoable as one step. */
  apply(doc: TemplateDoc) {
    this.begin();
    this.replace(doc);
  }

  private replace(doc: TemplateDoc) {
    this.doc = doc;
    this.selected = this.selected.filter((i) => i < doc.elements.length);
  }

  private sync() {
    this.canUndo = this.history.canUndo;
    this.canRedo = this.history.canRedo;
  }

  /** The elements, snapshotted, for the pure geometry in lib/template. */
  elements(): TemplateElement[] {
    return $state.snapshot(this.doc.elements) as TemplateElement[];
  }

  /** Replaces one element, as one undoable step. */
  update(index: number, e: TemplateElement) {
    this.begin();
    this.doc.elements[index] = e;
  }

  /** Adds elements on top, selected. */
  add(...elements: TemplateElement[]) {
    if (!elements.length) return;
    this.begin();
    const first = this.doc.elements.length;
    this.doc.elements.push(...elements);
    this.selected = elements.map((_, k) => first + k);
  }

  remove() {
    if (!this.selected.length) return;
    this.begin();
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
    const next = restack(this.elements(), this.selected, how);
    if (JSON.stringify(next.elements) === JSON.stringify(this.doc.elements)) return;
    this.begin();
    this.doc.elements = next.elements;
    this.selected = next.selected;
  }

  /** Moves the element at from to to, in the drawing order. */
  moveLayer(from: number, to: number) {
    this.begin();
    const next = moveLayer(this.elements(), this.selected, from, to);
    this.doc.elements = next.elements;
    this.selected = next.selected;
  }

  turn(delta: number) {
    if (!this.selected.length) return;
    this.begin();
    this.doc.elements = rotateElements(this.doc, this.selected, this.elements(), delta);
  }

  flip(across: boolean) {
    if (!this.selected.length) return;
    this.begin();
    this.doc.elements = flipElements(this.doc, this.selected, across);
  }

  arrange(how: { align?: Alignment; distribute?: 'across' | 'down' }, toLabel: boolean) {
    if (!this.selected.length) return;
    this.begin();
    this.doc.elements = arrange(this.doc, this.selected, { ...how, toLabel: toLabel || this.selected.length === 1 });
  }

  group() {
    if (this.selected.length < 2) return;
    this.begin();
    const name = newGroupName(this.doc.elements);
    this.doc.elements = this.doc.elements.map((e, i) => (this.selected.includes(i) ? { ...e, group: name } : e));
  }

  ungroup() {
    this.begin();
    this.doc.elements = this.doc.elements.map((e, i) => {
      if (!this.selected.includes(i) || !e.group) return e;
      const { group: _g, ...rest } = e;
      return rest;
    });
  }

  toggleLock(index: number) {
    const e = this.doc.elements[index];
    if (!e) return;
    this.begin();
    if (e.locked) {
      const { locked: _l, ...rest } = e;
      this.doc.elements[index] = rest;
    } else {
      this.doc.elements[index] = { ...e, locked: true };
      this.selected = this.selected.filter((i) => i !== index);
    }
  }

  /** The selection as the clipboard holds it. */
  copy(): string {
    return clip(this.doc, this.selected);
  }

  /** Adds elements from the clipboard's text, a little way from where they were. */
  paste(text: string): boolean {
    const elements = unclip(this.doc, text, gridStep(this.doc.units) * 4);
    if (!elements?.length) return false;
    this.add(...elements);
    return true;
  }
}

const key = Symbol('editor');

export function provideEditor(editor: Editor): Editor {
  return setContext(key, editor);
}

export function useEditor(): Editor {
  return getContext<Editor>(key);
}

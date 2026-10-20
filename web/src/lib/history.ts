// Undo and redo for a document: snapshots of past and undone states, each
// with the name of the change that followed it, for a history to show and
// jump through, as Krita's undo history does.

interface Step {
  snap: string;
  /** The change made from this state. */
  label: string;
}

export class History<T> {
  private past: Step[] = [];
  private future: Step[] = [];

  constructor(private readonly limit = 100) {}

  /** Records the state before a change, and what the change is. */
  record(state: T, label = 'Change'): void {
    const snap = JSON.stringify(state);
    if (this.past[this.past.length - 1]?.snap === snap) {
      // The same state again: the change is renamed for the latest.
      this.past[this.past.length - 1]!.label = label;
      this.future = [];
      return;
    }
    this.past.push({ snap, label });
    if (this.past.length > this.limit) this.past.shift();
    this.future = [];
  }

  get canUndo(): boolean {
    return this.past.length > 0;
  }

  get canRedo(): boolean {
    return this.future.length > 0;
  }

  /** The changes made, oldest first, then those undone, in the order they would be redone. */
  get changes(): { done: string[]; undone: string[] } {
    return { done: this.past.map((s) => s.label), undone: [...this.future].reverse().map((s) => s.label) };
  }

  /** Returns the previous state, given the current one, or null. */
  undo(current: T): T | null {
    const prev = this.past.pop();
    if (prev === undefined) return null;
    this.future.push({ snap: JSON.stringify(current), label: prev.label });
    return JSON.parse(prev.snap) as T;
  }

  /** Returns the next state, given the current one, or null. */
  redo(current: T): T | null {
    const next = this.future.pop();
    if (next === undefined) return null;
    this.past.push({ snap: JSON.stringify(current), label: next.label });
    return JSON.parse(next.snap) as T;
  }

  /**
   * Goes to the state after the first done changes, undoing or redoing as
   * needed, given the current state; returns it, or null if already there.
   */
  jump(current: T, done: number): T | null {
    let state: T | null = null;
    let at = current;
    while (this.past.length > done) {
      state = this.undo(at);
      if (state === null) break;
      at = state;
    }
    while (this.past.length < done && this.future.length) {
      state = this.redo(at);
      if (state === null) break;
      at = state;
    }
    return state;
  }

  /**
   * Returns the state before the last change and forgets the change, for one
   * cancelled before it was finished: unlike undo, it cannot be redone.
   */
  discard(): T | null {
    const prev = this.past.pop();
    return prev === undefined ? null : (JSON.parse(prev.snap) as T);
  }

  clear(): void {
    this.past = [];
    this.future = [];
  }
}

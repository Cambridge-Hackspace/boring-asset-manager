// Undo and redo for a document: snapshots of past and undone states.

export class History<T> {
  private past: string[] = [];
  private future: string[] = [];

  constructor(private readonly limit = 100) {}

  /** Records the state before a change. */
  record(state: T): void {
    const snap = JSON.stringify(state);
    if (this.past[this.past.length - 1] === snap) return;
    this.past.push(snap);
    if (this.past.length > this.limit) this.past.shift();
    this.future = [];
  }

  get canUndo(): boolean {
    return this.past.length > 0;
  }

  get canRedo(): boolean {
    return this.future.length > 0;
  }

  /** Returns the previous state, given the current one, or null. */
  undo(current: T): T | null {
    const prev = this.past.pop();
    if (prev === undefined) return null;
    this.future.push(JSON.stringify(current));
    return JSON.parse(prev) as T;
  }

  /** Returns the next state, given the current one, or null. */
  redo(current: T): T | null {
    const next = this.future.pop();
    if (next === undefined) return null;
    this.past.push(JSON.stringify(current));
    return JSON.parse(next) as T;
  }

  /**
   * Returns the state before the last change and forgets the change, for one
   * cancelled before it was finished: unlike undo, it cannot be redone.
   */
  discard(): T | null {
    const prev = this.past.pop();
    return prev === undefined ? null : (JSON.parse(prev) as T);
  }

  clear(): void {
    this.past = [];
    this.future = [];
  }
}

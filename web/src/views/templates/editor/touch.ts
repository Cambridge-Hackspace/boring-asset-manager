// Touch on the label, as Krita's default touch gestures: one finger is the
// tool's own, as the mouse is; two fingers pinch to zoom toward them and
// drag to pan, a second finger putting back what the first had begun; a
// quick tap of two fingers undoes, and of three redoes.

/** What the gestures do to the editor. */
export interface TouchActions {
  /** Puts back what one finger had begun, as a second comes down. */
  cancel(): void;
  pan(dx: number, dy: number): void;
  /** Zooms by a factor about a point, in screen pixels. */
  zoom(factor: number, x: number, y: number): void;
  undo(): void;
  redo(): void;
}

/** The longest a tap lasts, in milliseconds, and the furthest a finger moves in one, in pixels. */
export const TAP_MS = 300;
export const TAP_SLOP = 10;

type Point = [number, number];

export class Touches {
  private at = new Map<number, Point>();
  private pinch: { dist: number; mid: Point } | null = null;
  // Since the first finger came down: when, the most fingers down at once,
  // whether any moved past a tap, and where each came down.
  private tap: { start: number; most: number; moved: boolean; from: Map<number, Point> } | null = null;
  private gesture = false;

  constructor(private act: TouchActions) {}

  /** The fingers down: where they are between them, and how far apart the first two are. */
  private spread(): { dist: number; mid: Point } {
    const points = [...this.at.values()];
    const mid: Point = [points.reduce((s, p) => s + p[0], 0) / points.length, points.reduce((s, p) => s + p[1], 0) / points.length];
    const [a, b] = points;
    return { dist: a && b ? Math.hypot(a[0] - b[0], a[1] - b[1]) : 0, mid };
  }

  /** A finger comes down. True when it is a gesture's, not the tool's. */
  down(id: number, x: number, y: number, time: number): boolean {
    this.at.set(id, [x, y]);
    if (this.at.size === 1) {
      this.tap = { start: time, most: 1, moved: false, from: new Map([[id, [x, y]]]) };
      return false;
    }
    if (!this.gesture) this.act.cancel();
    this.gesture = true;
    if (this.tap) {
      this.tap.most = Math.max(this.tap.most, this.at.size);
      this.tap.from.set(id, [x, y]);
    }
    this.pinch = this.spread();
    return true;
  }

  /** A finger moves. True when it is a gesture's. */
  move(id: number, x: number, y: number): boolean {
    if (!this.at.has(id)) return false;
    this.at.set(id, [x, y]);
    const from = this.tap?.from.get(id);
    if (this.tap && from && Math.hypot(x - from[0], y - from[1]) > TAP_SLOP) this.tap.moved = true;
    if (this.pinch && this.at.size >= 2) {
      const now = this.spread();
      this.act.pan(now.mid[0] - this.pinch.mid[0], now.mid[1] - this.pinch.mid[1]);
      if (this.pinch.dist > 0 && now.dist > 0 && now.dist !== this.pinch.dist) this.act.zoom(now.dist / this.pinch.dist, now.mid[0], now.mid[1]);
      this.pinch = now;
    }
    return this.gesture;
  }

  /** A finger comes up, or is taken away. True when it was a gesture's. */
  up(id: number, time: number): boolean {
    if (!this.at.delete(id)) return false;
    const was = this.gesture;
    if (this.at.size >= 2) this.pinch = this.spread();
    else this.pinch = null;
    if (this.at.size === 0) {
      const t = this.tap;
      if (t && !t.moved && t.most >= 2 && time - t.start <= TAP_MS) {
        if (t.most === 2) this.act.undo();
        else this.act.redo();
      }
      this.tap = null;
      this.gesture = false;
    }
    return was;
  }
}

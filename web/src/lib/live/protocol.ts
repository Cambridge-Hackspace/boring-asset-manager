// The messages between the editor and the live renderer's worker
// (worker.ts), which runs the server's label renderer compiled to
// WebAssembly (cmd/bamlive).

export type ToWorker =
  | { type: 'image'; id: string; bytes: ArrayBuffer }
  | { type: 'render'; seq: number; template: string; data: string; placeholders: boolean; hidden: number[] };

export type FromWorker =
  | { type: 'ready'; fonts?: Record<string, ArrayBuffer> }
  | { type: 'failed'; message: string }
  | { type: 'image-failed'; id: string; message: string }
  | {
      type: 'frame';
      seq: number;
      width: number;
      height: number;
      pixels: ArrayBuffer;
      /** The JSON preview's description, as JSON; null when it cannot be drawn. */
      description: string | null;
      /** Why it cannot be drawn, as the server's preview says it, as JSON. */
      problems: string | null;
    };

/** The data a label is drawn with: POST /api/v1/templates/preview-data. */
export interface PreviewData {
  data: Record<string, unknown>;
  placeholders: boolean;
}

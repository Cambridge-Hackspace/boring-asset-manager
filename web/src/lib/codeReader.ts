// The code reader the camera scanner uses (docs/scanning.md): the browser's
// own BarcodeDetector where it has one, as Chrome on Android does, and
// otherwise the barcode-detector package's, which Safari on iOS needs. That
// one, and its WebAssembly, load only the first time someone scans there.

export interface Detected {
  rawValue: string;
}

export interface Detector {
  detect(source: HTMLVideoElement): Promise<Detected[]>;
}

export interface DetectorClass {
  new (options: { formats: string[] }): Detector;
  getSupportedFormats(): Promise<readonly string[]>;
}

/** The codes BAM! prints: QR, Micro QR and Code 128. */
export const scanFormats = ['qr_code', 'micro_qr_code', 'code_128'];

/** The browser's BarcodeDetector, or the packaged one, loaded, where it has none. */
export async function detectorClass(): Promise<DetectorClass> {
  const native = (globalThis as { BarcodeDetector?: DetectorClass }).BarcodeDetector;
  if (native) return native;
  const packaged = await import('./codeReaderPackaged');
  await packaged.load();
  return packaged.PackagedDetector;
}

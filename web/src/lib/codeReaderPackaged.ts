// The packaged code reader (src/lib/codeReader.ts): the barcode-detector
// package's BarcodeDetector, over zxing-cpp built as WebAssembly by
// zxing-wasm (internal/webui/NOTICE). Its WebAssembly is the build's own
// copy, which BAM! serves and the service worker keeps once fetched, never
// the CDN's the package would otherwise fetch it from: so it reads codes
// offline, and within the Content Security Policy.

import { BarcodeDetector, prepareZXingModule, purgeZXingModule } from 'barcode-detector/ponyfill';
import wasmURL from 'zxing-wasm/reader/zxing_reader.wasm?url';
import type { DetectorClass } from './codeReader';

const overrides = { locateFile: (path: string, prefix: string) => (path.endsWith('.wasm') ? wasmURL : prefix + path) };
prepareZXingModule({ overrides });

// It names the formats it takes as a union of names, which scanFormats are among.
export const PackagedDetector = BarcodeDetector as unknown as DetectorClass;

/**
 * Fetches and compiles the WebAssembly, so that failing to is told apart
 * from seeing no code. After a failure, the next try fetches it afresh.
 */
export async function load(): Promise<void> {
  try {
    await prepareZXingModule({ overrides, fireImmediately: true });
  } catch (err) {
    purgeZXingModule();
    prepareZXingModule({ overrides });
    throw err;
  }
}

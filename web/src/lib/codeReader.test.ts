import { afterEach, describe, expect, it, vi } from 'vitest';
import { detectorClass } from './codeReader';

// The packaged reader stood in for, so that whether it was loaded shows.
const packaged = vi.hoisted(() => ({ load: vi.fn(async () => {}), PackagedDetector: class {} }));
vi.mock('./codeReaderPackaged', () => packaged);

afterEach(() => {
  vi.unstubAllGlobals();
  packaged.load.mockClear();
});

describe('the code reader', () => {
  it('is the browser’s own where it has one, and the packaged one is never loaded', async () => {
    class Native {}
    vi.stubGlobal('BarcodeDetector', Native);
    expect(await detectorClass()).toBe(Native);
    expect(packaged.load).not.toHaveBeenCalled();
  });

  it('is the packaged one, loaded, where the browser has none', async () => {
    vi.stubGlobal('BarcodeDetector', undefined);
    expect(await detectorClass()).toBe(packaged.PackagedDetector);
    expect(packaged.load).toHaveBeenCalledTimes(1);
  });

  it('fails as loading the packaged one does, for the scanner to say so', async () => {
    vi.stubGlobal('BarcodeDetector', undefined);
    packaged.load.mockRejectedValueOnce(new TypeError('Failed to fetch'));
    await expect(detectorClass()).rejects.toThrow('Failed to fetch');
  });
});

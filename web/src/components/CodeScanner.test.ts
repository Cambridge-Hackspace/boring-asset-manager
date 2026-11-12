import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import CodeScanner from './CodeScanner.svelte';

const reader = vi.hoisted(() => ({ detectorClass: vi.fn() }));
vi.mock('../lib/codeReader', async (original) => ({ ...(await original<typeof import('../lib/codeReader')>()), ...reader }));

function camera(): MediaStreamTrack {
  const track = { stop: vi.fn() } as unknown as MediaStreamTrack;
  vi.stubGlobal('navigator', { ...navigator, mediaDevices: { getUserMedia: vi.fn(async () => ({ getTracks: () => [track] })) } });
  return track;
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  reader.detectorClass.mockReset();
});

describe('CodeScanner', () => {
  it('offers the camera wherever the browser has one, with or without its own code reader', () => {
    camera();
    vi.stubGlobal('BarcodeDetector', undefined);
    render(CodeScanner, { onfound: () => {} });
    expect(screen.getByRole('button', { name: 'Scan with the camera' })).toBeTruthy();
  });

  it('says it cannot scan only where the browser has no camera to offer', () => {
    vi.stubGlobal('navigator', { ...navigator, mediaDevices: undefined });
    render(CodeScanner, { onfound: () => {}, unsupported: 'No camera here.' });
    expect(screen.getByText('No camera here.')).toBeTruthy();
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('opens the camera once while the code reader loads', async () => {
    camera();
    reader.detectorClass.mockReturnValue(new Promise(() => {}));
    render(CodeScanner, { onfound: () => {} });
    await fireEvent.click(screen.getByRole('button', { name: 'Scan with the camera' }));
    const button = await screen.findByRole('button', { name: 'Starting the camera…' });
    expect((button as HTMLButtonElement).disabled).toBe(true);
    await fireEvent.click(button);
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledTimes(1);
  });

  it('says so when the code reader cannot be loaded, and lets the camera go', async () => {
    const track = camera();
    reader.detectorClass.mockRejectedValue(new TypeError('Failed to fetch'));
    render(CodeScanner, { onfound: () => {} });
    await fireEvent.click(screen.getByRole('button', { name: 'Scan with the camera' }));
    expect(await screen.findByText(/The code reader could not be loaded/)).toBeTruthy();
    expect(track.stop).toHaveBeenCalled();
    expect(await screen.findByRole('button', { name: 'Scan with the camera' })).toBeTruthy();
  });
});

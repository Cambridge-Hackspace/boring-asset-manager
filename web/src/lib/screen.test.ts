import { beforeEach, describe, expect, it } from 'vitest';
import { calibrate, forget, NOMINAL_PX_PER_MM, pxPerMm, screenKey } from './screen';

beforeEach(() => localStorage.clear());

describe('screen calibration', () => {
  it('names a screen by its size and pixel ratio', () => {
    expect(screenKey({ width: 2560, height: 1440 }, 1.25)).toBe('2560x1440@1.25');
    expect(screenKey({ width: 1920, height: 1080 }, 2)).not.toBe(screenKey({ width: 1920, height: 1080 }, 1));
  });

  it('uses the nominal inch until a screen is measured, each screen on its own', () => {
    expect(pxPerMm('a')).toEqual({ pxPerMm: NOMINAL_PX_PER_MM, calibrated: false });
    expect(calibrate('a', 300, 75)).toBe(4);
    expect(pxPerMm('a')).toEqual({ pxPerMm: 4, calibrated: true });
    expect(pxPerMm('b').calibrated).toBe(false);
    forget('a');
    expect(pxPerMm('a').calibrated).toBe(false);
  });

  it('ignores what it cannot read', () => {
    localStorage.setItem('bam.screen-calibration', '{"a": "wide", "b": -1, "c": 5}');
    expect(pxPerMm('a').calibrated).toBe(false);
    expect(pxPerMm('b').calibrated).toBe(false);
    expect(pxPerMm('c')).toEqual({ pxPerMm: 5, calibrated: true });
    localStorage.setItem('bam.screen-calibration', 'not json');
    expect(pxPerMm('c').calibrated).toBe(false);
  });
});

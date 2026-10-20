import { describe, expect, it } from 'vitest';
import { clampScale, fitView, fitWidthView, nextStep, parseZoom, scaleAt100, zoomAround } from './view';

describe('the view', () => {
  it('fits the label in the viewport, centered, with room round it', () => {
    expect(fitView(90, 29, 1000, 500)).toEqual({ scale: (1000 - 64) / 90, x: 32, y: (500 - 29 * ((1000 - 64) / 90)) / 2 });
    // A tall label fits by its height.
    const v = fitView(29, 90, 1000, 500);
    expect(v.scale).toBeCloseTo((500 - 64) / 90);
    expect(v.y).toBeCloseTo(32);
  });

  it('fits the width, its top at the top when it is taller than the viewport', () => {
    expect(fitWidthView(29, 90, 400, 500)).toEqual({ scale: (400 - 64) / 29, x: 32, y: 32 });
    const short = fitWidthView(90, 29, 1000, 500);
    expect(short.y).toBeCloseTo((500 - 29 * short.scale) / 2);
  });

  it('zooms about a point, which stays where it is', () => {
    const v = zoomAround({ scale: 2, x: 10, y: 20 }, 8, 110, 220);
    // The label point under (110, 220) was (50, 100), and is still there.
    expect([v.x + 50 * v.scale, v.y + 100 * v.scale]).toEqual([110, 220]);
  });

  it('keeps the zoom between 5% and 6400%', () => {
    expect(clampScale(0.001, 10)).toBe(0.5);
    expect(clampScale(10000, 10)).toBe(640);
    expect(clampScale(20, 10)).toBe(20);
  });

  it('takes 100% as one printer dot a screen pixel, or the label at its real size', () => {
    // 300 DPI in millimeters: 11.81 dots a millimeter, over a pixel ratio of 2.
    expect(scaleAt100(300 / 25.4, 2, false, 3.78, 1)).toBeCloseTo(300 / 25.4 / 2);
    expect(scaleAt100(300, 1, true, 4, 25.4)).toBeCloseTo(101.6);
  });

  it('steps through zoom levels, and reads one typed', () => {
    expect(nextStep(100, true)).toBe(150);
    expect(nextStep(100, false)).toBe(66.7);
    expect(nextStep(120, false)).toBe(100);
    expect(nextStep(6400, true)).toBe(6400);
    expect(nextStep(5, false)).toBe(5);
    expect(parseZoom('150%')).toBe(150);
    expect(parseZoom(' 75 ')).toBe(75);
    expect(parseZoom('2x')).toBe(200);
    expect(parseZoom('big')).toBeNull();
  });
});

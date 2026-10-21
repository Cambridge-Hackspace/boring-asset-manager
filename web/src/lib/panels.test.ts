import { beforeEach, describe, expect, it } from 'vitest';
import { PANELS_DEFAULT, PANELS_MAX, PANELS_MIN, clampPanels, storePanels, storedPanels } from './panels';

beforeEach(() => localStorage.clear());

describe('clampPanels', () => {
  it('keeps a width between the least and the most', () => {
    expect(clampPanels(100)).toBe(PANELS_MIN);
    expect(clampPanels(5000)).toBe(PANELS_MAX);
    expect(clampPanels(300.4)).toBe(300);
  });

  it('leaves the canvas room', () => {
    expect(clampPanels(600, 800)).toBe(480);
    // With no room to give, the panels keep their least.
    expect(clampPanels(600, 300)).toBe(PANELS_MIN);
  });
});

describe('storedPanels', () => {
  it('is the default until a width is kept', () => {
    expect(storedPanels()).toBe(PANELS_DEFAULT);
    storePanels(400);
    expect(storedPanels()).toBe(400);
    storePanels(PANELS_DEFAULT);
    expect(localStorage.getItem('bam.editor-panels-width')).toBeNull();
  });

  it('ignores what it cannot read, and bounds what it can', () => {
    localStorage.setItem('bam.editor-panels-width', 'wide');
    expect(storedPanels()).toBe(PANELS_DEFAULT);
    localStorage.setItem('bam.editor-panels-width', '9000');
    expect(storedPanels()).toBe(PANELS_MAX);
  });
});

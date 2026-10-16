import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/svelte';
import ReportChart from './ReportChart.svelte';

afterEach(cleanup);

const bars = [{ label: 'Assets', points: [{ label: 'Tools', value: 3 }, { label: 'Filament', value: 2 }] }];

describe('ReportChart', () => {
  it('draws bars, described for screen readers, with the data as a table', () => {
    render(ReportChart, { kind: 'bar', series: bars, title: 'By category' });
    expect(screen.getByRole('img', { name: 'Bar chart of By category: Tools 3, Filament 2' })).toBeTruthy();
    const table = screen.getByRole('table');
    expect(within(table).getByRole('columnheader', { name: 'Assets' })).toBeTruthy();
    expect(within(table).getByRole('rowheader', { name: 'Filament' }).nextElementSibling!.textContent).toBe('2');
  });

  it('draws a donut with its total, and a whole ring for a single slice', () => {
    const { container } = render(ReportChart, { kind: 'donut', series: bars, title: '' });
    expect(screen.getByRole('img', { name: 'Donut chart of Assets: Tools 3, Filament 2' }).textContent).toContain('5');
    expect(container.querySelectorAll('path')).toHaveLength(2);
    cleanup();
    const one = render(ReportChart, { kind: 'donut', series: [{ label: 'Assets', points: [{ label: 'Tools', value: 4 }] }], title: '' });
    const d = one.container.querySelector('path')!.getAttribute('d')!;
    expect(d.match(/A 40 40/g)).toHaveLength(2);
  });

  it('draws a line per series over the periods', () => {
    const series = [
      { label: 'Used (g)', points: [{ label: 'Sep 2026', value: 100 }, { label: 'Oct 2026', value: 300 }] },
      { label: 'Used (pcs)', points: [{ label: 'Sep 2026', value: 0 }, { label: 'Oct 2026', value: 4 }] },
    ];
    const { container } = render(ReportChart, { kind: 'line', series, title: 'Use' });
    expect(screen.getByRole('img', { name: /^Line chart of Use: Used \(g\): Sep 2026 100, Oct 2026 300; Used \(pcs\)/ })).toBeTruthy();
    expect(container.querySelectorAll('polyline')).toHaveLength(2);
    expect(within(screen.getByRole('table')).getByRole('columnheader', { name: 'Period' })).toBeTruthy();
  });
});

import { describe, expect, it } from 'vitest';
import { labelCells, visibleHeading } from './stack';

describe('stacked tables', () => {
  it('labels each cell with its visible column heading', () => {
    const table = document.createElement('table');
    table.innerHTML = `<thead><tr><th>Name</th><th><span class="sr-only">Select</span></th><th>Due back</th></tr></thead>
      <tbody><tr><td>Drill</td><td><input type="checkbox" /></td><td>Friday</td></tr></tbody>`;
    labelCells(table);
    const cells = Array.from(table.tBodies[0]!.rows[0]!.cells);
    expect(cells.map((c) => c.getAttribute('data-label'))).toEqual(['Name', null, 'Due back']);
    expect(visibleHeading(table.tHead!.rows[0]!.cells[1]!)).toBe('');
  });
});

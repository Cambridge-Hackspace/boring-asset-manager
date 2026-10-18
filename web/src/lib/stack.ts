// Tables that become cards on a phone (app.css, .stack-table): each cell is
// labeled with its column's heading, kept as rows come and go. Headings
// only screen readers see, such as "Actions", label nothing.

/** The heading a column's cells are labeled with: its visible text. */
export function visibleHeading(th: Element): string {
  const clone = th.cloneNode(true) as Element;
  clone.querySelectorAll('.sr-only').forEach((e) => e.remove());
  return clone.textContent?.trim() ?? '';
}

/** Labels each body cell with its column's heading. */
export function labelCells(table: HTMLTableElement): void {
  const heads = Array.from(table.tHead?.rows[0]?.cells ?? []).map(visibleHeading);
  for (const body of Array.from(table.tBodies)) {
    for (const row of Array.from(body.rows)) {
      Array.from(row.cells).forEach((cell, i) => {
        const h = heads[i];
        if (h) cell.setAttribute('data-label', h);
        else cell.removeAttribute('data-label');
      });
    }
  }
}

/** A Svelte action: the table stacks into cards on a phone. */
export function stack(table: HTMLTableElement): { destroy: () => void } {
  table.classList.add('stack-table');
  labelCells(table);
  const observer = new MutationObserver(() => labelCells(table));
  observer.observe(table, { childList: true, subtree: true });
  return { destroy: () => observer.disconnect() };
}

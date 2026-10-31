import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/svelte';
import RunProgress from './RunProgress.svelte';

afterEach(cleanup);

describe('RunProgress', () => {
  it('says the step, and draws a segment a step, those reached filled', () => {
    const { container } = render(RunProgress, { flowName: 'Take filament', progress: { step: 2, of: 3 } });
    expect(screen.getByText('Take filament · Step 2 of 3')).toBeTruthy();
    const bar = container.querySelector('[data-progress-bar]')!;
    expect(bar.getAttribute('aria-hidden')).toBe('true');
    expect([...bar.children].map((s) => s.className.includes('bg-zinc-900'))).toEqual([true, true, false]);
  });

  it('says only the name with one step, or none to count', () => {
    const { container } = render(RunProgress, { flowName: 'Collect it', progress: { step: 1, of: 1 } });
    expect(screen.getByText('Collect it').textContent?.trim()).toBe('Collect it');
    expect(container.querySelector('[data-progress-bar]')).toBeNull();
    cleanup();
    render(RunProgress, { flowName: 'Collect it' });
    expect(screen.getByText('Collect it')).toBeTruthy();
  });
});

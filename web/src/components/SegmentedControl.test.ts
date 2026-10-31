import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import SegmentedControl from './SegmentedControl.svelte';

afterEach(() => cleanup());

describe('SegmentedControl', () => {
  it('marks the view shown, and asks for another', async () => {
    const onchange = vi.fn();
    render(SegmentedControl, {
      label: 'Show',
      options: [
        { value: 'ready', label: 'Ready' },
        { value: 'sent', label: 'Sent' },
      ],
      value: 'ready',
      onchange,
    });
    const group = screen.getByRole('group', { name: 'Show' });
    expect(group.querySelectorAll('[aria-pressed="true"]')).toHaveLength(1);
    expect(screen.getByRole('button', { name: 'Ready' }).getAttribute('aria-pressed')).toBe('true');
    await fireEvent.click(screen.getByRole('button', { name: 'Sent' }));
    expect(onchange).toHaveBeenCalledWith('sent');
    // Never a primary button: the screen keeps its one.
    expect(group.querySelector('.btn-primary')).toBeNull();
  });
});

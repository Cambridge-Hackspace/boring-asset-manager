import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import ConfirmButton from './ConfirmButton.svelte';

afterEach(() => cleanup());

describe('ConfirmButton', () => {
  it('asks before acting, and can be cancelled', async () => {
    const onconfirm = vi.fn();
    render(ConfirmButton, { label: 'Delete', question: 'Delete the drill?', onconfirm });

    await fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    expect(screen.getByRole('group', { name: 'Delete the drill?' })).toBeTruthy();
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Delete' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onconfirm).not.toHaveBeenCalled();

    await fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    expect(onconfirm).toHaveBeenCalledTimes(1);
  });
});

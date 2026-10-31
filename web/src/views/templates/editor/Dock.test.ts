import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import { createRawSnippet } from 'svelte';
import Dock from './Dock.svelte';

afterEach(() => {
  cleanup();
  localStorage.clear();
});

const content = (text: string) => createRawSnippet(() => ({ render: () => `<p>${text}</p>` }));

describe('Dock', () => {
  it('says what a panel holds beside its title, collapsed too, without renaming it', async () => {
    render(Dock, {
      panels: [
        { id: 'layers', title: 'Layers', summary: '5 layers', content: content('The list') },
        { id: 'arrange', title: 'Arrange', content: content('Align') },
      ],
    });
    const layers = screen.getByRole('button', { name: 'Layers' });
    const heading = layers.closest('h2')!;
    expect(heading.querySelector('[data-summary]')!.textContent).toBe('5 layers');
    await fireEvent.click(layers);
    expect(layers.getAttribute('aria-expanded')).toBe('false');
    expect(screen.getByText('The list').closest('[hidden]')).not.toBeNull();
    expect(heading.querySelector('[data-summary]')!.textContent).toBe('5 layers');
    expect(screen.getByRole('button', { name: 'Arrange' }).closest('h2')!.querySelector('[data-summary]')).toBeNull();
  });
});

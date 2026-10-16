import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/svelte';
import HelpPage from './HelpPage.svelte';

afterEach(cleanup);

describe('HelpPage', () => {
  it('shows a topic, with the others to choose from', () => {
    render(HelpPage, { params: { topic: 'stock' } });
    expect(screen.getByRole('heading', { level: 1, name: 'Stock' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Stock' }).getAttribute('aria-current')).toBe('page');
    expect(screen.getByRole('link', { name: 'Scanning' }).getAttribute('href')).toBe('/help/scanning');
    expect(screen.getByText(/Mark restocked/)).toBeTruthy();
  });

  it('starts at getting started, and says when a topic is missing', () => {
    render(HelpPage, { params: {} });
    expect(screen.getByRole('heading', { level: 1, name: 'Getting started' })).toBeTruthy();
    cleanup();
    render(HelpPage, { params: { topic: 'nope' } });
    expect(screen.getByRole('heading', { level: 1, name: 'No such help' })).toBeTruthy();
  });
});

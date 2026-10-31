import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/svelte';
import { createRawSnippet } from 'svelte';
import PageHeader from './PageHeader.svelte';

afterEach(cleanup);

describe('PageHeader', () => {
  it('titles the page, says what it is for, and holds its actions', () => {
    const actions = createRawSnippet(() => ({ render: () => '<a class="btn-primary" href="/users/new">New user</a>' }));
    render(PageHeader, { title: 'Users', lede: 'The people who can sign in.', actions });
    expect(screen.getByRole('heading', { level: 1, name: 'Users' })).toBeTruthy();
    expect(screen.getByText('The people who can sign in.')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'New user' })).toBeTruthy();
  });

  it('leaves out what it is not given', () => {
    const { container } = render(PageHeader, { title: 'Backups' });
    expect(container.querySelectorAll('p')).toHaveLength(0);
    expect(container.querySelectorAll('h1 + *')).toHaveLength(0);
  });
});

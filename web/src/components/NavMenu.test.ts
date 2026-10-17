import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import { navigate } from '../lib/router.svelte';
import NavMenu from './NavMenu.svelte';

afterEach(() => {
  cleanup();
  navigate('/', true);
});

const links = [
  { href: '/users', label: 'Users' },
  { href: '/roles', label: 'Roles' },
];

describe('NavMenu', () => {
  it('opens and closes from its button', async () => {
    render(NavMenu, { label: 'Admin', links, itemClass: '' });
    const button = screen.getByRole('button', { name: 'Admin' });
    expect(button.getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByRole('link', { name: 'Users' })).toBeNull();

    await fireEvent.click(button);
    expect(button.getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByRole('link', { name: 'Users' }).getAttribute('href')).toBe('/users');
    expect(button.getAttribute('aria-controls')).toBe(screen.getByRole('list').id);

    await fireEvent.click(button);
    expect(screen.queryByRole('link', { name: 'Users' })).toBeNull();
  });

  it('closes on Escape, returning focus to its button', async () => {
    render(NavMenu, { label: 'Admin', links, itemClass: '' });
    const button = screen.getByRole('button', { name: 'Admin' });
    await fireEvent.click(button);
    screen.getByRole('link', { name: 'Roles' }).focus();
    await fireEvent.keyDown(screen.getByRole('link', { name: 'Roles' }), { key: 'Escape' });
    expect(screen.queryByRole('link', { name: 'Roles' })).toBeNull();
    expect(document.activeElement).toBe(button);
  });

  it('closes on a click elsewhere, and on moving to another page', async () => {
    render(NavMenu, { label: 'Admin', links, itemClass: '' });
    const button = screen.getByRole('button', { name: 'Admin' });
    await fireEvent.click(button);
    await fireEvent.click(document.body);
    expect(screen.queryByRole('link', { name: 'Users' })).toBeNull();

    await fireEvent.click(button);
    navigate('/users');
    flushSync();
    expect(screen.queryByRole('link', { name: 'Users' })).toBeNull();
  });

  it('marks itself when it holds the current page', async () => {
    navigate('/roles', true);
    render(NavMenu, { label: 'Admin', links, itemClass: '' });
    const button = screen.getByRole('button', { name: 'Admin' });
    expect(button.hasAttribute('data-current')).toBe(true);
    await fireEvent.click(button);
    expect(screen.getByRole('link', { name: 'Roles' }).getAttribute('aria-current')).toBe('page');
    expect(screen.getByRole('link', { name: 'Users' }).getAttribute('aria-current')).toBeNull();
  });
});

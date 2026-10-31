import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import type { Category } from '../lib/models';
import CategoryPicker from './CategoryPicker.svelte';

afterEach(() => cleanup());

const cat = (id: string, name: string, parent_id: string | null = null) => ({ id, name, parent_id }) as Category;
const categories = [cat('s', 'Storage'), cat('p', "Members' projects", 's'), cat('b', "Members' boxes", 's'), cat('b1', 'Small boxes', 'b'), cat('t', 'Tools')];

function picker(value: string[] = []) {
  const props = { value };
  render(CategoryPicker, { categories, value, label: 'Files into', id: 'cats', onchange: (v: string[]) => (props.value = v) });
  return props;
}

describe('CategoryPicker', () => {
  it('shows what is chosen, each with the way down to it, and removes one', async () => {
    const props = picker(['p', 't']);
    const field = screen.getByRole('group', { name: 'Files into' });
    expect(within(field).getByRole('button', { name: "Remove Members' projects" }).textContent).toContain("Storage / Members' projects");
    await fireEvent.click(screen.getByRole('button', { name: 'Remove Tools' }));
    expect(props.value).toEqual(['p']);
  });

  it('opens one level at a time, goes down and back up, and chooses', async () => {
    const props = picker();
    await fireEvent.click(screen.getByRole('button', { name: 'Choose categories' }));
    expect(screen.getAllByRole('checkbox').map((c) => c.closest('label')!.textContent!.trim())).toEqual(['Storage', 'Tools']);

    await fireEvent.click(screen.getByRole('button', { name: 'Open Storage, 2 categories under it' }));
    expect(screen.getByRole('navigation', { name: 'Where you are' }).textContent).toContain('Storage');
    expect(screen.getAllByRole('checkbox').map((c) => c.closest('label')!.textContent!.trim())).toEqual(['Storage itself', "Members' boxes", "Members' projects"]);
    await fireEvent.click(screen.getByRole('checkbox', { name: "Members' boxes" }));
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Storage itself' }));
    expect(props.value).toEqual(['b', 's']);
    expect(screen.getByRole('status').textContent).toBe('2 chosen');

    await fireEvent.click(screen.getByRole('button', { name: 'All categories' }));
    expect(screen.getAllByRole('checkbox')).toHaveLength(2);
    await fireEvent.click(screen.getByRole('button', { name: 'Done' }));
    expect(screen.queryByRole('checkbox')).toBeNull();
  });

  it('searches every level', async () => {
    picker();
    await fireEvent.click(screen.getByRole('button', { name: 'Choose categories' }));
    await fireEvent.input(screen.getByLabelText('Find a category'), { target: { value: 'box' } });
    expect(screen.getAllByRole('checkbox').map((c) => c.closest('label')!.textContent!.trim())).toEqual([
      "Storage / Members' boxes",
      "Storage / Members' boxes / Small boxes",
    ]);
    await fireEvent.input(screen.getByLabelText('Find a category'), { target: { value: 'nothing like it' } });
    expect(screen.getByText('No category has that in its name.')).toBeTruthy();
  });

  it('moves with the arrow keys, opening with → and going up with ←', async () => {
    picker();
    await fireEvent.click(screen.getByRole('button', { name: 'Choose categories' }));
    const storage = screen.getByRole('checkbox', { name: 'Storage' });
    storage.focus();
    await fireEvent.keyDown(storage, { key: 'ArrowDown' });
    expect(document.activeElement).toBe(screen.getByRole('checkbox', { name: 'Tools' }));
    await fireEvent.keyDown(document.activeElement!, { key: 'ArrowUp' });
    await fireEvent.keyDown(document.activeElement!, { key: 'ArrowRight' });
    expect(document.activeElement).toBe(screen.getByRole('checkbox', { name: 'Storage itself' }));
    await fireEvent.keyDown(document.activeElement!, { key: 'ArrowLeft' });
    expect(screen.getByRole('checkbox', { name: 'Tools' })).toBeTruthy();
  });
});

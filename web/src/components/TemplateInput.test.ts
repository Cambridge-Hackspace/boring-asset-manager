import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import TemplateInputHarness from '../test/TemplateInputHarness.svelte';
import type { AvailableValue } from '../lib/flowEditor';

afterEach(() => cleanup());

const values: AvailableValue[] = [
  { key: 'today', label: 'Today’s date', from: 'Every run', input: { kind: 'date' } },
  { key: 'user', label: 'Who runs it', from: 'Every run', input: { kind: 'text' } },
  { key: 'owner', label: 'Whose is it?', from: 'Form: About it', input: { kind: 'text' } },
  {
    key: 'wood',
    label: 'Which wood?',
    from: 'Form: Long boards',
    note: 'Empty when Form: Long boards is skipped: it is shown only when {category_id} is “Long boards”.',
    input: { kind: 'text' },
  },
];

/** Types into the box as a person does: the text, with the cursor at its end or at. */
async function type(box: HTMLInputElement, text: string, at = text.length) {
  box.value = text;
  box.setSelectionRange(at, at);
  await fireEvent.input(box);
}

const options = () => screen.queryAllByRole('option').map((o) => o.textContent?.replace(/\s+/g, ' ').trim());

describe('TemplateInput', () => {
  it('suggests, as { is typed, the values it can use, narrowing as it goes, and inserts the one chosen at the cursor', async () => {
    render(TemplateInputHarness, { values, start: '' });
    const box = screen.getByRole('combobox', { name: 'Its name' }) as HTMLInputElement;
    expect(box.getAttribute('aria-expanded')).toBe('false');

    await type(box, 'Left by {');
    expect(box.getAttribute('aria-expanded')).toBe('true');
    // Each says what it is and where it comes from, and when it may be empty.
    expect(options()).toEqual([
      '{today} Today’s date Every run',
      '{user} Who runs it Every run',
      '{owner} Whose is it? Form: About it',
      '{wood} Which wood? Form: Long boards Empty when Form: Long boards is skipped: it is shown only when {category_id} is “Long boards”.',
    ]);
    expect(box.getAttribute('aria-activedescendant')).toBe(screen.getAllByRole('option')[0]!.id);

    // Narrowed by what follows the {, the key's start first.
    await type(box, 'Left by {o');
    expect(options().map((o) => o!.split(' ')[0])).toEqual(['{owner}', '{today}', '{wood}']);
    await fireEvent.keyDown(box, { key: 'ArrowDown' });
    await fireEvent.keyDown(box, { key: 'ArrowUp' });
    await fireEvent.keyDown(box, { key: 'ArrowUp' });
    expect(screen.getAllByRole('option')[2]!.getAttribute('aria-selected')).toBe('true');
    await fireEvent.keyDown(box, { key: 'ArrowDown' });
    expect(screen.getAllByRole('option')[0]!.getAttribute('aria-selected')).toBe('true');
    await fireEvent.keyDown(box, { key: 'Enter' });
    expect(screen.getByTestId('value').textContent).toBe('Left by {owner}');
    expect(box.getAttribute('aria-expanded')).toBe('false');
    expect(box.selectionStart).toBe('Left by {owner}'.length);

    // In the middle of the text, Tab inserts it there, leaving the rest.
    await type(box, 'On {us on {owner}', 'On {us'.length);
    await fireEvent.keyDown(box, { key: 'Tab' });
    expect(screen.getByTestId('value').textContent).toBe('On {user} on {owner}');
    expect(box.selectionStart).toBe('On {user}'.length);
  });

  it('closes on Escape, inserting nothing', async () => {
    render(TemplateInputHarness, { values, start: '' });
    const box = screen.getByRole('combobox', { name: 'Its name' }) as HTMLInputElement;
    await type(box, '{');
    await fireEvent.keyDown(box, { key: 'Escape' });
    expect(box.getAttribute('aria-expanded')).toBe('false');
    expect(screen.getByTestId('value').textContent).toBe('{');
  });

  it('inserts, from its button, a value where the cursor was, chosen with the pointer', async () => {
    render(TemplateInputHarness, { values, start: 'Hello ' });
    const box = screen.getByRole('combobox', { name: 'Its name' }) as HTMLInputElement;
    box.focus();
    box.setSelectionRange(6, 6);
    await fireEvent.click(screen.getByRole('button', { name: 'Insert a value into Its name' }));
    expect(box.getAttribute('aria-expanded')).toBe('true');
    expect(options()).toHaveLength(values.length);
    await fireEvent.pointerDown(screen.getByRole('option', { name: /\{user\}/ }));
    expect(screen.getByTestId('value').textContent).toBe('Hello {user}');
  });

  it('marks a name it cannot use there', async () => {
    render(TemplateInputHarness, { values, start: '{owner}: {what}' });
    const box = screen.getByRole('combobox', { name: 'Its name' }) as HTMLInputElement;
    expect(box.getAttribute('aria-invalid')).toBe('true');
    expect(screen.getByText('{what} is not set before this step.')).toBeTruthy();
    expect(box.getAttribute('aria-describedby')).toBe('t-name-unknown');
    await type(box, '{owner}');
    expect(box.getAttribute('aria-invalid')).toBeNull();
    expect(screen.queryByText(/is not set before this step/)).toBeNull();
  });
});

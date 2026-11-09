import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import PersonPicker from './PersonPicker.svelte';
import { fakeApi } from '../test/fetch';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const people = [
  { id: 'u1', name: 'Ada' },
  { id: 'u2', name: 'Cy', no_account: true },
];

describe('PersonPicker', () => {
  it('offers the people matching what is typed, in the scope given, and names those chosen', async () => {
    fakeApi({ 'GET /people/choices': () => ({ people, can_add: false }) });
    const { component } = render(PersonPicker, { id: 'keeper', multiple: true, categoryIds: ['c1'], known: { u9: { id: 'u9', name: 'Old' } }, value: 'u9' });
    expect(screen.getByText('Old')).toBeTruthy();
    const input = screen.getByRole('combobox');
    await fireEvent.focus(input);
    await fireEvent.input(input, { target: { value: 'c' } });
    const option = await screen.findByRole('option', { name: 'Cy (no account)' });
    expect(screen.queryByRole('option', { name: 'Ada' })).toBeNull();
    expect(screen.queryByRole('option', { name: /Add/ })).toBeNull();
    await fireEvent.mouseDown(option);
    expect(screen.getByRole('button', { name: 'Remove Cy' })).toBeTruthy();
    expect(component).toBeTruthy();
    // Backspace in an empty box takes the last one out.
    await fireEvent.keyDown(screen.getByRole('combobox'), { key: 'Backspace' });
    expect(screen.queryByRole('button', { name: 'Remove Cy' })).toBeNull();
    await fireEvent.click(screen.getByRole('button', { name: 'Remove Old' }));
    expect(screen.queryByText('Old')).toBeNull();
    const fetchCalls = (fetch as unknown as { mock: { calls: [string][] } }).mock.calls;
    expect(fetchCalls[0]![0]).toContain('category_id=c1');
  });

  it('adds a name matching no one as a person without an account, for those who may', async () => {
    const { sent } = fakeApi({
      'GET /people/choices': () => ({ people, can_add: true }),
      'POST /people': (b) => ({ status: 201, body: { person: { id: 'u3', name: (b as { name: string }).name, no_account: true } } }),
    });
    render(PersonPicker, { id: 'keeper' });
    const input = screen.getByRole('combobox');
    await fireEvent.focus(input);
    await fireEvent.input(input, { target: { value: 'Zed' } });
    const add = await screen.findByRole('option', { name: 'Add “Zed” as a person without an account' });
    await fireEvent.mouseDown(add);
    expect(await screen.findByText('Zed (no account)')).toBeTruthy();
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({ name: 'Zed' });
    // Naming one person, it offers to change them instead of another box.
    expect(screen.queryByRole('combobox')).toBeNull();
    expect(screen.getByRole('button', { name: 'Change' })).toBeTruthy();
  });

  it('keeps those the viewer cannot see, saying so', () => {
    fakeApi({});
    render(PersonPicker, { id: 'keeper', hidden: 2 });
    expect(screen.getByText('and 2 people you cannot see')).toBeTruthy();
  });
});

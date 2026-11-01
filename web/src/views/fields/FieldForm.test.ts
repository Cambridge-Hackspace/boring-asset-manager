import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import FieldForm from './FieldForm.svelte';
import { fakeApi } from '../../test/fetch';
import type { Field } from '../../lib/models';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const field = (over: Partial<Field> = {}): Field => ({
  id: 'f1',
  key: 'condition',
  label: 'Condition',
  validation_type: 'select',
  options: { choices: [{ label: 'Good', value: 'good' }], default: 'good' },
  created_at: '',
  updated_at: '',
  ...over,
});

describe('FieldForm', () => {
  it('creates a select field, suggesting the key from the label', async () => {
    const { sent } = fakeApi({ 'POST /fields': (b) => ({ status: 201, body: { field: { ...field(), ...(b as object) } } }) });
    const onsaved = vi.fn();
    render(FieldForm, { onsaved, oncancel: () => {} });

    await fireEvent.input(screen.getByLabelText('Label'), { target: { value: 'Shelf Location' } });
    expect((screen.getByLabelText('Key') as HTMLInputElement).value).toBe('shelf_location');

    await fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'select' } });
    // One box a choice: its value is made from its label.
    await fireEvent.input(screen.getByLabelText('Choice 1'), { target: { value: 'Top Shelf' } });
    expect(screen.getByText('top_shelf')).toBeTruthy();
    expect(screen.queryByLabelText('Value of choice 1')).toBeNull();
    await fireEvent.click(screen.getByRole('button', { name: 'Add choice' }));
    // A blank second choice is dropped rather than sent.
    await fireEvent.submit(screen.getByRole('button', { name: 'Create field' }));

    await vi.waitFor(() => expect(onsaved).toHaveBeenCalled());
    expect(sent[0]).toEqual({
      method: 'POST',
      path: '/fields',
      body: { key: 'shelf_location', label: 'Shelf Location', validation_type: 'select', options: { choices: [{ label: 'Top Shelf', value: 'top_shelf' }], default: null } },
    });
  });

  it('stops making a value from the label once it is changed by hand', async () => {
    const { sent } = fakeApi({ 'POST /fields': (b) => ({ status: 201, body: { field: { ...field(), ...(b as object) } } }) });
    render(FieldForm, { onsaved: () => {}, oncancel: () => {} });
    await fireEvent.input(screen.getByLabelText('Label'), { target: { value: 'Size' } });
    await fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'select' } });
    await fireEvent.input(screen.getByLabelText('Choice 1'), { target: { value: 'Small' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Change value of choice 1' }));
    await fireEvent.input(screen.getByLabelText('Value of choice 1'), { target: { value: 's' } });
    await fireEvent.input(screen.getByLabelText('Choice 1'), { target: { value: 'Smallest' } });
    // Two choices made the same are said to clash before the server refuses them.
    await fireEvent.click(screen.getByRole('button', { name: 'Add choice' }));
    await fireEvent.input(screen.getByLabelText('Choice 2'), { target: { value: 'S' } });
    expect(screen.getByText(/Two choices are stored as/).textContent).toContain('s');
    await fireEvent.input(screen.getByLabelText('Choice 2'), { target: { value: '10 mm' } });
    expect(screen.queryByText(/Two choices are stored as/)).toBeNull();
    await fireEvent.submit(screen.getByRole('button', { name: 'Create field' }));
    await vi.waitFor(() => expect(sent).toHaveLength(1));
    expect((sent[0]!.body as Field).options!.choices).toEqual([
      { label: 'Smallest', value: 's' },
      { label: '10 mm', value: '10_mm' },
    ]);
  });

  it('never changes a value already stored by itself, and warns before it is changed', async () => {
    const { sent } = fakeApi({ 'PUT /fields/f1': (b) => ({ field: { ...field(), ...(b as object) } }) });
    render(FieldForm, { field: field(), onsaved: () => {}, oncancel: () => {} });
    await fireEvent.input(screen.getByLabelText('Choice 1'), { target: { value: 'Very good' } });
    expect(screen.getByText('good')).toBeTruthy();
    expect(screen.queryByRole('note')).toBeNull();
    await fireEvent.click(screen.getByRole('button', { name: 'Change value of choice 1' }));
    expect(screen.getByRole('note').textContent).toBe('Assets that hold the old value keep it, and it will match no choice.');
    // A choice added while editing is made from its label.
    await fireEvent.click(screen.getByRole('button', { name: 'Add choice' }));
    await fireEvent.input(screen.getByLabelText('Choice 2'), { target: { value: 'Worn out' } });
    // Removing the first leaves the second as it was.
    await fireEvent.click(screen.getByRole('button', { name: 'Remove choice 1' }));
    await fireEvent.input(screen.getByLabelText('Choice 1'), { target: { value: 'Worn through' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(sent).toHaveLength(1));
    expect((sent[0]!.body as Field).options!.choices).toEqual([{ label: 'Worn through', value: 'worn_through' }]);
  });

  it('edits without sending the key, and shows server errors', async () => {
    const { sent } = fakeApi({ 'PUT /fields/f1': () => ({ status: 400, body: { error: 'select default value must match one of the available choices' } }) });
    render(FieldForm, { field: field(), onsaved: () => {}, oncancel: () => {} });
    expect((screen.getByLabelText('Key') as HTMLInputElement).disabled).toBe(true);
    await fireEvent.submit(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByRole('alert')).toHaveProperty('textContent', expect.stringContaining('default value must match'));
    expect(sent[0]!.body).toEqual({ label: 'Condition', validation_type: 'select', options: { choices: [{ label: 'Good', value: 'good' }], default: 'good' } });
  });
});

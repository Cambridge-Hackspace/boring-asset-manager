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
    await fireEvent.input(screen.getByLabelText('Choice 1 label'), { target: { value: 'Top' } });
    await fireEvent.input(screen.getByLabelText('Choice 1 value'), { target: { value: 'top' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Add choice' }));
    // A blank second choice is dropped rather than sent.
    await fireEvent.submit(screen.getByRole('button', { name: 'Create field' }));

    await vi.waitFor(() => expect(onsaved).toHaveBeenCalled());
    expect(sent[0]).toEqual({
      method: 'POST',
      path: '/fields',
      body: { key: 'shelf_location', label: 'Shelf Location', validation_type: 'select', options: { choices: [{ label: 'Top', value: 'top' }], default: null } },
    });
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

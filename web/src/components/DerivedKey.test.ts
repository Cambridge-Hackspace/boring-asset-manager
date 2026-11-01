import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import DerivedKey from './DerivedKey.svelte';
import DerivedKeyHarness from '../test/DerivedKeyHarness.svelte';

afterEach(() => cleanup());

describe('DerivedKey', () => {
  it('follows the label until the key is changed by hand, then keeps it', async () => {
    render(DerivedKeyHarness, { follows: true });
    const label = screen.getByLabelText('Label');
    await fireEvent.input(label, { target: { value: 'Shelf Location' } });
    expect(screen.getByText('{shelf_location}')).toBeTruthy();
    expect(screen.getByTestId('key').textContent).toBe('shelf_location');

    // Change opens it, focused; editing it stops it following the label.
    await fireEvent.click(screen.getByRole('button', { name: 'Change key of question 1' }));
    const key = screen.getByLabelText('Key of question 1') as HTMLInputElement;
    expect(document.activeElement).toBe(key);
    await fireEvent.input(key, { target: { value: 'shelf' } });
    await fireEvent.input(label, { target: { value: 'Where is it?' } });
    expect(screen.getByTestId('key').textContent).toBe('shelf');
  });

  it('leaves a key something depends on as it is, and says what a rename breaks', async () => {
    render(DerivedKeyHarness, { follows: false, start: 'owner', warning: 'Step 3 (Create) uses {owner}.' });
    await fireEvent.input(screen.getByLabelText('Label'), { target: { value: 'Whose is it?' } });
    expect(screen.getByTestId('key').textContent).toBe('owner');
    expect(screen.queryByRole('note')).toBeNull();
    // Warned before anything is typed: on pressing Change.
    await fireEvent.click(screen.getByRole('button', { name: 'Change key of question 1' }));
    expect(screen.getByRole('note').textContent).toBe('Step 3 (Create) uses {owner}.');
    expect(screen.getByLabelText('Key of question 1').getAttribute('aria-describedby')).toBe('q-key-warning');
  });

  it('says it is made from the label while there is none', () => {
    render(DerivedKey, { id: 'k', value: '', from: '', what: 'key devices listen for', lead: 'Devices listen for' });
    expect(screen.getByText('made from the label')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Change key devices listen for' }).textContent).toBe('Change');
  });
});

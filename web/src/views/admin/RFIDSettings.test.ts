import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import RFIDSettings from './RFIDSettings.svelte';
import { fakeApi } from '../../test/fetch';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('RFID settings', () => {
  it('asks for a company prefix and filter only for GIAI, and saves them', async () => {
    const { sent } = fakeApi({
      'GET /settings/rfid': () => ({ scheme: 'text', company_prefix: '', filter: 0, code_type: '' }),
      'PUT /settings/rfid': (body) => body,
    });
    render(RFIDSettings);
    const section = await screen.findByRole('region', { name: 'RFID tags' });
    const save = within(section).getByRole('button', { name: 'Save' }) as HTMLButtonElement;
    await vi.waitFor(() => expect(save.disabled).toBe(false));
    expect(within(section).queryByLabelText('GS1 company prefix')).toBeNull();

    await fireEvent.change(within(section).getByLabelText('Scheme'), { target: { value: 'giai-96' } });
    await fireEvent.input(within(section).getByLabelText('GS1 company prefix'), { target: { value: '0614141' } });
    await fireEvent.input(within(section).getByLabelText('Filter'), { target: { value: '3' } });
    await fireEvent.change(within(section).getByLabelText('Code a tag holds'), { target: { value: 'code128' } });
    await fireEvent.click(save);
    expect(await within(section).findByText('Saved.')).toBeTruthy();
    expect(sent.find((r) => r.method === 'PUT')!.body).toEqual({ scheme: 'giai-96', company_prefix: '0614141', filter: 3, code_type: 'code128' });
  });

  it('says why the server refused them', async () => {
    fakeApi({
      'GET /settings/rfid': () => ({ scheme: 'giai-202', company_prefix: '0614141', filter: 3, code_type: '' }),
      'PUT /settings/rfid': () => ({ status: 400, body: { error: 'invalid setting: GIAI needs a GS1 company prefix of 6 to 12 digits' } }),
    });
    render(RFIDSettings);
    const section = await screen.findByRole('region', { name: 'RFID tags' });
    const save = within(section).getByRole('button', { name: 'Save' }) as HTMLButtonElement;
    await vi.waitFor(() => expect(save.disabled).toBe(false));
    expect((within(section).getByLabelText('GS1 company prefix') as HTMLInputElement).value).toBe('0614141');
    await fireEvent.click(save);
    expect(await within(section).findByText(/GIAI needs a GS1 company prefix/)).toBeTruthy();
  });
});

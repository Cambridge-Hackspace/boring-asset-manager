import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/svelte';
import FlowPreview from './FlowPreview.svelte';
import type { Step } from '../../lib/flows';

afterEach(cleanup);

describe('FlowPreview', () => {
  it('shows each type of question as a phone would ask it', () => {
    const step: Step = {
      id: 'about',
      kind: 'form',
      title: 'About it',
      questions: [
        { key: 'name', label: 'What is it?', required: true },
        { key: 'note', label: 'Anything else?', type: 'long_text', help: 'Where it came from' },
        { key: 'boards', label: 'How many?', type: 'number' },
        { key: 'back', label: 'Back by', type: 'date', default: '{today}' },
        { key: 'fragile', label: 'Fragile?', type: 'yes_no' },
        { key: 'wood', label: 'Which wood?', type: 'choice', options: [{ value: 'oak', label: 'Oak' }, { value: 'pine' }] },
        { key: 'photo', label: 'A photo', type: 'photo' },
      ],
    };
    render(FlowPreview, { step, flowName: 'Leave something', categories: [] });
    const preview = screen.getByRole('figure', { name: 'Preview, as a phone shows it' });
    const asked = [...preview.querySelectorAll('[data-question]')].map((q) => `${q.getAttribute('data-question')}: ${q.textContent?.replace(/\s+/g, ' ').trim()}`);
    expect(asked).toEqual([
      'text: What is it? *',
      'long_text: Anything else? Where it came from',
      'number: How many? A number',
      'date: Back by {today}',
      'yes_no: Fragile? YesNo',
      'choice: Which wood? Oakpine',
      'photo: A photo Take a photo, or choose one',
    ]);
  });

  it('says a Set fields step asks nothing', () => {
    render(FlowPreview, { step: { id: 'set', kind: 'set_fields', fields: [{ field: 'owner', value: '{user}' }] }, flowName: 'Leave', categories: [] });
    expect(screen.getByText('Done as soon as it is reached; nothing is asked.')).toBeTruthy();
  });
});

import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/svelte';
import FlowPreview from './FlowPreview.svelte';
import type { Step } from '../../lib/flows';
import type { Category, RelationType } from '../../lib/models';

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

  it('shows a Link step as a phone asks it, or says it asks nothing (#264)', () => {
    const kits = { id: 'kits', name: 'Kits', color_hex: '' } as Category;
    const types = [{ id: 'contains', name: 'contains', inverse_name: 'contained in', is_symmetric: false, is_single: true, is_location: true, is_followed: false, created_at: '', updated_at: '', link_count: 0 }] satisfies RelationType[];
    const step: Step = {
      id: 'link',
      kind: 'link',
      title: 'Which kit?',
      relation_type: 'contains',
      find: { find_by: ['search', 'list'] },
      allow_create: true,
      create_category: 'kits',
      repeat: true,
    };
    const { unmount } = render(FlowPreview, { step, flowName: 'Add to a kit', categories: [], allCategories: [kits], relationTypes: types });
    expect(screen.getByText('Search by name')).toBeTruthy();
    expect(screen.getByText('Something to link to')).toBeTruthy();
    expect(screen.getByText('Not listed? Add it to Kits')).toBeTruthy();
    expect(screen.getByText('Then Add another, or Done.')).toBeTruthy();
    unmount();
    render(FlowPreview, { step: { id: 'link', kind: 'link', relation_type: 'contains', assets: ['k1'] }, flowName: 'Add to a kit', categories: [], relationTypes: types });
    expect(screen.getByText('Links it, contained in what you chose, asking nothing.')).toBeTruthy();
  });
});

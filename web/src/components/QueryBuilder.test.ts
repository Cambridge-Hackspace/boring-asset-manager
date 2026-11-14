import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import QueryBuilder from './QueryBuilder.svelte';
import QueryProblem from './QueryProblem.svelte';
import type { Field } from '../lib/models';

const diameter: Field = { id: 'f1', key: 'diameter', label: 'Diameter', validation_type: 'number', created_at: '', updated_at: '' };

afterEach(cleanup);

describe('QueryBuilder', () => {
  it('writes the query from the conditions picked, and searches with it', async () => {
    const onuse = vi.fn();
    render(QueryBuilder, { fields: [diameter], categories: [], signedIn: true, onuse });
    const built = screen.getByTestId('built-query');
    const use = screen.getByRole('button', { name: 'Search with this query' }) as HTMLButtonElement;
    expect(use.disabled).toBe(true);

    await fireEvent.change(screen.getByLabelText('Condition 1: what to search by'), { target: { value: 'field.diameter' } });
    await fireEvent.change(screen.getByLabelText('Condition 1: how to compare'), { target: { value: '>=' } });
    await fireEvent.input(screen.getByLabelText('Condition 1: value'), { target: { value: '1.75' } });
    expect(built.textContent).toBe('field.diameter >= 1.75');

    await fireEvent.click(screen.getByRole('button', { name: 'Add a condition' }));
    await fireEvent.change(screen.getByLabelText('Condition 2: what to search by'), { target: { value: 'condition' } });
    await fireEvent.click(screen.getByLabelText('any'));
    expect(built.textContent).toBe('field.diameter >= 1.75 OR condition = ok');

    await fireEvent.change(screen.getByLabelText('Sort by'), { target: { value: 'name' } });
    expect(built.textContent).toBe('field.diameter >= 1.75 OR condition = ok ORDER BY name');

    await fireEvent.click(screen.getByRole('button', { name: 'Remove condition 2' }));
    await fireEvent.click(use);
    expect(onuse).toHaveBeenCalledWith('field.diameter >= 1.75 ORDER BY name');
  });

  it('asks for no value where a comparison takes none', async () => {
    render(QueryBuilder, { fields: [diameter], categories: [], signedIn: false, onuse: () => {} });
    await fireEvent.change(screen.getByLabelText('Condition 1: what to search by'), { target: { value: 'field.diameter' } });
    await fireEvent.change(screen.getByLabelText('Condition 1: how to compare'), { target: { value: 'IS EMPTY' } });
    expect(screen.queryByLabelText('Condition 1: value')).toBeNull();
    expect(screen.getByTestId('built-query').textContent).toBe('field.diameter IS EMPTY');
  });
});

describe('QueryProblem', () => {
  it('underlines the part of the query at fault', () => {
    render(QueryProblem, { query: 'field.colour = red', problem: { message: 'no field has the key "colour"', start: 0, end: 12 } });
    expect(screen.getByRole('alert').textContent).toContain('no field has the key "colour"');
    const fault = document.querySelector('mark.query-fault');
    expect(fault?.textContent).toBe('field.colour');
    expect(fault?.nextSibling?.textContent).toBe(' = red');
  });

  it('marks the end of a query that stops too soon', () => {
    render(QueryProblem, { query: 'name =', problem: { message: 'expected a value after =', start: 6, end: 6 } });
    expect(document.querySelector('mark.query-fault')?.textContent).toBe(' ');
  });
});

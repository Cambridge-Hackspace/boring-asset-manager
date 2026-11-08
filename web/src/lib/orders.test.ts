import { describe, expect, it } from 'vitest';
import type { PurchaseOrder, PurchaseOrderLine } from './models';
import {
  describeOrderChange,
  describeReceipt,
  followQuantity,
  lineDrafts,
  lineRequests,
  numberingExample,
  numberingProblem,
  onOrderByThing,
  onOrderText,
  orderOverdue,
  orderProblem,
  orderRequest,
  receiptDrafts,
  receiptEffect,
  receiptRequests,
  stateLabel,
} from './orders';

const line = (over: Partial<PurchaseOrderLine>): PurchaseOrderLine => ({
  id: 'l1', part_number: 'M5-100', description: 'M5 bolts', pack_size: 100, pack_unit: 'pcs', quantity: 3, quantity_received: 0, ...over,
});
const order = (over: Partial<PurchaseOrder> = {}): PurchaseOrder => ({
  id: 'o1', number: 1, reference: 'PO-0001', vendor_id: 'v1', vendor_name: 'Acme', state: 'ordered', vendor_reference: '', ordered_on: '2026-11-01',
  expected_on: '', received_on: '', deliver_to: '', notes: '', lines: [], can_manage: true, created_at: '', updated_at: '', ...over,
});

describe('orders', () => {
  it('names states, and says when an order is overdue', () => {
    expect(stateLabel('partly_received')).toBe('Partly received');
    expect(orderOverdue(order({ expected_on: '2026-11-05' }), '2026-11-08')).toBe(true);
    expect(orderOverdue(order({ expected_on: '2026-11-09' }), '2026-11-08')).toBe(false);
    expect(orderOverdue(order({ state: 'received', expected_on: '2026-11-05' }), '2026-11-08')).toBe(false);
  });

  it('turns lines into requests, leaving out hidden prices, and finds what is wrong', () => {
    const o = order({ currency: 'GBP', lines: [line({ catalog_entry_id: 'e1', asset_id: 'a1', unit_price: '4.50', quantity_received: 1 })] });
    const drafts = lineDrafts(o);
    expect(lineRequests(drafts)).toEqual([
      { id: 'l1', catalog_entry_id: 'e1', part_number: 'M5-100', description: 'M5 bolts', pack_size: 100, pack_unit: 'pcs', quantity: 3, unit_price: '4.50' },
    ]);
    expect(lineRequests(drafts, true)[0]).not.toHaveProperty('unit_price');
    const form = orderRequest(o);
    expect(orderProblem(form, drafts)).toBe('');
    expect(orderProblem({ ...form, vendor_id: '' }, drafts)).toBe('Choose the vendor it is to.');
    expect(orderProblem(form, [{ ...drafts[0]!, quantity: 0 }])).toMatch(/1 to 1000000/);
    expect(orderProblem(form, [{ ...drafts[0]!, quantity: 1, received: 2 }])).toMatch(/cannot order fewer/);
    expect(orderProblem(form, [{ ...drafts[0]!, unit_price: '£4' }])).toMatch(/without a currency symbol/);
    expect(orderProblem({ ...form, currency: '' }, drafts)).toMatch(/currency/);
    expect(orderProblem(form, [{ ...drafts[0]!, part_number: '', description: ' ' }])).toMatch(/part number or a description/);
  });

  it('starts a receipt with all that is to come, and keeps its defaults following the packs', () => {
    const o = order({
      lines: [
        line({ asset_id: 'a1', linked_name: 'Bin 3', quantity_received: 1 }),
        line({ id: 'l2', category_id: 'c1', linked_name: 'Filament', pack_size: null, quantity: 2 }),
        line({ id: 'l3', part_number: '', description: 'Delivery', pack_size: null, pack_unit: '', quantity: 1 }),
        line({ id: 'l4', quantity_received: 3 }),
      ],
    });
    const drafts = receiptDrafts(o);
    expect(drafts.map((d) => [d.line.id, d.quantity, d.restock, d.assets])).toEqual([
      ['l1', 2, '200', '2'],
      ['l2', 2, '', '2'],
      ['l3', 1, '', '1'],
    ]);
    expect(receiptEffect(drafts[0]!, 'pcs')).toBe('Adds 200 pcs to Bin 3.');
    expect(receiptEffect(drafts[1]!)).toBe('Makes 2 assets in Filament.');
    expect(receiptEffect(drafts[2]!)).toBe('Counted in; nothing is kept for it.');
    const one = followQuantity(drafts[0]!, 1);
    expect([one.quantity, one.restock]).toEqual([1, '100']);
    // A restock typed by hand stays as typed.
    expect(followQuantity({ ...drafts[0]!, restock: '150' }, 1).restock).toBe('150');

    drafts[1] = { ...drafts[1]!, like: 'a9', warranty_end: '2028-01-01' };
    drafts[2] = { ...drafts[2]!, on: false };
    expect(receiptRequests(drafts)).toEqual([
      { line_id: 'l1', quantity: 2, restock: 200 },
      { line_id: 'l2', quantity: 2, assets: 2, like: 'a9', warranty_end: '2028-01-01' },
    ]);
  });

  it('says what changed, what came, and what is on order', () => {
    expect(describeOrderChange({ what: 'line', to: '3 × M5-100 M5 bolts (100 pcs)' })).toBe('Line added: 3 × M5-100 M5 bolts (100 pcs)');
    expect(describeOrderChange({ what: 'price', label: 'M5-100', from: '4.50 GBP', to: '4.25 GBP' })).toBe('Price of M5-100: 4.50 GBP → 4.25 GBP');
    expect(describeOrderChange({ what: 'expected_on', to: '2026-12-01' })).toBe('Expected: 2026-12-01');
    expect(
      describeReceipt({
        id: 'e', kind: 'purchase_order.received', created_at: '',
        payload: { order_lines: [{ line_id: 'l1', part_number: 'M5-100', quantity: 2, restocked: 200 }, { line_id: 'l2', description: 'PLA', quantity: 1, assets: [{ id: 'a', name: 'Spool' }] }] },
      }),
    ).toEqual(['2 × M5-100: restocked by 200', '1 × PLA: made Spool']);
    const older = order({ id: 'o1', number: 1, lines: [line({ asset_id: 'a1' })] });
    const newer = order({ id: 'o2', number: 2, reference: 'PO-0002', state: 'draft', lines: [line({ asset_id: 'a1' }), line({ category_id: 'c1' })] });
    const done = order({ id: 'o3', number: 3, state: 'received', lines: [line({ category_id: 'c2' })] });
    const by = onOrderByThing([newer, older, done]);
    expect(by.get('a1')?.id).toBe('o1');
    expect(by.get('c1')?.id).toBe('o2');
    expect(by.has('c2')).toBe(false);
    expect(onOrderText(newer)).toBe('In draft PO-0002');
    expect(onOrderText(older)).toBe('On order: PO-0001');
  });

  it('shows and checks how numbers are shown', () => {
    expect(numberingExample('PO-', 4)).toBe('PO-0012');
    expect(numberingExample('', 0)).toBe('12');
    expect(numberingProblem('PO-', 4)).toBe('');
    expect(numberingProblem('', 0)).toBe('');
    expect(numberingProblem('PO/', 4)).toMatch(/letters/);
    expect(numberingProblem('P'.repeat(17), 4)).toMatch(/16/);
    expect(numberingProblem('PO-', 10)).toMatch(/0 to 9/);
  });
});

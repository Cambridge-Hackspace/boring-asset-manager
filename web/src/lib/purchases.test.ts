import { describe, expect, it } from 'vitest';
import { hasPurchase, priceProblem, priceText, purchaseRequest, warrantyEnded } from './purchases';
import { describeChange, describeEvent } from './assets';
import type { Purchase } from './models';

const bought: Purchase = {
  vendor_id: 'v1',
  vendor_name: 'Acme',
  order_date: '2026-01-05',
  received_date: '',
  warranty_end: '2028-01-09',
  price: '129.99',
  currency: 'GBP',
  order_number: 'PO-17',
};

describe('purchases', () => {
  it('says whether anything is recorded, or hidden', () => {
    expect(hasPurchase(undefined)).toBe(false);
    expect(hasPurchase({ vendor_id: '', vendor_name: '', order_date: '', received_date: '', warranty_end: '' })).toBe(false);
    expect(hasPurchase(bought)).toBe(true);
    expect(hasPurchase({ vendor_id: '', vendor_name: '', order_date: '', received_date: '', warranty_end: '', prices_hidden: true })).toBe(true);
  });

  it('writes a price with its currency', () => {
    expect(priceText(bought)).toBe('129.99 GBP');
    expect(priceText({ ...bought, price: undefined })).toBe('');
  });

  it('starts the form from the asset, every detail present', () => {
    expect(purchaseRequest(bought)).toEqual({
      vendor_id: 'v1',
      order_date: '2026-01-05',
      received_date: '',
      warranty_end: '2028-01-09',
      price: '129.99',
      currency: 'GBP',
      order_number: 'PO-17',
      invoice_attachment_id: '',
    });
    expect(Object.values(purchaseRequest(undefined)).every((v) => v === '')).toBe(true);
  });

  it('checks a price as typed', () => {
    const r = purchaseRequest(undefined);
    expect(priceProblem({ ...r, price: '12.50', currency: 'gbp' })).toBe('');
    expect(priceProblem({ ...r, price: '' })).toBe('');
    expect(priceProblem({ ...r, price: '£12', currency: 'GBP' })).toMatch(/without a currency symbol/);
    expect(priceProblem({ ...r, price: '12' })).toMatch(/currency/);
  });

  it('says whether a warranty has ended', () => {
    const today = new Date(2027, 2, 31);
    expect(warrantyEnded('2027-03-30', today)).toBe(true);
    expect(warrantyEnded('2027-03-31', today)).toBe(false);
    expect(warrantyEnded('', today)).toBe(false);
  });

  it('describes changes to purchase details, and a warranty ending, in history', () => {
    expect(describeChange({ what: 'vendor', from: 'Acme', to: 'Bolts R Us' }, 'asset.edited')).toBe('Vendor: Acme → Bolts R Us');
    expect(describeChange({ what: 'price', to: '12.50 GBP' }, 'asset.edited')).toBe('Price: 12.50 GBP');
    expect(describeChange({ what: 'warranty_end', from: '2027-01-01' }, 'asset.edited')).toBe('Warranty ends: 2027-01-01 removed');
    expect(describeChange({ what: 'invoice', from: 'invoice.pdf' }, 'asset.edited')).toBe('No longer the invoice: invoice.pdf');
    expect(describeEvent({ id: 'e', kind: 'warranty.expiring', created_at: '', payload: { trigger: 'schedule', due_at: '2027-03-31T00:00:00Z' } })).toBe(
      'Warranty ending on 2027-03-31',
    );
  });
});

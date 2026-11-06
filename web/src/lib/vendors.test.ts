import { describe, expect, it } from 'vitest';
import type { Vendor } from './models';
import { contactSummary, describeVendorChange, describeVendorEvent, mainContact, reorderHref, siteName, telHref, websiteHref } from './vendors';

const vendor = (over: Partial<Vendor> = {}): Vendor => ({
  id: 'v 1', name: 'Acme', website: '', account_number: '', address: '', notes: '', contacts: [], scope_ids: [], created_at: '', updated_at: '', ...over,
});

describe('vendor history', () => {
  it('says what an event was', () => {
    expect(describeVendorEvent({ id: 'e', kind: 'vendor.created', created_at: '', payload: {} })).toBe('Created');
    expect(describeVendorEvent({ id: 'e', kind: 'vendor.edited', created_at: '', payload: {} })).toBe('Edited');
  });

  it('says what each change was', () => {
    expect(describeVendorChange({ what: 'name', to: 'Acme' })).toBe('Name: Acme');
    expect(describeVendorChange({ what: 'account_number', from: 'A1', to: 'A2' })).toBe('Account number: A1 → A2');
    expect(describeVendorChange({ what: 'website', from: 'https://acme.example' })).toBe('Website: https://acme.example removed');
    expect(describeVendorChange({ what: 'notes' })).toBe('Notes changed');
    expect(describeVendorChange({ what: 'contact', key: 'c1', to: 'Ada (sales)' })).toBe('Contact added: Ada (sales)');
    expect(describeVendorChange({ what: 'contact', key: 'c1', from: 'Ada (sales)', to: 'Ada (support)' })).toBe('Contact: Ada (sales) → Ada (support)');
    expect(describeVendorChange({ what: 'contact', key: 'c1', from: 'Ada (sales)' })).toBe('Contact removed: Ada (sales)');
    expect(describeVendorChange({ what: 'scope', to: 'workshop' })).toBe('Put in the scope workshop');
    expect(describeVendorChange({ what: 'scope', from: 'workshop' })).toBe('Taken out of the scope workshop');
    expect(describeVendorChange({ what: 'attachment', to: 'Price list' })).toBe('Attachment added: Price list');
  });
});

describe('vendor websites', () => {
  it('shows a website compactly', () => {
    expect(siteName('https://www.acme.example/')).toBe('acme.example');
    expect(siteName('http://acme.example/shop')).toBe('acme.example/shop');
  });

  it('opens only web addresses', () => {
    expect(websiteHref('https://acme.example')).toBe('https://acme.example');
    expect(websiteHref('acme.example')).toBe('https://acme.example');
    expect(websiteHref('javascript:alert(1)')).toBeNull();
    expect(websiteHref('JaVaScRiPt:alert(1)')).toBeNull();
    expect(websiteHref('')).toBeNull();
  });

  it('reorders from its website, or from its page here when it has none', () => {
    expect(reorderHref(vendor({ website: 'https://acme.example/order' }))).toEqual({ href: 'https://acme.example/order', external: true });
    expect(reorderHref(vendor())).toEqual({ href: '/vendors/v%201', external: false });
  });
});

describe('vendor contacts', () => {
  it('shows the first contact, by name or how to reach them, with their role', () => {
    const ada = { id: 'c1', name: 'Ada', role: 'sales', email: 'ada@acme.example', phone: '' };
    expect(mainContact(vendor({ contacts: [ada, { ...ada, id: 'c2', name: 'Bob' }] }))).toEqual(ada);
    expect(mainContact(vendor())).toBeUndefined();
    expect(contactSummary(ada)).toBe('Ada (sales)');
    expect(contactSummary({ name: '', role: '', email: '', phone: '+44 1223' })).toBe('+44 1223');
  });

  it('dials a phone number by its digits', () => {
    expect(telHref('+44 (0)1223 555-010')).toBe('tel:+4401223555010');
    expect(telHref('01223 555010')).toBe('tel:01223555010');
  });
});

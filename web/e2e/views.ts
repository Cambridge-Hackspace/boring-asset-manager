import type { Page } from '@playwright/test';
import { unique } from './fixtures';

// Every view, with data that shows each of its states: for the accessibility
// audit (a11y.spec.ts) and the look's gate (gate.spec.ts, #156). Each is a path
// and the heading it shows, or '' for one named after its contents.

export async function seedViews(page: Page): Promise<[string, string][]> {
  const request = page.context().request;
  const post = async (path: string, body: unknown) => (await (await request.post('/api/v1' + path, { data: body })).json()) as any;
  // A field with a long key, and a token holding a role, so their lists show
  // a row each, as narrow as a phone makes them.
  await post('/fields', { key: unique('bearing_bore_in_millimeters').replace(/-/g, '_'), label: unique('Bearing bore in millimeters'), validation_type: 'number' });
  const reader = await post('/roles', { name: unique('Readers'), description: '', permissions: ['assets.read'] });
  await post('/tokens', { name: unique('Stock dashboard'), role_ids: [reader.role.id] });
  const cat = await post('/categories',{ name: unique('A11y'), color_hex: '#7c3aed', is_consumable: true, is_lendable: true, is_running_low: true });
  const asset = await post('/assets', { name: unique('Ladder'), category_ids: [cat.category.id] });
  // Counted by quantity and running low, with some history.
  const screws = await post('/assets', { name: unique('Screws'), stock: { quantity: 30, unit: 'pcs', low_stock_threshold: 20 } });
  await post(`/assets/${screws.asset.asset.id}/quantity`, { adjust: -15 });
  const archived = await post('/assets', { name: unique('Old saw'), generator_keys: [{ generator_type: 'qr', assigned_code: unique('OLD').toUpperCase() }] });
  await request.delete(`/api/v1/assets/${archived.asset.asset.id}`);
  // With a photo.
  await request.post(`/api/v1/assets/${asset.asset.asset.id}/photos`, {
    multipart: {
      file: {
        name: 'ladder.png',
        mimeType: 'image/png',
        buffer: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64'),
      },
    },
  });
  // In a box, so its page shows a location and links.
  const box = await post('/assets', { name: unique('Box') });
  const { relation_types } = await (await request.get('/api/v1/relation-types')).json();
  const contains = relation_types.find((r: { name: string }) => r.name === 'contains');
  await post(`/assets/${box.asset.asset.id}/links`, { type_id: contains.id, asset_id: asset.asset.asset.id, outgoing: true });
  // Out of service, so its page, the list and the home page show it.
  await request.put(`/api/v1/assets/${asset.asset.asset.id}/condition`, { data: { condition: 'out_of_service', note: 'Wobbly rung' } });
  const { template } = await post('/templates', {
    name: unique('A11y'), units: 'mm', width: 62, height: 29, dpi: 300,
    elements: [{ type: 'text', x: 2, y: 2, width: 50, height: 10, font_size: 12, content_binding: '{{asset.name}}' }],
  });

  const { reports } = await (await request.get('/api/v1/reports')).json();
  const charts = await post('/reports', {
    name: unique('Charts'),
    spec: {
      filter: {},
      sections: [
        { type: 'chart', title: 'By category', chart: 'bar', group_by: 'category', measure: 'assets' },
        { type: 'chart', title: 'By unit', chart: 'donut', group_by: 'unit', measure: 'quantity' },
        { type: 'chart', title: 'Used', chart: 'line', measure: 'used', period: 'week' },
      ],
    },
  });
  // An action its category offers, for the asset and actions pages.
  const identify = await post('/actions', { key: unique('identify').toLowerCase(), label: 'Identify', confirm: 'Light it up?' });
  await request.put(`/api/v1/categories/${cat.category.id}/actions`, { data: { actions: [{ action_id: identify.action.id, cascades: true }] } });
  // A printer in a pool, for the printers page.
  const a11yPrinter = await post('/printers', { name: unique('A11y QL'), driver: 'brother_ql', model: 'QL-810W', media: 'dk-22205', connection: { type: 'tcp', address: '127.0.0.1:9' } });
  await post('/printer-pools', { name: unique('A11y pool'), members: [{ printer_id: a11yPrinter.printer.id, share: 2 }] });
  // Custom stock loaded in a Rollo, for the printers page's stock.
  const bins = await post('/label-stock', { name: unique('A11y bins'), width_mm: 60, length_mm: 40, gap_mm: 3 });
  await post('/printers', { name: unique('A11y Rollo'), driver: 'tspl', model: 'Rollo X1038', media: bins.stock.id, connection: { type: 'tcp', address: '127.0.0.1:9' } });
  // One waiting for the QL with its name where the QL does not print, so
  // the spool shows a warning.
  const edge = await post('/templates', {
    name: unique('A11y edge'), units: 'mm', width: 62, height: 29, dpi: 300,
    elements: [{ type: 'text', x: 0, y: 2, width: 50, height: 10, font_size: 12, content_binding: '{{asset.name}}' }],
  });
  // An adaptive template whose code gives way to its name, for the
  // editor's adaptive settings and previews.
  const adaptive = await post('/templates', {
    name: unique('A11y adaptive'), units: 'mm', width: 62, height: 29, dpi: 300, sizing: 'adaptive',
    adapt: { min_width: 25, max_width: 102, min_height: 12, max_height: 60, continuous_length: 'proportional' },
    elements: [
      { type: 'generator', generator_type: 'qr', x: 2, y: 2, width: 25, height: 25, anchor_x: 'start', min_size: 15, when_too_small: 'alternative',
        alternative: { type: 'text', content_binding: '{{asset.name}}', font_size: 8 } },
      { type: 'text', x: 30, y: 2, width: 30, height: 10, font_size: 12, anchor_x: 'both', content_binding: '{{asset.name}}' },
    ],
  });
  const clamp = await post('/assets', { name: unique('Clamp') });
  await post('/spool', { asset_ids: [clamp.asset.asset.id], template_id: edge.template.id, destination: `printer:${a11yPrinter.printer.id}` });
  // Labels waiting in the print queue, one ready and one with problems.
  await post('/spool', { asset_ids: [asset.asset.asset.id], template_id: template.id, destination: 'download' });
  await post('/spool', { asset_ids: [box.asset.asset.id] });
  // And one downloaded, for the Jobs and Printed views.
  await post('/spool', { asset_ids: [screws.asset.asset.id], template_id: template.id, destination: 'download' });
  // A flow over the category, from its starter, for the flow pages.
  const { starters } = await (await request.get(`/api/v1/flow-starters?category_ids=${cat.category.id}`)).json();
  const flowSlug = unique('a11y-leave').toLowerCase();
  await post('/flows', { slug: flowSlug, name: unique('Leave it'), definition: starters[0].definition });
  const { entries } = await (await request.get('/api/v1/spool')).json();
  const screwsLabel = entries.find((e: { asset_id: string }) => e.asset_id === screws.asset.asset.id);
  await request.post('/api/v1/spool/download', { data: { entry_ids: [screwsLabel.id], format: 'pdf' } });
  return [
    ['/', 'Welcome'],
    ['/assets', 'Assets'],
    ['/assets/new', 'New asset'],
    ['/assets/import', 'Import assets'],
    [`/assets/${asset.asset.asset.id}`, ''],
    [`/assets/${asset.asset.asset.id}/edit`, 'Edit asset'],
    [`/assets/${screws.asset.asset.id}`, ''],
    [`/assets/${screws.asset.asset.id}/edit`, 'Edit asset'],
    [`/scan/${screws.asset.asset.id}`, ''],
    ['/scan', 'Scan'],
    ['/assets?archived=true', 'Assets'],
    ['/assets?condition=problem', 'Assets'],
    [`/assets/${archived.asset.asset.id}`, ''],
    ['/categories', 'Categories'],
    [`/categories/${cat.category.id}`, ''],
    [`/categories/${cat.category.id}?tab=fields`, ''],
    [`/categories/${cat.category.id}?tab=stock`, ''],
    [`/categories/${cat.category.id}?tab=labels`, ''],
    [`/categories/${cat.category.id}?tab=icon`, ''],
    [`/categories/${cat.category.id}?tab=links`, ''],
    [`/categories/${cat.category.id}?tab=scopes`, ''],
    ['/categories/new', 'New category'],
    ['/fields', 'Fields'],
    ['/templates', 'Label templates'],
    ['/templates/new', 'New template'],
    [`/templates/${template.id}/edit`, ''],
    [`/templates/${adaptive.template.id}/edit`, ''],
    ['/images', 'Images'],
    ['/icons', 'Icons'],
    ['/icons?tab=library', 'Icons'],
    ['/icons?tab=palette', 'Icons'],
    ['/icons?tab=states', 'Icons'],
    ['/icons?tab=settings', 'Icons'],
    ['/printers', 'Printers'],
    ['/notifications', 'Notifications'],
    ['/users', 'Users'],
    ['/roles', 'Roles'],
    ['/tokens', 'API tokens'],
    ['/backups', 'Backups'],
    ['/packages', 'Import & export'],
    ['/loans', 'Loans'],
    ['/flows', 'Flows'],
    ['/flows/new', 'New flow'],
    [`/flows/${flowSlug}/edit`, ''],
    [`/flows/${flowSlug}/poster`, ''],
    [`/f/${flowSlug}`, ''],
    ['/spool', 'Print queue'],
    ['/relations', 'Link types'],
    ['/actions', 'Actions'],
    ['/reports', 'Reports'],
    ['/reports/new', 'New report'],
    ['/scan-templates/new', 'New scan template'],
    [`/reports/${reports[0].id}`, ''],
    [`/reports/${charts.report.id}`, ''],
    ['/account', 'Your account'],
    ['/help/scanning', 'Scanning'],
    ['/help/spool', 'The print queue'],
    ['/help/actions', 'Actions'],
    ['/no-such-page', 'Page not found'],
  ];
}

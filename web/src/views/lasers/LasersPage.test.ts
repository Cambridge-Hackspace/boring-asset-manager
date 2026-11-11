import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import LasersPage from './LasersPage.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { DriverInfo, LaserJobAnswer, Material, Printer } from '../../lib/models';

const birch: Material = { id: 'birch', name: 'Birch plywood', engrave: { power: 30, speed: 150, passes: 1, lpi: 254 }, cut: { power: 100, speed: 8, passes: 2 }, preset: true };
const laser: Printer = { id: 'l1', name: 'Workshop laser', driver: 'test-laser', model: 'panel', media: '', connection: { type: 'tcp', address: 'laser.test:9100' }, created_at: '', updated_at: '' };
const drivers: DriverInfo[] = [
  {
    driver: 'test-laser', models: ['panel'], media: [],
    one_shot: [{ id: 'panel', work_width_mm: 200, work_height_mm: 150, materials: [birch], start_button: false, frames: true, interlocks: ['lid'], camera: false }],
  },
];
const prepared = (state: 'prepared' | 'running'): LaserJobAnswer => ({
  job: {
    id: 'j1', printer_id: 'l1', printer_name: 'Workshop laser', template_id: 't1', template_name: 'Tag', asset_id: null, material: birch,
    placement: { workpiece: { x: 10, y: 10, width: 60, height: 30 }, label: { x: 15, y: 15, width: 50, height: 20 }, mirror: false, cuts: 1 },
    state, created_at: '2026-11-10T10:00:00Z', started_at: null, ended_at: null,
  },
  held: true,
  cuts: [{ points: [{ x: 15, y: 15 }, { x: 65, y: 15 }, { x: 65, y: 35 }], closed: true }],
  warnings: [],
});

function api(lid: boolean) {
  return fakeApi({
    'GET /printers': () => ({ printers: [laser] }),
    'GET /printers/drivers': () => ({ drivers }),
    'GET /templates': () => ({ templates: [{ id: 't1', name: 'Tag', units: 'mm', width: 50, height: 20, dpi: 300 }] }),
    'GET /printers/l1/materials': () => ({ materials: [birch] }),
    'GET /printers/l1/jobs': () => ({ jobs: [] }),
    // An answer with a status field is sent as a body, not taken for the fake's envelope.
    'GET /printers/l1/status': () => ({
      body: { status: { state: 'idle', progress: 0, interlocks: [{ name: 'lid', ok: lid, message: lid ? undefined : 'The lid is open' }] }, model: drivers[0]!.one_shot![0], job: null },
    }),
    'POST /printers/l1/jobs': () => ({ status: 201, body: prepared('prepared') }),
    'POST /printers/l1/jobs/j1/start': () => prepared('running'),
  });
}

beforeEach(() => {
  session.permissions = ['labels.print', 'lasers.run'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('LasersPage', () => {
  it('prepares a job on a workpiece, shows where it goes, and starts it', async () => {
    const { sent } = api(true);
    render(LasersPage, { params: {} });
    expect((await screen.findAllByText(/Engraves at 30% and 150 mm\/s/)).length).toBe(2);
    expect((screen.getByLabelText('Width') as HTMLInputElement).value).toBe('50');
    await fireEvent.input(screen.getByLabelText('Width'), { target: { value: '60' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Prepare' }));
    await vi.waitFor(() =>
      expect(sent.find((s) => s.method === 'POST' && s.path === '/printers/l1/jobs')?.body).toEqual({
        template_id: 't1', material: 'birch', workpiece: { width_mm: 60, height_mm: 20, x_mm: 0, y_mm: 0 },
      }),
    );
    expect(await screen.findByRole('img', { name: /Where the label goes/ })).toBeTruthy();
    // Once prepared, Start is the one primary button.
    expect(screen.getByRole('button', { name: 'Prepare again' }).className).toContain('btn-secondary');
    const start = screen.getByRole('button', { name: 'Start' });
    expect(start.className).toContain('btn-primary');
    await fireEvent.click(start);
    await vi.waitFor(() => expect(sent.some((s) => s.method === 'POST' && s.path === '/printers/l1/jobs/j1/start')).toBe(true));
  });

  it('offers no Start while an interlock reports a problem, nor to someone who may not start lasers', async () => {
    api(false);
    render(LasersPage, { params: {} });
    await fireEvent.submit(await screen.findByRole('button', { name: 'Prepare' }));
    expect(await screen.findByText('Lid: The lid is open')).toBeTruthy();
    expect(((await screen.findByRole('button', { name: 'Start' })) as HTMLButtonElement).disabled).toBe(true);

    cleanup();
    session.permissions = ['labels.print'];
    api(true);
    render(LasersPage, { params: {} });
    await fireEvent.submit(await screen.findByRole('button', { name: 'Prepare' }));
    expect(await screen.findByText(/Someone allowed to start lasers frames and starts it/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Start' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Frame' })).toBeNull();
  });

  it('measures the workpiece with the camera, and frames only once the plan is confirmed over the photo', async () => {
    const withCamera = structuredClone(drivers);
    withCamera[0]!.one_shot![0]!.camera = true;
    const placed = prepared('prepared');
    placed.job.placement = { ...placed.job.placement, angle_deg: 9, placed: { x: 13, y: 13, width: 50, height: 20 }, camera: true, z_mm: 3 };
    const { sent } = fakeApi({
      'GET /printers': () => ({ printers: [laser] }),
      'GET /printers/drivers': () => ({ drivers: withCamera }),
      'GET /templates': () => ({ templates: [{ id: 't1', name: 'Tag', units: 'mm', width: 50, height: 20, dpi: 300 }] }),
      'GET /printers/l1/materials': () => ({ materials: [birch] }),
      'GET /printers/l1/jobs': () => ({ jobs: [] }),
      'GET /printers/l1/status': () => ({ body: { status: { state: 'idle', progress: 0, interlocks: [] }, model: withCamera[0]!.one_shot![0], job: null } }),
      'GET /printers/l1/calibration': () => ({
        camera: true, max_error_mm: 0.5, height_tolerance_mm: 1,
        calibration: { printer_id: 'l1', model: 'panel', image_width: 800, image_height: 600, z_mm: 3, marks: 9, rms_mm: 0.1, max_mm: 0.2, created_at: '2026-11-11T12:00:00Z' },
        layout: { marks: [], diameter_mm: 4, origin_diameter_mm: 6.4, scrap: [32.8, 26.8, 134.4, 96.4] },
      }),
      'POST /printers/l1/camera/measure': () => ({ workpiece: { width_mm: 72, height_mm: 38, x_mm: 56, y_mm: 51, angle_deg: 9, center: { x: 92, y: 70 } }, z_mm: 3 }),
      'GET /printers/l1/camera/view': () => ({}),
      'POST /printers/l1/jobs': () => ({ status: 201, body: placed }),
    });
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: vi.fn(() => 'blob:view'), revokeObjectURL: vi.fn() }));
    render(LasersPage, { params: {} });
    expect(await screen.findByTestId('calibration')).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Measure with the camera' }));
    await vi.waitFor(() => expect((screen.getByLabelText('Turned (°)') as HTMLInputElement).value).toBe('9'));
    await fireEvent.submit(screen.getByRole('button', { name: 'Prepare' }));
    await vi.waitFor(() =>
      expect(sent.find((s) => s.method === 'POST' && s.path === '/printers/l1/jobs')?.body).toEqual({
        template_id: 't1', material: 'birch', camera: true, workpiece: { width_mm: 72, height_mm: 38, x_mm: 56, y_mm: 51, angle_deg: 9, z_mm: 3 },
      }),
    );
    expect(await screen.findByRole('img', { name: /turned 9°, and the label on it, over the camera’s photo/ })).toBeTruthy();
    const frame = screen.getByRole('button', { name: 'Frame' }) as HTMLButtonElement;
    expect(frame.disabled).toBe(true);
    await fireEvent.click(screen.getByLabelText('The plan sits on the workpiece in the photo'));
    await vi.waitFor(() => expect(frame.disabled).toBe(false));
  });
});

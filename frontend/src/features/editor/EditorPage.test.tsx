import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom';
import type { Connection, Device, Lab, Network, Topology } from '../../types/api';
import { EditorPage } from './EditorPage';
import { fitPoints } from './viewFit';

const api = vi.hoisted(() => ({
  topology: vi.fn(),
  createNetwork: vi.fn(),
  update: vi.fn(),
  images: vi.fn(),
  templates: vi.fn(),
  scan: vi.fn(),
  deleteBulk: vi.fn(),
  createDevice: vi.fn(),
  positions: vi.fn(),
}));

vi.mock('../../api/endpoints', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/endpoints')>()),
  labsApi: {
    topology: api.topology,
    createNetwork: api.createNetwork,
    update: api.update,
    positions: api.positions,
    deleteBulk: api.deleteBulk,
    createDevice: api.createDevice,
  },
  imagesApi: { list: api.images, scan: api.scan },
  templatesApi: { list: api.templates },
}));
vi.mock('../notifications/NotificationBell', () => ({ NotificationBell: () => null }));

const CANVAS = { width: 1000, height: 600 };
const LAB_PERMS = { 'machine.add': true, 'net.manage': true, 'lab.save_topology': true, 'link.delete': true };

function lab(id: number): Lab {
  return {
    id: `lab-${id}`, owner_id: 1, owner: 'admin', name: `Lab ${id}`, description: '', canvas: null,
    created_at: 0, updated_at: 0, devices: 0, networks: 0, public_level: 'none', perms: LAB_PERMS, folder: '', folder_id: '',
  };
}
function device(id: number, x: number, y: number): Device {
  return { id: `pc${id}`, rid: id, lab_id: 'lab-1', name: `pc${id}`, x, y, state: 'stopped', type: 'pc', icon: '', custom_icon: '', icon_url: '', console: 'serial', last_error: '', pending: null } as unknown as Device;
}
function network(id: number, x: number, y: number, name = `Net${id}`): Network {
  return { id: name, lab_id: 'lab-1', name, type: 'isolated', subnet: '', x, y } as unknown as Network;
}
function topology(id: number, devices: Device[], networks: Network[] = [], connections: Connection[] = [], perms: Record<string, boolean> = LAB_PERMS): Topology {
  const item = lab(id);
  item.perms = perms;
  return { lab: item, instance: 0, devices, networks, connections };
}

/** The first `<g transform>` of the canvas is the camera: translate(x y) scale(k). */
function camera(): { x: number; y: number; k: number } | null {
  const svg = document.querySelector('[data-editor-canvas]');
  const match = /translate\(([-\d.e]+) ([-\d.e]+)\) scale\(([-\d.e]+)\)/.exec(svg?.querySelector(':scope > g')?.getAttribute('transform') ?? '');
  return match ? { x: Number(match[1]), y: Number(match[2]), k: Number(match[3]) } : null;
}
const transformOf = () => document.querySelector('[data-editor-canvas] > g')?.getAttribute('transform');

function Navigate({ to }: { to: string }) {
  const navigate = useNavigate();
  return <button type="button" onClick={() => navigate(to)}>switch to {to}</button>;
}

function renderEditor(start = '/labs/lab-1') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[start]}>
        <Navigate to="/labs/lab-2" />
        <Routes><Route path="/labs/:labName" element={<EditorPage />} /></Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

/** Renders the editor for an explicit lab instance (the route used by "Running labs"). */
function renderInstance(start: string, client: QueryClient) {
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[start]}>
        <Routes><Route path="/labs/:folderName/editor" element={<EditorPage />} /></Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

async function waitForCanvas() {
  await waitFor(() => expect(document.querySelector('[data-editor-canvas]')).not.toBeNull());
}

/** Opens the canvas context menu at a screen point; the menu is portalled to the document body. */
function openMenuAt(x: number, y: number) {
  fireEvent.contextMenu(document.querySelector('[data-editor-canvas]') as Element, { clientX: x, clientY: y });
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(() => ({
    x: 0, y: 0, left: 0, top: 0, right: CANVAS.width, bottom: CANVAS.height, width: CANVAS.width, height: CANVAS.height, toJSON: () => ({}),
  }) as DOMRect);
  api.images.mockResolvedValue([]);
  api.templates.mockResolvedValue([]);
  api.scan.mockResolvedValue({ added: 0, updated: 0 });
  api.update.mockResolvedValue({});
  api.positions.mockResolvedValue({ ok: true });
  api.deleteBulk.mockResolvedValue({ ok: true });
  api.createNetwork.mockImplementation(async (_labId: number, type: string, x: number, y: number) => network(99, x, y, type.toUpperCase()));
});

describe('fit to screen on entry', () => {
  it('fits a spread-out topology so every object is inside the canvas', async () => {
    const devices = [device(1, -6000, -3000), device(2, 9000, 4000)];
    const networks = [network(1, 12000, -2500)];
    api.topology.mockResolvedValue(topology(1, devices, networks));
    renderEditor('/labs/lab-1');
    await waitForCanvas();
    const expected = fitPoints(devices, networks, CANVAS);
    await waitFor(() => expect(camera()?.k).toBeCloseTo(expected.k, 6));
    const view = camera()!;
    for (const point of [...devices, ...networks]) {
      const sx = view.x + point.x * view.k, sy = view.y + point.y * view.k;
      expect(sx).toBeGreaterThanOrEqual(0); expect(sx).toBeLessThanOrEqual(CANVAS.width);
      expect(sy).toBeGreaterThanOrEqual(0); expect(sy).toBeLessThanOrEqual(CANVAS.height);
    }
  });

  it('centres an empty laboratory at zoom 1', async () => {
    api.topology.mockResolvedValue(topology(1, []));
    renderEditor('/labs/lab-1');
    await waitForCanvas();
    await waitFor(() => expect(camera()).toEqual({ x: CANVAS.width / 2, y: CANVAS.height / 2, k: 1 }));
  });

  it('keeps the user camera on later renders and does not refit', async () => {
    api.topology.mockResolvedValue(topology(1, [device(1, 0, 0), device(2, 300, 200)]));
    renderEditor('/labs/lab-1');
    await waitForCanvas();
    await waitFor(() => expect(camera()).not.toBeNull());
    const fitted = transformOf();
    fireEvent.wheel(document.querySelector('[data-editor-canvas]') as Element, { deltaY: -100, clientX: 500, clientY: 300 });
    const zoomed = transformOf();
    expect(zoomed).not.toBe(fitted);
    openMenuAt(10, 10);
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(transformOf()).toBe(zoomed);
  });

  it('fits again when switching to another laboratory', async () => {
    const first = [device(1, 0, 0)];
    const second = [device(2, -9000, -9000), device(3, 9000, 9000)];
    api.topology.mockImplementation(async (id: string) => (id === 'lab-1' ? topology(1, first) : topology(2, second)));
    renderEditor('/labs/lab-1');
    await waitForCanvas();
    await waitFor(() => expect(camera()?.k).toBeCloseTo(fitPoints(first, [], CANVAS).k, 6));
    fireEvent.click(screen.getByRole('button', { name: 'switch to /labs/lab-2' }));
    const expected = fitPoints(second, [], CANVAS);
    await waitFor(() => expect(camera()?.k).toBeCloseTo(expected.k, 6));
    expect(camera()?.x).toBeCloseTo(expected.x, 6);
  });
});

describe('tools panel visibility', () => {
  it('hides empty groups and separators when the user has no permissions', async () => {
    api.topology.mockResolvedValue(topology(1, [], [], [], {}));
    renderEditor('/labs/lab-1');
    await waitForCanvas();
    const aside = document.querySelector('[data-editor-side]') as HTMLElement;
    expect(within(aside).queryByText('Add')).toBeNull();
    expect(within(aside).queryByText('Start')).toBeNull();
    expect(within(aside).queryByText('Laboratory')).toBeNull();
    expect(within(aside).getAllByText('Status').length).toBeGreaterThan(0);
    expect(within(aside).getAllByText('View').length).toBeGreaterThan(0);
  });

  it('shows only the groups backed by available permissions', async () => {
    api.topology.mockResolvedValue(topology(1, [], [], [], { 'machine.add': true }));
    renderEditor('/labs/lab-1');
    await waitForCanvas();
    const aside = document.querySelector('[data-editor-side]') as HTMLElement;
    expect(within(aside).getByText('Add')).toBeInTheDocument();
    expect(within(aside).queryByText('Start')).toBeNull();
    expect(within(aside).queryByText('Laboratory')).toBeNull();
  });
});

describe('console launcher', () => {
  it('is a normal button pinned in the bottom of the tools panel', async () => {
    api.topology.mockResolvedValue(topology(1, [device(1, 0, 0)]));
    renderEditor('/labs/lab-1');
    await waitForCanvas();
    const button = screen.getByRole('button', { name: 'Console' });
    expect(button.closest('[data-editor-side-foot]')).not.toBeNull();
  });
});

describe('add network from the canvas context menu', () => {
  beforeEach(() => {
    api.topology.mockResolvedValue(topology(1, [device(1, 0, 0)]));
  });

  it('lists every network type in the submenu, which stays inside the menu', async () => {
    renderEditor('/labs/lab-1');
    await waitForCanvas();
    openMenuAt(300, 200);
    fireEvent.click(screen.getByRole('button', { name: /Add network/ }));
    const submenu = screen.getByRole('menu');
    expect(within(submenu).getByRole('button', { name: 'NAT' })).toBeEnabled();
    expect(within(submenu).queryByRole('button', { name: 'Isolated' })).toBeNull();
    expect(within(submenu).queryByRole('button', { name: 'Cloud' })).toBeNull();
    expect(submenu.parentElement).toHaveAttribute('data-ctx-menu');
  });

  it('adds a NAT network where the menu was opened', async () => {
    const label = 'NAT';
    const type = 'nat';
    renderEditor('/labs/lab-1');
    await waitForCanvas();
    await waitFor(() => expect(camera()).not.toBeNull());
    const view = camera()!;
    openMenuAt(300, 200);
    fireEvent.click(screen.getByRole('button', { name: /Add network/ }));
    fireEvent.click(within(screen.getByRole('menu')).getByRole('button', { name: label }));
    const worldX = Math.round((300 - view.x) / view.k / 10) * 10;
    const worldY = Math.round((200 - view.y) / view.k / 10) * 10;
    await waitFor(() => expect(api.createNetwork).toHaveBeenCalledWith('lab-1', type, worldX, worldY));
    const labels = [...document.querySelectorAll('[data-editor-canvas] [data-node-label]')].map((node) => node.textContent);
    expect(labels).toContain(label.toUpperCase());
  });

  it('closes on Escape and on a click outside, and not on a click inside the submenu', async () => {
    renderEditor('/labs/lab-1');
    await waitForCanvas();
    openMenuAt(300, 200);
    fireEvent.click(screen.getByRole('button', { name: /Add network/ }));
    fireEvent.mouseDown(within(screen.getByRole('menu')).getByRole('button', { name: 'NAT' }));
    expect(screen.getByRole('menu')).toBeInTheDocument();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('menu')).toBeNull();
    openMenuAt(300, 200);
    fireEvent.mouseDown(document.body);
    expect(screen.queryByRole('button', { name: /Add network/ })).toBeNull();
  });
});

describe('machine state of a foreign instance', () => {
  it('does not keep showing the cached "stopped" snapshot when the instance is running', async () => {
    // The administrator already opened this instance while the machines were running: the cache holds
    // a snapshot with state=stopped. Opening from "Running labs" must request a fresh
    // status immediately, not wait for the first poll a few seconds later.
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(['topology', 'lab-1', 5], topology(1, [device(1, 0, 0)]));
    api.topology.mockResolvedValue(topology(1, [{ ...device(1, 0, 0), state: 'running' }]));

    renderInstance('/labs/lab-1/editor?instance=5', client);
    await waitForCanvas();

    await waitFor(() => expect(api.topology).toHaveBeenCalledWith('lab-1', 5));
    // The machine no longer has a state dot: the state is visible by the icon color - a running machine is not gray.
    await waitFor(() => expect(document.querySelector('[data-editor-canvas] .node[data-tone="on"]')).not.toBeNull());
  });
});

describe('delete a cable from its context menu', () => {
  it('deletes the clicked cable even when it was not selected beforehand', async () => {
    const devices = [device(1, 0, 0), device(2, 300, 0)];
    const connections: Connection[] = [
      { id: 'c7', key: 'c7', lab_id: 'lab-1', a_type: 'device', a_id: 'pc1', a_name: 'pc1', a_if: 0, b_type: 'device', b_id: 'pc2', b_name: 'pc2', b_if: 0, up: true },
    ];
    api.topology.mockResolvedValue(topology(1, devices, [], connections));
    renderEditor('/labs/lab-1');
    await waitForCanvas();
    const hit = document.querySelector('[data-link-hit]');
    expect(hit).not.toBeNull();
    // Right click on a wire not raised in advance: the menu is created before setSelection is applied,
    // so deletion must take the identifier from the menu item itself.
    fireEvent.contextMenu(hit as Element, { clientX: 500, clientY: 300 });
    fireEvent.click(screen.getByRole('button', { name: 'Delete connection' }));
    await waitFor(() => expect(api.deleteBulk).toHaveBeenCalledWith('lab-1', [], [], ['c7']));
  });
});

describe('parallel cables between the same machines', () => {
  it('draws every cable in its own lane instead of stacking them', async () => {
    const devices = [device(1, 0, 0), device(2, 300, 0)];
    const connections: Connection[] = [
      { id: 'c11', key: 'c11', lab_id: 'lab-1', a_type: 'device', a_id: 'pc1', a_name: 'pc1', a_if: 0, b_type: 'device', b_id: 'pc2', b_name: 'pc2', b_if: 0, up: true },
      { id: 'c12', key: 'c12', lab_id: 'lab-1', a_type: 'device', a_id: 'pc1', a_name: 'pc1', a_if: 1, b_type: 'device', b_id: 'pc2', b_name: 'pc2', b_if: 1, up: true },
    ];
    api.topology.mockResolvedValue(topology(1, devices, [], connections));
    renderEditor('/labs/lab-1');
    await waitForCanvas();

    const lines = [...document.querySelectorAll('[data-link-hit]')];
    expect(lines).toHaveLength(2);
    const y1 = lines.map((line) => Number(line.getAttribute('y1')));
    const y2 = lines.map((line) => Number(line.getAttribute('y2')));
    // Horizontal wires are spread on both sides of the line between machine centers.
    expect(y1[0]).not.toBe(y1[1]);
    expect(Math.abs((y1[0] ?? 0) - (y1[1] ?? 0))).toBe(12);
    expect(y2[0]).toBe(y1[0]);
    expect(y2[1]).toBe(y1[1]);
  });

  it('keeps lanes apart when one cable is stored in the opposite direction', async () => {
    const devices = [device(1, 0, 0), device(2, 300, 0)];
    const connections: Connection[] = [
      { id: 'c21', key: 'c21', lab_id: 'lab-1', a_type: 'device', a_id: 'pc1', a_name: 'pc1', a_if: 0, b_type: 'device', b_id: 'pc2', b_name: 'pc2', b_if: 0, up: true },
      { id: 'c22', key: 'c22', lab_id: 'lab-1', a_type: 'device', a_id: 'pc2', a_name: 'pc2', a_if: 1, b_type: 'device', b_id: 'pc1', b_name: 'pc1', b_if: 1, up: true },
    ];
    api.topology.mockResolvedValue(topology(1, devices, [], connections));
    renderEditor('/labs/lab-1');
    await waitForCanvas();

    const lines = [...document.querySelectorAll('[data-link-hit]')];
    const y1 = lines.map((line) => Number(line.getAttribute('y1')));
    expect(Math.abs((y1[0] ?? 0) - (y1[1] ?? 0))).toBe(12);
  });
});

describe('interface naming when adding a machine', () => {
  it('sends the chosen interface type and starting port to the backend', async () => {
    api.topology.mockResolvedValue(topology(1, []));
    api.templates.mockResolvedValue([{
      id: 5, name: 'Linux', vendor: '', type: 'linux', category: '', icon: '',
      arch: 'x86_64', machine: 'pc', cpu: 1, ram_mb: 1024, nics: 1, nic_model: 'virtio-net-pci',
      iface_type: 'ens', iface_start: 0, disk_bus: 'virtio', bios: 'seabios', console: 'serial', extra_args: '',
    }]);
    const created = { ...device(9, 0, 0), iface_type: 'te', iface_start: 3 };
    api.createDevice.mockResolvedValue([created]);

    renderEditor('/labs/lab-1');
    await waitForCanvas();
    fireEvent.click(screen.getByRole('button', { name: 'Machine' }));
    fireEvent.click(await screen.findByText('Linux'));
    fireEvent.change(screen.getByLabelText('Interface type'), { target: { value: 'te' } });
    fireEvent.change(screen.getByLabelText('Starting port'), { target: { value: '3' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add' }));

    await waitFor(() => expect(api.createDevice).toHaveBeenCalled());
    const input = api.createDevice.mock.calls[0]?.[1] as Record<string, unknown>;
    expect(input['iface_type']).toBe('te');
    expect(input['iface_start']).toBe(3);
  });
});

describe('machine icon size', () => {
  const node = (id: number) => document.querySelector(`[data-editor-canvas] .node[data-key="dpc${id}"]`);
  const RESIZE_PERMS = { ...LAB_PERMS, 'machine.move': true };

  it('draws machine icons without any background plate', async () => {
    api.topology.mockResolvedValue(topology(1, [device(1, 0, 0)]));
    renderEditor('/labs/lab-1');
    await waitForCanvas();
    await waitFor(() => expect(node(1)).not.toBeNull());
    expect(node(1)?.querySelector('[data-node-plate]')).toBeNull();
    expect(node(1)?.querySelector('[data-node-icon]')?.getAttribute('width')).toBe('56');
  });

  it('resizes one icon from a corner handle and saves only that machine', async () => {
    api.topology.mockResolvedValue(topology(1, [device(1, 0, 0), device(2, 300, 0)], [], [], RESIZE_PERMS));
    renderEditor('/labs/lab-1');
    await waitForCanvas();
    await waitFor(() => expect(node(1)).toHaveAttribute('data-icon-size', '56'));

    fireEvent.mouseDown(node(1) as Element, { clientX: 500, clientY: 300 });
    fireEvent.mouseUp(node(1) as Element, { clientX: 500, clientY: 300 });
    await waitFor(() => expect(node(1)?.querySelectorAll('[data-resize-handle]').length).toBe(8));

    const east = node(1)?.querySelector('[data-resize-handle="e"]');
    expect(east).not.toBeNull();
    fireEvent.mouseDown(east as Element, { clientX: 500, clientY: 300 });
    fireEvent.mouseMove(window, { clientX: 545, clientY: 300 });
    fireEvent.mouseUp(window, { clientX: 545, clientY: 300 });

    await waitFor(() => expect(Number(node(1)?.getAttribute('data-icon-size'))).toBeGreaterThan(56));
    expect(node(2)).toHaveAttribute('data-icon-size', '56');
    await waitFor(() => expect(api.positions).toHaveBeenCalled());
    const [labId, devices, networks] = api.positions.mock.calls.at(-1) as [string, Array<{ id: string; icon_size?: number }>, unknown[]];
    expect(labId).toBe('lab-1');
    expect(devices).toHaveLength(1);
    const saved = devices[0];
    expect(saved?.id).toBe('pc1');
    expect(saved?.icon_size).toBeGreaterThan(56);
    expect(networks).toEqual([]);
  });

  it('uses each machine size, falls back to the canvas size and clamps invalid values', async () => {
    const first = device(1, 0, 0);
    first.icon_size = 40;
    const topo = topology(1, [first, device(2, 300, 0)]);
    topo.lab.canvas = { view: { x: 0, y: 0, k: 1 }, icon_size: 96 };
    api.topology.mockResolvedValue(topo);
    renderEditor('/labs/lab-1');
    await waitForCanvas();
    await waitFor(() => expect(node(1)).toHaveAttribute('data-icon-size', '40'));
    expect(node(2)).toHaveAttribute('data-icon-size', '96');

    const other = topology(2, [device(3, 0, 0)]);
    other.lab.canvas = { view: { x: 0, y: 0, k: 1 }, icon_size: 9999 };
    api.topology.mockImplementation(async (id: string) => (id === 'lab-1' ? topo : other));
    fireEvent.click(screen.getByRole('button', { name: 'switch to /labs/lab-2' }));
    await waitFor(() => expect(node(3)).toHaveAttribute('data-icon-size', '256'));
  });
});

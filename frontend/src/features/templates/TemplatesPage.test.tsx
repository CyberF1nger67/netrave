import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { TemplatesPage } from './TemplatesPage';
import { templatesApi } from '../../api/endpoints';
import { setLanguage } from '../../i18n';
import { useAuthStore } from '../../stores/auth';
import type { Template } from '../../types/api';

vi.mock('../../api/endpoints', () => ({
  templatesApi: { list: vi.fn(), create: vi.fn(), update: vi.fn(), remove: vi.fn() },
}));

const base = {
  category: '', icon: '', machine: 'pc', cpu: 1, ram_mb: 1024, nics: 1,
  nic_model: 'virtio-net-pci', iface_type: 'ens' as const, iface_start: 0, disk_bus: 'virtio',
  bios: 'seabios', console: 'serial' as const, extra_args: '',
  vnc: { resolution: 'auto' as const, bitrate: null, quality: null, compression: null, fps: null, encoding: 'auto' as const },
};

// Insertion order by id: id 1 is the oldest.
const templates: Template[] = [
  { ...base, id: 1, name: 'Zeta Router', vendor: 'Cisco', type: 'router', arch: 'x86_64' },
  { ...base, id: 2, name: 'Alpha Switch', vendor: 'MikroTik', type: 'switch', arch: 'x86_64' },
  { ...base, id: 3, name: 'Beta Router', vendor: 'MikroTik', type: 'router', arch: 'i386' },
  { ...base, id: 4, name: 'Gamma Linux', vendor: 'Ubuntu', type: 'linux', arch: 'x86_64' },
];

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}><TemplatesPage /></QueryClientProvider>);
}

const cardNames = () => Array.from(document.querySelectorAll('[data-template-name]')).map((el) => el.textContent);

describe('TemplatesPage filtering', () => {
  beforeEach(() => {
    setLanguage('en');
    useAuthStore.setState({ user: null, status: 'guest' } as never);
    vi.mocked(templatesApi.list).mockResolvedValue(templates);
  });

  it('shows the total count and sorts A–Z by default', async () => {
    renderPage();
    await screen.findByText('Alpha Switch');
    expect(screen.getByText(/Showing 4 of 4 templates/)).toBeInTheDocument();
    expect(cardNames()).toEqual(['Alpha Switch', 'Beta Router', 'Gamma Linux', 'Zeta Router']);
  });

  it('combines search, device type, vendor and sort at once', async () => {
    renderPage();
    await screen.findByText('Alpha Switch');
    fireEvent.change(screen.getByRole('combobox', { name: 'Device type' }), { target: { value: 'router' } });
    fireEvent.change(screen.getByRole('combobox', { name: 'Vendor' }), { target: { value: 'MikroTik' } });
    fireEvent.change(screen.getByRole('combobox', { name: 'Sort by' }), { target: { value: 'name_desc' } });
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'router' } });
    expect(cardNames()).toEqual(['Beta Router']);
    expect(screen.getByText(/Showing 1 of 4 templates/)).toBeInTheDocument();
  });

  it('sorts by order added, newest and oldest first', async () => {
    renderPage();
    await screen.findByText('Alpha Switch');
    fireEvent.change(screen.getByRole('combobox', { name: 'Sort by' }), { target: { value: 'added_new' } });
    expect(cardNames()).toEqual(['Gamma Linux', 'Beta Router', 'Alpha Switch', 'Zeta Router']);
    fireEvent.change(screen.getByRole('combobox', { name: 'Sort by' }), { target: { value: 'added_old' } });
    expect(cardNames()).toEqual(['Zeta Router', 'Alpha Switch', 'Beta Router', 'Gamma Linux']);
  });

  it('shows an empty state with a reset button and restores all templates on reset', async () => {
    renderPage();
    await screen.findByText('Alpha Switch');
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'nothing-matches' } });
    expect(screen.getByText('Templates not found')).toBeInTheDocument();
    const reset = screen.getAllByRole('button', { name: 'Reset filters' });
    fireEvent.click(reset[reset.length - 1]!);
    await waitFor(() => expect(cardNames()).toHaveLength(4));
    expect(screen.getByText(/Showing 4 of 4 templates/)).toBeInTheDocument();
  });

  it('searches by vendor and model, not only by name', async () => {
    renderPage();
    await screen.findByText('Alpha Switch');
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'cisco' } });
    expect(cardNames()).toEqual(['Zeta Router']);
  });

});

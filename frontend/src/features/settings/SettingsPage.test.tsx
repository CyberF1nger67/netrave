import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { SettingsPage } from './SettingsPage';

// Server responses are set in each test: the page reads global settings and KSM/UKSM state.
const settings = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
  ksmGet: vi.fn(),
  ksmSet: vi.fn(),
}));

vi.mock('../../api/endpoints', () => ({
  settingsApi: { get: settings.get, put: settings.put },
  ksmApi: { get: settings.ksmGet, set: settings.ksmSet },
}));

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}><SettingsPage /></QueryClientProvider>);
}

const mech = (enabled: boolean, extra: Record<string, unknown> = {}) =>
  ({ supported: true, available: true, enabled, saved_bytes: 0, merged: 0, ...extra });

describe('settings', () => {
  it('KSM and UKSM have separate switches, enabling one turns the other off', async () => {
    settings.get.mockResolvedValue({ session_ttl_hours: '24', max_upload_mb: '20480' });
    settings.ksmGet.mockResolvedValue({ ksm: mech(false), uksm: mech(false) });
    // After enabling KSM the server reports: KSM enabled, UKSM disabled.
    settings.ksmSet.mockResolvedValue({ ksm: mech(true), uksm: mech(false) });
    settings.put.mockResolvedValue({ ok: true });
    renderPage();

    const ksm = await screen.findByTestId('mechanism-ksm');
    const uksm = screen.getByTestId('mechanism-uksm');
    fireEvent.click(within(ksm).getByRole('checkbox'));
    await waitFor(() => expect(settings.ksmSet).toHaveBeenCalledWith('ksm', true));

    // The state comes from the server response: KSM is enabled, UKSM remains disabled.
    await waitFor(() => expect(within(ksm).getByRole('checkbox')).toBeChecked());
    expect(within(uksm).getByRole('checkbox')).not.toBeChecked();

    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(settings.put).toHaveBeenCalledWith({ session_ttl_hours: '24', max_upload_mb: '20480' }));
  });

  it('UKSM without kernel support is marked unavailable, KSM keeps working', async () => {
    settings.get.mockResolvedValue({ session_ttl_hours: '24', max_upload_mb: '20480' });
    settings.ksmGet.mockResolvedValue({
      ksm: mech(true),
      uksm: { supported: false, available: false, enabled: false, saved_bytes: 0, merged: 0, reason: 'page merging is not available in the kernel' },
    });
    renderPage();
    const uksm = await screen.findByTestId('mechanism-uksm');
    expect(within(uksm).getByText('page merging is not available in the kernel')).toBeInTheDocument();
    expect(within(uksm).queryByRole('checkbox')).toBeNull();
    expect(within(screen.getByTestId('mechanism-ksm')).getByRole('checkbox')).toBeChecked();
  });
});

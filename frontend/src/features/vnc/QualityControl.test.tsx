import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { ApiError } from '../../api/client';
import { QualityControl } from './QualityControl';
import { DEFAULT_QUALITY } from './quality';
import type { VncQualitySettings } from '../../types/api';
import type { VNCView } from './VNCView';

const mocks = vi.hoisted(() => ({ quality: vi.fn(), putQuality: vi.fn() }));

vi.mock('../../api/endpoints', () => ({
  consoleApi: { quality: mocks.quality, putQuality: mocks.putQuality },
}));

const STORED: VncQualitySettings = {
  resolution: '1280x720',
  bitrate: 25,
  quality: null,
  compression: null,
  fps: null,
  encoding: 'auto',
};

interface FakeView {
  view: VNCView;
  apply: ReturnType<typeof vi.fn>;
}

function fakeView(): FakeView {
  const apply = vi.fn();
  const view = {
    resolution: '1024×768',
    resolutionError: '',
    resolutionNotice: '',
    resolutionPending: false,
    quality: { ...DEFAULT_QUALITY },
    onQualityChange: null,
    applyQuality: apply,
  } as unknown as VNCView;
  return { view, apply };
}

beforeEach(() => {
  mocks.quality.mockReset().mockResolvedValue({ settings: STORED, defaults: DEFAULT_QUALITY });
  mocks.putQuality.mockReset().mockImplementation((_rid: number, settings: VncQualitySettings) =>
    Promise.resolve({ settings, defaults: DEFAULT_QUALITY }));
});

function setup(): FakeView {
  return fakeView();
}

describe('QualityControl', () => {
  it('restores the stored settings without forcing a resize and shows the live resolution', async () => {
    const { view, apply } = setup();
    render(<QualityControl rid={7} view={view} />);
    await waitFor(() => expect(apply).toHaveBeenCalledWith(STORED, { fitAuto: false }));
    fireEvent.click(screen.getByRole('button', { name: /Image quality/ }));
    expect(screen.getByText('Current resolution: 1024×768')).toBeInTheDocument();
    expect((screen.getByRole('combobox', { name: 'Resolution' }) as HTMLSelectElement).value).toBe('1280x720');
  });

  it('keeps preset, slider and number input in sync without touching the live console', async () => {
    const { view, apply } = setup();
    render(<QualityControl rid={7} view={view} />);
    await waitFor(() => expect(apply).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole('button', { name: /Image quality/ }));

    fireEvent.click(screen.getByRole('button', { name: '50' }));
    const slider = screen.getByRole('slider', { name: 'Bitrate' }) as HTMLInputElement;
    const input = screen.getByRole('spinbutton', { name: 'Bitrate' }) as HTMLInputElement;
    expect(slider.value).toBe('50');
    expect(input.value).toBe('50');

    fireEvent.change(slider, { target: { value: '37' } });
    expect(input.value).toBe('37');
    fireEvent.change(input, { target: { value: '42' } });
    expect(slider.value).toBe('42');

    // Before Apply the live console received only the restored settings.
    expect(apply).toHaveBeenCalledTimes(1);
    expect(screen.getByText('Pending changes — press Apply')).toBeInTheDocument();
  });

  it('persists and applies pending changes only on Apply', async () => {
    const { view, apply } = setup();
    render(<QualityControl rid={7} view={view} />);
    await waitFor(() => expect(apply).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole('button', { name: /Image quality/ }));
    fireEvent.click(screen.getByRole('button', { name: '10' }));
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

    await waitFor(() => expect(mocks.putQuality).toHaveBeenCalledWith(7, expect.objectContaining({ bitrate: 10, resolution: '1280x720' })));
    await waitFor(() => expect(apply).toHaveBeenLastCalledWith(expect.objectContaining({ bitrate: 10 }), { fitAuto: true }));
    expect(await screen.findByText('Applied')).toBeInTheDocument();
  });

  it('resets pending changes to the applied values', async () => {
    const { view, apply } = setup();
    render(<QualityControl rid={7} view={view} />);
    await waitFor(() => expect(apply).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole('button', { name: /Image quality/ }));
    fireEvent.click(screen.getByRole('button', { name: '50' }));
    fireEvent.click(screen.getByRole('button', { name: 'Reset' }));

    expect((screen.getByRole('slider', { name: 'Bitrate' }) as HTMLInputElement).value).toBe('25');
    expect((screen.getByRole('button', { name: 'Apply' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('fills the form with defaults and saves them after Apply', async () => {
    const { view, apply } = setup();
    render(<QualityControl rid={7} view={view} />);
    await waitFor(() => expect(apply).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole('button', { name: /Image quality/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Default' }));

    expect((screen.getByRole('combobox', { name: 'Resolution' }) as HTMLSelectElement).value).toBe('auto');
    expect((screen.getByRole('spinbutton', { name: 'Bitrate' }) as HTMLInputElement).value).toBe('');
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }));
    await waitFor(() => expect(mocks.putQuality).toHaveBeenCalledWith(7, DEFAULT_QUALITY));
  });

  it('shows a server validation error and keeps the draft', async () => {
    const { view, apply } = setup();
    mocks.putQuality.mockRejectedValueOnce(new ApiError('битрейт должен быть целым числом от 1 до 100 Мбит/с', 400, null));
    render(<QualityControl rid={7} view={view} />);
    await waitFor(() => expect(apply).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole('button', { name: /Image quality/ }));
    fireEvent.click(screen.getByRole('button', { name: '50' }));
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

    expect(await screen.findByText('битрейт должен быть целым числом от 1 до 100 Мбит/с')).toBeInTheDocument();
    expect((screen.getByRole('slider', { name: 'Bitrate' }) as HTMLInputElement).value).toBe('50');
    expect(apply).toHaveBeenCalledTimes(1);
  });

  it('shows a resolution error reported by the RFB client', async () => {
    const { view, apply } = setup();
    view.resolutionError = 'The display adapter does not support changing the resolution';
    render(<QualityControl rid={7} view={view} />);
    await waitFor(() => expect(apply).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole('button', { name: /Image quality/ }));
    expect(await screen.findByText('The display adapter does not support changing the resolution')).toBeInTheDocument();
  });
});

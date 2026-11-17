import type { VncEncoding, VncQualitySettings, VncResolution } from '../../types/api';

export const VNC_RESOLUTIONS: Array<{ value: VncResolution; label: string }> = [
  { value: 'auto', label: 'Auto' },
  { value: '640x480', label: '640×480' },
  { value: '800x600', label: '800×600' },
  { value: '1024x768', label: '1024×768' },
  { value: '1280x720', label: '1280×720' },
  { value: '1366x768', label: '1366×768' },
  { value: '1600x900', label: '1600×900' },
  { value: '1920x1080', label: '1920×1080' },
];

export const VNC_BITRATE_PRESETS = [
  { label: 'Economy', value: 2 },
  { label: 'Low', value: 5 },
  { label: 'Standard', value: 10 },
  { label: 'High', value: 25 },
  { label: 'Very high', value: 50 },
  { label: 'Maximum', value: 100 },
] as const;

export const VNC_ENCODINGS = [
  { value: 'auto', label: 'Auto' },
  { value: 'tight', label: 'Tight (compression/JPEG)' },
  { value: 'hextile', label: 'Hextile (lossless)' },
  { value: 'raw', label: 'Raw (maximum compatibility)' },
] as const satisfies ReadonlyArray<{ value: VncEncoding; label: string }>;

export const DEFAULT_QUALITY: VncQualitySettings = {
  resolution: 'auto',
  bitrate: null,
  quality: null,
  compression: null,
  fps: null,
  encoding: 'auto',
};

export interface EffectiveQuality {
  encoding: 'tight' | 'hextile' | 'raw';
  quality: number | null;
  compression: number | null;
  fps: number | null;
  derived: boolean;
}

const clamp = (value: number, min: number, max: number): number => Math.max(min, Math.min(max, value));

// Bitrate is a user-facing target, not a network limit. It is translated
// into real RFB parameters: JPEG/Tight compression level and update request rate.
export function effectiveQuality(settings: VncQualitySettings): EffectiveQuality {
  const encoding = settings.encoding === 'auto' ? 'tight' : settings.encoding;
  if (settings.bitrate === null) {
    return { encoding, quality: settings.quality, compression: settings.compression, fps: settings.fps, derived: false };
  }
  const ratio = (clamp(settings.bitrate, 1, 100) - 1) / 99;
  const quality = settings.quality ?? clamp(Math.round(1 + ratio * 8), 1, 9);
  const compression = settings.compression ?? clamp(Math.round(9 - ratio * 8), 1, 9);
  const fps = settings.fps ?? clamp(Math.round(5 + ratio * 55), 1, 60);
  return { encoding, quality, compression, fps, derived: true };
}

export function resolutionSize(value: VncResolution): { width: number; height: number } | null {
  if (value === 'auto') return null;
  const [width, height] = value.split('x').map((part) => Number.parseInt(part, 10));
  return width && height ? { width, height } : null;
}

// SetEncodings list in order of preference. Tight pseudo-encodings:
// -32 + JPEG level (1...9) and -256 + zlib level (1...9). QEMU applies
// JPEG quality only with lossy=on, so without explicit settings it stays lossless.
export function rfbEncodings(effective: EffectiveQuality): number[] {
  const encodings: number[] = [];
  if (effective.encoding === 'tight') encodings.push(7, 5, 0);
  else if (effective.encoding === 'hextile') encodings.push(5, 0);
  else encodings.push(0);
  encodings.push(-223, -308);
  if (effective.encoding === 'tight') {
    if (effective.quality !== null) encodings.push(-32 + effective.quality);
    if (effective.compression !== null) encodings.push(-256 + effective.compression);
  }
  return encodings;
}

export function qualityKey(settings: VncQualitySettings): string {
  return [
    settings.resolution, settings.bitrate ?? 'auto', settings.quality ?? 'auto',
    settings.compression ?? 'auto', settings.fps ?? 'auto', settings.encoding,
  ].join('|');
}

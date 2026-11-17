import { describe, expect, it } from 'vitest';
import { DEFAULT_QUALITY, VNC_BITRATE_PRESETS, VNC_RESOLUTIONS, effectiveQuality, qualityKey, resolutionSize, rfbEncodings } from './quality';
import type { VncQualitySettings } from '../../types/api';

const settings = (patch: Partial<VncQualitySettings> = {}): VncQualitySettings => ({ ...DEFAULT_QUALITY, ...patch });

describe('VNC quality mapping', () => {
  it('keeps the console lossless by default', () => {
    const effective = effectiveQuality(settings());
    expect(effective).toEqual({ encoding: 'tight', quality: null, compression: null, fps: null, derived: false });
    expect(rfbEncodings(effective)).toEqual([7, 5, 0, -223, -308]);
  });

  it('derives real RFB parameters from the bitrate target', () => {
    const low = effectiveQuality(settings({ bitrate: 1 }));
    expect(low.quality).toBe(1);
    expect(low.compression).toBe(9);
    expect(low.fps).toBe(5);

    const high = effectiveQuality(settings({ bitrate: 100 }));
    expect(high.quality).toBe(9);
    expect(high.compression).toBe(1);
    expect(high.fps).toBe(60);

    const middle = effectiveQuality(settings({ bitrate: 50 }));
    expect(middle.quality).toBeGreaterThan(1);
    expect(middle.quality).toBeLessThan(9);
    expect(middle.compression).toBeGreaterThan(1);
    expect(middle.compression).toBeLessThan(9);
  });

  it('encodes Tight quality and compression pseudo-encodings in the RFB range', () => {
    const encodings = rfbEncodings(effectiveQuality(settings({ bitrate: 9 })));
    expect(encodings.slice(0, 3)).toEqual([7, 5, 0]);
    const quality = encodings.find((value) => value >= -32 && value <= -23);
    const compression = encodings.find((value) => value >= -256 && value <= -247);
    expect(quality).toBeDefined();
    expect(compression).toBeDefined();
    expect(quality!).toBe(-32 + (effectiveQuality(settings({ bitrate: 9 })).quality ?? 0));
    expect(compression!).toBe(-256 + (effectiveQuality(settings({ bitrate: 9 })).compression ?? 0));
  });

  it('lets explicit advanced values override the derived ones', () => {
    const effective = effectiveQuality(settings({ bitrate: 100, quality: 2, compression: 8, fps: 12, encoding: 'tight' }));
    expect(effective).toEqual({ encoding: 'tight', quality: 2, compression: 8, fps: 12, derived: true });
  });

  it('does not request Tight pseudo-encodings for lossless encodings', () => {
    expect(rfbEncodings(effectiveQuality(settings({ encoding: 'hextile', bitrate: 5 })))).toEqual([5, 0, -223, -308]);
    expect(rfbEncodings(effectiveQuality(settings({ encoding: 'raw' })))).toEqual([0, -223, -308]);
  });

  it('limits the bitrate to the 1–100 range', () => {
    expect(effectiveQuality(settings({ bitrate: 0 })).quality).toBe(1);
    expect(effectiveQuality(settings({ bitrate: 500 })).quality).toBe(9);
  });

  it('parses manual resolutions and leaves Auto without a size', () => {
    expect(resolutionSize('auto')).toBeNull();
    expect(resolutionSize('1366x768')).toEqual({ width: 1366, height: 768 });
    expect(resolutionSize('1920x1080')).toEqual({ width: 1920, height: 1080 });
    expect(VNC_RESOLUTIONS.map((item) => item.value)).toEqual([
      'auto', '640x480', '800x600', '1024x768', '1280x720', '1366x768', '1600x900', '1920x1080',
    ]);
  });

  it('synchronises presets with the bitrate values', () => {
    expect(VNC_BITRATE_PRESETS.map((preset) => preset.value)).toEqual([2, 5, 10, 25, 50, 100]);
  });

  it('builds a stable key for dirty detection', () => {
    expect(qualityKey(settings())).toBe('auto|auto|auto|auto|auto|auto');
    expect(qualityKey(settings())).not.toBe(qualityKey(settings({ bitrate: 10 })));
  });
});

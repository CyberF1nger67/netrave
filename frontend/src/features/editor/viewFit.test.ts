import { describe, expect, it } from 'vitest';
import type { Device, Network } from '../../types/api';
import { fitPoints, MIN_ZOOM } from './viewFit';

const dev = (id: number, x: number, y: number) => ({ id, x, y }) as unknown as Device;
const net = (id: number, x: number, y: number) => ({ id, x, y }) as unknown as Network;

/** Screen position of a world point under a view. */
const toScreen = (view: { x: number; y: number; k: number }, x: number, y: number) => ({ sx: view.x + x * view.k, sy: view.y + y * view.k });

describe('fitPoints', () => {
  it('centres an empty laboratory at zoom 1', () => {
    expect(fitPoints([], [], { width: 800, height: 600 })).toEqual({ x: 400, y: 300, k: 1 });
  });

  it('keeps a single device inside the canvas and does not zoom in beyond the cap', () => {
    const rect = { width: 800, height: 600 };
    const view = fitPoints([dev(1, 300, 200)], [], rect);
    expect(view.k).toBeLessThanOrEqual(1.2);
    const { sx, sy } = toScreen(view, 300, 200);
    expect(sx).toBeGreaterThan(0); expect(sx).toBeLessThan(rect.width);
    expect(sy).toBeGreaterThan(0); expect(sy).toBeLessThan(rect.height);
  });

  it('places a single machine exactly in the centre of the canvas', () => {
    const rect = { width: 800, height: 600 };
    const view = fitPoints([dev(1, 300, 200)], [], rect);
    expect(toScreen(view, 300, 200)).toEqual({ sx: rect.width / 2, sy: rect.height / 2 });
  });

  it('keeps the whole visual extent of every node inside the padded canvas', () => {
    const rect = { width: 900, height: 500 };
    const devices = Array.from({ length: 20 }, (_, index) => dev(index + 1, (index % 5) * 1500 - 3000, Math.floor(index / 5) * 1200 - 1800));
    const view = fitPoints(devices, [], rect);
    for (const device of devices) {
      for (const sx of [toScreen(view, device.x - 28, 0).sx, toScreen(view, device.x + 28, 0).sx]) {
        expect(sx).toBeGreaterThanOrEqual(32 - 1e-6); expect(sx).toBeLessThanOrEqual(rect.width - 32 + 1e-6);
      }
      for (const sy of [toScreen(view, 0, device.y - 46).sy, toScreen(view, 0, device.y + 46).sy]) {
        expect(sy).toBeGreaterThanOrEqual(32 - 1e-6); expect(sy).toBeLessThanOrEqual(rect.height - 32 + 1e-6);
      }
    }
  });

  it('still makes every object visible when the topology is extremely large', () => {
    const rect = { width: 900, height: 500 };
    const devices = [dev(1, -1_000_000, -1_000_000), dev(2, 1_000_000, 1_000_000)];
    const view = fitPoints(devices, [], rect);
    expect(view.k).toBeLessThan(MIN_ZOOM);
    for (const device of devices) {
      const { sx, sy } = toScreen(view, device.x, device.y);
      expect(sx).toBeGreaterThanOrEqual(0); expect(sx).toBeLessThanOrEqual(rect.width);
      expect(sy).toBeGreaterThanOrEqual(0); expect(sy).toBeLessThanOrEqual(rect.height);
    }
  });

  it('makes every device and network visible in a large, widely spread topology', () => {
    const rect = { width: 900, height: 500 };
    const devices = [dev(1, -6000, -3000), dev(2, 9000, 4000), dev(3, 0, 0)];
    const networks = [net(1, 12000, -2500)];
    const view = fitPoints(devices, networks, rect);
    expect(view.k).toBeGreaterThanOrEqual(MIN_ZOOM);
    for (const point of [...devices, ...networks]) {
      const { sx, sy } = toScreen(view, point.x, point.y);
      expect(sx).toBeGreaterThanOrEqual(0); expect(sx).toBeLessThanOrEqual(rect.width);
      expect(sy).toBeGreaterThanOrEqual(0); expect(sy).toBeLessThanOrEqual(rect.height);
    }
  });

  it('reserves more room for larger machine icons', () => {
    const rect = { width: 200, height: 200 };
    const small = fitPoints([dev(1, 0, 0)], [], rect, 56);
    const large = fitPoints([dev(1, 0, 0)], [], rect, 128);
    expect(large.k).toBeLessThan(small.k);
    for (const [view, half] of [[small, 28], [large, 64]] as const) {
      expect(toScreen(view, -half, 0).sx).toBeGreaterThanOrEqual(32 - 1e-6);
      expect(toScreen(view, half, 0).sx).toBeLessThanOrEqual(rect.width - 32 + 1e-6);
    }
  });

  it('centres the bounding box of several objects', () => {
    const rect = { width: 1000, height: 800 };
    const view = fitPoints([dev(1, 100, 100), dev(2, 300, 300)], [], rect);
    const left = toScreen(view, 100, 100).sx, right = toScreen(view, 300, 300).sx;
    expect(Math.abs(left - (rect.width - right))).toBeLessThan(1e-6);
  });
});

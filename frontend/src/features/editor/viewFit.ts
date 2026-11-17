import type { Device, Network } from '../../types/api';

export interface View { x: number; y: number; k: number }

/** Lowest zoom the canvas controls normally reach. */
export const MIN_ZOOM = 0.02;
/** Automatic fit may go below the control limit so an extremely large topology still fits. */
const FIT_MIN_ZOOM = 1e-6;
const FIT_MAX_ZOOM = 1.2;
/** Screen-space gap between the topology bounds and the canvas edges. */
const FIT_PADDING = 32;

/** Machine icon edge in world units: the editor keeps icons square and proportional. */
export const BASE_ICON_SIZE = 56;
export const MIN_ICON_SIZE = 24;
export const MAX_ICON_SIZE = 256;

/** Clamps a stored or requested icon size to the supported range. */
export function clampIconSize(value: unknown): number {
  const number = typeof value === 'number' && Number.isFinite(value) ? value : BASE_ICON_SIZE;
  if (number <= 0) return BASE_ICON_SIZE;
  return Math.min(MAX_ICON_SIZE, Math.max(MIN_ICON_SIZE, Math.round(number)));
}

/** Side of a device icon: its own saved size, else the laboratory fallback. */
export function iconSizeOf(device: Pick<Device, 'icon_size'>, fallback = BASE_ICON_SIZE): number {
  return clampIconSize(device.icon_size || fallback);
}

/**
 * Visual half-size of a node relative to its anchor point (the point stored in
 * `x`/`y`). These mirror the SVG shapes rendered by EditorPage: the icon of a
 * device (square, no background plate) and the name label under it.
 */
const NETWORK_HALF_WIDTH = { cloud: 42, nat: 42, isolated: 26 } as const;
const NETWORK_HALF_HEIGHT = 50;

/** Approximate half-width of a 12px centred label, so long names are not clipped. */
function labelHalfWidth(name: string | undefined): number {
  return ((name?.length ?? 0) * 6.2) / 2;
}

interface Bounds { minX: number; minY: number; maxX: number; maxY: number }

/**
 * Bounding box of everything drawn on the topology: device icons, network shapes,
 * their labels and, because links connect node anchors, every connection too.
 *
 * The extent reserved for each node is the same on both sides of its anchor. This keeps
 * the anchor grid centred in the result (a single machine is exactly in the middle)
 * while still leaving enough room for the label under every device.
 */
function topologyBounds(devices: Device[], networks: Network[], fallbackIconSize: number): Bounds | null {
  if (!devices.length && !networks.length) return null;

  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  let deviceHalfX = 0, deviceHalfY = 0;
  for (const device of devices) {
    if (device.x < minX) minX = device.x;
    if (device.x > maxX) maxX = device.x;
    if (device.y < minY) minY = device.y;
    if (device.y > maxY) maxY = device.y;
    // The icon has no background plate: its bounds are exactly the picture, and the label is below it.
    const half = iconSizeOf(device, fallbackIconSize) / 2;
    deviceHalfX = Math.max(deviceHalfX, half, labelHalfWidth(device.name));
    deviceHalfY = Math.max(deviceHalfY, half + 18);
  }
  let networkHalfX = 0, networkHalfY = 0;
  for (const network of networks) {
    if (network.x < minX) minX = network.x;
    if (network.x > maxX) maxX = network.x;
    if (network.y < minY) minY = network.y;
    if (network.y > maxY) maxY = network.y;
    networkHalfX = Math.max(networkHalfX, NETWORK_HALF_WIDTH[network.type] ?? NETWORK_HALF_WIDTH.isolated, labelHalfWidth(network.name));
    networkHalfY = Math.max(networkHalfY, NETWORK_HALF_HEIGHT);
  }
  const leftX = Math.max(deviceHalfX, networkHalfX);
  const leftY = Math.max(deviceHalfY, networkHalfY);
  return { minX: minX - leftX, minY: minY - leftY, maxX: maxX + leftX, maxY: maxY + leftY };
}

/**
 * Camera that places every device and network inside the canvas, centred.
 * An empty laboratory is centred at zoom 1.
 *
 * The fit is computed in screen space (padding is subtracted from the available
 * canvas size), so the result stays correct at any zoom level and with any window size.
 */
export function fitPoints(devices: Device[], networks: Network[], rect: { width: number; height: number }, fallbackIconSize = BASE_ICON_SIZE): View {
  const bounds = topologyBounds(devices, networks, clampIconSize(fallbackIconSize));
  if (!bounds) return { x: rect.width / 2, y: rect.height / 2, k: 1 };

  const width = Math.max(1, bounds.maxX - bounds.minX);
  const height = Math.max(1, bounds.maxY - bounds.minY);

  // Keep a small gap from the visible workspace edges, independently of the resulting zoom.
  const paddedWidth = Math.max(1, rect.width - FIT_PADDING * 2);
  const paddedHeight = Math.max(1, rect.height - FIT_PADDING * 2);
  const k = Math.min(FIT_MAX_ZOOM, Math.max(FIT_MIN_ZOOM, Math.min(paddedWidth / width, paddedHeight / height)));

  return {
    k,
    x: (rect.width - width * k) / 2 - bounds.minX * k,
    y: (rect.height - height * k) / 2 - bounds.minY * k,
  };
}

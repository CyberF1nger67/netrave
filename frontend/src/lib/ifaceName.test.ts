import { describe, expect, it } from 'vitest';
import { ifaceName } from './format';

describe('ifaceName', () => {
  it('numbers from the starting port', () => {
    expect(ifaceName({ iface_type: 'ge', iface_start: 0 }, 0)).toBe('ge0');
    expect(ifaceName({ iface_type: 'ge', iface_start: 0 }, 2)).toBe('ge2');
  });

  it('shift -1: nic0 is MGMT, nic1 is ge0', () => {
    const iface = { iface_type: 'ge', iface_start: -1 };
    expect(ifaceName(iface, 0)).toBe('MGMT');
    expect(ifaceName(iface, 1)).toBe('ge0');
    expect(ifaceName(iface, 2)).toBe('ge1');
  });
});

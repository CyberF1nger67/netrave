import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import type { Connection, Device } from '../../types/api';
import { ConnectDialog } from './dialogs';

vi.mock('../../api/endpoints', () => ({
  labsApi: { createConnection: vi.fn() },
  devicesApi: {},
}));

vi.mock('../../stores/toast', () => ({
  guard: (fn: () => Promise<void>) => fn(),
  toast: { success: vi.fn(), warning: vi.fn(), error: vi.fn() },
}));

const pc = (id: number, nics: number): Device => ({ id: `pc${id}`, nics, name: `pc${id}` } as unknown as Device);

describe('ConnectDialog with an already used interface', () => {
  it('offers a new physical interface for the second cable between the same machines', () => {
    const connections: Connection[] = [
      { id: 'c1', key: 'c1', lab_id: 'lab', a_type: 'device', a_id: 'pc1', a_name: 'pc1', a_if: 0, b_type: 'device', b_id: 'pc2', b_name: 'pc2', b_if: 0, up: true },
    ];
    render(
      <ConnectDialog labId="lab" a="dpc1" b="dpc2" devices={[pc(1, 1), pc(2, 1)]} networks={[]}
        connections={connections} onClose={() => {}} onConnected={() => {}} />,
    );

    const selects = screen.getAllByRole('combobox') as HTMLSelectElement[];
    expect(selects).toHaveLength(2);
    // Both occupied ports (ens0) are skipped; the new interface ens1 is selected by default.
    expect(selects.map((select) => select.value)).toEqual(['1', '1']);
  });
});

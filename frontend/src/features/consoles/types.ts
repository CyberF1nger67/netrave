import type { MachineState } from '../../types/api';

export interface ConsoleDevice {
  rid: number;
  name: string;
  lab_id: number;
  state: MachineState | string;
  console: 'serial' | 'vnc' | string;
  lab_perms?: Record<string, boolean>;
  icon_url?: string;
  icon?: string;
  type?: string;
}

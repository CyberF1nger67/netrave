export interface ConsoleEntry {
  id: number;
  name: string;
  lab: string;
  owner: string | null;
  kind: 'serial' | 'vnc';
}

const KEY = 'netrave.consoles.v2';
export const WINDOW_ID = Math.random().toString(36).slice(2, 10);

export function readConsoles(): ConsoleEntry[] {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) ?? '[]') as unknown;
    if (!Array.isArray(raw)) return [];
    return raw.map((item) => {
      const value = item as Record<string, unknown>;
      return { ...value, lab: typeof value['lab'] === 'string' ? value['lab'] : '' };
    }).filter((item): item is ConsoleEntry => {
      if (typeof item !== 'object' || item === null) return false;
      const value = item as Record<string, unknown>;
      return typeof value['id'] === 'number' && typeof value['name'] === 'string'
        && (typeof value['lab'] === 'string' || typeof value['lab'] === 'number')
        && (value['kind'] === 'serial' || value['kind'] === 'vnc');
    });
  } catch {
    return [];
  }
}

export function writeConsoles(list: ConsoleEntry[]): void {
  try {
    localStorage.setItem(KEY, JSON.stringify(list));
  } catch {
    // localStorage may be unavailable
  }
}

export function closeConsolesForLab(labId: string): void {
  writeConsoles(readConsoles().filter((entry) => entry.lab !== labId));
}

export function closeAllConsoles(): void {
  writeConsoles([]);
}

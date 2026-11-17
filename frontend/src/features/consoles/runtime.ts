import type { ConsoleDevice } from './types';

export function openConsole(device: ConsoleDevice, kind?: 'serial' | 'vnc'): void {
  void import('./dock').then(({ getDock }) => {
    getDock().open(device, false, kind);
  });
}

export function focusDeviceConsole(rid: number): void {
  void import('./dock').then(({ focusDeviceConsole: focus }) => focus(rid));
}

export function setConsoleDevices(source: () => import('./types').ConsoleDevice[]): void {
  void import('./dock').then(({ setDeviceSource }) => setDeviceSource(source));
}

export function showConsoleLauncher(anchor: HTMLElement): void {
  void import('./dock').then(({ getDock }) => { getDock().showLauncher(anchor); });
}

export function closeAllConsoleSessions(): void {
  void import('./dock').then(({ closeAllConsoleSessions: close }) => close());
}

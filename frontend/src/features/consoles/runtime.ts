import type { ConsoleDevice } from './types';
import { t } from '../../i18n';
import { toast } from '../../stores/toast';

// Browsers do not let a page switch to another tab by itself, so the sender says where the terminal went.
function notifyOtherTab(): void {
  toast.info(t('The terminal is open in another browser tab — switch to it'));
}

export function openConsole(device: ConsoleDevice, kind?: 'serial' | 'vnc' | undefined): void {
  void import('./dock').then(async ({ getDock, routeOpen }) => {
    // Another window's terminal takes the machine when there is one; otherwise this window opens its own.
    if (await routeOpen(device, kind)) { notifyOtherTab(); return; }
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
  void import('./dock').then(async ({ getDock, routeRaise }) => {
    // Another window's active terminal takes the button; without one, the local dock or machine list opens.
    if (await routeRaise()) { notifyOtherTab(); return; }
    getDock().showLauncher(anchor);
  });
}

export function closeAllConsoleSessions(): void {
  void import('./dock').then(({ closeAllConsoleSessions: close }) => close());
}

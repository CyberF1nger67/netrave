import { currentLanguage, t } from '../i18n';

export function fmtBytes(n: number): string {
  if (!n) return '0';
  const units = [t('B'), t('KB'), t('MB'), t('GB'), t('TB')];
  let value = n;
  let index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index++;
  }
  return value.toFixed(index ? 1 : 0) + ' ' + (units[index] ?? units[0]);
}

export const fmtTime = (ts: number): string => new Date(ts * 1000).toLocaleString('ru-RU');

export function fmtUptime(seconds: number): string {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const labels = [t('d'), t('h'), t('m')];
  return (days ? `${days}${labels[0]} ` : '') + `${hours}${labels[1]} ${minutes}${labels[2]}`;
}

export const fmtNum = (n: number): string =>
  Number(n).toLocaleString('ru-RU', { maximumFractionDigits: 1 });

// The decimal comma is applied explicitly: some Node builds (small-icu) fall back to '.' for ru-RU.
export const fmtPct = (value: number): string => fmtNum(Number(value.toFixed(1))).replace('.', ',') + '%';

export function fmtDisk(bytes: number): string {
  const mb = bytes / 1048576;
  if (mb < 1024) return Math.round(mb).toLocaleString(currentLanguage() === 'ru' ? 'ru-RU' : 'en-US') + ' ' + t('MB');
  return (mb / 1024).toFixed(2).replace('.', currentLanguage() === 'ru' ? ',' : '.') + ' ' + t('GB');
}

export const fmtGB = (bytes: number): string =>
  (bytes / 1073741824).toLocaleString(currentLanguage() === 'ru' ? 'ru-RU' : 'en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 });

/**
 * Guest-visible interface name for a machine port: ens0, te1, ge2 — prefix chosen in the machine settings,
 * number = starting port + index (the same rule as on the backend).
 */
export function ifaceName(iface: { iface_type?: string; iface_start?: number } | null | undefined, index: number): string {
  const type = iface?.iface_type === 'te' || iface?.iface_type === 'ge' ? iface.iface_type : 'ens';
  const number = (iface?.iface_start ?? 0) + index;
  // A number < 0 is possible only with a -1 shift: nic0 is the MGMT control port.
  return number < 0 ? 'MGMT' : `${type}${number}`;
}

export const levelOf = (pct: number): 'ok' | 'warn' | 'bad' =>
  pct >= 85 ? 'bad' : pct >= 60 ? 'warn' : 'ok';

export function errorText(error: unknown): string {
  if (error instanceof TypeError) return t('No connection to the server. Check your connection and try again.');
  if (error instanceof Error) return error.message;
  return String(error);
}

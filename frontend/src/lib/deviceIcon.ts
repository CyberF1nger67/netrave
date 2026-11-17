/**
  * Standard machine icons. The files live in `public/img` and are embedded in the backend's
  * static files. The keys match `DeviceIcons` on the server and are stored in
  * `device.icon`/`template.icon`, so they must not be renamed.
 */
export interface StandardIcon {
  key: string;
  label: string;
  src: string;
}

export const STANDARD_ICONS: StandardIcon[] = [
  { key: '', label: 'Auto', src: '' },
  { key: 'pc', label: 'Desktop', src: '/img/pc.svg' },
  { key: 'workstation', label: 'Workstation', src: '/img/workstation.svg' },
  { key: 'laptop', label: 'Laptop', src: '/img/laptop.svg' },
  { key: 'monitor', label: 'Monitor', src: '/img/monitor.svg' },
  { key: 'server', label: 'Server', src: '/img/server.svg' },
  { key: 'server-linux', label: 'Linux server', src: '/img/server-linux.svg' },
  { key: 'switch', label: 'Switch', src: '/img/switch.svg' },
  { key: 'router', label: 'Router', src: '/img/router.svg' },
  { key: 'firewall', label: 'Firewall', src: '/img/firewall.svg' },
  { key: 'cloud', label: 'Cloud', src: '/img/cloud.svg' },
];

const BY_KEY = new Map(STANDARD_ICONS.map((item) => [item.key, item.src]));

/** Automatic icon by machine or network type. */
export function iconFor(type: string): string {
  if (BY_KEY.has(type)) return BY_KEY.get(type) || '/img/pc.svg';
  switch (type) {
    case 'router':
      return '/img/router.svg';
    case 'firewall':
      return '/img/firewall.svg';
    case 'switch':
      return '/img/switch.svg';
    case 'server':
      return '/img/server.svg';
    case 'linux':
      return '/img/server-linux.svg';
    case 'laptop':
      return '/img/laptop.svg';
    case 'monitor':
      return '/img/monitor.svg';
    case 'nat':
    case 'cloud':
    case 'isolated':
      return '/img/cloud.svg';
    case 'windows':
    default:
      return '/img/pc.svg';
  }
}

/** Machine image URL: the custom icon takes precedence over the standard one, then the icon by type. */
export function deviceIconSrc(device: { icon_url?: string; icon?: string; type?: string }): string {
  return device.icon_url || iconFor(device.icon || device.type || 'generic');
}

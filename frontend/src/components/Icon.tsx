import type { JSX } from 'react';

export const ICON_PATHS = {
  menu: 'M4 7h16M4 12h16M4 17h16',
  chevron: 'M15 6l-6 6 6 6',
  moon: 'M21 12.8A9 9 0 1111.2 3a7 7 0 009.8 9.8z',
  logout: 'M9 21H5a2 2 0 01-2-2V5a2 2 0 012-2h4M16 17l5-5-5-5M21 12H9',
  bell: 'M18 16v-5a6 6 0 10-12 0v5l-2 2h16zM10 21a2 2 0 004 0',
  mail: 'M4 6h16v12H4zM4 7l8 6 8-6',
  info: 'M12 21a9 9 0 100-18 9 9 0 000 18zM12 8h.01M11 12h1v4h1',
  success: 'M20 6L9 17l-5-5',
  warning: 'M12 9v4M12 17h.01M10.3 3.9L1.8 18a2 2 0 001.7 3h17a2 2 0 001.7-3L13.7 3.9a2 2 0 00-3.4 0z',
  error: 'M12 21a9 9 0 100-18 9 9 0 000 18zM9 9l6 6M15 9l-6 6',
  read: 'M5 12l4 4 10-10',
  unread: 'M12 21a9 9 0 100-18 9 9 0 000 18z',
  trash: 'M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13',
  eye: 'M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7S2 12 2 12zM12 15a3 3 0 100-6 3 3 0 000 6z',
  eyeOff: 'M3 3l18 18M10.6 5.1A10 10 0 0112 5c6.4 0 10 7 10 7a18 18 0 01-3.2 4.2M6.6 6.6A18 18 0 002 12s3.6 7 10 7a10 10 0 005.4-1.6M9.9 9.9a3 3 0 004.2 4.2',
  alert: 'M12 9v4M12 17h.01M10.3 3.9L1.8 18a2 2 0 001.7 3h17a2 2 0 001.7-3L13.7 3.9a2 2 0 00-3.4 0z',
  arrow: 'M5 12h14M13 6l6 6-6 6',
  user: 'M12 12a4 4 0 100-8 4 4 0 000 8zM4 20a8 8 0 0116 0',
  lock: 'M6 11h12v10H6zM8 11V7a4 4 0 018 0v4',
  labs: 'M4 5h5v5H4zM15 14h5v5h-5zM15 5h5v5h-5zM9 7.5h6M17.5 10v4M6.5 10v4',
  running: 'M3 12h4l2.5-6 4 12 2.5-6H21',
  templates: 'M12 3l9 5-9 5-9-5 9-5zM3 13l9 5 9-5',
  images: 'M4 6c0-1.1 3.6-2 8-2s8 .9 8 2v12c0 1.1-3.6 2-8 2s-8-.9-8-2V6zM4 6c0 1.1 3.6 2 8 2s8-.9 8-2M4 12c0 1.1 3.6 2 8 2s8-.9 8-2',
  icons: 'M4 4h7v7H4zM13 4h7v7h-7zM4 13h7v7H4zM13 13h7v7h-7z',
  dashboard: 'M4 15a8 8 0 1116 0M12 15l3.5-3.5M8 19h8',
  logs: 'M8 6h12M8 12h12M8 18h12M4 6h.01M4 12h.01M4 18h.01',
  settings: 'M4 7h9M17 7h3M4 17h3M11 17h9M15 5v4M9 15v4',
  themes: 'M12 3a9 9 0 100 18c1.1 0 1.8-.9 1.8-1.8 0-.5-.2-.9-.5-1.2-.3-.3-.5-.7-.5-1.2 0-1 .8-1.8 1.8-1.8H17a4 4 0 004-4c0-4.4-4-8-9-8zM7.5 11.5h.01M10 7.5h.01M14.5 7.5h.01',
  users: 'M16 19v-1a4 4 0 00-4-4H8a4 4 0 00-4 4v1M12 11a4 4 0 100-8 4 4 0 000 8z',
  account: 'M12 12a4 4 0 100-8 4 4 0 000 8zM4 20a8 8 0 0116 0',
  play: 'M8 5v14l11-7z',
  stop: 'M6 6h12v12H6z',
  pause: 'M8 5h3v14H8zM13 5h3v14h-3z',
  restart: 'M20 11a8 8 0 1 0-2.3 5.7M20 5v6h-6',
  term: 'M4 5h16v11H4zM8 20h8M12 16v4M7.5 9l2 2-2 2M12.5 13h3.5',
  open: 'M14 4h6v6M20 4l-9 9M10 6H5v13h13v-5',
  up: 'M6 15l6-6 6 6',
  down: 'M6 9l6 6 6-6',
  folder: 'M3 7a2 2 0 012-2h4l2 2h8a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2z',
  edit: 'M4 20h4L19 9l-4-4L4 16v4zM14 6l4 4',
  machine: 'M4 6h16v10H4zM9 20h6M12 16v4',
  network: 'M5 5h4v4H5zM15 15h4v4h-4zM15 5h4v4h-4zM9 7h6M17 9v6M7 9v6',
  access: 'M16 19v-1a4 4 0 00-4-4H8a4 4 0 00-4 4v1M12 11a4 4 0 100-8 4 4 0 000 8z',
  export: 'M12 4v11M7 10l5 5 5-5M5 19h14',
  wipe: 'M4 20h10M6 16l8-10 4 3-6 9-4-2zM6 16l-2 4',
  plus: 'M12 5v14M5 12h14',
  minus: 'M5 12h14',
  zoomIn: 'M11 5a6 6 0 110 12 6 6 0 010-12zM16 16l4 4M8 11h6M11 8v6',
  zoomOut: 'M11 5a6 6 0 110 12 6 6 0 010-12zM16 16l4 4M8 11h6',
  fit: 'M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5',
  status: 'M3 12h4l2.5-6 5 12 2.5-6H21',
  globe: 'M12 21a9 9 0 100-18 9 9 0 000 18zM3 12h18M12 3c2.5 2.5 3.5 5.5 3.5 9s-1 6.5-3.5 9c-2.5-2.5-3.5-5.5-3.5-9S9.5 5.5 12 3z',
} as const;

export type IconName = keyof typeof ICON_PATHS;

export function Icon({ name, size = 18, className = '' }: { name: IconName; size?: number; className?: string }): JSX.Element {
  return (
    <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor"
      strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" className={`inline-block max-w-none shrink-0 ${className}`} aria-hidden="true">
      <path d={ICON_PATHS[name]} />
    </svg>
  );
}

import type { JSX } from 'react';

export function LogoMark({ size = 24 }: { size?: number }): JSX.Element {
  return (
    <svg viewBox="0 0 40 40" width={size} height={size} aria-hidden="true">
      <path d="M20 4l13.5 7.8v15.6L20 36 6.5 27.4V11.8z" className="fill-none stroke-white stroke-[2.4] [stroke-linejoin:round]" />
      <path d="M13 15l7 5 7-5M20 20v7" className="fill-none stroke-white/85 stroke-2 [stroke-linecap:round]" />
      <circle cx="13" cy="15" r="2.6" className="fill-white" />
      <circle cx="27" cy="15" r="2.6" className="fill-white" />
      <circle cx="20" cy="28" r="2.6" className="fill-white" />
    </svg>
  );
}

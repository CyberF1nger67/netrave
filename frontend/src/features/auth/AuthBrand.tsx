import type { JSX } from 'react';

// NetRave wordmark and mark for the sign-in screen, built with Tailwind classes
// only so the screen remains independent of the shared app chrome.
export function AuthBrand(): JSX.Element {
  return (
    <div className="flex min-w-0 items-center gap-3">
      <span className="grid h-11 w-11 place-items-center rounded-2xl bg-gradient-to-br from-[#6366f1] to-[#3b82f6] shadow-[0_12px_32px_-12px_rgba(99,102,241,0.8)]">
        <svg viewBox="0 0 40 40" width={24} height={24} aria-hidden="true">
          <path d="M20 4l13.5 7.8v15.6L20 36 6.5 27.4V11.8z" fill="none" strokeWidth={2.4} strokeLinejoin="round" className="stroke-white" />
          <path d="M13 15l7 5 7-5M20 20v7" fill="none" strokeWidth={2} strokeLinecap="round" className="stroke-white/85" />
          <circle cx="13" cy="15" r="2.6" className="fill-white" />
          <circle cx="27" cy="15" r="2.6" className="fill-white" />
          <circle cx="20" cy="28" r="2.6" className="fill-white" />
        </svg>
      </span>
      <span>
        <span className="block text-[19px] font-extrabold leading-none tracking-tight text-[#e7edf5]">NetRave</span>
        <span className="mt-1 hidden text-[10.5px] font-medium uppercase tracking-[0.18em] text-[#8b99ad] sm:block">QEMU · KVM · NETWORK</span>
      </span>
    </div>
  );
}

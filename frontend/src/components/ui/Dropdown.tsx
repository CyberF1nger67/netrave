import { useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';

export interface DropdownOption {
  value: string;
  label: ReactNode;
  disabled?: boolean;
}

const BUTTON = 'flex min-h-9 w-full items-center justify-between gap-2 rounded-[9px] border border-line bg-bg-2 px-2.5 py-1.5 text-left text-[13.5px] text-text transition-[border-color,background,box-shadow] duration-150 hover:border-line-2 focus-visible:border-accent focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent-soft disabled:opacity-45';
const PANEL = 'absolute left-0 right-0 top-[calc(100%+4px)] z-[95] max-h-[280px] overflow-auto rounded-[9px] border border-line bg-panel p-1 shadow-app animate-fade-in';
const ITEM = 'flex w-full items-center justify-between gap-2 rounded-md border border-transparent bg-transparent px-2 py-1.5 text-left text-[13px] text-text hover:bg-panel2 disabled:cursor-default disabled:opacity-45';

/**
  * Dropdown styled like the other elements: the same border, background and radius
  * as inputs and buttons. The value is exposed to screen readers and tests via
  * `role="listbox"`/`option`, so the list remains keyboard accessible.
 */
export function Dropdown({ value, options, onChange, disabled, title, ariaLabel, className = '' }: {
  value: string;
  options: DropdownOption[];
  onChange: (value: string) => void;
  disabled?: boolean;
  title?: string;
  ariaLabel?: string;
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const current = options.find((option) => option.value === value);

  useEffect(() => {
    if (!open) return;
    const down = (event: MouseEvent) => {
      if (!(event.target instanceof Node) || !root.current?.contains(event.target)) setOpen(false);
    };
    const key = (event: KeyboardEvent) => { if (event.key === 'Escape') setOpen(false); };
    document.addEventListener('mousedown', down);
    document.addEventListener('keydown', key);
    return () => { document.removeEventListener('mousedown', down); document.removeEventListener('keydown', key); };
  }, [open]);

  const pick = (next: string) => { setOpen(false); if (next !== value) onChange(next); };

  return (
    <div ref={root} className={`relative min-w-0 ${className}`}>
      <button type="button" className={BUTTON} disabled={disabled} title={title} aria-haspopup="listbox" aria-expanded={open} aria-label={ariaLabel}
        onClick={(event) => { event.stopPropagation(); setOpen((state) => !state); }}>
        <span className="min-w-0 truncate">{current?.label ?? ''}</span>
        <svg viewBox="0 0 12 8" width="12" height="8" aria-hidden="true" className={`shrink-0 transition-transform duration-200 ${open ? 'rotate-180' : ''}`}>
          <path d="M1 1.5 6 6.5l5-5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" opacity=".7" />
        </svg>
      </button>
      {open && (
        <div className={PANEL} role="listbox" onClick={(event) => event.stopPropagation()}>
          {options.map((option) => (
            <button key={option.value} type="button" role="option" aria-selected={option.value === value} disabled={option.disabled}
              className={`${ITEM}${option.value === value ? ' bg-accent-soft font-semibold' : ''}`} onClick={() => pick(option.value)}>
              <span className="min-w-0 truncate">{option.label}</span>
              {option.value === value && <span aria-hidden="true" className="text-accent">✓</span>}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

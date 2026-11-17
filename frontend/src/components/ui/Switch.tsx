import type { ReactNode } from 'react';

/**
  * Reliable switch (toggle). It is the same `<input type="checkbox">`, so
  * forms, tests and `checked/onChange` handlers keep working, but visually
  * the element looks like a modern slider and is colored with the theme color.
 */
export function Switch({ checked, onChange, disabled, label, title, ariaLabel, className = '' }: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
  /** Label next to the switch. */
  label?: ReactNode;
  title?: string;
  /** Accessible name for switches without a visible label (e.g. in tables). */
  ariaLabel?: string;
  className?: string;
}) {
  return (
    <label className={`app-switch${disabled ? ' app-switch-disabled' : ''}${label ? '' : ' app-switch-compact'} ${className}`} title={title}>
      <input type="checkbox" checked={checked} disabled={disabled} aria-label={ariaLabel} onChange={(event) => onChange(event.target.checked)} />
      <span className="app-switch-track" aria-hidden="true"><i className="app-switch-knob" /></span>
      {label && <span className="app-switch-label">{label}</span>}
    </label>
  );
}

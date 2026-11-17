import { useId } from 'react';

/**
  * Circular checkbox for selecting items in tables. It is a regular `<input type="checkbox">`:
  * state and handlers are the same as `<Switch>`, but visually it is a circle with a check in theme colors.
  * The indeterminate state (some items selected) is shown as a dash.
 */
export function RoundCheck({ checked, indeterminate = false, onChange, title, ariaLabel, disabled, className = '' }: {
  checked: boolean;
  /** Some items are selected: the intermediate state is shown. */
  indeterminate?: boolean;
  onChange: (checked: boolean) => void;
  title?: string;
  ariaLabel?: string;
  disabled?: boolean;
  className?: string;
}) {
  const id = useId();
  return (
    <label className={`app-round-check${disabled ? ' app-round-check-disabled' : ''} ${className}`} title={title} htmlFor={id}>
      <input id={id} type="checkbox" checked={checked} disabled={disabled} aria-label={ariaLabel}
        aria-checked={indeterminate ? 'mixed' : checked}
        onChange={(event) => onChange(event.target.checked)} />
      <span className="app-round-check-box" aria-hidden="true">
        <svg viewBox="0 0 16 16" width="11" height="11" className={indeterminate && !checked ? 'app-round-check-dash' : 'app-round-check-tick'}>
          {indeterminate && !checked
            ? <path d="M3.5 8h9" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
            : <path d="M3.2 8.4l3 3 6.6-6.8" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" />}
        </svg>
      </span>
    </label>
  );
}

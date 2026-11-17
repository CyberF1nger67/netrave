import { useEffect, useState } from 'react';
import type { InputHTMLAttributes } from 'react';

type NumberFieldProps = Omit<InputHTMLAttributes<HTMLInputElement>, 'value' | 'onChange' | 'type' | 'min' | 'max'> & {
  value: number;
  onValueChange: (value: number) => void;
  min: number;
  max: number;
};

const toInteger = (text: string) => Math.trunc(Number(text));
const inRange = (n: number, min: number, max: number) => Number.isFinite(n) && n >= min && n <= max;

// A numeric input with a local string draft. The user may clear the field,
// type multi-digit numbers, or pass through intermediate values without the
// field snapping back. The parent receives a number only when the draft is a
// valid integer inside [min, max], so an invalid intermediate state is never
// written up. Out-of-range or empty text is corrected on blur.
export function NumberField({ value, onValueChange, min, max, onBlur, ...rest }: NumberFieldProps) {
  const [draft, setDraft] = useState(String(value));

  // Follow the parent only when its committed value changes (data loaded,
  // a template picked), never on every render, so typing is not overwritten.
  useEffect(() => {
    setDraft(String(value));
  }, [value]);

  return (
    <input
      {...rest}
      type="number"
      inputMode="numeric"
      min={min}
      max={max}
      step={1}
      value={draft}
      onChange={(event) => {
        const text = event.target.value;
        setDraft(text);
        if (text.trim() === '') return;
        const next = toInteger(text);
        if (inRange(next, min, max) && next !== value) onValueChange(next);
      }}
      onBlur={(event) => {
        const text = event.target.value.trim();
        const parsed = toInteger(text);
        if (text === '' || !Number.isFinite(parsed)) {
          setDraft(String(value));
        } else {
          const clamped = Math.min(max, Math.max(min, parsed));
          setDraft(String(clamped));
          if (clamped !== value) onValueChange(clamped);
        }
        onBlur?.(event);
      }}
    />
  );
}

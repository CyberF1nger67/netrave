import { useState } from 'react';
import type { InputHTMLAttributes, Ref } from 'react';
import { Eye, EyeOff } from 'lucide-react';
import { t } from '../../i18n';

interface PasswordInputProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'type'> {
  inputRef?: Ref<HTMLInputElement>;
}

/**
 * Password field with an independent show/hide control. The toggle is a
 * separate button so it never submits the surrounding form.
 */
export function PasswordInput({ inputRef, className = '', disabled, ...props }: PasswordInputProps) {
  const [visible, setVisible] = useState(false);
  const label = visible ? t('Hide password') : t('Show password');
  return (
    <div className="relative min-w-0">
      <input
        {...props}
        ref={inputRef}
        type={visible ? 'text' : 'password'}
        disabled={disabled}
        className={`pr-11 ${className}`}
      />
      <button
        type="button"
        title={label}
        aria-label={label}
        aria-pressed={visible}
        disabled={disabled}
        onClick={() => setVisible((value) => !value)}
        className="absolute inset-y-0 right-0 flex w-11 items-center justify-center rounded-l-none rounded-r-[var(--radius-sm)] border-0 bg-transparent p-0 text-muted shadow-none hover:bg-transparent hover:text-text"
      >
        {visible ? <EyeOff size={18} aria-hidden="true" /> : <Eye size={18} aria-hidden="true" />}
      </button>
    </div>
  );
}

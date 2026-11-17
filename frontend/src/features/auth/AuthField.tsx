import type { ComponentPropsWithRef, ReactNode } from 'react';
import { Icon } from '../../components/Icon';
import type { IconName } from '../../components/Icon';
import { authUi } from './authTheme';

interface AuthFieldProps extends ComponentPropsWithRef<'input'> {
  id: string;
  label: string;
  icon: IconName;
  invalid?: boolean;
  trailing?: ReactNode;
}

// A labelled input with a leading icon and an optional trailing control.
// The label is associated with the input via htmlFor, so it stays accessible.
export function AuthField({ id, label, icon, invalid = false, trailing, className = '', ...input }: AuthFieldProps) {
  return (
    <>
      <label htmlFor={id} className={authUi.label}>{label}</label>
      <div className="relative">
        <span className={authUi.leadingIcon}><Icon name={icon} size={18} className="inline-block shrink-0" /></span>
        <input
          {...input}
          id={id}
          aria-invalid={invalid || undefined}
          className={`${authUi.input}${trailing ? ` ${authUi.inputWithToggle}` : ''}${invalid ? ` ${authUi.inputInvalid}` : ''} ${className}`.trim()}
        />
        {trailing}
      </div>
    </>
  );
}

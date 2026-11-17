import { useCallback, useState } from 'react';
import type { FormEvent } from 'react';
import { Modal } from './Modal';
import { PasswordInput } from './PasswordInput';
import { errorText } from '../../lib/format';
import { toast } from '../../stores/toast';
import { t } from '../../i18n';

const primary = 'border-transparent bg-gradient-to-br from-accent to-accent-2 font-semibold text-on-accent shadow-[0_6px_18px_-6px_rgba(99,102,241,.55)] hover:brightness-110';

interface PasswordDialogProps {
  title?: string;
  description?: string;
  /** Account changes require the current password; administrator resets do not. */
  requireCurrent?: boolean;
  successMessage?: string;
  onClose: () => void;
  onSubmit: (current: string, next: string, confirm: string) => Promise<void>;
}

/**
 * Native password dialog used both for the account page and for administrator
 * resets. It owns confirmation matching, inline errors and the saving guard so
 * callers only implement the API request.
 */
export function PasswordDialog({
  title,
  description,
  requireCurrent = true,
  successMessage,
  onClose,
  onSubmit,
}: PasswordDialogProps) {
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);

  const clear = () => {
    setCurrent('');
    setNext('');
    setConfirm('');
    setError('');
  };

  const handleClose = useCallback(() => {
    if (saving) return;
    setCurrent('');
    setNext('');
    setConfirm('');
    setError('');
    onClose();
  }, [saving, onClose]);

  const mismatch = confirm.length > 0 && next !== confirm;
  const lengthInvalid = next.length > 0 && (next.length < 8 || next.length > 128);
  const complete = (!requireCurrent || current.length > 0) && next.length >= 8 && next.length <= 128 && confirm === next;
  const visibleError = error || (mismatch ? t('Passwords do not match') : '');

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (saving) return;
    if (requireCurrent && !current) {
      setError(t('Enter current password'));
      return;
    }
    if (!next) {
      setError(t('Enter new password'));
      return;
    }
    if (next.length < 8 || next.length > 128) {
      setError(t('New password must contain 8 to 128 characters'));
      return;
    }
    if (!confirm) {
      setError(t('Enter the same password again'));
      return;
    }
    if (next !== confirm) {
      setError(t('Passwords do not match'));
      return;
    }
    setError('');
    setSaving(true);
    try {
      await onSubmit(current, next, confirm);
      clear();
      if (successMessage) toast.success(successMessage);
      onClose();
    } catch (cause) {
      setError(errorText(cause));
      setSaving(false);
    }
  };

  return (
    <Modal title={title ?? t('Change Password')} onClose={handleClose}>
      <form noValidate onSubmit={submit}>
        {description && <p className="mb-4 mt-0 text-sm leading-relaxed text-muted">{description}</p>}
        {requireCurrent && (
          <>
            <label htmlFor="password-dialog-current">{t('Current password')}</label>
            <PasswordInput
              id="password-dialog-current"
              autoComplete="current-password"
              value={current}
              disabled={saving}
              placeholder={t('Enter current password')}
              onChange={(event) => { setCurrent(event.target.value); if (error) setError(''); }}
            />
          </>
        )}
        <label htmlFor="password-dialog-new">{t('New password (8–128 characters)')}</label>
        <PasswordInput
          id="password-dialog-new"
          autoComplete="new-password"
          minLength={8}
          maxLength={128}
          value={next}
          disabled={saving}
          placeholder={t('Enter new password')}
          onChange={(event) => { setNext(event.target.value); if (error) setError(''); }}
        />
        <label htmlFor="password-dialog-confirm">{t('Confirm Password')}</label>
        <PasswordInput
          id="password-dialog-confirm"
          autoComplete="new-password"
          minLength={8}
          maxLength={128}
          value={confirm}
          disabled={saving}
          placeholder={t('Enter the same password again')}
          onChange={(event) => { setConfirm(event.target.value); if (error) setError(''); }}
        />
        {(visibleError || lengthInvalid) && (
          <div role="alert" aria-live="polite" className="mt-2.5 text-xs leading-relaxed text-bad">
            {visibleError || t('New password must contain 8 to 128 characters')}
          </div>
        )}
        <div className="mt-5 flex flex-wrap justify-end gap-2 max-sm:[&>button]:flex-auto">
          <button type="button" disabled={saving} onClick={handleClose}>{t('Cancel')}</button>
          <button type="submit" className={primary} disabled={saving || !complete}>{saving ? t('Saving…') : t('Save Changes')}</button>
        </div>
      </form>
    </Modal>
  );
}

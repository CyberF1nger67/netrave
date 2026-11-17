import { useState } from 'react';
import { canChangePassword, useAuthStore } from '../../stores/auth';
import { authApi } from '../../api/endpoints';
import { PasswordDialog } from '../../components/ui/PasswordDialog';
import { t, useLanguageStore } from '../../i18n';

/** Local state of an open password change dialog - for the profile menu. */
export function useChangePassword(): { open: boolean; show: () => void; close: () => void } {
  const [open, setOpen] = useState(false);
  return { open, show: () => setOpen(true), close: () => setOpen(false) };
}

/**
  * Changes the current user's password. Opened from the profile menu with a separate
  * button and uses the shared password change dialog.
 */
export function ChangePasswordDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const user = useAuthStore((state) => state.user);
  useLanguageStore((state) => state.language);
  if (!open) return null;
  if (!canChangePassword(user)) {
    return (
      <PasswordDialog
        requireCurrent
        description={t('Changing the password is disabled by the administrator')}
        successMessage={t('Password changed successfully')}
        onClose={onClose}
        onSubmit={async () => undefined}
      />
    );
  }
  return (
    <PasswordDialog
      requireCurrent
      successMessage={t('Password changed successfully')}
      onClose={onClose}
      onSubmit={async (current, next, confirm) => {
        await authApi.changePassword(current, next, confirm);
      }}
    />
  );
}

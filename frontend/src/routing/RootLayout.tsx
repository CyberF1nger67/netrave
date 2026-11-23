import { useEffect } from 'react';
import { Outlet } from 'react-router-dom';
import { Toaster } from '../components/Toaster';
import { ConfirmHost } from '../components/ConfirmHost';
import { closeAllConsoleSessions } from '../features/consoles/runtime';
import { useAuthStore } from '../stores/auth';
import { useThemeStore } from '../stores/theme';
import { useLanguageStore } from '../i18n';

/** Tree root: one-time session check, user theme and global toasts. */
export function RootLayout() {
  const user = useAuthStore((state) => state.user);
  const bootstrap = useAuthStore((state) => state.bootstrap);
  const loadTheme = useThemeStore((state) => state.load);
  const resetTheme = useThemeStore((state) => state.reset);
  useLanguageStore((state) => state.language);

  useEffect(() => {
    if (useAuthStore.getState().status === 'loading') void bootstrap();
  }, [bootstrap]);

  useEffect(() => {
    if (user) void loadTheme(user.id);
    else {
      resetTheme();
      closeAllConsoleSessions();
    }
  }, [user, loadTheme, resetTheme]);

  return (
    <>
      <Outlet />
      <Toaster />
      <ConfirmHost />
    </>
  );
}

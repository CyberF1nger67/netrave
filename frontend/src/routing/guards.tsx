import { Navigate, Outlet, useLocation } from 'react-router-dom';
import { canPage, useAuthStore } from '../stores/auth';
import type { PageDef } from '../stores/auth';
import { Splash } from '../components/layout/Splash';
import { NotificationsLayer } from '../features/notifications/NotificationsLayer';
import { paths } from './paths';

/**
  * Private part of the application. Until the session is verified, nothing is rendered;
  * an unauthorized user is sent to login, and we remember where they were going.
 */
export function RequireAuth() {
  const status = useAuthStore((state) => state.status);
  const user = useAuthStore((state) => state.user);
  const location = useLocation();
  if (status === 'loading') return <Splash />;
  if (status === 'guest' || !user) {
    return <Navigate to={paths.login} replace state={{ from: `${location.pathname}${location.search}` }} />;
  }
  return (
    <>
      <Outlet />
      <NotificationsLayer />
    </>
  );
}

/** Login page: an authorized user is returned to where they were sent from. */
export function GuestOnly() {
  const status = useAuthStore((state) => state.status);
  const user = useAuthStore((state) => state.user);
  const location = useLocation();
  if (status === 'loading') return <Splash />;
  if (user) {
    const from = (location.state as { from?: unknown } | null)?.from;
    const target = typeof from === 'string' && from.startsWith('/') && !from.startsWith(paths.login) ? from : paths.labs;
    return <Navigate to={target} replace />;
  }
  return <Outlet />;
}

/** Permission for a section. Without it, send to labs that are available to everyone. */
export function RequirePage({ page }: { page: PageDef }) {
  const user = useAuthStore((state) => state.user);
  if (!canPage(user, page)) return <Navigate to={paths.labs} replace />;
  return <Outlet />;
}

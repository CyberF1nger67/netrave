import type { RouteObject } from 'react-router-dom';
import { DashboardPage } from '../../features/dashboard/DashboardPage';
import { RequirePage } from '../guards';
import { PAGES } from '../pages';
import { paths } from '../paths';

// Administration and the journal are loaded on demand: they are not needed in the initial bundle.
export const systemRoutes: RouteObject[] = [
  {
    element: <RequirePage page={PAGES.dashboard} />,
    children: [{ path: paths.dashboard, element: <DashboardPage /> }],
  },
  {
    element: <RequirePage page={PAGES.journal} />,
    children: [
      { path: paths.journal, lazy: () => import('../../features/logs/LogsPage').then((m) => ({ Component: m.LogsPage })) },
    ],
  },
  {
    element: <RequirePage page={PAGES.settings} />,
    children: [{ path: paths.settings, lazy: () => import('../../features/settings/SettingsPage').then((m) => ({ Component: m.SettingsPage })) }],
  },
  {
    element: <RequirePage page={PAGES.users} />,
    children: [{ path: paths.users, lazy: () => import('../../features/users/UsersPage').then((m) => ({ Component: m.UsersPage })) }],
  },
  {
    element: <RequirePage page={PAGES.messages} />,
    children: [{ path: paths.messages, lazy: () => import('../../features/messages/MessagesPage').then((m) => ({ Component: m.MessagesPage })) }],
  },
  {
    element: <RequirePage page={PAGES.roles} />,
    children: [{ path: paths.roles, lazy: () => import('../../features/roles/RolesPage').then((m) => ({ Component: m.RolesPage })) }],
  },
];

import { Navigate } from 'react-router-dom';
import type { RouteObject } from 'react-router-dom';
import { ShellLayout } from '../components/layout/ShellLayout';
import { Splash } from '../components/layout/Splash';
import { LoginPage } from '../features/auth/LoginPage';
import { NotFoundPage } from '../features/errors/NotFoundPage';
import { GuestOnly, RequireAuth } from './guards';
import { RootLayout } from './RootLayout';
import { editorRoutes, labRoutes } from './modules/labs';
import { resourceRoutes } from './modules/resources';
import { systemRoutes } from './modules/system';
import { paths } from './paths';

/**
  * Single route tree.
  * - /login - guests only; everything else - authenticated users only.
  * - The editor is rendered without the side panel, other pages - inside ShellLayout.
  * - Any unknown URL of an authenticated user lands on the 404 page.
 */
export const routeTree: RouteObject[] = [
  {
    element: <RootLayout />,
    // Shown while a lazy route chunk is loading when its URL is opened directly.
    hydrateFallbackElement: <Splash />,
    children: [
      {
        element: <GuestOnly />,
        children: [{ path: paths.login, element: <LoginPage /> }],
      },
      {
        element: <RequireAuth />,
        children: [
          ...editorRoutes,
          {
            element: <ShellLayout />,
            children: [
              { index: true, element: <Navigate to={paths.labs} replace /> },
              ...labRoutes,
              ...resourceRoutes,
              ...systemRoutes,
              { path: '*', element: <NotFoundPage /> },
            ],
          },
        ],
      },
    ],
  },
];

import type { RouteObject } from 'react-router-dom';
import { LabsPage } from '../../features/labs/LabsPage';
import { RunningPage } from '../../features/running/RunningPage';
import { paths } from '../paths';

// The full-screen topology editor is rendered outside the shared shell, but behind authentication.
// URLs: `/labs/<folder>/<lab>` and `/labs/<lab>` for labs at the root. A single segment
// may also be a folder, so the route `/labs/:folderName` decides on the spot: a folder opens the list
// inside the shell (child route), a lab opens the editor. The numeric `/labs/<id>/editor` is handled
// by the same screen (see LabByNamePage).
export const editorRoutes: RouteObject[] = [
  {
    path: '/labs/:labName/editor',
    lazy: () => import('../../features/editor/LabByNamePage').then((m) => ({ Component: m.LabByNamePage })),
  },
  {
    path: '/labs/:folderName/:labName',
    lazy: () => import('../../features/editor/LabByNamePage').then((m) => ({ Component: m.LabByNamePage })),
  },
  {
    path: '/labs/:folderName',
    lazy: () => import('../../features/editor/LabOrFolderPage').then((m) => ({ Component: m.LabOrFolderPage })),
    children: [{ index: true, element: <LabsPage /> }],
  },
];

export const labRoutes: RouteObject[] = [
  { path: paths.labs, element: <LabsPage /> },
  { path: paths.running, element: <RunningPage /> },
];

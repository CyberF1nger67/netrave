import type { RouteObject } from 'react-router-dom';
import { TemplatesPage } from '../../features/templates/TemplatesPage';
import { ImagesPage } from '../../features/images/ImagesPage';
import { IconsPage } from '../../features/icons/IconsPage';
import { ThemesPage } from '../../features/themes/ThemesPage';
import { RequirePage } from '../guards';
import { PAGES } from '../pages';
import { paths } from '../paths';

export const resourceRoutes: RouteObject[] = [
  {
    element: <RequirePage page={PAGES.templates} />,
    children: [{ path: paths.templates, element: <TemplatesPage /> }],
  },
  {
    element: <RequirePage page={PAGES.images} />,
    children: [{ path: paths.images, element: <ImagesPage /> }],
  },
  {
    element: <RequirePage page={PAGES.icons} />,
    children: [{ path: paths.icons, element: <IconsPage /> }],
  },
  { path: paths.themes, element: <ThemesPage /> },
];

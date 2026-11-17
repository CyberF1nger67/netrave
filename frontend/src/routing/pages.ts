import type { IconName } from '../components/Icon';
import type { PageDef } from '../stores/auth';
import type { t } from '../i18n';
import { paths } from './paths';

export type TranslationKey = Parameters<typeof t>[0];

export type NavGroup = 'Work' | 'Resources' | 'System' | 'Administration';

export interface PageMeta extends PageDef {
  title: TranslationKey;
  path: string;
  icon: IconName;
}

/** Page registry: title, access permission, path and icon. Used by both the router and navigation. */
export const PAGES = {
  labs: { title: 'Laboratories', path: paths.labs, icon: 'labs' },
  running: { title: 'Running labs', path: paths.running, icon: 'running' },
  templates: { title: 'Templates', path: paths.templates, perm: 'templates', icon: 'templates' },
  images: { title: 'Images', path: paths.images, perm: 'images', icon: 'images' },
  icons: { title: 'Icons', path: paths.icons, perm: 'icons', icon: 'icons' },
  themes: { title: 'Themes', path: paths.themes, icon: 'themes' },
  dashboard: { title: 'Dashboard', path: paths.dashboard, perm: 'dashboard', icon: 'dashboard' },
  journal: { title: 'Journal', path: paths.journal, perm: 'logs', icon: 'logs' },
  settings: { title: 'Settings', path: paths.settings, admin: true, icon: 'settings' },
  users: { title: 'Users', path: paths.users, perm: 'users.view', icon: 'users' },
  roles: { title: 'Roles', path: paths.roles, admin: true, icon: 'users' },
} satisfies Record<string, PageMeta>;

export type PageKey = keyof typeof PAGES;

/** Side menu groups and the pages in them. The account is opened from the profile menu, so it is not in the groups. */
export const NAV_GROUPS: Array<{ title: NavGroup; items: PageKey[] }> = [
  { title: 'Work', items: ['labs', 'running'] },
  { title: 'Resources', items: ['templates', 'images', 'icons'] },
  { title: 'System', items: ['dashboard', 'journal', 'settings', 'themes'] },
  { title: 'Administration', items: ['users', 'roles'] },
];

/** Page key by the first path segment, e.g. `/users/5` -> `users`. */
export function pageKeyFromPath(pathname: string): PageKey | undefined {
  const segment = pathname.split('/')[1] ?? '';
  return Object.prototype.hasOwnProperty.call(PAGES, segment) ? (segment as PageKey) : undefined;
}

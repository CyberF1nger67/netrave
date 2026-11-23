// Single description of application URLs. All transitions, links and redirects are built only from here,
// so URLs can be changed in one place.
const segment = (value: string): string => encodeURIComponent(value);
const instance = (id?: number | string): string => id === undefined ? '' : `?instance=${encodeURIComponent(String(id))}`;

export const paths = {
  login: '/login',
  labs: '/labs',
  running: '/running',
  templates: '/templates',
  images: '/images',
  icons: '/icons',
  themes: '/themes',
  dashboard: '/dashboard',
  journal: '/journal',
  settings: '/settings',
  users: '/users',
  messages: '/messages',
  roles: '/roles',
  /** Human-readable folder URL: `/labs/<folder_name>`. */
  folder: (folderName: string) => `/labs/${segment(folderName)}`,
  /** Lab URL at the root (without a folder): `/labs/<lab_name>`. */
  rootLab: (labName: string, instanceId?: number | string) => `/labs/${segment(labName)}${instance(instanceId)}`,
  /** Lab URL inside a folder: `/labs/<folder_name>/<lab_name>`. */
  lab: (folderName: string, labName: string, instanceId?: number | string) =>
    `/labs/${segment(folderName)}/${segment(labName)}${instance(instanceId)}`,
  /** Editor URL built by notifications and running labs: `/labs/<name>/editor`. */
  editor: (labName: string, instanceId?: number | string) =>
    `/labs/${segment(labName)}/editor${instance(instanceId)}`,
} as const;

/**
  * Builds a human-readable lab URL from its folder. Root and folder names are unique together,
  * so `/labs/<name>` unambiguously points either to a folder or to a lab at the root.
 */
export function labPath(lab: { name: string; folder_id?: string }, _folderName?: string | null, instanceId?: number | string): string {
  // Laboratory names are unique across the whole tree, so one URL shape serves every lab.
  return paths.editor(lab.name, instanceId);
}

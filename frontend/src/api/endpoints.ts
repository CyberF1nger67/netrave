import { RawBody, request, requestBlob, requestText, uploadFile, uploadImage } from './client';
import type { NicMAC } from '../types/api';
import type {
  AuditEntry, Connection, DashboardLive, Device, DeviceCreateInput, IconItem, IconUpload, ImageItem,
  FolderAccess, Lab, LabFolder, LabInstance, LabPermissions, LabStatus, LabCanvas, Session, Settings, Snapshot, SystemInfo,
  RbacMember, RbacPermission, RbacRole,
  Template, ThemeCatalog, ThemeResponse, ThemeState, Topology, User, VncQualityResponse, VncQualitySettings, WipeResult,
} from '../types/api';

const q = (params: Record<string, string | number | undefined>): string => {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') search.set(key, String(value));
  }
  const text = search.toString();
  return text ? `?${text}` : '';
};

const json = <T>(method: 'POST' | 'PUT' | 'DELETE', path: string, body?: unknown): Promise<T> =>
  request<T>(method, path, body);

export const authApi = {
  login: (username: string, password: string) => json<Session>('POST', '/api/login', { username, password }),
  logout: () => json<{ ok: boolean }>('POST', '/api/logout'),
  me: () => request<Session>('GET', '/api/me'),
  // The server expects the `new` field (see internal/api: changePassword). `confirm`
  // is passed for match validation on the server.
  changePassword: (current: string, next: string, confirm?: string) => json<{ ok: boolean }>('PUT', '/api/me/password', { current, new: next, confirm }),
  getTheme: () => request<ThemeResponse>('GET', '/api/me/theme'),
  getThemePresets: () => request<ThemeCatalog>('GET', '/api/me/theme/presets'),
  putTheme: (theme: ThemeState) => json<ThemeResponse>('PUT', '/api/me/theme', theme),
};

export const systemApi = {
  info: () => request<SystemInfo>('GET', '/api/system'),
  live: () => request<DashboardLive>('GET', '/api/dashboard/live'),
};

/** One page-merging mechanism. */
export interface PageMergingMechanism {
  supported: boolean;
  available: boolean;
  enabled: boolean;
  saved_bytes: number;
  merged: number;
  reason?: string;
}

/** State of both mechanisms: KSM and UKSM are enabled separately and do not run simultaneously. */
export interface PageMergingStatus {
  ksm: PageMergingMechanism;
  uksm: PageMergingMechanism;
}

export const ksmApi = {
  get: () => request<PageMergingStatus>('GET', '/api/ksm'),
  set: (mechanism: 'ksm' | 'uksm', enabled: boolean) =>
    json<PageMergingStatus>('PUT', '/api/ksm', { mechanism, enabled }),
};

export const usersApi = {
  list: () => request<User[]>('GET', '/api/users'),
  create: (username: string, password: string, role: string, confirm?: string, roleId?: number) =>
    json<{ id: number }>('POST', '/api/users', { username, password, role, confirm, role_id: roleId ?? 0 }),
  update: (id: number, patch: { role?: string; disabled?: boolean; password?: string; confirm?: string }) =>
    json<{ ok: boolean }>('PUT', `/api/users/${id}`, patch),
  remove: (id: number) => json<{ ok: boolean }>('DELETE', `/api/users/${id}`),
  folders: (id: number) => request<FolderAccess>('GET', `/api/users/${id}/folders`),
  setFolders: (id: number, folderIds: number[]) =>
    json<{ ok: boolean; folder_ids: number[] }>('PUT', `/api/users/${id}/folders`, { folder_ids: folderIds }),
};

export const rolesApi = {
  list: () => request<RbacRole[]>('GET', '/api/roles'),
  catalog: () => request<RbacPermission[]>('GET', '/api/roles/permissions'),
  get: (id: number) => request<RbacRole>('GET', `/api/roles/${id}`),
  create: (name: string, description: string) =>
    json<RbacRole>('POST', '/api/roles', { name, description }),
  update: (id: number, patch: {
    name?: string; description?: string; permissions?: string[]; folder_ids?: number[]; max_running_labs?: number;
  }) => json<RbacRole>('PUT', `/api/roles/${id}`, patch),
  remove: (id: number) => json<{ ok: boolean }>('DELETE', `/api/roles/${id}`),
  setPermissions: (id: number, permissions: string[]) =>
    json<{ ok: boolean; permissions: string[] }>('PUT', `/api/roles/${id}/permissions`, { permissions }),
  setFolders: (id: number, folderIds: number[]) =>
    json<{ ok: boolean; folder_ids: number[] }>('PUT', `/api/roles/${id}/folders`, { folder_ids: folderIds }),
  members: (id: number) => request<RbacMember[]>('GET', `/api/roles/${id}/members`),
  addMembers: (id: number, userIds: number[]) =>
    json<{ ok: boolean; user_ids: number[] }>('POST', `/api/roles/${id}/members`, { user_ids: userIds }),
  removeMember: (id: number, userId: number) =>
    json<{ ok: boolean }>('DELETE', `/api/roles/${id}/members/${userId}`),
};

export const consoleApi = {
  quality: (rid: number) => request<VncQualityResponse>('GET', `/api/consoles/${rid}/quality`),
  putQuality: (rid: number, settings: VncQualitySettings) =>
    json<VncQualityResponse>('PUT', `/api/consoles/${rid}/quality`, settings),
};

export const settingsApi = {
  get: () => request<Settings>('GET', '/api/settings'),
  put: (settings: Record<string, string>) => json<{ ok: boolean }>('PUT', '/api/settings', settings),
};

export const logsApi = {
  list: (limit = 300) => request<AuditEntry[]>('GET', `/api/logs${q({ limit })}`),
  clear: () => json<{ ok: boolean }>('DELETE', '/api/logs'),
};

export const labsApi = {
  list: (search = '', folder?: number) => request<Lab[]>('GET', `/api/labs${q({ q: search, folder })}`),
  get: (id: number) => request<Lab>('GET', `/api/labs/${id}`),
  create: (name: string, description: string) => json<Lab>('POST', '/api/labs', { name, description }),
  update: (id: number, patch: { name?: string; description?: string; canvas?: LabCanvas }) =>
    json<{ ok: boolean }>('PUT', `/api/labs/${id}`, patch),
  remove: (id: number) => json<{ ok: boolean }>('DELETE', `/api/labs/${id}`),
  copy: (id: number) => json<Lab>('POST', `/api/labs/${id}/copy`),
  exportLab: (id: number) => requestBlob(`/api/labs/${id}/export`),
  importLab: (yaml: string) => request<{ id: number; warnings?: string[] }>('POST', '/api/labs/import', new RawBody(yaml, 'application/yaml')),
  topology: (id: number, instance?: number) =>
    request<Topology>('GET', `/api/labs/${id}/topology${q({ instance })}`),
  getByInstance: (id: number, instance?: number) =>
    request<Lab>('GET', `/api/labs/${id}${q({ instance })}`),
  running: (search = '', owner = '') =>
    request<import('../types/api').RunningLab[]>('GET', `/api/labs/running${q({ q: search, owner })}`),
  instances: (id: number) => request<LabInstance[]>('GET', `/api/labs/${id}/instances`),
  permissions: (id: number) => request<LabPermissions>('GET', `/api/labs/${id}/permissions`),
  setPermission: (labId: number, userId: number, changes: Record<string, boolean>) =>
    json<{ ok: boolean }>('PUT', `/api/labs/${labId}/permissions/${userId}`, { changes }),
  setPublic: (id: number, level: 'none' | 'view') => json<{ ok: boolean }>('PUT', `/api/labs/${id}/public`, { level }),
  positions: (id: number, devices: Array<{ id: number; x: number; y: number; icon_size?: number }>, networks: Array<{ id: number; x: number; y: number }>) =>
    json<{ ok: boolean }>('PUT', `/api/labs/${id}/positions`, { devices, networks }),
  deleteBulk: (id: number, devices: number[], networks: number[], connections: number[]) =>
    json<{ ok: boolean; warnings?: string[] }>('POST', `/api/labs/${id}/delete`, { devices, networks, connections }),
  paste: (id: number, devices: number[], networks: number[], dx: number, dy: number) =>
    json<{ devices: Device[]; networks: import('../types/api').Network[]; connections: Connection[] }>('POST', `/api/labs/${id}/paste`, { devices, networks, dx, dy }),
  createDevice: async (id: number, input: DeviceCreateInput): Promise<Device[]> => {
    const result = await json<Device | Device[]>('POST', `/api/labs/${id}/devices`, input);
    return Array.isArray(result) ? result : [result];
  },
  updateDevice: (rid: number, patch: Record<string, unknown>) => json<Device>('PUT', `/api/devices/${rid}`, patch),
  createNetwork: (id: number, type: string, x: number, y: number) =>
    json<import('../types/api').Network>('POST', `/api/labs/${id}/networks`, { type, x, y }),
  updateNetwork: (id: number, patch: Record<string, unknown>) =>
    json<import('../types/api').Network>('PUT', `/api/networks/${id}`, patch),
  createConnection: (id: number, input: Record<string, unknown>) =>
    json<Connection>('POST', `/api/labs/${id}/connections`, input),
  setConnectionState: (connId: number, up: boolean, instance?: number) =>
    json<{ id: number; up: boolean }>('PUT', `/api/connections/${connId}/state${q({ instance })}`, { up }),
  startAll: (id: number, instance?: number) => json<{ ok: boolean }>('POST', `/api/labs/${id}/start-all${q({ instance })}`),
  stopAll: (id: number, instance?: number) => json<{ ok: boolean }>('POST', `/api/labs/${id}/stop-all${q({ instance })}`),
  wipe: (id: number, instance?: number) => json<WipeResult>('POST', `/api/labs/${id}/wipe${q({ instance })}`),
  status: (id: number, instance?: number) => request<LabStatus>('GET', `/api/labs/${id}/status${q({ instance })}`),
};

export const devicesApi = {
  action: (rid: number, action: string, query = '') =>
    json<{ ok: boolean }>('POST', `/api/devices/${rid}/${action}${query}`),
  nics: (rid: number) => request<NicMAC[]>('GET', `/api/devices/${rid}/nics`),
  clone: (rid: number) => json<Device>('POST', `/api/devices/${rid}/clone`),
  commit: (rid: number, mode: 'snapshot' | 'image' | 'original', name: string, stopFirst: boolean) =>
    json<{ ok: boolean; image_id: number }>('POST', `/api/devices/${rid}/commit`, { mode, name, stop_first: stopFirst }),
  log: (rid: number) => requestText(`/api/devices/${rid}/log`),
  snapshots: (rid: number) => request<Snapshot[]>('GET', `/api/devices/${rid}/snapshots`),
  createSnapshot: (rid: number, name: string) => json<Snapshot>('POST', `/api/devices/${rid}/snapshots`, { name }),
  restoreSnapshot: (id: number) => json<{ ok: boolean }>('POST', `/api/snapshots/${id}/restore`),
  deleteSnapshot: (id: number) => json<{ ok: boolean }>('DELETE', `/api/snapshots/${id}`),
};

export const templatesApi = {
  list: () => request<Template[]>('GET', '/api/templates'),
  create: (data: Record<string, unknown>) => json<Template>('POST', '/api/templates', data),
  update: (id: number, data: Record<string, unknown>) => json<Template>('PUT', `/api/templates/${id}`, data),
  remove: (id: number) => json<{ ok: boolean }>('DELETE', `/api/templates/${id}`),
  duplicate: (id: number) => json<Template>('POST', `/api/templates/${id}/duplicate`, {}),
};

export const imagesApi = {
  list: () => request<ImageItem[]>('GET', '/api/images'),
  scan: (quiet = false) => json<{ added: number; updated: number; problems?: string[] }>('POST', `/api/images/scan${quiet ? '?quiet=1' : ''}`),
  rename: (id: number, name: string) => json<{ ok: boolean }>('PUT', `/api/images/${id}`, { name }),
  remove: (id: number) => json<{ ok: boolean }>('DELETE', `/api/images/${id}`),
  upload: (dir: string, file: File, onProgress?: (loaded: number) => void) =>
    uploadFile<{ ok: boolean }>(`/api/images/upload${q({ dir, file: file.name })}`, file, onProgress),
};

export const iconsApi = {
  list: () => request<IconItem[]>('GET', '/api/icon-library'),
  upload: (file: File, name?: string) =>
    uploadImage(`/api/icon-library${q({ name })}`, file) as Promise<IconUpload>,
  rename: (id: number, name: string) => json<{ ok: boolean }>('PUT', `/api/icon-library/${id}`, { name }),
  remove: (id: number) => json<{ ok: boolean }>('DELETE', `/api/icon-library/${id}`),
};

export const foldersApi = {
  list: () => request<LabFolder[]>('GET', '/api/lab-folders'),
  create: (name: string, parentId: number | null) => json<LabFolder>('POST', '/api/lab-folders', { name, parent_id: parentId }),
  update: (id: number, patch: { name?: string; parent_id?: number | null }) =>
    json<LabFolder>('PUT', `/api/lab-folders/${id}`, patch),
  remove: (id: number, moveTo?: number) =>
    json<{ ok: boolean; labs: number; moved_labs: number }>('DELETE', `/api/lab-folders/${id}${moveTo ? `?move_to=${moveTo}` : ''}`),
  placeLab: (labId: number, folderId: number | null) =>
    json<{ ok: boolean }>('PUT', `/api/labs/${labId}/folder`, { folder_id: folderId }),
};

export const notificationsApi = {
  list: (limit = 300) => request<import('../types/api').NotificationsResponse>('GET', `/api/notifications${q({ limit })}`),
  setRead: (id: number, read: boolean) => json<{ ok: boolean }>('PUT', `/api/notifications/${id}`, { read }),
  remove: (id: number) => json<{ ok: boolean }>('DELETE', `/api/notifications/${id}`),
  readAll: () => json<{ ok: boolean }>('POST', '/api/notifications/read-all'),
  clearRead: () => json<{ ok: boolean }>('POST', '/api/notifications/clear-read'),
};

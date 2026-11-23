import { RawBody, request, requestBlob, requestText, uploadFile, uploadImage } from './client';
import type { NicMAC } from '../types/api';
import type {
  AuditEntry, Connection, DashboardLive, Message, Device, DeviceCreateInput, IconItem, IconUpload, ImageItem,
  Lab, LabFolder, LabInstance, LabPermissions, LabStatus, LabCanvas, Session, Settings, SystemInfo,
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

export interface MessageRequest {
  text: string;
  level: 'info' | 'warning';
  all: boolean;
  user_ids: number[];
}

// Administrator messages: delivered as notifications to all active users or to the listed ones.
export const messagesApi = {
  send: (body: MessageRequest) => json<{ sent: number }>('POST', '/api/messages', body),
};

export const usersApi = {
  list: () => request<User[]>('GET', '/api/users'),
  create: (username: string, password: string, role: string, confirm?: string, roleId?: number) =>
    json<{ id: number }>('POST', '/api/users', { username, password, role, confirm, role_id: roleId ?? 0 }),
  update: (id: number, patch: { role?: string; disabled?: boolean; password?: string; confirm?: string }) =>
    json<{ ok: boolean }>('PUT', `/api/users/${id}`, patch),
  remove: (id: number) => json<{ ok: boolean }>('DELETE', `/api/users/${id}`),
};

export const rolesApi = {
  list: () => request<RbacRole[]>('GET', '/api/roles'),
  catalog: () => request<RbacPermission[]>('GET', '/api/roles/permissions'),
  get: (id: number) => request<RbacRole>('GET', `/api/roles/${id}`),
  create: (name: string, description: string) =>
    json<RbacRole>('POST', '/api/roles', { name, description }),
  update: (id: number, patch: {
    name?: string; description?: string; permissions?: string[]; max_running_labs?: number;
  }) => json<RbacRole>('PUT', `/api/roles/${id}`, patch),
  remove: (id: number) => json<{ ok: boolean }>('DELETE', `/api/roles/${id}`),
  setPermissions: (id: number, permissions: string[]) =>
    json<{ ok: boolean; permissions: string[] }>('PUT', `/api/roles/${id}/permissions`, { permissions }),
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
  list: (search = '', folder?: string) => request<Lab[]>('GET', `/api/labs${q({ q: search, folder })}`),
  get: (name: string) => request<Lab>('GET', `/api/labs/${encodeURIComponent(name)}`),
  create: (name: string, description: string) => json<Lab>('POST', '/api/labs', { name, description }),
  update: (name: string, patch: { name?: string; description?: string; canvas?: LabCanvas }) =>
    json<{ ok: boolean }>('PUT', `/api/labs/${encodeURIComponent(name)}`, patch),
  remove: (name: string) => json<{ ok: boolean }>('DELETE', `/api/labs/${encodeURIComponent(name)}`),
  copy: (name: string) => json<Lab>('POST', `/api/labs/${encodeURIComponent(name)}/copy`),
  exportLab: (name: string) => requestBlob(`/api/labs/${encodeURIComponent(name)}/export`),
  importLab: (yaml: string) => request<{ id: string; warnings?: Message[] }>('POST', '/api/labs/import', new RawBody(yaml, 'application/yaml')),
  topology: (name: string, instance?: number) =>
    request<Topology>('GET', `/api/labs/${encodeURIComponent(name)}/topology${q({ instance })}`),
  getByInstance: (name: string, instance?: number) =>
    request<Lab>('GET', `/api/labs/${encodeURIComponent(name)}${q({ instance })}`),
  running: (search = '', owner = '') =>
    request<import('../types/api').RunningLab[]>('GET', `/api/labs/running${q({ q: search, owner })}`),
  instances: (name: string) => request<LabInstance[]>('GET', `/api/labs/${encodeURIComponent(name)}/instances`),
  permissions: (name: string) => request<LabPermissions>('GET', `/api/labs/${encodeURIComponent(name)}/permissions`),
  setPermission: (labName: string, userId: number, changes: Record<string, boolean>) =>
    json<{ ok: boolean }>('PUT', `/api/labs/${encodeURIComponent(labName)}/permissions/${userId}`, { changes }),
  setPublic: (name: string, level: 'none' | 'view') => json<{ ok: boolean }>('PUT', `/api/labs/${encodeURIComponent(name)}/public`, { level }),
  positions: (name: string, devices: Array<{ id: string; x: number; y: number; icon_size?: number }>, networks: Array<{ id: string; x: number; y: number }>) =>
    json<{ ok: boolean }>('PUT', `/api/labs/${encodeURIComponent(name)}/positions`, { devices, networks }),
  deleteBulk: (name: string, devices: string[], networks: string[], connections: string[]) =>
    json<{ ok: boolean; warnings?: Message[] }>('POST', `/api/labs/${encodeURIComponent(name)}/delete`, { devices, networks, connections }),
  paste: (name: string, devices: string[], networks: string[], dx: number, dy: number) =>
    json<{ devices: Device[]; networks: import('../types/api').Network[]; connections: Connection[] }>('POST', `/api/labs/${encodeURIComponent(name)}/paste`, { devices, networks, dx, dy }),
  createDevice: async (name: string, input: DeviceCreateInput): Promise<Device[]> => {
    const result = await json<Device | Device[]>('POST', `/api/labs/${encodeURIComponent(name)}/devices`, input);
    return Array.isArray(result) ? result : [result];
  },
  updateDevice: (labName: string, deviceName: string, patch: Record<string, unknown>) =>
    json<Device>('PUT', `/api/labs/${encodeURIComponent(labName)}/devices/${encodeURIComponent(deviceName)}`, patch),
  createNetwork: (name: string, type: string, x: number, y: number) =>
    json<import('../types/api').Network>('POST', `/api/labs/${encodeURIComponent(name)}/networks`, { type, x, y }),
  updateNetwork: (labName: string, networkName: string, patch: Record<string, unknown>) =>
    json<import('../types/api').Network>('PUT', `/api/labs/${encodeURIComponent(labName)}/networks/${encodeURIComponent(networkName)}`, patch),
  createConnection: (name: string, input: Record<string, unknown>) =>
    json<Connection>('POST', `/api/labs/${encodeURIComponent(name)}/connections`, input),
  setConnectionState: (labName: string, key: string, up: boolean, instance?: number) =>
    json<{ key: string; up: boolean }>('PUT', `/api/labs/${encodeURIComponent(labName)}/connections/${encodeURIComponent(key)}/state${q({ instance })}`, { up }),
  startAll: (name: string, instance?: number) => json<{ ok: boolean }>('POST', `/api/labs/${encodeURIComponent(name)}/start-all${q({ instance })}`),
  stopAll: (name: string, instance?: number) => json<{ ok: boolean }>('POST', `/api/labs/${encodeURIComponent(name)}/stop-all${q({ instance })}`),
  wipe: (name: string, instance?: number) => json<WipeResult>('POST', `/api/labs/${encodeURIComponent(name)}/wipe${q({ instance })}`),
  status: (name: string, instance?: number) => request<LabStatus>('GET', `/api/labs/${encodeURIComponent(name)}/status${q({ instance })}`),
};

export const devicesApi = {
  action: (rid: number, action: string, query = '') =>
    json<{ ok: boolean }>('POST', `/api/devices/${rid}/${action}${query}`),
  nics: (rid: number) => request<NicMAC[]>('GET', `/api/devices/${rid}/nics`),
  clone: (rid: number) => json<Device>('POST', `/api/devices/${rid}/clone`),
  commit: (rid: number, mode: 'image' | 'original', name: string, stopFirst: boolean) =>
    json<{ ok: boolean; image_id: string }>('POST', `/api/devices/${rid}/commit`, { mode, name, stop_first: stopFirst }),
  log: (rid: number) => requestText(`/api/devices/${rid}/log`),
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
  scan: (quiet = false) => json<{ added: number; updated: number; problems?: Message[] }>('POST', `/api/images/scan${quiet ? '?quiet=1' : ''}`),
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
  create: (name: string, parent: string) => json<LabFolder>('POST', '/api/lab-folders', { name, parent }),
  update: (path: string, patch: { name?: string; parent?: string | null }) =>
    json<LabFolder>('PUT', '/api/lab-folders', { path, ...patch }),
  remove: (path: string, moveTo?: string) => {
    const search = new URLSearchParams({ path });
    if (moveTo) search.set('move_to', moveTo);
    return json<{ ok: boolean; labs: number; moved_labs: number }>('DELETE', `/api/lab-folders?${search.toString()}`);
  },
  placeLab: (labName: string, folder: string | null) =>
    json<{ ok: boolean }>('PUT', `/api/labs/${encodeURIComponent(labName)}/folder`, { folder }),
};

export const notificationsApi = {
  list: (limit = 300) => request<import('../types/api').NotificationsResponse>('GET', `/api/notifications${q({ limit })}`),
  setRead: (id: number, read: boolean) => json<{ ok: boolean }>('PUT', `/api/notifications/${id}`, { read }),
  remove: (id: number) => json<{ ok: boolean }>('DELETE', `/api/notifications/${id}`),
  readAll: () => json<{ ok: boolean }>('POST', '/api/notifications/read-all'),
  clearRead: () => json<{ ok: boolean }>('POST', '/api/notifications/clear-read'),
};


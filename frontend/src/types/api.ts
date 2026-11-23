export type Role = 'admin' | 'user';

export interface RoleRef {
  id: number;
  name: string;
}

export interface User {
  id: number;
  username: string;
  role: Role;
  disabled: boolean;
  created_at: number;
  roles: RoleRef[] | null;
  role_perms: string[] | null;
}

export interface RbacPermission {
  key: string;
  title: string;
  group: string;
  group_key: string;
}

export interface RbacRole {
  id: number;
  name: string;
  description: string;
  permissions: string[];
  member_count: number;
  created_at: number;
  updated_at: number;
  /** Built-in administrator role: shown read-only. */
  system?: boolean;
  /** How many labs the role allows to run simultaneously (0 - no limit). */
  max_running_labs?: number;
}

export interface RbacMember {
  id: number;
  username: string;
  role: Role;
  disabled: boolean;
  created_at: number;
}

export interface Session {
  user: User;
  csrf: string;
  /** Architecture of the host: x86_64 or aarch64. Templates default to it. */
  host_arch?: string;
}

export interface CanvasView {
  x: number;
  y: number;
  k: number;
}

export interface LabCanvas {
  view?: CanvasView;
  /** Machine icon size on the diagram in world units (32-128). */
  icon_size?: number;
}

export interface Lab {
  /** Same as name (the laboratory identifier). */
  id: string;
  owner_id: number;
  owner: string;
  name: string;
  description: string;
  canvas: LabCanvas | null;
  created_at: number;
  updated_at: number;
  devices: number;
  networks: number;
  public_level: 'none' | 'view';
  perms: Record<string, boolean>;
  /** Folder path ("" - root). */
  folder: string;
  /** Same as folder; kept for URL helpers. */
  folder_id: string;
  /** At least one machine of an instance is running (set on single-laboratory responses). */
  running?: boolean;
}

type IfaceType = 'ens' | 'te' | 'ge';

export interface DeviceParams {
  arch: string;
  machine: string;
  cpu: number;
  ram_mb: number;
  nics: number;
  nic_model: string;
  iface_type: IfaceType;
  iface_start: number;
  disk_bus: string;
  bios: string;
  console: 'serial' | 'vnc';
  extra_args: string;
}

export type MachineState = 'stopped' | 'running' | 'paused' | 'starting' | 'stopping' | 'error';

export interface Device extends DeviceParams {
  /** Same as name. */
  id: string;
  lab_id: string;
  template_id: number | null;
  image_id: string | null;
  name: string;
  x: number;
  y: number;
  state: MachineState;
  pid: number;
  vnc_port: number;
  serial_port: number;
  last_error: string;
  type: string;
  icon: string;
  custom_icon: string;
  icon_url: string;
  /** Icon side on the diagram in world units; 0 - default size. */
  icon_size: number;
  rid: number;
  pending: string[] | null;
  /**
      * Network interface states by NIC number: 0 - link disabled or interface without
      * an address, 1 - disabled or without an address, 2 - configured and working. A missing number
      * means the state is unknown.
   */
  ipv4?: Record<string, number>;
}

export type NetworkType = 'isolated' | 'nat' | 'cloud';

export interface Network {
  /** Same as name. */
  id: string;
  lab_id: string;
  name: string;
  type: NetworkType;
  subnet: string;
  gateway: string;
  dhcp_enabled: boolean;
  dhcp_start: string;
  dhcp_end: string;
  host_iface: string;
  bridge_name: string;
  x: number;
  y: number;
}

export interface Connection {
  /** Same as key. */
  id: string;
  /** Stable key derived from the cable ends. */
  key: string;
  lab_id: string;
  a_type: 'device' | 'network';
  /** Same as a_name (the device or network name). */
  a_id: string;
  a_name: string;
  a_if: number;
  b_type: 'device' | 'network';
  /** Same as b_name (the device or network name). */
  b_id: string;
  b_name: string;
  b_if: number;
  up: boolean;
  warnings?: Message[];
}

export interface Topology {
  lab: Lab;
  instance: number;
  devices: Device[];
  networks: Network[];
  connections: Connection[];
}

export interface Template extends DeviceParams {
  id: number;
  name: string;
  vendor: string;
  type: string;
  category: string;
  icon: string;
  /** Default disk (image) for new machines from the template; 0 - none. */
  image_id?: number;
  /** Hash of the machine's custom PNG icon (empty - standard). */
  custom_icon?: string;
  /** URL of the custom icon for the UI. */
  custom_icon_url?: string;
  /** Default VNC console image settings for machines from the template. */
  vnc: VncQualitySettings;
}

export interface ImageDisk {
  name: string;
  format: string;
}

export interface ImageItem {
  id: number;
  name: string;
  dirname: string;
  disks: ImageDisk[];
  cdroms: string[];
  size_bytes: number;
  used_by: number;
  device_type: string;
  qemu: Record<string, string>;
}

export interface LabFolder {
  /** Slash-separated path relative to the laboratory root; the root is "". */
  path: string;
  name: string;
  /** Parent folder path ("" - the root). */
  parent: string;
  lab_count: number;
  created_at: number;
  updated_at: number;
}

export interface IconItem {
  id: number;
  hash: string;
  name: string;
  width: number;
  height: number;
  size_bytes: number;
  has_alpha: boolean;
  url: string;
  used_by: number;
  created_at: number;
}

export interface IconUpload extends IconItem {
  created: boolean;
}

export interface NotificationItem {
  id: number;
  level: 'info' | 'success' | 'warning' | 'error';
  category: string;
  /** English keys: the interface translates them with the parameters (see translateMessage). */
  title: string;
  body: string;
  params?: Record<string, string>;
  link: string;
  read: boolean;
  created_at: number;
}

export interface NotificationCategory {
  key: string;
  label: string;
}

export interface NotificationsResponse {
  items: NotificationItem[];
  unread: number;
  categories: NotificationCategory[];
}

export interface RunningLab {
  id: string;
  name: string;
  owner_id: number;
  owner: string;
  owner_role: Role;
  instance_id: number;
  instance_user_id: number;
  instance_user: string;
  started_at: number;
  devices: number;
  running: number;
  busy: number;
  errors: number;
  stale: number;
  /** wiped - the lab was wiped recently and has not been started again yet. */
  status: 'running' | 'partial' | 'busy' | 'error' | 'stale' | 'stopped' | 'wiped';
  can_open: boolean;
  can_wipe: boolean;
  can_start: boolean;
  can_stop: boolean;
  own: boolean;
  mine: boolean;
  /** Time of the last Wipe of this instance (0 - never wiped). */
  wiped_at: number;
  host_cpus: number;
  cpu_pct: number;
  cpu_ready: boolean;
  ram_bytes: number;
}

export interface MachineStatus {
  rid: number;
  device_id: string;
  name: string;
  type: string;
  icon_url: string;
  console: 'serial' | 'vnc';
  state: MachineState;
  running: boolean;
  pid: number;
  cpu_pct: number;
  cpu_ready: boolean;
  ram_bytes: number;
  disk_bytes: number;
}

export interface LabStatus {
  lab_id: string;
  instance: number;
  host_cpus: number;
  host_ram_bytes: number;
  time: number;
  machines: MachineStatus[];
  totals: {
    machines: number;
    running: number;
    cpu_pct: number;
    ram_bytes: number;
    disk_bytes: number;
  };
}

export interface SystemCheck {
  name: string;
  ok: boolean;
  detail: string;
}

export interface SystemInfo {
  version: string;
  uptime_sec: number;
  checks: SystemCheck[];
  mem_total: number;
  mem_avail: number;
  disk_free: number;
  disk_total: number;
  data_dir: string;
  disks_dir: string;
  labs_dir: string;
  listen_addr: string;
  qemu_binary: string;

  /** Kernel parameters are available, merging can be enabled and disabled. */
  ksm_available?: boolean;
  /** Merging is enabled. */
  ksm_enabled?: boolean;
  /** How much memory is saved by KSM, bytes. */
  ksm_saved?: number;
  /** UKSM: kernel parameters are available. */
  uksm_available?: boolean;
  /** UKSM is enabled. */
  uksm_enabled?: boolean;
  /** How much memory is saved by UKSM, bytes. */
  uksm_saved?: number;
  counts: {
    labs: number;
    devices: number;
    devices_running: number;
    images: number;
    users: number;
  };
}

export interface DashboardLive {
  running_vms: number;
  cpu_percent: number;
  cpu_ready: boolean;
  cpus: number;
}

export interface Settings {
  session_ttl_hours: string;
  max_upload_mb: string;
}

export interface AuditEntry {
  id: number;
  ts: number;
  username: string;
  action: string;
  /** English text of the record. Keyed records also carry msg_key and msg_params in data. */
  detail: string;
  ip: string;
  data?: Record<string, unknown>;
}

/** A text the server sends as an English key with parameters: translate it with translateMessage. */
export interface Message {
  key: string;
  params?: Record<string, string>;
}

export interface PermCatalogItem {
  key: string;
  title: string;
  group: string;
}

export interface LabPermissions {
  owner_id: number;
  owner: string;
  public_level: 'none' | 'view';
  catalog: PermCatalogItem[];
  users: Array<{
    id: number;
    username: string;
    role: Role;
    disabled: boolean;
    /** Effective permissions: direct permissions paired with those inherited from roles. */
    perms: Record<string, boolean>;
    /** Permissions set directly for this lab. */
    own_perms?: Record<string, boolean>;
    /** Permissions inherited from assigned roles. */
    role_perms?: Record<string, boolean>;
  }>;
}

export interface ThemeColors {
  [key: string]: string;
}

export interface CustomTheme {
  id: string;
  name: string;
  base: 'dark' | 'light';
  colors: ThemeColors;
}

/** The user's own version of a ready-made theme: changed name, base and colors. */
export interface PresetOverride {
  name: string;
  base: 'dark' | 'light';
  colors: ThemeColors;
}

export interface ThemeState {
  active: string;
  custom: CustomTheme[];
  /** Ready-made themes this user has changed, by preset id. The shared files are not touched. */
  presets?: Record<string, PresetOverride>;
  /** Ready-made themes this user has deleted, by preset id. */
  hidden?: string[];
}

export interface ThemePreset {
  id: string;
  name: string;
  base: 'dark' | 'light';
  standard?: boolean;
  sw?: string[];
  colors?: ThemeColors;
}

/** Themes come from the server from separate YAML files in the shared catalog. */
export interface ThemeCatalog {
  standard: ThemePreset[];
  presets: ThemePreset[];
}

export interface ThemeResponse {
  theme: ThemeState | null;
}

export interface LabInstance {
  id: number;
  username: string;
  running: number;
  total: number;
}

export interface WipeResult {
  ok: boolean;
  devices: number;
  failed: Message[];
  instance: number;
}

export interface DeviceCreateInput extends Partial<DeviceParams> {
  template_id: number;
  image_id?: string | null;
  name?: string;
  count?: number;
  x: number;
  y: number;
  icon?: string;
  custom_icon?: string;
}

export interface NicMAC {
  nic: number;
  mac: string;
}

export type VncResolution =
  | 'auto' | '640x480' | '800x600' | '1024x768' | '1280x720'
  | '1366x768' | '1600x900' | '1920x1080';

export type VncEncoding = 'auto' | 'tight' | 'hextile' | 'raw';

// Numeric fields with a null value mean "Automatic" mode.
export interface VncQualitySettings {
  resolution: VncResolution;
  bitrate: number | null;
  quality: number | null;
  compression: number | null;
  fps: number | null;
  encoding: VncEncoding;
}

export interface VncQualityResponse {
  settings: VncQualitySettings;
  defaults: VncQualitySettings;
}

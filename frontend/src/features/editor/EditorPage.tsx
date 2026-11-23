import { askConfirm } from '../../stores/dialogs';
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Navigate, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { paths } from '../../routing/paths';
import { devicesApi, imagesApi, labsApi, templatesApi } from '../../api/endpoints';
import type { Connection, Device, ImageItem, Lab, Network, Template } from '../../types/api';
import { useAuthStore } from '../../stores/auth';
import { guard, toast } from '../../stores/toast';
import { deviceIconSrc, iconFor } from '../../lib/deviceIcon';
import { ifaceName } from '../../lib/format';
import { closeConsolesForLab } from '../consoles/registry';
import { focusDeviceConsole, openConsole, setConsoleDevices, showConsoleLauncher } from '../consoles/runtime';
import { NotificationBell } from '../notifications/NotificationBell';
import { AccessDialog, LabDialog, WipeLabDialog } from '../labs/LabDialogs';
import { StatusDialog } from '../status/StatusDialog';
import { Icon } from '../../components/Icon';
import { t, translateMessage, useLanguageStore } from '../../i18n';
import { AddMachineDialog, ConnectDialog, DeviceSettingsDialog, LogDialog, NetworkSettingsDialog, SaveImageDialog } from './dialogs';
import { ContextMenu, type MenuItem } from './ContextMenu';
import { useFitOnEntry } from './useFitOnEntry';
import { BASE_ICON_SIZE, clampIconSize, fitPoints, iconSizeOf, MIN_ZOOM, type View } from './viewFit';

const STATE_RU: Record<string, string> = {
  stopped: 'Stopped', running: 'Running', paused: 'Paused', starting: 'Starting',
  stopping: 'Stopping', error: 'Error',
};
/**
  * Network interface state for the diagram. The server sends a code by NIC number:
  * 0 - interface disabled by the administrator or explicitly marked "no address",
  * 1 - disabled or without an address, 2 - has an address, 3 - state not provided.
  * Data comes from the topology, which is updated by polling, so colors
  * change on their own, without reloading the page.
 */
type PortState = 'bad' | 'ok' | 'unknown';

function portState(device: Device | null, nic: number): PortState {
  if (!device) return 'unknown';
  const value = device.ipv4?.[String(nic)];
  if (typeof value !== 'number' || !Number.isFinite(value) || value >= 3) return 'unknown';
  return value <= 1 ? 'bad' : 'ok';
}

/**
  * Wire color: green only when all ports at its ends are configured and working;
  * red - if at least one port is disabled or has no address; wires to disabled
  * machines remain gray.
 */
function cableTone(ends: Array<{ device: Device; nic: number }>): 'ok' | 'bad' | 'off' {
  const live = (device: Device) => device.state === 'running' || device.state === 'paused';
  if (!ends.some(({ device }) => live(device))) return 'off';
  if (ends.some(({ device, nic }) => portState(device, nic) === 'bad')) return 'bad';
  if (ends.some(({ device, nic }) => live(device) && portState(device, nic) === 'ok')) return 'ok';
  return 'off';
}

/** A disabled machine is drawn gray; an enabled one uses the normal icon and label colors. */
function machineTone(device: Device): 'on' | 'off' {
  return device.state === 'running' || device.state === 'paused' ? 'on' : 'off';
}

const PORT_COLOR: Record<PortState, string> = { bad: 'var(--bad)', ok: 'var(--ok)', unknown: 'var(--muted)' };

const SIDE_KEY = 'netrave.edSide';
function nicMAC(rid: number, nic: number): string {
  const hex = (value: number) => (value & 0xff).toString(16).padStart(2, '0');
  return ['52', '54', hex(rid >> 16), hex(rid >> 8), hex(rid), hex(nic)].join(':');
}

type Key = string;
const keyOfDevice = (id: string): Key => `d${id}`;
const keyOfNetwork = (id: string): Key => `n${id}`;
const keyOfConnection = (id: string): Key => `c${id}`;
const idOf = (key: Key): string => key.slice(1);

/** Spacing between parallel wires connecting the same pair of nodes. */
const CABLE_GAP = 12;

/**
  * Resize handles of the selected machine: four corners and four sides.
  * The position is given in fractions of half the icon, so the handles follow its size.
 */
const RESIZE_HANDLES = [
  { dir: 'nw', x: -1, y: -1, cursor: 'nwse-resize' },
  { dir: 'n', x: 0, y: -1, cursor: 'ns-resize' },
  { dir: 'ne', x: 1, y: -1, cursor: 'nesw-resize' },
  { dir: 'e', x: 1, y: 0, cursor: 'ew-resize' },
  { dir: 'se', x: 1, y: 1, cursor: 'nwse-resize' },
  { dir: 's', x: 0, y: 1, cursor: 'ns-resize' },
  { dir: 'sw', x: -1, y: 1, cursor: 'nesw-resize' },
  { dir: 'w', x: -1, y: 0, cursor: 'ew-resize' },
] as const;
type ResizeDir = (typeof RESIZE_HANDLES)[number]['dir'];

const endpointKey = (connection: Connection, end: 'a' | 'b'): string =>
  `${connection[end === 'a' ? 'a_type' : 'b_type']}:${connection[end === 'a' ? 'a_id' : 'b_id']}`;

/** The pair key does not depend on wire direction: A->B and B->A lie in the same "lane". */
const connectionPairKey = (connection: Connection): string => {
  const a = endpointKey(connection, 'a'), b = endpointKey(connection, 'b');
  return a < b ? `${a}|${b}` : `${b}|${a}`;
};

/**
  * Offset of the wire across the line. Several wires between the same pair of nodes
  * are laid out side by side (by ascending id) instead of being drawn on top of each other.
 */
function cableLaneOffsets(connections: Connection[]): Map<string, number> {
  const groups = new Map<string, Connection[]>();
  for (const connection of connections) {
    const key = connectionPairKey(connection);
    const group = groups.get(key);
    if (group) group.push(connection); else groups.set(key, [connection]);
  }
  const offsets = new Map<string, number>();
  for (const group of groups.values()) {
    group.sort((left, right) => left.id.localeCompare(right.id));
    group.forEach((connection, index) => {
      offsets.set(connection.id, (index - (group.length - 1) / 2) * CABLE_GAP);
    });
  }
  return offsets;
}

type Dialog =
  | { kind: 'add-machine'; x: number; y: number }
  | { kind: 'device'; device: Device }
  | { kind: 'network'; network: Network }
  | { kind: 'connect'; a: Key; b: Key }
  | { kind: 'saveImage'; device: Device }
  | { kind: 'log'; device: Device }
  | { kind: 'status' }
  | { kind: 'lab' }
  | { kind: 'access' }
  | { kind: 'wipe' };

export function EditorPage({ labName, instanceOverride }: { labName?: string | undefined; instanceOverride?: number | undefined } = {}) {
  const params = useParams();
  const [search] = useSearchParams();
  const navigate = useNavigate();
  const user = useAuthStore((state) => state.user);
  // The laboratory name is the identifier; the numeric variable name is kept for the many
  // URL and query-key usages below.
  const labId = labName ?? params['labName'] ?? params['folderName'] ?? '';
  const instanceId = instanceOverride ?? (Number.parseInt(search.get('instance') ?? search.get('i') ?? '', 10) || 0);

  const topologyQuery = useQuery({
    queryKey: ['topology', labId, instanceId],
    queryFn: () => labsApi.topology(labId, instanceId || undefined),
    enabled: Boolean(labId),
    // The topology contains not only the diagram but also machine state (running/stopped, PID).
    // It cannot be kept "forever fresh": when reopening another user's instance from
    // "Running labs", a stale snapshot was shown until polling fired after 4 s.
    staleTime: 0,
    refetchOnMount: 'always',
  });
  const templatesQuery = useQuery({ queryKey: ['templates'], queryFn: templatesApi.list, enabled: Boolean(labId) });
  const imagesQuery = useQuery({ queryKey: ['images'], queryFn: () => imagesApi.list(), enabled: Boolean(labId) });

  const [lab, setLab] = useState<Lab | null>(null);
  const [devices, setDevices] = useState<Device[]>([]);
  const [networks, setNetworks] = useState<Network[]>([]);
  const [connections, setConnections] = useState<Connection[]>([]);
  const [templates, setTemplates] = useState<Template[]>([]);
  const [images, setImages] = useState<ImageItem[]>([]);
  const [selection, setSelection] = useState<Set<Key>>(new Set());
  const [view, setView] = useState<View>({ x: 0, y: 0, k: 1 });
  /** Default icon size; each machine may have its own. */
  const [fallbackIconSize, setFallbackIconSize] = useState(BASE_ICON_SIZE);
  const [status, setStatus] = useState(t('Saved'));
  const [statusBad, setStatusBad] = useState(false);
  const [sideOpen, setSideOpen] = useState(() => { try { return localStorage.getItem(SIDE_KEY) !== '1'; } catch { return true; } });
  const [drawer, setDrawer] = useState(false);
  const [menu, setMenu] = useState<{ x: number; y: number; items: MenuItem[] } | null>(null);
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const [band, setBand] = useState<{ x: number; y: number; w: number; h: number } | null>(null);
  const [tempLink, setTempLink] = useState<{ x1: number; y1: number; x2: number; y2: number } | null>(null);
  const [clip, setClip] = useState<{ devices: string[]; networks: string[] } | null>(null);
  /** The session (lab + instance) whose topology is currently loaded into the component state. */
  const [syncedKey, setSyncedKey] = useState<string | null>(null);
  const pasteN = useRef(0);
  const svgRef = useRef<SVGSVGElement>(null);
  /** Session whose saved canvas (camera and fallback icon size) has been loaded into state. */
  const canvasLoaded = useRef<string | null>(null);
  /** Sizes already changed on screen but not yet confirmed by the server. */
  const pendingIconSizes = useRef<Map<string, number>>(new Map());
  /** While dragging a handle the icon changes without animation - otherwise it lags behind the cursor. */
  const [iconResizing, setIconResizing] = useState(false);
  const dragging = useRef(false);
  const dialogRef = useRef(false);
  const menuRef = useRef(false);
  const viewTimer = useRef(0);
  const pollTimer = useRef(0);
  // Deleted wires and newly created ones: responses from polling/cache started before the change
  // must not bring them back to the diagram (otherwise the cable "blinks" after deletion).
  const doomedConns = useRef<Set<string>>(new Set());
  const pendingConns = useRef<Map<string, Connection>>(new Map());
  const lastClick = useRef<{ key: string; at: number } | null>(null);
  const hoverTimer = useRef<number | null>(null);
  const spaceRef = useRef(false);
  const [space, setSpace] = useState(false);
  useLanguageStore((state) => state.language);

  useEffect(() => { dialogRef.current = dialog !== null; }, [dialog]);
  useEffect(() => { menuRef.current = menu !== null; }, [menu]);

  const allow = useCallback((perm: string): boolean => Boolean(lab?.perms?.[perm]), [lab]);

  // Reconciles the wire list from the server with local intentions: removes deleted ones and
  // holds created ones until the server confirms them. This protects against a race with polling.
  const mergeConnections = useCallback((list: Connection[]): Connection[] => {
    // The server has confirmed the deletion (the wire is no longer in the response) - no need to hide it anymore.
    if (doomedConns.current.size) {
      for (const id of doomedConns.current) {
        if (!list.some((connection) => connection.id === id)) doomedConns.current.delete(id);
      }
    }
    const out = doomedConns.current.size ? list.filter((connection) => !doomedConns.current.has(connection.id)) : [...list];
    if (pendingConns.current.size) {
      for (const [id, connection] of pendingConns.current) {
        if (out.some((item) => item.id === id)) pendingConns.current.delete(id);
        else out.push(connection);
      }
    }
    return out;
  }, []);

  // A just-changed icon size must not be rolled back by a polling response
  // started before the drag. The value is cleared when the server returns it unchanged.
  const mergeIconSizes = useCallback((list: Device[]): Device[] => list.map((device) => {
    const pending = pendingIconSizes.current.get(device.id);
    if (pending === undefined) return device;
    if (device.icon_size === pending) {
      pendingIconSizes.current.delete(device.id);
      return device;
    }
    return { ...device, icon_size: pending };
  }), []);

  const sessionKey = `${labId}:${instanceId}`;
  // Copy the query snapshot into component state before paint. With a plain effect the
  // editor would briefly render the previous laboratory (or "not found") while the new
  // topology is already available, and the camera would be fitted one frame later.
  useLayoutEffect(() => {
    const data = topologyQuery.data;
    if (!data) return;
    setLab(data.lab);
    setDevices(mergeIconSizes(data.devices));
    setNetworks(data.networks);
    setConnections(mergeConnections(data.connections));
    // The old shared size from canvas is read only when entering the lab: it serves
    // as the default size for machines that do not have their own icon_size yet.
    if (canvasLoaded.current !== sessionKey) {
      canvasLoaded.current = sessionKey;
      setFallbackIconSize(clampIconSize(data.lab.canvas?.icon_size));
    }
    setSyncedKey(sessionKey);
  }, [topologyQuery.data, sessionKey, mergeConnections, mergeIconSizes]);

  useEffect(() => { if (templatesQuery.data) setTemplates(templatesQuery.data); }, [templatesQuery.data]);
  useEffect(() => { if (imagesQuery.data) setImages(imagesQuery.data); }, [imagesQuery.data]);

  const refreshImages = useCallback(async () => {
    try {
      await imagesApi.scan(true);
    } catch { /* image directory may be unavailable */ }
    try {
      const list = await imagesApi.list();
      setImages(list);
    } catch { /* image catalog may be unavailable */ }
  }, []);

  useEffect(() => { if (labId) void refreshImages(); }, [labId, refreshImages]);

  const refreshTopology = useCallback(async (force = false) => {
    if (!labId || (!force && (dragging.current || dialogRef.current || menuRef.current))) return;
    try {
      const data = await labsApi.topology(labId, instanceId || undefined);
      setLab(data.lab);
      setDevices(mergeIconSizes(data.devices));
      setNetworks(data.networks);
      setConnections(mergeConnections(data.connections));
      setSelection((current) => new Set([...current].filter((key) => {
        const id = idOf(key);
        return key[0] === 'd' ? data.devices.some((device) => device.id === id)
          : key[0] === 'n' ? data.networks.some((network) => network.id === id)
            : data.connections.some((connection) => connection.id === id);
      })));
    } catch {
      // transient network error: the next poll retries
    }
  }, [labId, instanceId, mergeConnections, mergeIconSizes]);

  useEffect(() => {
    if (!labId) return;
    const busy = devices.some((device) => device.state === 'starting' || device.state === 'stopping');
    pollTimer.current = window.setTimeout(function tick() {
      void refreshTopology().then(() => {
        pollTimer.current = window.setTimeout(tick, devices.some((device) => device.state === 'starting' || device.state === 'stopping') ? 1500 : 4000);
      });
    }, busy ? 1500 : 4000);
    return () => window.clearTimeout(pollTimer.current);
  }, [labId, refreshTopology, devices]);

  useEffect(() => {
    const typing = (event: KeyboardEvent) => {
      const target = event.target;
      return target instanceof HTMLElement && (/^(INPUT|SELECT|TEXTAREA)$/.test(target.tagName) || Boolean(target.closest('[data-term], [data-vnc], [data-console-dock], [data-console-panel]')));
    };
    const down = (event: KeyboardEvent) => {
      if (!lab || typing(event) || dialog) return;
      if (event.code === 'Space') { spaceRef.current = true; setSpace(true); event.preventDefault(); return; }
      const mod = event.ctrlKey || event.metaKey;
      if (event.key === 'Delete' || event.key === 'Backspace') { event.preventDefault(); void deleteSelection(); }
      else if (mod && event.key.toLowerCase() === 'c') copySelection();
      else if (mod && event.key.toLowerCase() === 'v') { event.preventDefault(); void pasteClipboard(); }
      else if (mod && event.key.toLowerCase() === 'a') {
        event.preventDefault();
        setSelection(new Set([...devices.map((device) => keyOfDevice(device.id)), ...networks.map((network) => keyOfNetwork(network.id))]));
      } else if (event.key === 'Escape') { setMenu(null); setSelection(new Set()); }
    };
    const up = (event: KeyboardEvent) => { if (event.code === 'Space') { spaceRef.current = false; setSpace(false); } };
    window.addEventListener('keydown', down);
    window.addEventListener('keyup', up);
    return () => { window.removeEventListener('keydown', down); window.removeEventListener('keyup', up); };
  });

  useEffect(() => {
    setConsoleDevices(() => devices.map((device) => ({ rid: device.rid, name: device.name, lab_id: labId, state: device.state, console: device.console, lab_perms: lab?.perms ?? {} })));
    return () => setConsoleDevices(() => []);
  }, [devices, lab, labId]);

  useEffect(() => () => { closeConsolesForLab(labId); }, [labId]);

  // The camera is saved to canvas with a delay; each icon's size is stored separately
  // on the machine (PUT /api/labs/{id}/positions), so records do not overwrite each other.
  const saveView = useCallback((nextView: View) => {
    if (!lab || !allow('lab.save_topology')) return;
    window.clearTimeout(viewTimer.current);
    viewTimer.current = window.setTimeout(() => {
      void Promise.resolve(labsApi.update(lab.id, {
        canvas: {
          view: { x: Math.round(nextView.x), y: Math.round(nextView.y), k: Math.round(nextView.k * 1000) / 1000 },
          // The previous shared size remains the default size for machines without their own.
          icon_size: clampIconSize(fallbackIconSize),
        },
      })).catch(() => undefined);
    }, 800);
  }, [lab, allow, fallbackIconSize]);

  useEffect(() => () => { window.clearTimeout(viewTimer.current); }, []);

  const applyView = (next: View) => { setView(next); saveView(next); };

  const zoomAt = (factor: number, cx: number, cy: number) => {
    setView((current) => {
      const k = Math.min(3, Math.max(MIN_ZOOM, current.k * factor));
      const f = k / current.k;
      const next = { k, x: cx - (cx - current.x) * f, y: cy - (cy - current.y) * f };
      saveView(next);
      return next;
    });
  };
  const zoomBy = (factor: number) => {
    const rect = svgRef.current?.getBoundingClientRect();
    if (rect) zoomAt(factor, rect.width / 2, rect.height / 2);
  };

  const worldPoint = (event: { clientX: number; clientY: number }): [number, number] => {
    const rect = svgRef.current?.getBoundingClientRect();
    if (!rect) return [0, 0];
    return [(event.clientX - rect.left - view.x) / view.k, (event.clientY - rect.top - view.y) / view.k];
  };
  const centerWorld = (): [number, number] => {
    const rect = svgRef.current?.getBoundingClientRect();
    if (!rect) return [0, 0];
    const jitter = () => Math.round((Math.random() - 0.5) * 80);
    return [Math.round((rect.width / 2 - view.x) / view.k) + jitter(), Math.round((rect.height / 2 - view.y) / view.k) + jitter()];
  };
  const snap = (value: number) => Math.round(value / 10) * 10;

  const setStatusText = (text: string, bad = false) => { setStatus(text); setStatusBad(bad); };
  const run = async <T,>(action: () => Promise<T>, success?: string): Promise<T | undefined> => {
    setStatusText(t('Saving…'));
    try {
      const result = await action();
      setStatusText(t('Saved'));
      if (success) toast.success(success);
      return result;
    } catch (error) {
      setStatusText(t('Save error'), true);
      toast.error(error instanceof Error ? error.message : String(error));
      return undefined;
    }
  };

  /** Saves one icon's size together with its position (layout endpoint). */
  const saveIconSize = async (device: Device, size: number) => {
    if (!lab) return;
    const saved = await run(() => labsApi.positions(lab.id, [{ id: device.id, x: device.x, y: device.y, icon_size: size }], []));
    if (!saved) {
      // A size not confirmed by the server cannot be shown as saved.
      pendingIconSizes.current.delete(device.id);
      setDevices((current) => current.map((item) => item.id === device.id ? { ...item, icon_size: device.icon_size } : item));
    }
  };

  /**
      * Drags the selected machine's handle: the icon's side changes in the direction of movement
      * (corners - along both axes), while the center stays in place. The size is saved on release.
   */
  const startIconResize = (event: React.MouseEvent, device: Device, dir: ResizeDir) => {
    if (!allow('machine.move')) return;
    event.preventDefault();
    event.stopPropagation();
    const startSize = iconSizeOf(device, fallbackIconSize);
    const [startX, startY] = worldPoint(event);
    const dirX = dir.includes('e') ? 1 : dir.includes('w') ? -1 : 0;
    const dirY = dir.includes('s') ? 1 : dir.includes('n') ? -1 : 0;
    let latest = startSize;
    let moved = false;
    const move = (moveEvent: MouseEvent) => {
      const [x, y] = worldPoint(moveEvent);
      const dx = (x - startX) * dirX;
      const dy = (y - startY) * dirY;
      const delta = dirX && dirY ? (dx + dy) / 2 : dirX ? dx : dy;
      if (!moved && Math.abs(delta) < 0.5) return;
      if (!moved) setIconResizing(true);
      moved = true;
      dragging.current = true;
      latest = clampIconSize(startSize + delta * 2);
      pendingIconSizes.current.set(device.id, latest);
      setDevices((current) => current.map((item) => item.id === device.id ? { ...item, icon_size: latest } : item));
    };
    const up = () => {
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mouseup', up);
      dragging.current = false;
      setIconResizing(false);
      if (!moved) return;
      const size = latest;
      pendingIconSizes.current.set(device.id, size);
      void saveIconSize(device, size);
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
  };

  const patchLab = async (patch: { name?: string }) => {
    if (!lab) return;
    await run(async () => { await labsApi.update(lab.id, patch); setLab({ ...lab, ...patch }); });
  };

  const selectionParts = (keys: Set<Key> = selection) => {
    const dev: string[] = [], net: string[] = [], con: string[] = [];
    for (const key of keys) (key[0] === 'd' ? dev : key[0] === 'n' ? net : con).push(idOf(key));
    return { dev, net, con };
  };

  // keys - an explicit set for context menu actions: it is created before setSelection is applied,
  // so it must not rely on the selection state of that render.
  const deleteSelection = async (keys?: Set<Key>) => {
    if (!lab) return;
    const { dev, net, con } = selectionParts(keys ?? selection);
    if ((dev.length && !allow('machine.delete')) || (net.length && !allow('net.manage')) || (con.length && !allow('link.delete')) || ((dev.length || net.length) && !allow('link.delete'))) {
      toast.error(t('No rights to delete the selection')); return;
    }
    const total = dev.length + net.length + con.length;
    if (!total) return;
    if ((keys ?? selection).size > 1 && total > 1 && !(await askConfirm({ title: `${t('Delete selected objects?')} (${total})`, danger: true }))) return;
    const liveCable = connections.some((connection) => con.includes(connection.id) && [connection.a_type === 'device' ? connection.a_id : 0, connection.b_type === 'device' ? connection.b_id : 0].some((id) => {
      const device = devices.find((item) => item.id === id);
      return device && (device.state === 'running' || device.state === 'paused');
    }));
    if (liveCable && !(await askConfirm({ title: t('The cable is connected to a running machine. It will be disconnected immediately. Delete?'), danger: true }))) return;
    // Remove wires immediately without waiting for a response: server synchronization (TAP/QMP) may
    // take time, while the diagram state must change instantly. On error, restore them.
    const doomed = connections.filter((connection) => con.includes(connection.id)
      || (connection.a_type === 'device' && dev.includes(connection.a_id)) || (connection.b_type === 'device' && dev.includes(connection.b_id))
      || (connection.a_type === 'network' && net.includes(connection.a_id)) || (connection.b_type === 'network' && net.includes(connection.b_id)));
    const snapshot = doomed.filter((connection) => !doomedConns.current.has(connection.id));
    doomed.forEach((connection) => doomedConns.current.add(connection.id));
    if (snapshot.length) setConnections((current) => current.filter((connection) => !doomedConns.current.has(connection.id)));
    const result = await run(() => labsApi.deleteBulk(lab.id, dev, net, con));
    if (!result) {
      doomed.forEach((connection) => doomedConns.current.delete(connection.id));
      setConnections((current) => [...current, ...snapshot.filter((saved) => !current.some((connection) => connection.id === saved.id))]);
      return;
    }
    (result.warnings ?? []).forEach((warning) => toast.warning(`${t('Cable disconnected, but the interface is still attached:')} ${translateMessage(warning.key, warning.params)}`));
    setDevices((current) => current.filter((device) => !dev.includes(device.id)));
    setNetworks((current) => current.filter((network) => !net.includes(network.id)));
    setSelection(new Set());
    void refreshTopology(true);
  };

  const copySelection = () => {
    const { dev, net } = selectionParts();
    if (!dev.length && !net.length) return;
    setClip({ devices: dev, networks: net });
    pasteN.current = 0;
    toast.info(`${t('Copied:')} ${dev.length + net.length}`);
  };

  const pasteClipboard = async () => {
    if (!lab || !clip) return;
    if (!allow('machine.add')) { toast.error(t('No rights to add objects')); return; }
    pasteN.current += 1;
    const offset = 40 * pasteN.current;
    const result = await run(() => labsApi.paste(lab.id, clip.devices, clip.networks, offset, offset));
    if (!result) return;
    setDevices((current) => [...current, ...result.devices]);
    setNetworks((current) => [...current, ...result.networks]);
    setConnections((current) => [...current, ...result.connections]);
    setSelection(new Set([...result.devices.map((device) => keyOfDevice(device.id)), ...result.networks.map((network) => keyOfNetwork(network.id))]));
  };

  const duplicateSelection = async () => {
    if (!lab) return;
    const { dev, net } = selectionParts();
    if (!dev.length && !net.length) return;
    const result = await run(() => labsApi.paste(lab.id, dev, net, 40, 40));
    if (!result) return;
    setDevices((current) => [...current, ...result.devices]);
    setNetworks((current) => [...current, ...result.networks]);
    setConnections((current) => [...current, ...result.connections]);
    setSelection(new Set([...result.devices.map((device) => keyOfDevice(device.id)), ...result.networks.map((network) => keyOfNetwork(network.id))]));
  };

  const savePositions = async (items: Array<{ key: Key; x: number; y: number }>) => {
    if (!lab) return;
    await run(() => labsApi.positions(lab.id,
      items.filter((item) => item.key[0] === 'd').map((item) => ({ id: idOf(item.key), x: item.x, y: item.y })),
      items.filter((item) => item.key[0] === 'n').map((item) => ({ id: idOf(item.key), x: item.x, y: item.y }))));
  };

  const moveNodes = (items: Array<{ key: Key; x: number; y: number }>) => {
    const map = new Map(items.map((item) => [item.key, item]));
    setDevices((current) => current.map((device) => { const item = map.get(keyOfDevice(device.id)); return item ? { ...device, x: item.x, y: item.y } : device; }));
    setNetworks((current) => current.map((network) => { const item = map.get(keyOfNetwork(network.id)); return item ? { ...network, x: item.x, y: item.y } : network; }));
  };

  // Hovering over a machine switches its console. The delay prevents the terminal from reconfiguring
  // when the cursor merely passes over machines.
  const scheduleHoverFocus = (rid: number) => {
    if (hoverTimer.current) window.clearTimeout(hoverTimer.current);
    hoverTimer.current = window.setTimeout(() => { hoverTimer.current = null; focusDeviceConsole(rid); }, 250);
  };
  const cancelHoverFocus = () => {
    if (hoverTimer.current) window.clearTimeout(hoverTimer.current);
    hoverTimer.current = null;
  };

  const startPan = (event: React.MouseEvent) => {
    const start = { x: event.clientX, y: event.clientY, vx: view.x, vy: view.y };
    const move = (moveEvent: MouseEvent) => applyView({ ...view, x: start.vx + moveEvent.clientX - start.x, y: start.vy + moveEvent.clientY - start.y });
    const up = () => { window.removeEventListener('mousemove', move); window.removeEventListener('mouseup', up); };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
  };

  const startNodeDrag = (event: React.MouseEvent, key: Key) => {
    const multi = event.shiftKey || event.ctrlKey || event.metaKey;
    setSelection((current) => {
      const next = new Set(current);
      if (multi) { if (next.has(key)) next.delete(key); else next.add(key); } else if (!next.has(key)) { next.clear(); next.add(key); }
      return next;
    });
    if (!allow('machine.move')) return;
    const keys = multi ? [...selection, key] : [key];
    const items = keys.filter((entry) => entry[0] !== 'c').map((entry) => {
      const id = idOf(entry);
      const object = entry[0] === 'd' ? devices.find((device) => device.id === id) : networks.find((network) => network.id === id);
      return object ? { key: entry, x: object.x, y: object.y } : null;
    }).filter((item): item is { key: Key; x: number; y: number } => item !== null);
    if (!items.length) return;
    const start = items.map((item) => ({ ...item }));
    const startX = event.clientX, startY = event.clientY;
    let latest = items.map((item) => ({ ...item }));
    let moved = false;
    const move = (moveEvent: MouseEvent) => {
      if (!moved && Math.hypot(moveEvent.clientX - startX, moveEvent.clientY - startY) < 3) return;
      moved = true; dragging.current = true;
      const dx = (moveEvent.clientX - startX) / view.k, dy = (moveEvent.clientY - startY) / view.k;
      latest = items.map((item, index) => ({ key: item.key, x: (start[index]?.x ?? 0) + dx, y: (start[index]?.y ?? 0) + dy }));
      moveNodes(latest);
    };
    const up = () => {
      window.removeEventListener('mousemove', move); window.removeEventListener('mouseup', up);
      dragging.current = false;
      if (!moved) return;
      const snapped = latest.map((item) => ({ key: item.key, x: snap(item.x), y: snap(item.y) }));
      moveNodes(snapped); void savePositions(snapped);
    };
    window.addEventListener('mousemove', move); window.addEventListener('mouseup', up);
  };

  const startLink = (_event: React.MouseEvent, key: Key) => {
    const object = key[0] === 'd' ? devices.find((device) => device.id === idOf(key)) : networks.find((network) => network.id === idOf(key));
    if (!object) return;
    setTempLink({ x1: object.x, y1: object.y, x2: object.x, y2: object.y });
    // During cable dragging text must not be selected: otherwise the browser treats
    // the drag as a selection and releasing the button over the target does not create a connection.
    const previousUserSelect = document.body.style.userSelect;
    document.body.style.userSelect = 'none';
    const move = (moveEvent: MouseEvent) => {
      const [x, y] = worldPoint(moveEvent);
      setTempLink({ x1: object.x, y1: object.y, x2: x, y2: y });
    };
    let finished = false;
    const finish = () => {
      if (finished) return;
      finished = true;
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mouseup', up);
      window.removeEventListener('keydown', escape);
      document.body.style.userSelect = previousUserSelect;
      setTempLink(null);
    };
    const up = (upEvent: MouseEvent) => {
      finish();
      // The target is found by the visible element under the cursor: connection points and port
      // labels lie on top of the node and are not `.node` themselves.
      const target = document.elementFromPoint(upEvent.clientX, upEvent.clientY)?.closest('.node');
      const targetKey = target instanceof Element ? target.getAttribute('data-key') : null;
      if (targetKey && targetKey !== key) setDialog({ kind: 'connect', a: key, b: targetKey });
    };
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') finish(); };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
    window.addEventListener('keydown', escape);
  };

  const startBand = (event: React.MouseEvent) => {
    const rect = svgRef.current?.getBoundingClientRect();
    if (!rect) return;
    const startX = event.clientX - rect.left, startY = event.clientY - rect.top;
    const additive = event.shiftKey || event.ctrlKey || event.metaKey;
    let moved = false;
    let box: { x: number; y: number; w: number; h: number } | null = null;
    const move = (moveEvent: MouseEvent) => {
      const cx = moveEvent.clientX - rect.left, cy = moveEvent.clientY - rect.top;
      if (!moved && Math.hypot(cx - startX, cy - startY) < 4) return;
      moved = true;
      box = { x: Math.min(startX, cx), y: Math.min(startY, cy), w: Math.abs(cx - startX), h: Math.abs(cy - startY) };
      setBand(box);
    };
    const up = () => {
      window.removeEventListener('mousemove', move); window.removeEventListener('mouseup', up);
      setBand(null);
      if (!moved) { if (!additive) setSelection(new Set()); return; }
      if (!box) return;
      const selectionBox = box;
      const inside = (object: { x: number; y: number }) => {
        const px = object.x * view.k + view.x, py = object.y * view.k + view.y;
        return px >= selectionBox.x && px <= selectionBox.x + selectionBox.w && py >= selectionBox.y && py <= selectionBox.y + selectionBox.h;
      };
      const hit = [...devices.filter(inside).map((device) => keyOfDevice(device.id)), ...networks.filter(inside).map((network) => keyOfNetwork(network.id))];
      setSelection((current) => additive ? new Set([...current, ...hit]) : new Set(hit));
    };
    window.addEventListener('mousemove', move); window.addEventListener('mouseup', up);
  };

  // Double-click on a machine and the sidebar "Console" button share one path: the machine's
  // terminal goes to the active terminal window, or opens a floating one.
  const openDeviceConsole = (device: (typeof devices)[number]): void => {
    if (device.state !== 'running' && device.state !== 'paused') { toast.error(t('Device is not running — start it to open the console')); return; }
    const need = device.console === 'vnc' ? 'console.vnc' : 'console.serial';
    if (!allow(need)) { toast.error(t('No rights to the console of this machine')); return; }
    openConsole({ rid: device.rid, name: device.name, lab_id: labId, state: device.state, console: device.console, lab_perms: lab?.perms ?? {} }, device.console === 'vnc' ? 'vnc' : 'serial');
  };

  const onCanvasDown = (event: React.MouseEvent) => {
    if (event.button === 2) return;
    const target = event.target instanceof Element ? event.target : null;
    const node = target?.closest('.node');
    const link = target?.closest('.link');
    if (event.button === 1 || (event.button === 0 && spaceRef.current)) { event.preventDefault(); startPan(event); return; }
    if (event.button !== 0) return;
    setMenu(null);
    // Without this Safari starts native text selection or image dragging
    // and does not deliver subsequent mousemove events: dragging and selecting machines break.
    event.preventDefault();
    if (node) {
      const key = node.getAttribute('data-key');
      if (!key) return;
      if (event.shiftKey) { startLink(event, key); return; }
      const now = Date.now();
      if (key[0] === 'd' && lastClick.current?.key === key && now - lastClick.current.at < 450) {
        lastClick.current = null;
        const device = devices.find((item) => item.id === idOf(key));
        if (device) openDeviceConsole(device);
        return;
      }
      lastClick.current = key[0] === 'd' ? { key, at: now } : null;
      startNodeDrag(event, key);
      return;
    }
    if (link) {
      const key = link.getAttribute('data-key');
      if (!key) return;
      setSelection((current) => {
        const next = new Set(event.shiftKey || event.ctrlKey || event.metaKey ? current : []);
        if (next.has(key)) next.delete(key); else next.add(key);
        return next;
      });
      return;
    }
    startBand(event);
  };

  const deviceMenu = (device: Device, key?: Key): MenuItem[] => {
    const idle = device.state === 'stopped' || device.state === 'error';
    const live = device.state === 'running' || device.state === 'paused';
    const items: MenuItem[] = [];
    if (idle && allow('machine.start')) items.push({ label: '▶ '+t('Start'), run: () => void deviceAction(device, 'start') });
    if (live && allow('machine.stop')) items.push({ label: '■ '+t('Stop'), run: () => void deviceAction(device, 'stop') }, { label: t('Force stop'), run: () => void deviceAction(device, 'stop', '?force=1') });
    if (live && allow('machine.restart')) items.push({ label: '↻ '+t('Restart'), run: () => void deviceAction(device, 'restart') });
    if (device.state === 'running' && allow('machine.stop')) items.push({ label: '⏸ '+t('Pause'), run: () => void deviceAction(device, 'suspend') });
    if (device.state === 'paused' && allow('machine.stop')) items.push({ label: '▶ '+t('Resume'), run: () => void deviceAction(device, 'resume') });
    if (allow('machine.start') || allow('machine.stop') || allow('machine.restart')) items.push({ separator: true });
    items.push({ label: device.console === 'vnc' ? t('Open screen (VNC)') : t('Open terminal'), disabled: !live || !allow(device.console === 'vnc' ? 'console.vnc' : 'console.serial'), run: () => openConsole({ rid: device.rid, name: device.name, lab_id: labId, state: device.state, console: device.console, lab_perms: lab?.perms ?? {} }) }, { separator: true });
    if (allow('machine.edit')) items.push({ label: t('Settings…'), run: () => setDialog({ kind: 'device', device }) });
    if (allow('machine.snapshot')) items.push({ label: t('Save image…'), run: () => setDialog({ kind: 'saveImage', device }) });
    items.push({ label: t('Start log…'), run: () => setDialog({ kind: 'log', device }) });
    if (allow('machine.clone')) items.push({ label: t('Clone'), disabled: !idle, run: () => void cloneDevice(device) });
    if (allow('machine.wipe')) items.push({ label: t('Wipe (reset disk)…'), run: () => void wipeDevice(device) });
    items.push({ separator: true }, { label: t('Copy'), run: copySelection });
    if (allow('machine.add')) items.push({ label: t('Duplicate'), run: () => void duplicateSelection() });
    if (allow('machine.delete')) items.push({ label: t('Delete'), run: () => void deleteSelection(key && !selection.has(key) ? new Set([key]) : undefined) });
    return items;
  };

  const onContextMenu = (event: React.MouseEvent) => {
    event.preventDefault();
    const target = event.target instanceof Element ? event.target : null;
    const node = target?.closest('.node');
    const link = target?.closest('.link');
    const [wx, wy] = worldPoint(event);
    if (node) {
      const key = node.getAttribute('data-key');
      if (!key) return;
      if (!selection.has(key)) setSelection(new Set([key]));
      if (selection.size > 1 && selection.has(key)) {
        const items: MenuItem[] = [{ label: t('Copy'), run: copySelection }];
        if (allow('machine.add')) items.push({ label: t('Duplicate'), run: () => void duplicateSelection() });
        if (allow('machine.delete')) items.push({ separator: true }, { label: t('Delete'), run: () => void deleteSelection() });
        setMenu({ x: event.clientX, y: event.clientY, items });
        return;
      }
      if (key[0] === 'd') {
        const device = devices.find((item) => item.id === idOf(key));
        if (device) setMenu({ x: event.clientX, y: event.clientY, items: deviceMenu(device, key) });
      } else {
        const network = networks.find((item) => item.id === idOf(key));
        if (network) setMenu({ x: event.clientX, y: event.clientY, items: [
          ...(allow('net.manage') ? [{ label: t('Network settings…'), run: () => setDialog({ kind: 'network', network }) }, { separator: true }] : []),
          ...(allow('iface.manage') ? [{ label: t('Interfaces and IP addresses…'), run: () => setDialog({ kind: 'connect', a: key, b: '' }) }] : []),
          { label: t('Copy'), run: copySelection },
          ...(allow('net.manage') ? [{ label: t('Delete'), run: () => void deleteSelection(selection.has(key) ? undefined : new Set([key])) }] : []),
        ] });
      }
    } else if (link) {
      const key = link.getAttribute('data-key');
      if (!key) return;
      setSelection(new Set([key]));
      const connection = connections.find((item) => item.id === idOf(key));
      if (!connection) return;
      const items: MenuItem[] = [];
      if (allow('link.create')) items.push({ label: connection.up ? t('Disable cable') : t('Enable cable'), run: () => void setCable(connection, !connection.up) });
      if (allow('link.delete')) items.push({ label: t('Delete connection'), run: () => void deleteSelection(selection.has(key) ? undefined : new Set([key])) });
      if (items.length) setMenu({ x: event.clientX, y: event.clientY, items });
    } else {
      const x = snap(wx), y = snap(wy);
      const items: MenuItem[] = [];
      if (allow('machine.add') || allow('net.manage')) items.push(
        { label: t('Add machine'), run: () => setDialog({ kind: 'add-machine', x, y }) },
        { label: t('Add network'), sub: [
          { label: t('NAT'), run: () => void addNetwork('nat', x, y) },
        ] },
        { separator: true },
        { label: t('Paste'), disabled: !clip, run: () => void pasteClipboard() });
      items.push({ label: t('Select all'), run: () => setSelection(new Set([...devices.map((device) => keyOfDevice(device.id)), ...networks.map((network) => keyOfNetwork(network.id))])) });
      setMenu({ x: event.clientX, y: event.clientY, items });
    }
  };

  const deviceAction = async (device: Device, action: string, query = '') => {
    await run(async () => devicesApi.action(device.rid, action, query));
    await refreshTopology(true);
  };
  const cloneDevice = async (device: Device) => {
    if (!allow('machine.clone')) { toast.error(t('No rights to clone')); return; }
    const created = await run(() => devicesApi.clone(device.rid));
    if (created) { setDevices((current) => [...current, created]); setSelection(new Set([keyOfDevice(created.id)])); await refreshTopology(true); }
  };
  const wipeDevice = async (device: Device) => {
    if (!allow('machine.wipe')) { toast.error(t('No rights to reset the disk')); return; }
    if (!(await askConfirm({ title: `${t('Reset disk of device?')} (${device.name})`, danger: true }))) return;
    await deviceAction(device, 'wipe');
  };
  const setCable = async (connection: Connection, up: boolean) => {
    await run(async () => { await labsApi.setConnectionState(labId, connection.key, up, instanceId || undefined); setConnections((current) => current.map((item) => item.id === connection.id ? { ...item, up } : item)); }, up ? t('Cable connected') : t('Cable disconnected'));
  };
  const addNetwork = async (type: string, x: number, y: number) => {
    const network = await run(() => labsApi.createNetwork(labId, type, x, y));
    if (network) { setNetworks((current) => [...current, network]); setSelection(new Set([keyOfNetwork(network.id)])); }
  };

  const startAll = async () => { await run(() => labsApi.startAll(labId, instanceId || undefined)); await refreshTopology(true); };
  const stopAll = async () => { await run(() => labsApi.stopAll(labId, instanceId || undefined)); await refreshTopology(true); };

  const fit = () => {
    const rect = svgRef.current?.getBoundingClientRect();
    if (rect) applyView(fitPoints(devices, networks, rect, fallbackIconSize));
  };

  useFitOnEntry({
    sessionKey,
    ready: syncedKey === sessionKey,
    version: topologyQuery.dataUpdatedAt,
    target: svgRef,
    compute: (rect) => fitPoints(devices, networks, rect, fallbackIconSize),
    apply: applyView,
  });

  const groupedNodes = useMemo(() => ({ devices, networks }), [devices, networks]);
  const cableLanes = useMemo(() => cableLaneOffsets(connections), [connections]);

  if (!labId) return <Navigate to={paths.labs} replace />;
  if (topologyQuery.isLoading) return <div className="grid min-h-screen place-items-center bg-bg"><div className="text-muted">{t('Loading editor…')}</div></div>;
  if (topologyQuery.error || !lab) return <div className="grid min-h-screen place-items-center bg-bg"><div className="text-bad">{topologyQuery.error?.message ?? t('Laboratory not found')}</div></div>;

  const isAdminElsewhere = user?.role === 'admin' && lab.owner_id !== user.id;
  const runCount = devices.filter((device) => device.state === 'running' || device.state === 'paused').length;
  const collapsed = !sideOpen;
  const iconBtn = 'inline-flex size-9 min-h-9 items-center justify-center rounded-app-sm border border-transparent bg-transparent p-0 text-muted transition-colors hover:bg-panel2 hover:text-text [&_svg]:m-auto';
  const groupClass = `mb-2 first:mt-0 last:mb-0 [&+&]:border-t [&+&]:border-line [&+&]:pt-2${collapsed ? ' min-[901px]:mb-1.5 min-[901px]:[&+&]:pt-1.5' : ''}`;
  const groupTitle = `whitespace-nowrap px-[11px] pb-1 text-[10.5px] font-semibold uppercase tracking-[.09em] text-muted${collapsed ? ' min-[901px]:m-0 min-[901px]:h-0 min-[901px]:overflow-hidden min-[901px]:p-0 min-[901px]:opacity-0' : ''}`;
  // Tool groups are assembled from available items: empty groups and their
  // headings do not take up menu space when permissions are limited.
  const canAddMachine = allow('machine.add');
  const canResizeIcons = allow('machine.move');
  const canManageNetworks = allow('net.manage');
  const canStartAll = allow('machine.start');
  const canStopAll = allow('machine.stop');
  const showAddGroup = canAddMachine || canManageNetworks;
  const showStartGroup = canStartAll || canStopAll;
  const showLabGroup = allow('lab.edit_meta') || allow('lab.manage_access') || allow('lab.export') || allow('lab.wipe');

  return (
    <div className="group/editor flex h-dvh flex-col overflow-hidden bg-bg" data-editor data-collapsed={collapsed} data-drawer={drawer}>
      {isAdminElsewhere && <div className="border-b border-warn bg-[#3b2a0a] px-3 py-1.5 text-[13px] text-warn">{t('Owner')}: «{lab.owner}» · ID {lab.id} · {runCount} / {devices.length}</div>}
      <div className="relative flex h-14 flex-none flex-nowrap items-center gap-2 overflow-visible border-b border-line bg-panel px-2.5 shadow-[0_1px_0_var(--line)] min-[901px]:gap-2.5 min-[901px]:px-3.5">
        <button type="button" className={`${iconBtn} min-[901px]:hidden`} title={t('Tools')} onClick={() => { if (window.matchMedia('(max-width: 900px)').matches) setDrawer((value) => !value); else { const next = !sideOpen; setSideOpen(next); try { localStorage.setItem(SIDE_KEY, next ? '0' : '1'); } catch { /* ignore */ } } }}><Icon name="menu" /></button>
        <button type="button" title={t('Back to laboratories')} onClick={() => navigate(paths.labs)}><Icon name="chevron" size={16} /><span className="max-[900px]:hidden">{t('Laboratories')}</span></button>
        <button type="button" title={t('Running laboratories')} onClick={() => navigate(paths.running)}><Icon name="running" size={16} /></button>
        <input className="w-[150px] min-w-0 flex-[0_1_150px] border-transparent bg-transparent font-semibold hover:border-line hover:bg-bg-2 focus:border-line focus:bg-bg-2 min-[901px]:w-[200px] min-[901px]:flex-[0_1_200px] min-[1100px]:w-[260px] min-[1100px]:flex-[0_1_260px]" value={lab.name} maxLength={80} disabled={!allow('lab.edit_meta')}
          onChange={(event) => setLab({ ...lab, name: event.target.value })}
          onBlur={() => { if (lab.name !== topologyQuery.data?.lab.name) void patchLab({ name: lab.name }); }} />
        <span className={`whitespace-nowrap text-[12.5px] ${statusBad ? 'text-bad' : 'text-muted'}`}>{status}</span>
        <div className="flex-1" />
        <NotificationBell />
      </div>
      <div className="relative flex min-h-0 flex-1 overflow-hidden">
        <aside data-editor-side className={`absolute inset-y-0 left-0 z-[6] flex w-[284px] flex-none flex-col overflow-hidden border-r border-line bg-panel shadow-[0_0_60px_rgba(0,0,0,.5)] transition-transform duration-300 [transition-timing-function:var(--ease)] motion-reduce:transition-none ${drawer ? 'translate-x-0' : '-translate-x-[106%]'} min-[901px]:static min-[901px]:h-full min-[901px]:translate-x-0 min-[901px]:shadow-none min-[901px]:transition-[width,transform,box-shadow] ${collapsed ? 'min-[901px]:w-[62px]' : 'min-[901px]:w-64'}`} aria-label={t('Tools')}>
          <div className={`flex h-[52px] flex-none items-center gap-2 border-b border-line px-3.5 transition-[padding] duration-300 [transition-timing-function:var(--ease)] ${collapsed ? 'justify-center min-[901px]:px-0' : 'min-[901px]:pl-4 min-[901px]:pr-2.5'}`}><span className={`min-w-0 flex-1 whitespace-nowrap text-[11.5px] font-semibold uppercase tracking-[.1em] text-muted transition-[opacity,transform] duration-200 ${collapsed ? 'min-[901px]:hidden' : ''}`}>{t('Tools')}</span><button type="button" className={`${iconBtn} size-8 min-h-8 flex-none self-center rounded-[9px] max-[900px]:hidden`} title={t('Minimize')} onClick={() => { const next = !sideOpen; setSideOpen(next); try { localStorage.setItem(SIDE_KEY, next ? '0' : '1'); } catch { /* ignore */ } }}><Icon name="chevron" className={`transition-transform duration-300 ${collapsed ? 'rotate-180' : ''}`} /></button></div>
          <div className={`min-h-0 flex-1 overflow-y-auto overflow-x-hidden pb-5 pt-2.5 [scrollbar-width:thin] ${collapsed ? 'px-2' : 'px-[9px]'}`}>
            {showAddGroup && (
              <div className={groupClass}><div className={groupTitle}>{t('Add')}</div>
                {canAddMachine && <Tool label={t('Machine')} icon="machine" run={() => { const [x, y] = centerWorld(); setDialog({ kind: 'add-machine', x, y }); }} />}
                {canManageNetworks && <>
                  <Tool label={t('NAT')} icon="network" run={() => { const [x, y] = centerWorld(); void addNetwork('nat', x, y); }} />
                </>}
              </div>
            )}
            {showStartGroup && (
              <div className={groupClass}><div className={groupTitle}>{t('Start')}</div>
                {canStartAll && <Tool label={t('Start all')} icon="play" primary run={() => void startAll()} />}
                {canStopAll && <Tool label={t('Stop all')} icon="stop" run={() => void stopAll()} />}
              </div>
            )}
            <div className={groupClass}><div className={groupTitle}>{t('Status')}</div><Tool label={t('Status')} icon="status" run={() => setDialog({ kind: 'status' })} /></div>
            <div className={groupClass}><div className={groupTitle}>{t('View')}</div>
              <Tool label={t('Zoom in')} icon="zoomIn" run={() => zoomBy(1.2)} />
              <Tool label={t('Zoom out')} icon="zoomOut" run={() => zoomBy(1 / 1.2)} />
              <Tool label={t('Fit to screen')} icon="fit" run={fit} />
            </div>
            {showLabGroup && (
              <div className={groupClass}><div className={groupTitle}>{t('Laboratory')}</div>
                {allow('lab.edit_meta') && <Tool label={t('Parameters')} icon="settings" run={() => setDialog({ kind: 'lab' })} />}
                {allow('lab.manage_access') && <Tool label={t('Access permissions')} icon="access" run={() => setDialog({ kind: 'access' })} />}
                {allow('lab.export') && <Tool label={t('Export')} icon="export" run={() => void exportLab(lab)} />}
                {allow('lab.wipe') && <Tool label={t('Wipe laboratory')} icon="wipe" danger run={() => setDialog({ kind: 'wipe' })} />}
              </div>
            )}
          </div>
          <div data-editor-side-foot className="flex-none border-t border-line bg-panel px-[9px] pb-2.5 pt-2">
            <button type="button" className="group/tool flex min-h-10 w-full items-center justify-start gap-3 rounded-[11px] border border-accent/35 bg-accent/[.12] px-[11px] py-2 text-[13.5px] font-semibold text-text transition-[background,border-color] duration-200 hover:border-accent/55 hover:bg-accent/[.18]" title={t('Console')}
              onClick={(event) => {
                // A selected machine opens exactly as a double-click would; otherwise the usual launcher.
                const key = [...selection].find((item) => item[0] === 'd');
                const device = key ? devices.find((item) => item.id === idOf(key)) : undefined;
                if (device) openDeviceConsole(device); else showConsoleLauncher(event.currentTarget);
              }}>
              <Icon name="term" className="text-accent transition-transform duration-200 group-hover/tool:scale-110" /><span className={`max-w-[200px] overflow-hidden text-ellipsis whitespace-nowrap transition-[opacity,max-width] duration-200 ${collapsed ? 'min-[901px]:max-w-0 min-[901px]:opacity-0' : ''}`}>{t('Console')}</span>
            </button>
          </div>
        </aside>
        <div className="relative min-h-0 min-w-0 flex-1 overflow-hidden">
          <svg ref={svgRef} data-editor-canvas className={`block h-full w-full select-none bg-[radial-gradient(var(--line)_1px,transparent_1px)] [background-size:24px_24px] ${space ? 'cursor-grab' : ''}`} onMouseDown={onCanvasDown} onContextMenu={onContextMenu}
            onWheel={(event) => { event.preventDefault(); const rect = svgRef.current?.getBoundingClientRect(); if (rect) zoomAt(event.deltaY < 0 ? 1.1 : 1 / 1.1, event.clientX - rect.left, event.clientY - rect.top); }}>
            <g transform={`translate(${view.x} ${view.y}) scale(${view.k})`}>
              <g>
                {connections.map((connection) => {
                  const a = connection.a_type === 'device' ? groupedNodes.devices.find((device) => device.id === connection.a_id) : groupedNodes.networks.find((network) => network.id === connection.a_id);
                  const b = connection.b_type === 'device' ? groupedNodes.devices.find((device) => device.id === connection.b_id) : groupedNodes.networks.find((network) => network.id === connection.b_id);
                  if (!a || !b) return null;
                  const key = keyOfConnection(connection.id);
                  const ends = [connection.a_type === 'device' ? a : null, connection.b_type === 'device' ? b : null].filter((item): item is Device => item !== null);
                  const up = ends.every((device) => device.state === 'running' || device.state === 'paused');
                  const cut = connection.up === false;
                  const sel = selection.has(key);
                  // Wire color mirrors the state of interfaces at its ends: green - everything
                  // is configured, red - the interface is disabled or has no address, gray - the machine is off.
                  const wireEnds = [{ device: a as Device, nic: connection.a_if }, { device: b as Device, nic: connection.b_if }]
                    .filter((_, index) => (index === 0 ? connection.a_type : connection.b_type) === 'device');
                  const tone = cableTone(wireEnds);
                  const dx = b.x - a.x, dy = b.y - a.y, length = Math.hypot(dx, dy) || 1, ux = dx / length, uy = dy / length;
                  // Wires of one node pair run in parallel with the same step. The offset
                  // direction is normalized by the canonical order of the ends, so a wire
                  // saved in the reverse direction does not land on its neighbor.
                  const lane = cableLanes.get(connection.id) ?? 0;
                  const reversed = endpointKey(connection, 'a') > endpointKey(connection, 'b');
                  const shift = reversed ? -lane : lane;
                  const ox = -uy * shift, oy = ux * shift;
                  const ax = a.x + ox, ay = a.y + oy, bx = b.x + ox, by = b.y + oy;
                  // Ports and labels sit at the edge of a specific machine's icon: each has its own size.
                  const aHalf = connection.a_type === 'device' ? iconSizeOf(a as Device, fallbackIconSize) / 2 : 0;
                  const bHalf = connection.b_type === 'device' ? iconSizeOf(b as Device, fallbackIconSize) / 2 : 0;
                  const portOffsetA = aHalf + 2, portLabelOffsetA = aHalf + 20;
                  const portOffsetB = bHalf + 2, portLabelOffsetB = bHalf + 20;
                  const portTip = (device: Device, nic: number) => `${ifaceName(device, nic)} — MAC ${nicMAC(device.rid, nic)}: ${portState(device, nic) === 'bad' ? t('Interface is disabled or has no IP address') : portState(device, nic) === 'ok' ? t('Interface is configured') : t('Interface state is unknown')}`;
                  const stroke = cut ? 'var(--warn)' : sel ? 'var(--accent)' : tone === 'ok' ? 'var(--ok)' : tone === 'bad' ? 'var(--bad)' : 'var(--muted)';
                  // Indicator at the connection point: red - port not configured, green - configured and working.
                  const portDot = (device: Device, nic: number, x: number, y: number) => {
                    const state = portState(device, nic);
                    if (state === 'unknown') return null;
                    return <circle data-port-state={state} cx={x} cy={y} r={4.5} fill={PORT_COLOR[state]} stroke="var(--bg)" strokeWidth={1.5} pointerEvents="none" />;
                  };
                  const portLabel = (device: Device, nic: number, x: number, y: number) => {
                    const state = portState(device, nic);
                    return <text data-connection-label data-tone={state} fill={PORT_COLOR[state]} fontSize={10} fontWeight={state === 'bad' ? 700 : 400} pointerEvents="none" x={x} y={y} textAnchor="middle">{ifaceName(device, nic)}<title>{portTip(device, nic)}</title></text>;
                  };
                  return (
                    <g key={connection.id} className="link" data-key={key} data-tone={tone}>
                      <title>{cut ? t('Cable disconnected') : tone === 'bad' ? t('Connection is not ready: an interface is disabled or has no IP address') : tone === 'ok' ? t('Cable connected') : `${t('Stopped')}: ${ends.filter((device) => !(device.state === 'running' || device.state === 'paused')).map((device) => device.name).join(', ')}`}</title>
                      <line data-link-hit stroke="transparent" strokeWidth={14} style={{ cursor: 'pointer' }} x1={ax} y1={ay} x2={bx} y2={by} />
                      <line stroke={stroke} strokeWidth={sel ? 3 : 2} strokeDasharray={cut ? '2 6' : up ? undefined : '7 5'} opacity={cut ? .8 : tone === 'off' ? .55 : 1} pointerEvents="none" x1={ax} y1={ay} x2={bx} y2={by} />
                      {connection.a_type === 'device' && portDot(a as Device, connection.a_if, ax + ux * portOffsetA, ay + uy * portOffsetA)}
                      {connection.b_type === 'device' && portDot(b as Device, connection.b_if, bx - ux * portOffsetB, by - uy * portOffsetB)}
                      {connection.a_type === 'device' && portLabel(a as Device, connection.a_if, ax + ux * portLabelOffsetA, ay + uy * portLabelOffsetA + 4)}
                      {connection.b_type === 'device' && portLabel(b as Device, connection.b_if, bx - ux * portLabelOffsetB, by - uy * portLabelOffsetB + 4)}
                    </g>
                  );
                })}
                {tempLink && <line stroke="var(--accent)" strokeWidth={2} strokeDasharray="6 4" pointerEvents="none" x1={tempLink.x1} y1={tempLink.y1} x2={tempLink.x2} y2={tempLink.y2} />}
              </g>
              <g>
                {devices.map((device) => {
                  const key = keyOfDevice(device.id);
                  const sel = selection.has(key);
                  const tone = machineTone(device);
                  const size = iconSizeOf(device, fallbackIconSize);
                  const half = size / 2;
                  return (
                    <g key={device.id} className="node" data-key={key} data-tone={tone} data-icon-size={size} transform={`translate(${device.x} ${device.y})`}
                      onMouseEnter={() => scheduleHoverFocus(device.rid)} onMouseLeave={cancelHoverFocus}>
                      <title>{`${device.name}\n${t((STATE_RU[device.state] ?? device.state) as Parameters<typeof t>[0])}${device.last_error ? '\n' + device.last_error : ''}`}</title>
                      <rect fill="transparent" stroke="none" style={{ cursor: 'move' }} x={-half} y={-half} width={size} height={size + 18} rx={12} />
                      {/* Icon without a background plate. Selection is a border, not a CSS filter: filters on SVG images are slow in Safari. */}
                      {sel && <rect x={-half - 4} y={-half - 4} width={size + 8} height={size + 8} rx={14} fill="none" stroke="var(--accent)" strokeWidth={2} pointerEvents="none" />}
                      <image data-node-icon href={deviceIconSrc(device)} x={-half} y={-half} width={size} height={size}
                        preserveAspectRatio="xMidYMid meet" pointerEvents="none" opacity={tone === 'off' ? 0.5 : 1}
                        style={{ transition: iconResizing ? 'none' : 'x 160ms var(--ease), y 160ms var(--ease), width 160ms var(--ease), height 160ms var(--ease)' }} />
                      <text data-node-label y={half + 12} textAnchor="middle" fontSize={12} fill={tone === 'off' ? 'var(--muted)' : 'var(--text)'} pointerEvents="none">{device.name}</text>
                      {sel && canResizeIcons && RESIZE_HANDLES.map((handle) => (
                        <g key={handle.dir} data-resize-handle={handle.dir} transform={`translate(${handle.x * half} ${handle.y * half})`}
                          style={{ cursor: handle.cursor }} onMouseDown={(event) => startIconResize(event, device, handle.dir)}>
                          {/* Handles of constant screen size: independent of camera zoom. */}
                          <circle r={5 / view.k} fill="var(--panel)" stroke="var(--accent)" strokeWidth={1.5 / view.k} />
                        </g>
                      ))}
                    </g>
                  );
                })}
                {networks.map((network) => {
                  const key = keyOfNetwork(network.id);
                  const sel = selection.has(key);
                  return (
                    <g key={network.id} className="node" data-key={key} transform={`translate(${network.x} ${network.y})`}>
                      <title>{`${network.name}${network.subnet ? '\n' + network.subnet : ''}`}</title>
                      {network.type === 'cloud' || network.type === 'nat' ? (
                        <>{sel && <rect x={-46} y={-32} width={92} height={64} rx={10} fill="none" stroke="var(--accent)" strokeWidth={2} pointerEvents="none" />}<rect fill="transparent" x={-42} y={-28} width={84} height={56} rx={8} /><image href={iconFor(network.type)} x={-42} y={-28} width={84} height={56} pointerEvents="none" /></>
                      ) : <circle r={26} fill="#444b57" stroke={sel ? 'var(--accent)' : 'var(--line)'} strokeWidth={sel ? 3 : 2} />}
                      {network.type === 'isolated' && <text y={6} textAnchor="middle" fontSize={14} fontWeight={700} fill="#fff" pointerEvents="none">{t('NET')}</text>}
                      <text data-node-label y={44} textAnchor="middle" fontSize={12} fill="var(--text)" pointerEvents="none">{network.name}</text>
                    </g>
                  );
                })}
              </g>
            </g>
            {band && <rect fill="rgba(59,130,246,.15)" stroke="var(--accent)" strokeWidth={1} pointerEvents="none" x={band.x} y={band.y} width={band.w} height={band.h} />}
          </svg>
        </div>
        {drawer && <div className="absolute inset-0 z-[5] bg-[rgba(3,6,12,.5)] backdrop-blur-[2px] motion-safe:animate-[fadeIn_.25s_both] min-[901px]:hidden" onClick={() => setDrawer(false)} />}
      </div>
      {menu && <ContextMenu x={menu.x} y={menu.y} items={menu.items} onClose={() => setMenu(null)} />}
      {dialog?.kind === 'add-machine' && <AddMachineDialog labId={labId} x={dialog.x} y={dialog.y} templates={templates} images={images} onClose={() => setDialog(null)}
        onDone={(created) => { setDevices((current) => [...current, ...created]); setSelection(new Set(created.map((device) => keyOfDevice(device.id)))); }} />}
      {dialog?.kind === 'device' && <DeviceSettingsDialog labName={lab.name} device={dialog.device} images={images} onClose={() => setDialog(null)}
        onSaved={(saved) => { setDevices((current) => current.map((device) => device.rid === saved.rid ? saved : device)); void refreshTopology(true); }} />}
      {dialog?.kind === 'network' && <NetworkSettingsDialog labName={lab.name} network={dialog.network} onClose={() => setDialog(null)} onSaved={() => { void refreshTopology(true); }} />}
      {dialog?.kind === 'connect' && <ConnectDialog labId={labId} a={dialog.a} b={dialog.b} devices={devices} networks={networks} connections={connections}
        onClose={() => setDialog(null)}
        onConnected={(connection) => { pendingConns.current.set(connection.key, connection); setConnections((current) => [...current, connection]); void refreshTopology(true); }} />}
      {dialog?.kind === 'saveImage' && <SaveImageDialog device={dialog.device} onClose={() => setDialog(null)} onChanged={() => { void refreshImages(); void refreshTopology(true); }} />}
      {dialog?.kind === 'log' && <LogDialog device={dialog.device} onClose={() => setDialog(null)} />}
      {dialog?.kind === 'status' && <StatusDialog labId={labId} labName={lab.name} instance={instanceId || undefined} onClose={() => setDialog(null)} />}
      {dialog?.kind === 'lab' && <LabDialog lab={lab} onClose={() => setDialog(null)} onSaved={() => { void refreshTopology(true); }} />}
      {dialog?.kind === 'access' && <AccessDialog lab={lab} onClose={() => { setDialog(null); void refreshTopology(true); }} />}
      {dialog?.kind === 'wipe' && <WipeLabDialog lab={lab} instance={instanceId || undefined} onClose={() => setDialog(null)} onDone={() => { void refreshTopology(true); }} />}
    </div>
  );
}

function Tool({ label, icon, run, primary, danger }: { label: string; icon: Parameters<typeof Icon>[0]['name']; run: () => void; primary?: boolean; danger?: boolean }) {
  return (
    <button type="button" className={`group/tool flex min-h-10 w-full items-center justify-start gap-3 rounded-[11px] border border-transparent bg-transparent px-[11px] py-2 text-[13.5px] font-medium text-text whitespace-nowrap transition-[background,border-color,transform,color,box-shadow] duration-[220ms] [transition-timing-function:var(--ease)] hover:translate-x-[3px] hover:border-line hover:bg-panel2 hover:shadow-[0_6px_16px_-12px_rgba(0,0,0,.6)] active:translate-x-[2px] active:scale-[.985] focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent group-data-[collapsed=true]/editor:min-[901px]:justify-center group-data-[collapsed=true]/editor:min-[901px]:gap-0 group-data-[collapsed=true]/editor:min-[901px]:px-0 group-data-[collapsed=true]/editor:min-[901px]:py-[9px] group-data-[collapsed=true]/editor:min-[901px]:hover:translate-x-0 ${primary ? 'hover:border-ok/35' : danger ? 'text-bad hover:border-bad/35 hover:bg-bad/[.11]' : ''}`} title={label} aria-label={label} onClick={run}>
      <Icon name={icon} className={`transition-transform duration-200 group-hover/tool:scale-110 ${primary ? 'text-ok' : danger ? 'text-bad' : 'text-accent'}`} /><span className="max-w-[200px] overflow-hidden text-ellipsis whitespace-nowrap transition-[opacity,max-width] duration-200 group-data-[collapsed=true]/editor:min-[901px]:max-w-0 group-data-[collapsed=true]/editor:min-[901px]:opacity-0">{label}</span>
    </button>
  );
}

async function exportLab(lab: Lab): Promise<void> {
  await guard(async () => {
    const blob = await labsApi.exportLab(lab.id);
    const anchor = document.createElement('a');
    anchor.href = URL.createObjectURL(blob);
    anchor.download = `${lab.name}.yaml`;
    document.body.append(anchor); anchor.click(); anchor.remove();
    URL.revokeObjectURL(anchor.href);
  });
}

import { create } from 'zustand';
import { notificationsApi } from '../api/endpoints';
import type { NotificationCategory, NotificationItem } from '../types/api';
import { useAuthStore } from './auth';

const POLL_MS = 8000;
const FETCH_LIMIT = 300;

interface NotificationsState {
  items: NotificationItem[];
  unread: number;
  categories: NotificationCategory[];
  open: boolean;
  wsLive: boolean;
  loaded: boolean;
  refresh: () => Promise<void>;
  start: () => void;
  stop: () => void;
  setOpen: (open: boolean) => void;
  toggle: () => void;
  mark: (id: number, read: boolean) => Promise<void>;
  remove: (id: number) => Promise<void>;
  readAll: () => Promise<void>;
  clearRead: () => Promise<void>;
  /** Adds a message that is shown only in this browser (for example, an error toast). */
  addLocal: (level: NotificationItem['level'], title: string) => void;
}

let ws: WebSocket | null = null;
let retryTimer: number | null = null;
let pollTimer: number | null = null;
let retry = 1000;
let started = false;
// Client-side items get negative ids so they never clash with server ids.
let localSeq = -1;

function currentUserID(): number | null {
  return useAuthStore.getState().user?.id ?? null;
}

function sameUser(id: number): boolean {
  return currentUserID() === id;
}

function scheduleReconnect(): void {
  if (retryTimer !== null || ws !== null || !started || currentUserID() === null) return;
  retryTimer = window.setTimeout(() => {
    retryTimer = null;
    retry = Math.min(retry * 2, 30000);
    connect();
  }, retry);
}

function connect(): void {
  if (!started || ws || currentUserID() === null) return;
  const protocol = location.protocol === 'https:' ? 'wss://' : 'ws://';
  let socket: WebSocket;
  try {
    socket = new WebSocket(`${protocol}${location.host}/ws/notifications`);
  } catch {
    scheduleReconnect();
    return;
  }
  ws = socket;
  socket.onopen = () => {
    if (ws !== socket) return;
    retry = 1000;
    useNotificationsStore.setState({ wsLive: true });
    void useNotificationsStore.getState().refresh();
  };
  socket.onmessage = (event) => {
    if (ws !== socket) return;
    try {
      const message = JSON.parse(String(event.data)) as {
        type?: string;
        item?: NotificationItem;
        unread?: number;
      };
      if (message.type === 'new' && message.item) {
        const item = message.item;
        useNotificationsStore.setState((state) => {
          const exists = state.items.some((entry) => entry.id === item.id);
          const items = exists
            ? state.items.map((entry) => (entry.id === item.id ? item : entry))
            : [item, ...state.items].slice(0, FETCH_LIMIT);
          return { items, unread: message.unread ?? state.unread };
        });
      } else if (message.type === 'sync') {
        useNotificationsStore.setState({ unread: message.unread ?? 0 });
        void useNotificationsStore.getState().refresh();
      }
    } catch {
      // ignore malformed frames
    }
  };
  socket.onclose = () => {
    if (ws === socket) {
      ws = null;
      useNotificationsStore.setState({ wsLive: false });
    }
    scheduleReconnect();
  };
  socket.onerror = () => {
    try {
      socket.close();
    } catch {
      // connection is already closed
    }
  };
}

function disconnect(): void {
  if (retryTimer !== null) {
    clearTimeout(retryTimer);
    retryTimer = null;
  }
  if (ws) {
    const socket = ws;
    ws = null;
    socket.onopen = null;
    socket.onmessage = null;
    socket.onerror = null;
    socket.onclose = null;
    try {
      socket.close();
    } catch {
      // connection is already closed
    }
  }
  useNotificationsStore.setState({ wsLive: false });
}

export const useNotificationsStore = create<NotificationsState>((set, get) => ({
  items: [],
  unread: 0,
  categories: [],
  open: false,
  wsLive: false,
  loaded: false,
  refresh: async () => {
    const uid = currentUserID();
    if (uid === null) {
      set({ items: [], unread: 0, categories: [], wsLive: false, loaded: false });
      disconnect();
      return;
    }
    try {
      const response = await notificationsApi.list(FETCH_LIMIT);
      // The response may arrive after a logout or an account switch.
      if (!sameUser(uid)) return;
      // Client-side items survive a refresh: they exist only in this browser.
      const local = get().items.filter((item) => item.id < 0);
      set({
        items: [...response.items, ...local],
        unread: response.unread + local.filter((item) => !item.read).length,
        categories: response.categories,
        loaded: true,
      });
    } catch {
      // Network unavailable: keep the last snapshot, but stop showing an empty spinner state.
      if (sameUser(uid)) set({ loaded: true });
    }
  },
  start: () => {
    if (currentUserID() === null || started) return;
    started = true;
    if (pollTimer === null) {
      pollTimer = window.setInterval(() => {
        if (!get().wsLive) void get().refresh();
      }, POLL_MS);
    }
    connect();
    void get().refresh();
  },
  stop: () => {
    started = false;
    if (pollTimer !== null) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
    disconnect();
    set({ items: [], unread: 0, categories: [], open: false, loaded: false });
  },
  setOpen: (open) => {
    set({ open });
    if (open) void get().refresh();
  },
  toggle: () => {
    const open = !get().open;
    set({ open });
    if (open) void get().refresh();
  },
  mark: async (id, read) => {
    const item = get().items.find((entry) => entry.id === id);
    if (!item || item.read === read) return;
    set((state) => ({
      items: state.items.map((entry) => (entry.id === id ? { ...entry, read } : entry)),
      unread: Math.max(0, state.unread + (read ? -1 : 1)),
    }));
    if (id < 0) return;
    try {
      await notificationsApi.setRead(id, read);
    } catch {
      // A refresh below restores the authoritative state.
    }
    await get().refresh();
  },
  remove: async (id) => {
    const item = get().items.find((entry) => entry.id === id);
    if (!item) return;
    set((state) => ({
      items: state.items.filter((entry) => entry.id !== id),
      unread: item.read ? state.unread : Math.max(0, state.unread - 1),
    }));
    if (id < 0) return;
    try {
      await notificationsApi.remove(id);
    } catch {
      // A refresh below restores the authoritative state.
    }
    await get().refresh();
  },
  readAll: async () => {
    if (get().unread === 0 && get().items.every((item) => item.read)) return;
    set((state) => ({
      items: state.items.map((item) => (item.read ? item : { ...item, read: true })),
      unread: 0,
    }));
    try {
      await notificationsApi.readAll();
    } catch {
      // A refresh below restores the authoritative state.
    }
    await get().refresh();
  },
  addLocal: (level, title) => {
    const item: NotificationItem = {
      id: localSeq--, level, category: 'system', title, body: '', link: '', read: false,
      created_at: Math.floor(Date.now() / 1000),
    };
    set((state) => ({ items: [item, ...state.items].slice(0, FETCH_LIMIT), unread: state.unread + 1 }));
  },
  clearRead: async () => {
    if (!get().items.some((item) => item.read)) return;
    set((state) => ({ items: state.items.filter((item) => !item.read) }));
    try {
      await notificationsApi.clearRead();
    } catch {
      // A refresh below restores the authoritative state.
    }
    await get().refresh();
  },
}));

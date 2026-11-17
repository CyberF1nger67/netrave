import { create } from 'zustand';
import { authApi } from '../api/endpoints';
import { setCsrfToken, setUnauthorizedHandler } from '../api/client';
import type { User } from '../types/api';

type AuthStatus = 'loading' | 'guest' | 'authenticated';

interface AuthState {
  user: User | null;
  status: AuthStatus;
  /** Session expired on the server: show a notification on the login page. */
  sessionExpired: boolean;
  bootstrap: () => Promise<void>;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  clear: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  status: 'loading',
  sessionExpired: false,
  bootstrap: async () => {
    try {
      const session = await authApi.me();
      setCsrfToken(session.csrf);
      set({ user: session.user, status: 'authenticated' });
    } catch {
      setCsrfToken('');
      set({ user: null, status: 'guest' });
    }
  },
  login: async (username, password) => {
    const session = await authApi.login(username, password);
    setCsrfToken(session.csrf);
    set({ user: session.user, status: 'authenticated', sessionExpired: false });
  },
  logout: async () => {
    try {
      await authApi.logout();
    } finally {
      setCsrfToken('');
      set({ user: null, status: 'guest', sessionExpired: false });
    }
  },
  // Called when the session expires (401 response from the server).
  clear: () => {
    setCsrfToken('');
    set({ user: null, status: 'guest', sessionExpired: true });
  },
}));

setUnauthorizedHandler(() => {
  if (useAuthStore.getState().user) useAuthStore.getState().clear();
});

const ADMIN_SECTION_PERMS: Record<string, string> = {
  dashboard: 'admin.access',
  logs: 'admin.access',
  templates: 'admin.access',
  images: 'admin.access',
};

export function can(user: User | null, perm: string): boolean {
  if (!user) return false;
  if (user.role === 'admin') return true;
  if ((user.role_perms ?? []).includes(perm)) return true;
  const rolePerm = ADMIN_SECTION_PERMS[perm];
  return Boolean(rolePerm && (user.role_perms ?? []).includes(rolePerm));
}

export function canManageFolders(user: User | null): boolean {
  return can(user, 'folders.manage');
}

export function canMoveLabs(user: User | null): boolean {
  return can(user, 'folders.move_labs') || canManageFolders(user);
}

export function canCreateLabs(user: User | null): boolean {
  if (!user) return false;
  if (user.role === 'admin') return true;
  if (!(user.roles ?? []).length) return true;
  return can(user, 'laboratories.create');
}

// Changing one's own password: admin - always, a user without roles - as before,
// after roles are assigned an explicit users.password permission is required.
export function canChangePassword(user: User | null): boolean {
  if (!user) return false;
  if (user.role === 'admin') return true;
  if (!(user.roles ?? []).length) return true;
  return (user.role_perms ?? []).includes('users.password');
}

export interface PageDef {
  title: string;
  perm?: string;
  admin?: boolean;
}

export function canPage(user: User | null, page: PageDef): boolean {
  if (!user) return false;
  if (page.admin) return user.role === 'admin';
  if (page.perm) return can(user, page.perm);
  return true;
}


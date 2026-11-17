import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider, createMemoryRouter } from 'react-router-dom';
import { routeTree } from './routes';
import { useAuthStore } from '../stores/auth';
import type { User } from '../types/api';

vi.mock('../features/notifications/NotificationsLayer', () => ({ NotificationsLayer: () => null }));

/** In-memory router: the same route set, without binding to the address bar. */
function createTestRouter(initialEntries: string[]) {
  return createMemoryRouter(routeTree, { initialEntries });
}

const admin: User = { id: 1, username: 'admin', role: 'admin', disabled: false, created_at: 0, roles: null, role_perms: null };
const plain: User = { ...admin, id: 2, username: 'plain', role: 'user', roles: [], role_perms: [] };

function renderAt(path: string) {
  const router = createTestRouter([path]);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return router;
}

function signIn(user: User) {
  act(() => useAuthStore.setState({ user, status: 'authenticated', sessionExpired: false }));
}

beforeEach(() => {
  useAuthStore.setState({ user: null, status: 'guest', sessionExpired: false });
  // Pages load data via relative URLs, which do not exist in jsdom: respond with empty JSON.
  vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } })));
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('защита маршрутов', () => {
  it('гостя перенаправляет на вход и запоминает исходный адрес', async () => {
    const router = renderAt('/users');
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'));
    expect(router.state.location.state).toEqual({ from: '/users' });
  });

  it('после входа возвращает пользователя на исходную страницу', async () => {
    const router = renderAt('/users');
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'));
    signIn(admin);
    await waitFor(() => expect(router.state.location.pathname).toBe('/users'));
  });

  it('при истечении сессии уводит на вход и помечает её как истёкшую', async () => {
    signIn(admin);
    const router = renderAt('/labs');
    await waitFor(() => expect(router.state.location.pathname).toBe('/labs'));
    act(() => useAuthStore.getState().clear());
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'));
    expect(useAuthStore.getState().sessionExpired).toBe(true);
    expect(router.state.location.state).toEqual({ from: '/labs' });
  });

  it('пользователь без права не попадает в раздел и возвращается в лаборатории', async () => {
    signIn(plain);
    const router = renderAt('/users');
    await waitFor(() => expect(router.state.location.pathname).toBe('/labs'));
  });

  it('уже вошедшего пользователя не пускает на страницу входа', async () => {
    signIn(admin);
    const router = renderAt('/login');
    await waitFor(() => expect(router.state.location.pathname).toBe('/labs'));
  });

  it('корень открывает лаборатории', async () => {
    signIn(admin);
    const router = renderAt('/');
    await waitFor(() => expect(router.state.location.pathname).toBe('/labs'));
  });
});

describe('404 и числовой адрес редактора', () => {
  it('неизвестный адрес показывает страницу 404 внутри оболочки', async () => {
    signIn(admin);
    renderAt('/no-such-page');
    expect(await screen.findByText('Page not found')).toBeInTheDocument();
  });

  it('числовой адрес редактора открывает лабораторию по номеру и сохраняет instance', async () => {
    signIn(admin);
    const labs = [
      { id: 7, owner_id: 1, owner: 'admin', name: 'Периметр', description: '', canvas: null, created_at: 0, updated_at: 0, devices: 0, networks: 0, public_level: 'none' as const, perms: {}, folder_id: 3 },
    ];
    const folders = [{ id: 3, parent_id: null, name: 'Сети', lab_count: 1, created_at: 0, updated_at: 0 }];
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.match(/\/api\/labs\/\d+/)) return new Response(JSON.stringify(labs[0]), { status: 200, headers: { 'Content-Type': 'application/json' } });
      if (url.includes('/api/lab-folders') || url.includes('/api/folders')) return new Response(JSON.stringify(folders), { status: 200, headers: { 'Content-Type': 'application/json' } });
      if (url.includes('/api/labs')) return new Response(JSON.stringify(labs), { status: 200, headers: { 'Content-Type': 'application/json' } });
      return new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } });
    }));
    const router = renderAt('/labs/7/editor?instance=2');
    // The lab URL is preserved, and the editor opens by its id.
    await waitFor(() => expect(router.state.location.pathname).toBe('/labs/7/editor'));
    await screen.findByText('Loading editor…');
    expect(router.state.location.search).toBe('?instance=2');
  });
});

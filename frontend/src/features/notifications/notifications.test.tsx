import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { NotificationItem } from '../../types/api';
import { useAuthStore } from '../../stores/auth';
import { useNotificationsStore } from '../../stores/notifications';
import { NotificationBell } from './NotificationBell';
import { NotificationsPanel } from './NotificationsPanel';

const api = vi.hoisted(() => ({
  list: vi.fn(),
  setRead: vi.fn(),
  remove: vi.fn(),
  readAll: vi.fn(),
  clearRead: vi.fn(),
}));

vi.mock('../../api/endpoints', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/endpoints')>()),
  notificationsApi: api,
}));

function item(id: number, patch: Partial<NotificationItem> = {}): NotificationItem {
  return {
    id,
    level: 'info',
    category: 'machine',
    title: `Event ${id}`,
    body: `Body ${id}`,
    link: '',
    read: false,
    created_at: 1_700_000_000 + id,
    ...patch,
  };
}

let server: NotificationItem[] = [];

function renderPanel() {
  return render(
    <MemoryRouter>
      <NotificationBell />
      <NotificationsPanel />
    </MemoryRouter>,
  );
}

function panel(container: HTMLElement): HTMLElement {
  const element = container.querySelector<HTMLElement>('#notifications-panel');
  if (!element) throw new Error('panel not found');
  return element;
}

beforeEach(() => {
  server = [];
  vi.clearAllMocks();
  api.list.mockImplementation(async () => ({
    items: server,
    unread: server.filter((entry) => !entry.read).length,
    categories: [{ key: 'machine', label: 'Machines' }],
  }));
  api.setRead.mockImplementation(async (id: number, read: boolean) => {
    server = server.map((entry) => (entry.id === id ? { ...entry, read } : entry));
    return { ok: true };
  });
  api.remove.mockImplementation(async (id: number) => {
    server = server.filter((entry) => entry.id !== id);
    return { ok: true };
  });
  api.readAll.mockImplementation(async () => {
    server = server.map((entry) => ({ ...entry, read: true }));
    return { ok: true };
  });
  api.clearRead.mockImplementation(async () => {
    server = server.filter((entry) => !entry.read);
    return { ok: true };
  });
  useAuthStore.setState({
    user: { id: 1, username: 'admin', role: 'admin', disabled: false, created_at: 0, roles: [], role_perms: [] },
    status: 'authenticated',
  });
  useNotificationsStore.getState().stop();
  useNotificationsStore.setState({ categories: [{ key: 'machine', label: 'Machines' }] });
});

describe('notifications dropdown', () => {
  it('shows the unread counter and toggles on the bell', async () => {
    server = [item(1), item(2, { read: true })];
    useNotificationsStore.setState({ items: server, unread: 1, loaded: true });
    const { container } = renderPanel();
    const bell = screen.getByRole('button', { name: 'Notifications' });
    const count = bell.querySelector('[data-notification-count]');

    expect(count).toHaveTextContent('1');
    expect(count).not.toHaveAttribute('hidden');
    expect(panel(container)).toHaveAttribute('aria-hidden', 'true');

    fireEvent.click(bell);
    await waitFor(() => expect(panel(container)).toHaveAttribute('data-open', 'true'));
    expect(panel(container)).toHaveAttribute('aria-hidden', 'false');

    fireEvent.click(bell);
    await waitFor(() => expect(panel(container)).not.toHaveAttribute('data-open', 'true'));
  });

  it('closes on Escape and on an outside click', async () => {
    server = [item(1)];
    useNotificationsStore.setState({ items: server, unread: 1, loaded: true });
    const { container } = renderPanel();
    const bell = screen.getByRole('button', { name: 'Notifications' });

    fireEvent.click(bell);
    await waitFor(() => expect(panel(container)).toHaveAttribute('data-open', 'true'));
    fireEvent.keyDown(document, { key: 'Escape' });
    await waitFor(() => expect(panel(container)).not.toHaveAttribute('data-open', 'true'));

    fireEvent.click(bell);
    await waitFor(() => expect(panel(container)).toHaveAttribute('data-open', 'true'));
    fireEvent.mouseDown(document.body);
    await waitFor(() => expect(panel(container)).not.toHaveAttribute('data-open', 'true'));
  });

  it('renders one list sorted by time with unread and read states', () => {
    server = [item(2), item(1), item(3, { read: true })];
    useNotificationsStore.setState({ items: server, unread: 2, open: true, loaded: true });
    const { container } = renderPanel();
    const list = within(panel(container)).getByRole('list');
    const rows = within(list).getAllByRole('listitem');

    expect(rows).toHaveLength(3);
    expect(rows[0]).toHaveTextContent('Event 3');
    expect(rows[1]).toHaveTextContent('Event 2');
    expect(rows[2]).toHaveTextContent('Event 1');
    expect(rows[0]).toHaveAttribute('data-read', 'true');
    expect(rows[1]).toHaveAttribute('data-read', 'false');
    expect(within(rows[1] as HTMLElement).getByText('Unread')).toBeInTheDocument();
    expect(within(rows[0] as HTMLElement).getByText('Read')).toBeInTheDocument();
    expect(within(rows[1] as HTMLElement).getByText('Machines')).toBeInTheDocument();
  });

  it('paginates long lists and reveals more on demand', () => {
    server = Array.from({ length: 25 }, (_, index) => item(index + 1));
    useNotificationsStore.setState({ items: server, unread: 25, open: true, loaded: true });
    const { container } = renderPanel();
    const list = within(panel(container)).getByRole('list');

    expect(within(list).getAllByRole('listitem')).toHaveLength(21);
    fireEvent.click(within(list).getByRole('button', { name: /Show more/ }));
    expect(within(list).getAllByRole('listitem')).toHaveLength(25);
  });

  it('marks a notification read and deletes it through the API', async () => {
    server = [item(7), item(8)];
    useNotificationsStore.setState({ items: server, unread: 2, open: true, loaded: true });
    const { container } = renderPanel();
    const first = within(panel(container)).getAllByRole('listitem')[0] as HTMLElement;

    fireEvent.click(within(first).getByRole('button', { name: /Event 8/ }));
    await waitFor(() => expect(api.setRead).toHaveBeenCalledWith(8, true));

    const del = within(first).getByRole('button', { name: 'Delete notification' });
    fireEvent.click(del);
    await waitFor(() => expect(api.remove).toHaveBeenCalledWith(8));
  });

  it('supports read all and clear read', async () => {
    server = [item(1), item(2, { read: true })];
    useNotificationsStore.setState({ items: server, unread: 1, open: true, loaded: true });
    const { container } = renderPanel();

    fireEvent.click(within(panel(container)).getByRole('button', { name: 'Read all' }));
    await waitFor(() => expect(api.readAll).toHaveBeenCalled());

    fireEvent.click(within(panel(container)).getByRole('button', { name: /Clear read/ }));
    await waitFor(() => expect(api.clearRead).toHaveBeenCalled());
  });

  it('shows a friendly empty state', () => {
    useNotificationsStore.setState({ items: [], unread: 0, open: true, loaded: true });
    const { container } = renderPanel();

    expect(within(panel(container)).getByText('There are no notifications yet')).toBeInTheDocument();
    expect(within(panel(container)).getByText('Events will appear here')).toBeInTheDocument();
    const bell = screen.getByRole('button', { name: 'Notifications' });
    expect(bell.querySelector('[data-notification-count]')).toHaveAttribute('hidden');
  });

  it('does not mark everything read just by opening the panel', async () => {
    server = [item(1), item(2)];
    useNotificationsStore.setState({ items: server, unread: 2, loaded: true });
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: 'Notifications' }));

    await waitFor(() => expect(api.list).toHaveBeenCalled());
    expect(api.readAll).not.toHaveBeenCalled();
    expect(api.setRead).not.toHaveBeenCalled();
  });
});

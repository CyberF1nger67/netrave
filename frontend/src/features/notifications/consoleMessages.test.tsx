import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';

// The server list is mocked: the console window must fetch and show an unread administrator message.
const api = vi.hoisted(() => ({ list: vi.fn() }));

vi.mock('../../api/endpoints', () => ({
  notificationsApi: { list: api.list, setRead: vi.fn() },
}));

import { ConsoleMessages } from './ConsoleMessages';
import { useAuthStore } from '../../stores/auth';
import { useNotificationsStore } from '../../stores/notifications';
import type { NotificationItem, User } from '../../types/api';

const user: User = { id: 2, username: 'bob', role: 'user', disabled: false, created_at: 0, roles: [], role_perms: null };
const message: NotificationItem = {
  id: 7, level: 'info', category: 'message', title: 'Message from {sender}', body: '{text}', link: '',
  read: false, created_at: 100, params: { sender: 'root', text: 'Reboot at 22:00' },
};

describe('console window messages', () => {
  beforeEach(() => {
    // A socket that never opens: only the list request matters here.
    vi.stubGlobal('WebSocket', class {
      onopen: unknown = null;
      onmessage: unknown = null;
      onerror: unknown = null;
      onclose: unknown = null;
      close(): void {}
    });
    api.list.mockReset();
    api.list.mockResolvedValue({ items: [message], unread: 1, categories: [] });
    useAuthStore.setState({ user, status: 'authenticated' });
    useNotificationsStore.setState({ items: [], unread: 0 });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    useAuthStore.setState({ user: null, status: 'guest' });
  });

  it('shows an unread administrator message without the main shell', async () => {
    render(<ConsoleMessages />);
    await waitFor(() => expect(screen.getByRole('dialog', { name: 'Message from root' })).toHaveTextContent('Reboot at 22:00'));
    expect(api.list).toHaveBeenCalled();
  });
});

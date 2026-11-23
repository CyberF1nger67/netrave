import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

// Server calls are mocked: the read marker sent when the dialog is closed is checked.
const api = vi.hoisted(() => ({ setRead: vi.fn(), list: vi.fn() }));

vi.mock('../../api/endpoints', () => ({
  notificationsApi: { setRead: api.setRead, list: api.list },
}));

import { MessagePopup } from './MessagePopup';
import { useNotificationsStore } from '../../stores/notifications';
import type { NotificationItem } from '../../types/api';

function message(id: number, text: string, created: number, read = false): NotificationItem {
  return {
    id, level: 'warning', category: 'message', title: 'Message from {sender}', body: '{text}', link: '',
    read, created_at: created, params: { sender: 'root', text },
  };
}

describe('message popup', () => {
  beforeEach(() => {
    api.setRead.mockReset();
    api.setRead.mockResolvedValue({ ok: true });
    api.list.mockReset();
    api.list.mockResolvedValue({ items: [], unread: 0, categories: [] });
    useNotificationsStore.setState({ items: [], unread: 0 });
  });

  it('shows an unread message from an administrator as a dialog', () => {
    useNotificationsStore.setState({ items: [message(1, 'Maintenance tonight', 100)] });
    render(<MessagePopup />);
    const dialog = screen.getByRole('dialog', { name: 'Message from root' });
    expect(dialog).toHaveTextContent('Maintenance tonight');
  });

  it('does not show read messages or other notifications', () => {
    useNotificationsStore.setState({
      items: [
        message(1, 'Old', 100, true),
        { id: 2, level: 'info', category: 'machine', title: 'Machine started', body: '', link: '', read: false, created_at: 200 },
      ],
    });
    render(<MessagePopup />);
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('stays open on Escape or a click outside and closes only by its button', async () => {
    useNotificationsStore.setState({ items: [message(1, 'Stay', 100)] });
    render(<MessagePopup />);
    const dialog = screen.getByRole('dialog', { name: 'Message from root' });
    fireEvent.keyDown(document, { key: 'Escape' });
    fireEvent.mouseDown(dialog.parentElement!);
    expect(screen.getByRole('dialog', { name: 'Message from root' })).toBeInTheDocument();
    expect(api.setRead).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Got it' }));
    await waitFor(() => expect(api.setRead).toHaveBeenCalledWith(1, true));
  });

  it('shows the oldest unread message first and marks it read on "Got it"', async () => {
    useNotificationsStore.setState({
      items: [message(2, 'Second', 200), message(1, 'First', 100)],
    });
    render(<MessagePopup />);
    expect(screen.getByRole('dialog')).toHaveTextContent('First');
    fireEvent.click(screen.getByRole('button', { name: 'Got it' }));
    await waitFor(() => expect(api.setRead).toHaveBeenCalledWith(1, true));
  });
});

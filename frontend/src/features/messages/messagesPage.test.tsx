import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

// Server calls are mocked: what the page sends is checked.
const api = vi.hoisted(() => ({ list: vi.fn(), send: vi.fn() }));

vi.mock('../../api/endpoints', () => ({
  usersApi: { list: api.list },
  messagesApi: { send: api.send },
}));

import { MessagesPage } from './MessagesPage';
import type { User } from '../../types/api';

const base: User = { id: 1, username: 'root', role: 'admin', disabled: false, created_at: 0, roles: [], role_perms: null };
const users: User[] = [
  base,
  { ...base, id: 2, username: 'bob', role: 'user' },
  { ...base, id: 3, username: 'gone', role: 'user', disabled: true },
];

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}><MessagesPage /></QueryClientProvider>);
}

describe('administrator messages page', () => {
  beforeEach(() => {
    api.list.mockReset();
    api.send.mockReset();
    api.list.mockResolvedValue(users);
    api.send.mockResolvedValue({ sent: 1 });
  });

  it('sends to the selected active users only', async () => {
    renderPage();
    fireEvent.click(await screen.findByLabelText('bob'));
    fireEvent.change(screen.getByLabelText('Message text'), { target: { value: '  Reboot at 22:00  ' } });
    fireEvent.change(screen.getByLabelText('Level'), { target: { value: 'warning' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send' }));
    await waitFor(() => expect(api.send).toHaveBeenCalledWith({
      text: 'Reboot at 22:00', level: 'warning', all: false, user_ids: [2],
    }));
  });

  it('a disabled user cannot be selected', async () => {
    renderPage();
    expect(await screen.findByRole('checkbox', { name: /gone/ })).toBeDisabled();
  });

  it('sends to every active user when "All active users" is chosen', async () => {
    renderPage();
    await screen.findByLabelText('bob');
    fireEvent.click(screen.getByLabelText('All active users'));
    fireEvent.change(screen.getByLabelText('Message text'), { target: { value: 'Hello everyone' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send' }));
    await waitFor(() => expect(api.send).toHaveBeenCalledWith({
      text: 'Hello everyone', level: 'info', all: true, user_ids: [],
    }));
  });

  it('refuses an empty text', async () => {
    renderPage();
    fireEvent.click(await screen.findByLabelText('bob'));
    fireEvent.click(screen.getByRole('button', { name: 'Send' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Enter the message text');
    expect(api.send).not.toHaveBeenCalled();
  });

  it('refuses a message without recipients', async () => {
    renderPage();
    await screen.findByLabelText('bob');
    fireEvent.change(screen.getByLabelText('Message text'), { target: { value: 'Hi' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Choose the recipients');
    expect(api.send).not.toHaveBeenCalled();
  });
});

describe('switching between "all" and single recipients', () => {
  beforeEach(() => {
    api.list.mockReset();
    api.send.mockReset();
    api.list.mockResolvedValue(users);
    api.send.mockResolvedValue({ sent: 1 });
  });

  it('choosing one user after "all" switches to that user and stays enabled', async () => {
    renderPage();
    await screen.findByLabelText('bob');
    fireEvent.click(screen.getByLabelText('All active users'));
    expect(screen.getByLabelText('bob')).not.toBeDisabled();
    fireEvent.click(screen.getByLabelText('bob'));
    expect(screen.getByLabelText('All active users')).not.toBeChecked();
    fireEvent.change(screen.getByLabelText('Message text'), { target: { value: 'Only bob' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send' }));
    await waitFor(() => expect(api.send).toHaveBeenCalledWith({
      text: 'Only bob', level: 'info', all: false, user_ids: [2],
    }));
  });

  it('unchecking "all" leaves no recipients until a user is picked', async () => {
    renderPage();
    await screen.findByLabelText('bob');
    fireEvent.click(screen.getByLabelText('All active users'));
    fireEvent.click(screen.getByLabelText('All active users'));
    expect(screen.getByText('Recipients: 0')).toBeInTheDocument();
  });
});

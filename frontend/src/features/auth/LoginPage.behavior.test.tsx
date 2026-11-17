import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { LoginPage } from './LoginPage';
import { useAuthStore } from '../../stores/auth';
import { ApiError } from '../../api/client';
import { setLanguage } from '../../i18n';

describe('LoginPage behaviour', () => {
  const login = vi.fn<(username: string, password: string) => Promise<void>>();

  beforeEach(() => {
    setLanguage('en');
    login.mockReset();
    login.mockResolvedValue(undefined);
    useAuthStore.setState({ user: null, status: 'guest', login, sessionExpired: false });
  });

  const fill = (user: string, pass: string) => {
    fireEvent.change(screen.getByLabelText('Username'), { target: { value: user } });
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: pass } });
  };

  it.each([
    [new ApiError('x', 401, null), 'Invalid username or password.'],
    [new ApiError('x', 429, null), 'Too many attempts. Try again later.'],
    [new ApiError('x', 403, null), 'The request was rejected. Reload the page and try again.'],
    [new ApiError('x', 503, null), 'Server error. Try again later.'],
  ])('maps backend status %# to a clear message', async (cause, message) => {
    login.mockRejectedValue(cause);
    render(<LoginPage />);
    fill('admin', 'wrong');
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(message));
  });

  it('shows a connection message when the server cannot be reached', async () => {
    login.mockRejectedValue(new TypeError('Failed to fetch'));
    render(<LoginPage />);
    fill('admin', 'secret');
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('No connection to the server'));
  });

  it('keeps the entered username after a failed attempt', async () => {
    login.mockRejectedValue(new ApiError('x', 401, null));
    render(<LoginPage />);
    fill('admin', 'wrong');
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument());
    expect(screen.getByLabelText('Username')).toHaveValue('admin');
  });

  it('submits once and disables the form while the request is pending', async () => {
    let resolve!: () => void;
    login.mockImplementation(() => new Promise<void>((done) => { resolve = done; }));
    render(<LoginPage />);
    fill('admin', 'secret');
    const form = screen.getByRole('button', { name: 'Sign in' }).closest('form')!;
    fireEvent.submit(form);
    fireEvent.submit(form);
    expect(login).toHaveBeenCalledTimes(1);
    expect(screen.getByRole('button', { name: /Checking/ })).toBeDisabled();
    expect(screen.getByLabelText('Username')).toBeDisabled();
    resolve();
  });

  it('shows the session-expired notice when the store reports it', () => {
    useAuthStore.setState({ sessionExpired: true });
    render(<LoginPage />);
    expect(screen.getByRole('status')).toHaveTextContent('Your session has expired.');
  });
});

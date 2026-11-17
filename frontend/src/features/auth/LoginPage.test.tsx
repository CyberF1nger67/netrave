import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { LoginPage } from './LoginPage';
import { useAuthStore } from '../../stores/auth';
import { setLanguage } from '../../i18n';

describe('LoginPage', () => {
  const login = vi.fn<(username: string, password: string) => Promise<void>>();

  beforeEach(() => {
    setLanguage('en');
    login.mockReset();
    login.mockResolvedValue(undefined);
    useAuthStore.setState({ user: null, status: 'guest', login });
  });

  it('validates fields before sending credentials', () => {
    render(<LoginPage />);

    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(screen.getByRole('alert')).toHaveTextContent('Enter username');

    fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'admin' } });
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(screen.getByRole('alert')).toHaveTextContent('Enter password');
    expect(login).not.toHaveBeenCalled();
  });

  it('submits credentials and clears the error while typing', async () => {
    render(<LoginPage />);

    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(screen.getByRole('alert')).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'admin' } });
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'secret' } });

    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    await waitFor(() => expect(login).toHaveBeenCalledWith('admin', 'secret'));
  });

  it('toggles password visibility', () => {
    render(<LoginPage />);
    const password = screen.getByLabelText('Password');
    expect(password).toHaveAttribute('type', 'password');

    fireEvent.click(screen.getByRole('button', { name: 'Show password' }));
    expect(password).toHaveAttribute('type', 'text');

    fireEvent.click(screen.getByRole('button', { name: 'Hide password' }));
    expect(password).toHaveAttribute('type', 'password');
  });

  it('switches language without reloading the page', () => {
    render(<LoginPage />);

    fireEvent.click(screen.getByRole('button', { name: 'Language' }));
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'Русский' }));

    expect(screen.getByRole('heading', { name: 'Вход в NetRave' })).toBeInTheDocument();
  });

  it('shows the Caps Lock hint only while it is pressed', () => {
    render(<LoginPage />);
    const password = screen.getByLabelText('Password');

    const capsEvent = (pressed: boolean) => {
      const event = new KeyboardEvent('keydown', { key: 'CapsLock', bubbles: true });
      Object.defineProperty(event, 'getModifierState', { value: () => pressed });
      return event;
    };

    fireEvent(password, capsEvent(true));
    expect(screen.getByText('Caps Lock is on')).toBeInTheDocument();

    fireEvent(password, capsEvent(false));
    expect(screen.queryByText('Caps Lock is on')).not.toBeInTheDocument();
  });

  it('shows a readable message for rejected credentials', async () => {
    login.mockRejectedValue(new Error('Unauthorized'));
    render(<LoginPage />);

    fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'admin' } });
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'wrong' } });
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Invalid username or password.'));
  });
});

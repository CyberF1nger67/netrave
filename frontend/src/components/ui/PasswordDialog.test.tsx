import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { PasswordDialog } from './PasswordDialog';

function fill(current: string, next: string, confirm: string) {
  fireEvent.change(screen.getByLabelText('Current password'), { target: { value: current } });
  fireEvent.change(screen.getByLabelText('New password (8–256 characters)'), { target: { value: next } });
  fireEvent.change(screen.getByLabelText('Confirm Password'), { target: { value: confirm } });
}

describe('password dialog', () => {
  it('shows a real-time mismatch and prevents submission', () => {
    render(<PasswordDialog onClose={vi.fn()} onSubmit={vi.fn()} />);
    fill('old-secret', 'new-secret-123', 'other-secret-123');
    expect(screen.getByText('Passwords do not match')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Save Changes' })).toBeDisabled();
  });

  it('submits the confirmation and closes on success', async () => {
    const onClose = vi.fn();
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<PasswordDialog onClose={onClose} onSubmit={onSubmit} successMessage="Password changed successfully" />);
    fill('old-secret', 'new-secret-123', 'new-secret-123');
    fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(onSubmit).toHaveBeenCalledWith('old-secret', 'new-secret-123', 'new-secret-123');
  });

  it('keeps the dialog open and shows a backend error', async () => {
    const onClose = vi.fn();
    const onSubmit = vi.fn().mockRejectedValue(new Error('current password is incorrect'));
    render(<PasswordDialog onClose={onClose} onSubmit={onSubmit} />);
    fill('wrong-secret', 'new-secret-123', 'new-secret-123');
    fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }));
    expect(await screen.findByText('current password is incorrect')).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
  });

  it('does not close with Escape while saving', async () => {
    const onClose = vi.fn();
    let finish!: () => void;
    const onSubmit = vi.fn().mockReturnValue(new Promise<void>((resolve) => { finish = resolve; }));
    render(<PasswordDialog onClose={onClose} onSubmit={onSubmit} />);
    fill('old-secret', 'new-secret-123', 'new-secret-123');
    fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }));
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).not.toHaveBeenCalled();
    finish();
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  it('closes with Escape when idle', () => {
    const onClose = vi.fn();
    render(<PasswordDialog onClose={onClose} onSubmit={vi.fn()} />);
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalled();
  });
});

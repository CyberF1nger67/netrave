import { act, fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { ConfirmHost } from './ConfirmHost';
import { askConfirm, askText } from '../stores/dialogs';

describe('ConfirmHost', () => {
  it('resolves to true only when the user confirms a destructive action', async () => {
    render(<ConfirmHost />);
    let answer: Promise<boolean> = Promise.resolve(false);
    act(() => { answer = askConfirm({ title: 'Delete laboratory?', danger: true }); });
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }));
    await expect(answer).resolves.toBe(true);
  });

  it('resolves to false when the user cancels', async () => {
    render(<ConfirmHost />);
    let answer: Promise<boolean> = Promise.resolve(true);
    act(() => { answer = askConfirm({ title: 'Restart?' }); });
    fireEvent.click(await screen.findByRole('button', { name: 'Cancel' }));
    await expect(answer).resolves.toBe(false);
  });

  it('returns the trimmed text and null for an empty value', async () => {
    render(<ConfirmHost />);
    let answer: Promise<string | null> = Promise.resolve('x');
    act(() => { answer = askText({ title: 'New image name:', initial: 'disk1', confirmLabel: 'Save' }); });
    const input = await screen.findByDisplayValue('disk1');
    fireEvent.change(input, { target: { value: '  renamed  ' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await expect(answer).resolves.toBe('renamed');

    act(() => { answer = askText({ title: 'New image name:', initial: '' }); });
    fireEvent.click(await screen.findByRole('button', { name: 'Confirm' }));
    await expect(answer).resolves.toBeNull();
  });
});

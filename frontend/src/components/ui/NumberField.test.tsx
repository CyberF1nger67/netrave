import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { NumberField } from './NumberField';

function Harness({ initial, onCommit, min = 1, max = 64 }: { initial: number; onCommit: (n: number) => void; min?: number; max?: number }) {
  const [value, setValue] = useState(initial);
  return (
    <>
      <label>Amount<NumberField min={min} max={max} value={value} onValueChange={(next) => { setValue(next); onCommit(next); }} /></label>
    </>
  );
}

describe('NumberField', () => {
  it('lets the user clear the field completely without snapping back', () => {
    const onCommit = vi.fn();
    render(<Harness initial={2} onCommit={onCommit} />);
    const input = screen.getByLabelText('Amount') as HTMLInputElement;
    fireEvent.change(input, { target: { value: '' } });
    expect(input.value).toBe('');
    expect(onCommit).not.toHaveBeenCalled();
  });

  it('accepts multi-digit input typed character by character', () => {
    const onCommit = vi.fn();
    render(<Harness initial={1} onCommit={onCommit} max={524288} min={64} />);
    const input = screen.getByLabelText('Amount') as HTMLInputElement;
    fireEvent.change(input, { target: { value: '' } });
    fireEvent.change(input, { target: { value: '2' } });
    fireEvent.change(input, { target: { value: '20' } });
    fireEvent.change(input, { target: { value: '2048' } });
    expect(input.value).toBe('2048');
    expect(onCommit).toHaveBeenLastCalledWith(2048);
  });

  it('keeps an out-of-range intermediate value locally and does not send it', () => {
    const onCommit = vi.fn();
    render(<Harness initial={4} onCommit={onCommit} min={1} max={64} />);
    const input = screen.getByLabelText('Amount') as HTMLInputElement;
    fireEvent.change(input, { target: { value: '999' } });
    expect(input.value).toBe('999');
    expect(onCommit).not.toHaveBeenCalled();
  });

  it('clamps an out-of-range value to the maximum on blur', () => {
    const onCommit = vi.fn();
    render(<Harness initial={4} onCommit={onCommit} min={1} max={64} />);
    const input = screen.getByLabelText('Amount') as HTMLInputElement;
    fireEvent.change(input, { target: { value: '999' } });
    fireEvent.blur(input);
    expect(input.value).toBe('64');
    expect(onCommit).toHaveBeenLastCalledWith(64);
  });

  it('restores the last committed value when left empty and blurred', () => {
    const onCommit = vi.fn();
    render(<Harness initial={8} onCommit={onCommit} />);
    const input = screen.getByLabelText('Amount') as HTMLInputElement;
    fireEvent.change(input, { target: { value: '' } });
    fireEvent.blur(input);
    expect(input.value).toBe('8');
    expect(onCommit).not.toHaveBeenCalled();
  });

  it('does not overwrite typing when the parent re-renders with the same value', () => {
    const onCommit = vi.fn();
    const { rerender } = render(<Harness initial={3} onCommit={onCommit} />);
    const input = screen.getByLabelText('Amount') as HTMLInputElement;
    fireEvent.change(input, { target: { value: '' } });
    rerender(<Harness initial={3} onCommit={onCommit} />);
    expect(input.value).toBe('');
  });
});

import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { RoundCheck } from './RoundCheck';

describe('round checkbox', () => {
  it('stays a checkbox and passes its state outward', () => {
    const onChange = vi.fn();
    render(<RoundCheck checked={false} ariaLabel="Select icon" onChange={onChange} />);
    const box = screen.getByRole('checkbox', { name: 'Select icon' });
    expect(box).not.toBeChecked();
    fireEvent.click(box);
    expect(onChange).toHaveBeenCalledWith(true);
  });

  it('shows the indeterminate state for selecting all rows', () => {
    render(<RoundCheck checked={false} indeterminate ariaLabel="Select all" onChange={() => undefined} />);
    expect(screen.getByRole('checkbox', { name: 'Select all' })).toHaveAttribute('aria-checked', 'mixed');
  });
});
